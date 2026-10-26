package imports_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/captures"
	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/identity"
	"github.com/JuanKsPty/chenchen/api/internal/imports"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/movements"
	"github.com/JuanKsPty/chenchen/api/internal/rules"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/transfers"
)

// An import links each purchase the owner noted to the line it can only be,
// and the line takes what the owner said about it.

func TestAnImportLinksEachNotedPurchaseAndCarriesWhatTheOwnerSaid(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cats := categories.NewService(e.db)
	svc := captures.NewService(e.db, cats)
	card := e.openCard(t, "1111")
	fun, _, _ := cats.FindByName(ctx, e.user, "Entretenimiento")
	salud, _, _ := cats.FindByName(ctx, e.user, "Salud")

	cafe, err := svc.Add(ctx, e.user, captures.AddInput{AccountID: card, Amount: 950, Merchant: "Café de Prueba",
		Day: jan(18), CategoryID: &fun.ID, Note: "con Ana"})
	if err != nil {
		t.Fatal(err)
	}
	pharmacy, err := svc.Add(ctx, e.user, captures.AddInput{AccountID: card, Amount: 425, Merchant: "Farmacia",
		Day: jan(19), Note: "receta"})
	if err != nil {
		t.Fatal(err)
	}
	if pharmacy.CategoryID != nil {
		t.Fatalf("fixture: no rule should file %q", pharmacy.Merchant)
	}
	super, err := svc.Add(ctx, e.user, captures.AddInput{AccountID: card, Amount: 1200, Merchant: "Super 99", Day: jan(25)})
	if err != nil {
		t.Fatal(err)
	}

	// Imported the way the web does it, to see what the owner is told.
	rec := e.upload(t, "tarjeta.txt", []byte(cardFile))
	var result struct {
		Accounts []map[string]any `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || rec.Code != http.StatusOK || len(result.Accounts) != 1 {
		t.Fatalf("import = %d %s", rec.Code, rec.Body)
	}
	if got := result.Accounts[0]; got["captures_linked"] != float64(2) || got["captures_missing"] != nil {
		t.Errorf("the import says %v linked and %v missing; want 2 and nothing", got["captures_linked"], got["captures_missing"])
	}

	moves := movements.NewService(e.db, cats)
	for _, tt := range []struct {
		capture        uuid.UUID
		category       uuid.UUID
		by, note, what string
	}{
		// The owner filed the café: their choice beats the rule that filed
		// its line from the bank's own category.
		{cafe.ID, fun.ID, "user", "con Ana", "the café"},
		// Nobody filed the pharmacy: its line keeps what the rule said, and
		// takes the note.
		{pharmacy.ID, salud.ID, "rule", "receta", "the pharmacy"},
	} {
		c, err := e.db.Q().CaptureByID(ctx, e.user, tt.capture)
		if err != nil || c.State != store.CaptureLinked || c.LinkedBy == nil || *c.LinkedBy != store.LinkedByAuto {
			t.Fatalf("%s = %+v, %v; want linked by the import", tt.what, c, err)
		}
		line, err := moves.Get(ctx, e.user, *c.EntryID)
		if err != nil {
			t.Fatal(err)
		}
		if line.Origin != store.OriginStatement || line.CategoryID == nil || *line.CategoryID != tt.category ||
			*line.CategorySource != tt.by || line.Note != tt.note || line.Amount != c.Amount {
			t.Errorf("%s's line = %+v", tt.what, line)
		}
	}
	if c, _ := e.db.Q().CaptureByID(ctx, e.user, super.ID); c.State != store.CapturePending {
		t.Errorf("a purchase no line prints is %s, want pending", c.State)
	}
	if got := e.spent(t); got != 1430+1200 {
		t.Errorf("spent = %s, want 26.30: the statement and the unprinted 12.00", got)
	}
}

func TestAPurchaseNotedAfterItsLineLinksAtOnceAndLeavesTheOwnersChoices(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cats := categories.NewService(e.db)
	svc := captures.NewService(e.db, cats)
	moves := movements.NewService(e.db, cats)
	card := e.importFile(t, "tarjeta.txt", []byte(cardFile)).Accounts[0].AccountID
	compras, _, _ := cats.FindByName(ctx, e.user, "Compras")
	fun, _, _ := cats.FindByName(ctx, e.user, "Entretenimiento")
	salud, _, _ := cats.FindByName(ctx, e.user, "Salud")
	if _, _, err := rules.NewService(e.db, cats).Create(ctx, e.user, rules.Input{MatchText: "farmacia", CategoryID: &compras.ID}); err != nil {
		t.Fatal(err)
	}

	// The owner already filed the café's line and wrote on it.
	cafeLine := e.lineOf(t, "CAFE DE PRUEBA")
	if _, err := moves.SetCategory(ctx, e.user, cafeLine, &compras.ID, "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := moves.SetNote(ctx, e.user, cafeLine, "desayuno"); err != nil {
		t.Fatal(err)
	}
	before := e.spent(t)

	// Noted afterwards, each links on the spot: the café keeps the owner's
	// category and note, whatever the purchase says...
	cafe, err := svc.Add(ctx, e.user, captures.AddInput{AccountID: card, Amount: 950, Merchant: "Café de Prueba",
		Day: jan(18), CategoryID: &fun.ID, Note: "con Ana"})
	if err != nil {
		t.Fatal(err)
	}
	if cafe.State != store.CaptureLinked || cafe.Line == nil || cafe.Line.ID != cafeLine ||
		*cafe.Line.CategoryID != compras.ID || *cafe.Line.CategorySource != "user" || cafe.Line.Note != "desayuno" {
		t.Errorf("the café = %+v with line %+v", cafe.Capture, cafe.Line)
	}
	// ...and a rule against a rule keeps the line's, filed from the bank's
	// richer text.
	pharmacy, err := svc.Add(ctx, e.user, captures.AddInput{AccountID: card, Amount: 425, Merchant: "Farmacia", Day: jan(19)})
	if err != nil {
		t.Fatal(err)
	}
	if *pharmacy.CategoryID != compras.ID || *pharmacy.CategorySource != "rule" {
		t.Errorf("the pharmacy is filed under %v by %v, want Compras by the rule", pharmacy.CategoryID, pharmacy.CategorySource)
	}
	if pharmacy.State != store.CaptureLinked || pharmacy.Line == nil || *pharmacy.Line.CategoryID != salud.ID ||
		*pharmacy.Line.CategorySource != "rule" {
		t.Errorf("the pharmacy = %+v with line %+v", pharmacy.Capture, pharmacy.Line)
	}
	if got := e.spent(t); got != before {
		t.Errorf("spent = %s, was %s: a purchase whose line is imported never counts twice", got, before)
	}
}

func TestAnAmbiguousPairStaysPrintedAndIsNotLinked(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	svc := captures.NewService(e.db, categories.NewService(e.db))
	card := e.openCard(t, "1111")
	twins := []uuid.UUID{e.note(t, card, jan(10), -350, "Café"), e.note(t, card, jan(10), -350, "Café")}
	movie := e.note(t, card, jan(12), -700, "Cine")

	// One line for two coffees, two lines for one movie: none is obvious.
	r := e.importFile(t, "enero.txt", []byte(cardHeader+"\r\n"+
		"**** 1111;**** 1111;10/01/2026;10/01/2026;CAFE;R1;Comida y Bebida;-3.5;\r\n"+
		"**** 1111;**** 1111;12/01/2026;12/01/2026;CINE;R2;Entretenimiento;-7.00;\r\n"+
		"**** 1111;**** 1111;13/01/2026;13/01/2026;CINE;R3;Entretenimiento;-7.00;\r\n"))
	if n := r.Accounts[0].CapturesLinked; n != 0 {
		t.Errorf("the import linked %d", n)
	}
	for _, id := range append(twins, movie) {
		if c, err := e.db.Q().CaptureByID(ctx, e.user, id); err != nil || c.State != store.CapturePrinted {
			t.Errorf("%s = %+v, %v; want printed", c.Merchant, c, err)
		}
	}
	// Printed, none counts: the lines do, once.
	if got := e.spent(t); got != 350+1400 {
		t.Errorf("spent = %s, want 17.50", got)
	}
	review, err := svc.Review(ctx, e.user)
	if err != nil || len(review) != 3 {
		t.Fatalf("Review = %+v, %v", review, err)
	}
	for _, item := range review {
		want := 1
		if item.Capture.ID == movie {
			want = 2
		}
		if len(item.Candidates) != want {
			t.Errorf("%s has %d candidates, want %d", item.Capture.Merchant, len(item.Candidates), want)
		}
	}
}

func TestAPairTheOwnerUnlinkedIsNeverLinkedAgain(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	svc := captures.NewService(e.db, categories.NewService(e.db))
	card := e.openCard(t, "1111")
	cafe := e.note(t, card, jan(18), -950, "Café de Prueba")
	if r := e.importFile(t, "tarjeta.txt", []byte(cardFile)); r.Accounts[0].CapturesLinked != 1 {
		t.Fatalf("the import linked %d, want the café", r.Accounts[0].CapturesLinked)
	}

	// "No es esta compra": the line is another coffee, so both count.
	got, err := svc.Unlink(ctx, e.user, cafe)
	if err != nil || got.State != store.CapturePending {
		t.Fatalf("Unlink = %+v, %v", got, err)
	}
	if got := e.spent(t); got != 1430+950 {
		t.Errorf("spent = %s, want 23.80", got)
	}

	// The next statement prints that line again, and goes past the
	// purchase's day without another: it is not linked again, and it is
	// missing now. The import says so, once.
	line := "**** 1111;**** 2222;18/01/2026;19/01/2026;CAFE DE PRUEBA PANAMA;R3;Comida y Bebida;-9.5;\r\n"
	r := e.importFile(t, "segundo.txt", []byte(cardHeader+"\r\n"+line+
		"**** 1111;**** 2222;25/01/2026;25/01/2026;OTRA TIENDA;R5;Tiendas y almacenes;-3.00;\r\n"))
	if a := r.Accounts[0]; a.Duplicates != 1 || a.CapturesLinked != 0 || a.CapturesMissing != 1 {
		t.Errorf("the import: %d duplicates, %d linked, %d missing; want 1, 0, 1", a.Duplicates, a.CapturesLinked, a.CapturesMissing)
	}
	if c, _ := e.db.Q().CaptureByID(ctx, e.user, cafe); c.State != store.CaptureMissing {
		t.Errorf("the café is %s, want missing", c.State)
	}
	if got := e.spent(t); got != 1430+300 {
		t.Errorf("spent = %s, want 17.30", got)
	}
	r = e.importFile(t, "tercero.txt", []byte(cardHeader+"\r\n"+
		"**** 1111;**** 2222;28/01/2026;28/01/2026;KIOSCO;R6;Tiendas y almacenes;-1.00;\r\n"))
	if a := r.Accounts[0]; a.CapturesMissing != 0 {
		t.Errorf("a later import counts the café missing again: %d", a.CapturesMissing)
	}
	review, err := svc.Review(ctx, e.user)
	if err != nil || len(review) != 1 || review[0].Capture.ID != cafe || len(review[0].Near) != 0 {
		t.Errorf("Review = %+v, %v; want the café without the line it is not", review, err)
	}
}

// Linking only says which line a purchase was. Nothing that builds a balance,
// a drift, a reconciliation or a transfer may notice.
func TestLinkingNeverTouchesBalancesDriftReconcileOrTransfers(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cats := categories.NewService(e.db)
	svc := captures.NewService(e.db, cats)
	fun, _, _ := cats.FindByName(ctx, e.user, "Entretenimiento")

	card := e.importFile(t, "tarjeta.txt", []byte(cardFile)).Accounts[0].AccountID
	if _, err := e.accounts.AddCheckpoint(ctx, e.user, card, accounts.CheckpointInput{
		AsOf: jan(14), Balance: -10000, Pin: true,
	}); err != nil {
		t.Fatal(err)
	}
	bank := e.importFile(t, "estado.csv", bacFileOn("15/01/2026", "600.00", "500.00",
		"10/01/2026, 1, 4A, SUPER, 100.00, 0.00, 500.00")).Accounts[0].AccountID
	// A purchase of the card payment's size, noted on the bank before the
	// payment's statement arrives. The import finds the payment is a
	// transfer first, so the purchase is never linked to it on a guess.
	bait := e.note(t, bank, jan(18), -10000, "Tienda")
	e.importFile(t, "estado2.csv", bacFileOn("31/01/2026", "500.00", "400.00",
		"18/01/2026, 2, 4A, PAGO TC, 100.00, 0.00, 400.00"))
	payment := e.lineOf(t, "PAGO TC")
	if c, _ := e.db.Q().CaptureByID(ctx, e.user, bait); c.State != store.CapturePrinted {
		t.Fatalf("the bait = %s, want printed and not linked", c.State)
	}

	read := func() balanceFacts {
		t.Helper()
		var f balanceFacts
		all, err := e.accounts.List(ctx, e.user)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range all {
			f.Accounts = append(f.Accounts, accountFacts{a.ID, a.Balance, a.Anchored, a.Drift, a.Movements, a.LastDay})
			rec, err := e.accounts.Reconcile(ctx, e.user, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			f.Opening = append(f.Opening, rec.Opening)
			f.Checks = append(f.Checks, rec.Checks...)
			f.FirstBreaks = append(f.FirstBreaks, rec.FirstBreak)
			for _, l := range rec.Lines {
				f.Lines = append(f.Lines, reconciledLine{lineFactsOf(l.Entry), l.Computed, l.Difference})
			}
			ordered, err := e.db.Q().AccountEntriesInOrder(ctx, e.user, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, l := range ordered {
				f.Ordered = append(f.Ordered, lineFactsOf(l))
			}
		}
		f.Assets, f.Debts, f.Holdings = accounts.NetWorth(all)
		if f.Sums, err = e.db.Q().SumsByAccount(ctx, e.user); err != nil {
			t.Fatal(err)
		}
		if f.Suggestions, err = transfers.NewService(e.db).Suggestions(ctx, e.user); err != nil {
			t.Fatal(err)
		}
		if f.Candidates, err = e.db.Q().TransferCandidates(ctx, e.user, transfers.Window, civil.Date{}, civil.Date{}); err != nil {
			t.Fatal(err)
		}
		for i := range f.Candidates {
			f.Candidates[i].Out, f.Candidates[i].In = withoutNotes(f.Candidates[i].Out), withoutNotes(f.Candidates[i].In)
		}
		p, err := e.db.Q().EntryByID(ctx, e.user, payment)
		if err != nil {
			t.Fatal(err)
		}
		f.PaymentKind = p.EffectiveKind()
		return f
	}
	before := read()
	if before.PaymentKind != store.KindTransfer || len(before.Suggestions) != 0 {
		t.Fatalf("the fixture is not what this test assumes: %+v", before)
	}

	// Every way a purchase meets its line: noted after it and linked at once,
	// linked by hand with a tip, unlinked, rejected, discarded, restored,
	// corrected, and linked by hand to a line that counts as a transfer.
	cafe, err := svc.Add(ctx, e.user, captures.AddInput{AccountID: card, Amount: 950, Merchant: "Café", Day: jan(18),
		CategoryID: &fun.ID, Note: "con Ana"})
	if err != nil || cafe.State != store.CaptureLinked {
		t.Fatalf("Add = %+v, %v", cafe, err)
	}
	tip, err := svc.Add(ctx, e.user, captures.AddInput{AccountID: card, Amount: 400, Merchant: "Farmacia", Day: jan(19), Note: "propina"})
	if err != nil {
		t.Fatal(err)
	}
	steps := []func() (captures.Capture, error){
		func() (captures.Capture, error) { return svc.Link(ctx, e.user, tip.ID, e.lineOf(t, "FARMACIA")) },
		func() (captures.Capture, error) { return svc.Unlink(ctx, e.user, cafe.ID) },
		func() (captures.Capture, error) { return svc.Reject(ctx, e.user, cafe.ID, e.lineOf(t, "DESGRAVAMEN")) },
		func() (captures.Capture, error) { return svc.Discard(ctx, e.user, cafe.ID) },
		func() (captures.Capture, error) { return svc.Restore(ctx, e.user, cafe.ID) },
		func() (captures.Capture, error) {
			amount := money.Cents(1000)
			return svc.Edit(ctx, e.user, cafe.ID, captures.EditInput{Amount: &amount})
		},
		func() (captures.Capture, error) { return svc.Link(ctx, e.user, bait, payment) },
	}
	for i, step := range steps {
		if _, err := step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if after := read(); !reflect.DeepEqual(after, before) {
			t.Errorf("step %d moved something:\nbefore %+v\nafter  %+v", i, before, after)
		}
	}
	// What linking may change, it did.
	if l, _ := e.db.Q().EntryByID(ctx, e.user, e.lineOf(t, "FARMACIA")); l.Note != "propina" {
		t.Errorf("the pharmacy's line has note %q, want the purchase's", l.Note)
	}
}

// --- fixtures ----------------------------------------------------------------

// balanceFacts is everything a balance, a drift, a reconciliation or a
// transfer is built from, without what the owner says about a line.
type balanceFacts struct {
	Accounts                []accountFacts
	Assets, Debts, Holdings money.Cents
	Sums                    map[uuid.UUID]store.AccountSums
	Opening                 []money.Cents
	Checks                  []accounts.Check
	FirstBreaks             []*uuid.UUID
	Lines                   []reconciledLine
	Ordered                 []lineFacts
	Suggestions             []store.TransferCandidate
	Candidates              []store.TransferCandidate
	PaymentKind             string
}

type accountFacts struct {
	ID        uuid.UUID
	Balance   money.Cents
	Anchored  bool
	Drift     *accounts.Drift
	Movements int
	LastDay   civil.Date
}

type reconciledLine struct {
	Line       lineFacts
	Computed   money.Cents
	Difference *money.Cents
}

// lineFacts is what a statement printed about a line and how it counts:
// everything but its category and note, which linking may carry over.
type lineFacts struct {
	ID          uuid.UUID
	BookedOn    civil.Date
	PostedOn    *civil.Date
	BalanceOn   civil.Date
	Seq         int
	Amount      money.Cents
	Kind        string
	UserKind    *string
	Description string
	BankBalance *money.Cents
}

func lineFactsOf(e store.Entry) lineFacts {
	return lineFacts{e.ID, e.BookedOn, e.PostedOn, e.BalanceOn, e.Seq, e.Amount, e.Kind, e.UserKind, e.Description, e.BankBalance}
}

func withoutNotes(e store.Entry) store.Entry {
	e.CategoryID, e.CategorySource, e.Note = nil, nil, ""
	return e
}

// bacFileOn is bacFile downloaded on another day: the day its balances are as
// of, and its period's end.
func bacFileOn(day, inicial, libros string, detail ...string) []byte {
	return bytes.Replace(bacFile(inicial, libros, detail...), []byte("30/06/2026"), []byte(day), 1)
}

// lineOf is the id of the only printed line whose text contains search.
func (e env) lineOf(t *testing.T, search string) uuid.UUID {
	t.Helper()
	var found []uuid.UUID
	err := e.db.Q().EachEntry(context.Background(), store.EntryFilter{UserID: e.user, Search: search}, func(m store.Entry) error {
		if m.Origin == store.OriginStatement {
			found = append(found, m.ID)
		}
		return nil
	})
	if err != nil || len(found) != 1 {
		t.Fatalf("lines matching %q = %v, %v; want one", search, found, err)
	}
	return found[0]
}

// upload imports a file through the HTTP handler, as the web does.
func (e env) upload(t *testing.T, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req = req.WithContext(identity.WithUser(req.Context(), e.user))
	rec := httptest.NewRecorder()
	imports.NewHandler(e.imports, e.db).Routes().ServeHTTP(rec, req)
	return rec
}
