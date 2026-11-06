package mcpserver_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/JuanKsPty/chenchen/api/internal/mcpserver"
	"github.com/JuanKsPty/chenchen/api/internal/oauth"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// A file that continues an account and agrees with it enters on its own; the
// same file again changes nothing.
func TestAFileThatContinuesItsAccountEntersOnItsOwn(t *testing.T) {
	r := newRig(t)
	r.importAsOwner(t, r.owner, "junio.csv", june)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))

	st := r.send(t, cs, "Transacciones del mes.csv", july)
	if st.Status != store.UploadImported || st.Reason != "" || st.Account == nil || st.Account.Last4 != "0001" ||
		st.Lines == nil || st.Lines.New != 1 || st.MatchesBank == nil || !*st.MatchesBank {
		t.Fatalf("import_status = %+v", st)
	}
	a, err := r.accounts.Get(context.Background(), r.owner, r.accountOf(t, r.owner, product))
	if err != nil || a.Balance != 70075 {
		t.Errorf("balance = %s, %v; want 700.75", a.Balance, err)
	}
	if n := r.count(t, "statement_uploads WHERE data IS NOT NULL"); n != 0 {
		t.Errorf("%d uploads still hold their file", n)
	}

	again := r.send(t, cs, "Transacciones del mes (1).csv", july)
	if again.Status != store.UploadUnchanged || again.Lines == nil || again.Lines.New != 0 {
		t.Errorf("the same file again = %+v", again)
	}
	if n := r.count(t, "entries"); n != 3 {
		t.Errorf("entries = %d, want 3", n)
	}
}

// A card's movements print no balance, so they never say where the card
// starts: a card that never had a starting balance takes them all the same,
// and so does a file of several cards the owner has.
func TestCardMovementsWithoutABalanceEnter(t *testing.T) {
	r := newRig(t)
	r.importAsOwner(t, r.owner, "tarjeta.txt", cardFile("1111", "3333"))
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))

	later := "Cuenta;Tarjeta;Fecha tran;Fecha proceso;Descripcion;Referencia;Categoria;Cargos (Db);Pagos (Cr)\r\n" +
		"**** 1111;**** 1111;02/02/2026;03/02/2026;FARMACIA DE PRUEBA;R7;Salud;-4.25;\r\n"
	st := r.send(t, cs, "ULTIMOS-MOVIMIENTOS-TARJETA.txt", []byte(later))
	if st.Status != store.UploadImported || st.Account == nil || st.Account.Last4 != "1111" || st.Lines == nil ||
		st.Lines.New != 1 || st.MatchesBank != nil {
		t.Errorf("one card = %+v", st)
	}
	both := later + "**** 3333;**** 3333;04/02/2026;04/02/2026;LIBRERIA DE PRUEBA;R8;Otros;-12.00;\r\n"
	if st := r.send(t, cs, "ULTIMOS-MOVIMIENTOS-TARJETA (1).txt", []byte(both)); st.Status != store.UploadImported {
		t.Errorf("both cards = %+v", st)
	}
	if n := r.count(t, "entries"); n != 4 {
		t.Errorf("entries = %d, want 4", n)
	}
}

// Files sent at once are read one after another, each in its turn: waiting
// for the ones ahead never holds a file back for review.
func TestFilesSentAtOnceEachEnterInTurn(t *testing.T) {
	r := newRig(t)
	r.importAsOwner(t, r.owner, "tarjeta.txt", cardFile("1111", "3333"))
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))

	const n = 4
	files := make([][]byte, n)
	addresses := make([]prepared, n)
	for i := range n {
		files[i] = []byte("Cuenta;Tarjeta;Fecha tran;Fecha proceso;Descripcion;Referencia;Categoria;Cargos (Db);Pagos (Cr)\r\n" +
			fmt.Sprintf("**** 1111;**** 1111;0%d/03/2026;0%d/03/2026;COMERCIO %d;RX%d;Otros;-%d.00;\r\n", i+1, i+1, i, i, i+1))
		addresses[i] = prepare(t, cs, fmt.Sprintf("ULTIMOS-MOVIMIENTOS-TARJETA (%d).txt", i), files[i])
	}
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			req, err := http.NewRequest(http.MethodPut, addresses[i].UploadURL, bytes.NewReader(files[i]))
			if err != nil {
				return
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			res.Body.Close()
			codes[i] = res.StatusCode
		})
	}
	wg.Wait()
	for i := range n {
		var st status
		mustCall(t, cs, mcpserver.ToolImportStatus, map[string]any{"upload_id": addresses[i].UploadID}, &st)
		if codes[i] != http.StatusOK || st.Status != store.UploadImported {
			t.Errorf("file %d: PUT %d, import_status %+v", i, codes[i], st)
		}
	}
	if got := r.count(t, "entries"); got != 2+n {
		t.Errorf("entries = %d, want %d", got, 2+n)
	}
}

// Nothing that arrives through an upload ever creates an account: a file of
// an account the owner does not have waits, and so does one that holds such
// an account beside one the owner has. Nothing of either is imported.
func TestMCPUploadNeverCreatesAccounts(t *testing.T) {
	r := newRig(t)
	r.importAsOwner(t, r.owner, "tarjeta.txt", cardFile("1111"))
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	accounts, entries := r.count(t, "accounts"), r.count(t, "entries")

	for name, data := range map[string][]byte{
		"otra.csv":    bacFile("100000999", "10.00", "5.00", "31/07/2026", "02/07/2026, 9, CP, COMPRA DE PRUEBA, 5.00, 0.00, 5.00"),
		"tarjeta.txt": cardFile("1111", "3333"),
	} {
		st := r.send(t, cs, name, data)
		if st.Status != store.UploadNeedsReview || !strings.Contains(st.Reason, "todavía no está en chenchén") ||
			st.Account != nil || st.Lines != nil {
			t.Errorf("%s: import_status = %+v", name, st)
		}
	}
	if n := r.count(t, "accounts"); n != accounts {
		t.Errorf("accounts %d → %d", accounts, n)
	}
	if n := r.count(t, "entries"); n != entries {
		t.Errorf("entries %d → %d", entries, n)
	}
	if n := r.count(t, "statement_uploads WHERE status = 'needs_review' AND data IS NOT NULL"); n != 2 {
		t.Errorf("uploads kept for review = %d, want 2", n)
	}
}

// A file that does not agree with itself, or with where the account stands,
// waits for the owner with a reason Claude can repeat to them.
func TestAFileThatDoesNotAgreeWaitsForTheOwner(t *testing.T) {
	r := newRig(t)
	r.importAsOwner(t, r.owner, "junio.csv", june)
	account := r.accountOf(t, r.owner, product)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))

	for _, tt := range []struct {
		name, reason string
		data         []byte
	}{
		{"an opening the account never had", "El saldo inicial no coincide con el que tenías: el archivo empieza en 760.00 y chenchén tenía 750.75.",
			bacFile(product, "760.00", "710.00", "31/07/2026", "03/07/2026, 3, CP, SUPERMERCADO DE PRUEBA, 50.00, 0.00, 710.00")},
		{"a break in its own balances", "Los saldos del archivo dejan de cuadrar en la línea 5.",
			bacFile(product, "750.75", "700.75", "31/07/2026", "03/07/2026, 3, CP, SUPERMERCADO DE PRUEBA, 50.00, 0.00, 600.00")},
		{"a warning", "El archivo trae una advertencia: El saldo inicial (750.75) más los movimientos (-50.00) no da el saldo en libros que muestra el banco (650.75).",
			bacFile(product, "750.75", "650.75", "31/07/2026", "03/07/2026, 3, CP, SUPERMERCADO DE PRUEBA, 50.00, 0.00, 700.75")},
	} {
		st := r.send(t, cs, "julio.csv", tt.data)
		if st.Status != store.UploadNeedsReview || st.Reason != tt.reason || st.Account == nil ||
			st.Account.ID != account.String() || st.MatchesBank != nil {
			t.Errorf("%s: import_status = %+v", tt.name, st)
		}
	}
	if n := r.count(t, "entries"); n != 2 {
		t.Errorf("entries = %d, want june's 2", n)
	}
}

// A file no reader recognises, and a workbook built to unzip to far more than
// the API holds, are unreadable, and kept for the owner to see.
func TestAnUnreadableFileIsSaidSo(t *testing.T) {
	r := newRig(t)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))

	for name, data := range map[string][]byte{
		"notas.csv":   []byte("esto no es un estado de cuenta"),
		"bomba.xlsx":  zipBomb(t, 80<<20),
		"vacio.xlsx":  []byte("PK\x03\x04 no es un zip"),
		"factura.pdf": []byte("%PDF-1.4\nno es un estado"),
	} {
		st := r.send(t, cs, name, data)
		if st.Status != store.UploadUnreadable || st.Reason == "" {
			t.Errorf("%s: import_status = %+v", name, st)
		}
	}
	if n := r.count(t, "accounts"); n != 0 {
		t.Errorf("accounts = %d", n)
	}
}

// The address answers the same to every file it takes, whatever the file
// turned out to be: only its id.
func TestTheAddressAnswersTheSameWhateverTheFile(t *testing.T) {
	r := newRig(t)
	r.importAsOwner(t, r.owner, "junio.csv", june)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))

	var bodies []string
	headers := map[string]bool{}
	for _, f := range []struct {
		name string
		data []byte
		want string
	}{
		{"julio.csv", july, store.UploadImported},
		{"otra.csv", bacFile("100000999", "1.00", "1.00", "31/07/2026"), store.UploadNeedsReview},
		{"notas.csv", []byte("hola"), store.UploadUnreadable},
	} {
		p := prepare(t, cs, f.name, f.data)
		req, err := http.NewRequest(http.MethodPut, p.UploadURL, bytes.NewReader(f.data))
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body bytes.Buffer
		_, _ = body.ReadFrom(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: PUT = %d %s", f.name, res.StatusCode, body.String())
		}
		bodies = append(bodies, strings.Replace(body.String(), p.UploadID, "<id>", 1))
		headers[fmt.Sprint(res.Header.Get("Content-Type"), res.Header.Get("Content-Length") != "")] = true
		var st status
		mustCall(t, cs, mcpserver.ToolImportStatus, map[string]any{"upload_id": p.UploadID}, &st)
		if st.Status != f.want {
			t.Errorf("%s: %s, want %s", f.name, st.Status, f.want)
		}
	}
	for _, b := range bodies {
		if b != `{"upload_id":"<id>"}` {
			t.Errorf("PUT answered %s", b)
		}
	}
	if len(headers) != 1 {
		t.Errorf("headers differ: %v", headers)
	}
}

// An address takes one file, the one declared, once, while it lasts.
// Anything else is refused and nothing of it kept; a refused body does not
// spend the address.
func TestAnAddressTakesOneFileOnce(t *testing.T) {
	r := newRig(t)
	r.importAsOwner(t, r.owner, "junio.csv", june)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	p := prepare(t, cs, "julio.csv", july)

	other := bytes.Replace(july, []byte("SUPERMERCADO"), []byte("SUPERMERCADX"), 1)
	for name, body := range map[string][]byte{
		"another file of the same size": other,
		"a shorter file":                july[:len(july)-1],
		"a longer file":                 append(append([]byte{}, july...), '\n'),
		"nothing":                       {},
	} {
		if code, answer := r.put(t, p.UploadURL, body); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, code, answer)
		}
	}
	if n := r.count(t, "statement_uploads WHERE received_at IS NULL AND data IS NULL AND status = 'pending'"); n != 1 {
		t.Errorf("after refusals the upload is not untouched")
	}
	if n := r.count(t, "entries"); n != 2 {
		t.Errorf("entries = %d", n)
	}

	if code, body := r.put(t, p.UploadURL, july); code != http.StatusOK {
		t.Fatalf("the declared file = %d %s", code, body)
	}
	if code, _ := r.put(t, p.UploadURL, july); code != http.StatusNotFound {
		t.Errorf("the address used twice = %d, want 404", code)
	}

	// An address past its time takes nothing.
	late := prepare(t, cs, "julio.csv", july)
	if err := r.db.Exec(context.Background(),
		`UPDATE statement_uploads SET expires_at = now() - interval '1 second' WHERE id = $1`, late.UploadID); err != nil {
		t.Fatal(err)
	}
	if code, _ := r.put(t, late.UploadURL, july); code != http.StatusNotFound {
		t.Errorf("an expired address = %d, want 404", code)
	}
	// Nor does one never handed out.
	if code, _ := r.put(t, r.srv.URL+"/api/mcp/uploads/"+strings.Repeat("A", 43), july); code != http.StatusNotFound {
		t.Errorf("a made-up address = %d, want 404", code)
	}

	// Its path is a secret: the access log names the route, never it.
	token := p.UploadURL[strings.LastIndex(p.UploadURL, "/")+1:]
	if logs := r.logs.String(); strings.Contains(logs, token) || !strings.Contains(logs, "/api/mcp/uploads/{token}") {
		t.Errorf("the access log of the address:\n%s", logs)
	}
	// And prepare_import refuses what it could never take.
	for name, args := range map[string]map[string]any{
		"too large":  {"filename": "x.csv", "sha256": digest(july), "size": 20<<20 + 1},
		"no size":    {"filename": "x.csv", "sha256": digest(july), "size": 0},
		"a bad hash": {"filename": "x.csv", "sha256": "abc", "size": 10},
		"no name":    {"filename": " ", "sha256": digest(july), "size": 10},
	} {
		if failed, text := call(t, cs, mcpserver.ToolPrepareImport, args, nil); !failed {
			t.Errorf("prepare_import with %s: %s", name, text)
		}
	}
}

// A conversation cannot pile up addresses: twenty wait at most.
func TestOpenAddressesAreBounded(t *testing.T) {
	r := newRig(t)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	for range 20 {
		prepare(t, cs, "julio.csv", july)
	}
	if failed, text := call(t, cs, mcpserver.ToolPrepareImport, map[string]any{"filename": "julio.csv",
		"sha256": digest(july), "size": len(july)}, nil); !failed || !strings.Contains(text, "esperando su archivo") {
		t.Errorf("the 21st address = %v %s", failed, text)
	}
	if n := r.count(t, "statement_uploads"); n != 20 {
		t.Errorf("uploads = %d, want 20", n)
	}
}

// The owner sees the files that wait, reads one as the web's dry run would,
// and imports it as the web's import would, or sets it aside. Nobody else
// can do any of it.
func TestTheOwnerReviewsWhatWaits(t *testing.T) {
	r := newRig(t)
	r.importAsOwner(t, r.owner, "junio.csv", june)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	newAccount := bacFile("100000999", "10.00", "5.00", "31/07/2026", "02/07/2026, 9, CP, COMPRA DE PRUEBA, 5.00, 0.00, 5.00")
	mismatch := bacFile(product, "760.00", "710.00", "31/07/2026", "03/07/2026, 3, CP, SUPERMERCADO DE PRUEBA, 50.00, 0.00, 710.00")
	waiting := r.send(t, cs, "otra.csv", newAccount)
	aside := r.send(t, cs, "julio.csv", mismatch)
	owner, stranger := r.session(t, r.owner), r.session(t, r.stranger)

	var list struct {
		Uploads []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			Status   string `json:"status"`
			Reason   string `json:"reason"`
			Account  *struct {
				ID    string `json:"id"`
				Last4 string `json:"last4"`
			} `json:"account"`
		} `json:"uploads"`
	}
	res, body := r.do(t, http.MethodGet, "/api/imports/uploads?status=needs_review", owner, "", nil)
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &list) != nil || len(list.Uploads) != 2 {
		t.Fatalf("GET uploads = %d %s", res.StatusCode, body)
	}
	if u := list.Uploads[0]; u.ID != waiting.UploadID || u.Filename != "otra.csv" || u.Reason == "" || u.Account != nil {
		t.Errorf("the first waiting = %+v", u)
	}
	if u := list.Uploads[1]; u.ID != aside.UploadID || u.Account == nil || u.Account.Last4 != "0001" {
		t.Errorf("the second waiting = %+v", u)
	}
	if res, body := r.do(t, http.MethodGet, "/api/imports/uploads", stranger, "", nil); res.StatusCode != http.StatusOK ||
		strings.TrimSpace(string(body)) != `{"uploads":[]}` {
		t.Errorf("the stranger's list = %d %s", res.StatusCode, body)
	}

	// The dry run says what the web's says of the same file.
	res, stored := r.do(t, http.MethodGet, "/api/imports/uploads/"+waiting.UploadID+"/dry-run", owner, "", nil)
	if res.StatusCode != http.StatusOK || !sameJSON(stored, r.dryRun(t, "otra.csv", newAccount)) {
		t.Errorf("dry run = %d %s", res.StatusCode, stored)
	}

	// Nobody else reaches any of it.
	for _, path := range []string{"/dry-run", "/import", "/dismiss"} {
		method := http.MethodPost
		if path == "/dry-run" {
			method = http.MethodGet
		}
		if res, body := r.do(t, method, "/api/imports/uploads/"+waiting.UploadID+path, stranger, "", nil); res.StatusCode != http.StatusNotFound {
			t.Errorf("the stranger's %s = %d %s", path, res.StatusCode, body)
		}
	}

	// The owner imports the new account's file: it is theirs to create.
	res, body = r.do(t, http.MethodPost, "/api/imports/uploads/"+waiting.UploadID+"/import", owner, "", nil)
	var imported struct {
		Unchanged bool `json:"unchanged"`
		Accounts  []struct {
			Created bool `json:"created"`
			New     int  `json:"new"`
		} `json:"accounts"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &imported) != nil || len(imported.Accounts) != 1 ||
		!imported.Accounts[0].Created || imported.Accounts[0].New != 1 {
		t.Errorf("import = %d %s", res.StatusCode, body)
	}
	var st status
	mustCall(t, cs, mcpserver.ToolImportStatus, map[string]any{"upload_id": waiting.UploadID}, &st)
	if st.Status != store.UploadImported || st.Account == nil || st.Account.Last4 != "0999" {
		t.Errorf("after the owner's import: %+v", st)
	}
	if res, _ := r.do(t, http.MethodPost, "/api/imports/uploads/"+waiting.UploadID+"/import", owner, "", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("importing it again = %d, want 404: its file is gone", res.StatusCode)
	}

	// The other is set aside, and its file goes.
	if res, body := r.do(t, http.MethodPost, "/api/imports/uploads/"+aside.UploadID+"/dismiss", owner, "", nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("dismiss = %d %s", res.StatusCode, body)
	}
	if res, _ := r.do(t, http.MethodPost, "/api/imports/uploads/"+aside.UploadID+"/dismiss", owner, "", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("dismissing it again = %d, want 404", res.StatusCode)
	}
	if n := r.count(t, "statement_uploads WHERE data IS NOT NULL"); n != 0 {
		t.Errorf("%d uploads still hold their file", n)
	}
	if res, body := r.do(t, http.MethodGet, "/api/imports/uploads", owner, "", nil); res.StatusCode != http.StatusOK ||
		strings.TrimSpace(string(body)) != `{"uploads":[]}` {
		t.Errorf("nothing waits now: %d %s", res.StatusCode, body)
	}
}

// Housekeeping ends what nobody will use: an address whose file never came,
// and a file that waited a month.
func TestHousekeepingExpiresUploads(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	unused := prepare(t, cs, "julio.csv", july)
	fresh := prepare(t, cs, "julio.csv", july)
	old := r.send(t, cs, "notas.csv", []byte("hola"))
	recent := r.send(t, cs, "otras.csv", []byte("adios"))
	for sql, id := range map[string]string{
		`UPDATE statement_uploads SET expires_at = now() - interval '1 minute' WHERE id = $1`:  unused.UploadID,
		`UPDATE statement_uploads SET created_at = now() - interval '31 days' WHERE id = $1`:   old.UploadID,
		`UPDATE statement_uploads SET created_at = now() - interval '29 days' WHERE id = $1`:   recent.UploadID,
		`UPDATE statement_uploads SET expires_at = now() + interval '5 minutes' WHERE id = $1`: fresh.UploadID,
	} {
		if err := r.db.Exec(ctx, sql, id); err != nil {
			t.Fatal(err)
		}
	}
	n, err := r.uploads.Expire(ctx)
	if err != nil || n != 2 {
		t.Fatalf("Expire = %d, %v; want 2", n, err)
	}
	for id, want := range map[string]string{unused.UploadID: store.UploadExpired, fresh.UploadID: store.UploadPending,
		old.UploadID: store.UploadExpired, recent.UploadID: store.UploadUnreadable} {
		var st status
		mustCall(t, cs, mcpserver.ToolImportStatus, map[string]any{"upload_id": id}, &st)
		if st.Status != want {
			t.Errorf("%s = %s, want %s", id, st.Status, want)
		}
	}
	if n := r.count(t, "statement_uploads WHERE data IS NOT NULL"); n != 1 {
		t.Errorf("uploads holding a file = %d, want only the recent one", n)
	}
}

// --- helpers ----------------------------------------------------------------------

// dryRun is what the web's dry run says of a file chosen by hand.
func (r *rig) dryRun(t *testing.T, filename string, data []byte) []byte {
	t.Helper()
	var body bytes.Buffer
	w := multipartFile(t, &body, filename, data)
	res, raw := r.do(t, http.MethodPost, "/api/imports/dry-run", r.session(t, r.owner), w, &body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/imports/dry-run = %d %s", res.StatusCode, raw)
	}
	return raw
}

// multipartFile writes a form with one file and returns its content type.
func multipartFile(t *testing.T, body *bytes.Buffer, filename string, data []byte) string {
	t.Helper()
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return w.FormDataContentType()
}

func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return bytes.Equal(ja, jb)
}

// zipBomb is an .xlsx whose one sheet unzips to size bytes of blank space,
// a few hundred KB on the wire.
func zipBomb(t *testing.T, size int) []byte {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	w, err := z.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	blank := bytes.Repeat([]byte(" "), 1<<20)
	for written := 0; written < size; written += len(blank) {
		if _, err := w.Write(blank); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if out.Len() > 1<<20 {
		t.Fatalf("fixture: the bomb weighs %d bytes", out.Len())
	}
	return out.Bytes()
}
