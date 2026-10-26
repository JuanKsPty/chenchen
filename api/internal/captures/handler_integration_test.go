package captures_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/captures"
	"github.com/JuanKsPty/chenchen/api/internal/identity"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

func TestOneOwnerNeverReachesAnothersPurchasesOrLines(t *testing.T) {
	f := newFixture(t)
	a := f.user
	b := f.newUser(t, "b@example.com")
	aCard := f.account(t, a, "credit_card")
	// Noted before their lines arrive, so nothing links them: one printed
	// purchase with its line, and two equal ones that share one.
	open := f.add(t, aCard, day, 950, "Kiosco")
	twins := []captures.Capture{f.add(t, aCard, day, 350, "Café"), f.add(t, aCard, day, 350, "Café")}
	aLine := f.line(t, aCard, day, -950, store.KindExpense, "CAFE")
	f.line(t, aCard, day, -350, store.KindExpense, "CAFE")
	bCard := f.account(t, b, "credit_card")
	bOpen := f.add(t, bCard, day, 950, "Café")

	for _, tt := range []struct {
		method, target, body string
		status               int
		code                 string
	}{
		{http.MethodPatch, "/" + open.ID.String(), `{"merchant": "Mío"}`, http.StatusNotFound, "capture_not_found"},
		{http.MethodPost, "/" + open.ID.String() + "/link", entry(aLine.ID), http.StatusNotFound, "capture_not_found"},
		{http.MethodDelete, "/" + twins[0].ID.String() + "/link", "", http.StatusNotFound, "capture_not_found"},
		{http.MethodPost, "/" + open.ID.String() + "/reject", entry(aLine.ID), http.StatusNotFound, "capture_not_found"},
		{http.MethodPut, "/" + open.ID.String() + "/discard", "", http.StatusNotFound, "capture_not_found"},
		{http.MethodDelete, "/" + open.ID.String() + "/discard", "", http.StatusNotFound, "capture_not_found"},
		// B's own purchase cannot reach A's line either.
		{http.MethodPost, "/" + bOpen.ID.String() + "/link", entry(aLine.ID), http.StatusNotFound, "entry_not_found"},
		{http.MethodPost, "/" + bOpen.ID.String() + "/reject", entry(aLine.ID), http.StatusNotFound, "entry_not_found"},
		// Nor note a purchase in A's account.
		{http.MethodPost, "/", fmt.Sprintf(`{"account_id": %q, "amount": "5.00", "merchant": "Mío", "day": %q}`, aCard.ID, day),
			http.StatusNotFound, "account_not_found"},
	} {
		rec := f.serve(t, b, tt.method, tt.target, tt.body)
		if rec.Code != tt.status || errorCode(t, rec) != tt.code {
			t.Errorf("B: %s %s = %d %s; want %d %s", tt.method, tt.target, rec.Code, rec.Body, tt.status, tt.code)
		}
	}
	var review struct {
		Captures []json.RawMessage `json:"captures"`
	}
	rec := f.serve(t, b, http.MethodGet, "/review", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &review); err != nil || rec.Code != http.StatusOK || len(review.Captures) != 0 {
		t.Errorf("B's review = %d %s", rec.Code, rec.Body)
	}

	// A's purchase is as it was: printed still, so its line was neither
	// linked nor rejected, and it was not renamed or discarded.
	if c := f.get(t, a, open.ID); c.State != store.CapturePrinted || c.Merchant != "Kiosco" || c.DiscardedAt != nil {
		t.Errorf("A's purchase = %+v", c)
	}
	if c := f.get(t, b, bOpen.ID); c.State != store.CapturePending || c.EntryID != nil {
		t.Errorf("B's purchase = %+v", c)
	}
	rec = f.serve(t, a, http.MethodGet, "/review", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &review); err != nil || len(review.Captures) != 3 {
		t.Errorf("A's review = %d %s; want the purchase and the twins", rec.Code, rec.Body)
	}
}

func TestTheHandlerSaysWhatIsWrong(t *testing.T) {
	f := newFixture(t)
	card, checking := f.account(t, f.user, "credit_card"), f.account(t, f.user, "checking")
	brokerage := f.account(t, f.user, "brokerage")

	// Everything wrong with a typed purchase is said at once.
	rec := f.serve(t, f.user, http.MethodPost, "/", `{"amount": "abc", "merchant": "  ", "day": "2999-01-01"}`)
	fields := errorFields(t, rec)
	if rec.Code != http.StatusUnprocessableEntity || fields["account_id"] != "Elige una cuenta." ||
		fields["amount"] != "El monto no tiene un formato válido. Usa por ejemplo «12.50»." ||
		fields["merchant"] != "Escribe dónde fue la compra." || fields["day"] != "El día no puede ser después de hoy." {
		t.Errorf("POST with everything wrong = %d %s", rec.Code, rec.Body)
	}
	for _, tt := range []struct {
		body, field, message string
	}{
		{`"amount": "-5", "merchant": "x"`, "amount", "El monto debe ser mayor que cero."},
		{`"amount": "", "merchant": "x"`, "amount", "Escribe el monto."},
		{`"amount": "1.234", "merchant": "x"`, "amount", "El monto admite como máximo dos decimales."},
		{`"amount": "4111111111111111", "merchant": "x"`, "amount", "El monto no puede pasar de 10,000,000.00."},
		{`"amount": "99999999999999999999", "merchant": "x"`, "amount", "El monto no puede pasar de 10,000,000.00."},
		{`"amount": "5", "merchant": "` + strings.Repeat("a", 141) + `"`, "merchant", "El comercio admite como máximo 140 caracteres."},
		{`"amount": "5", "merchant": "x", "note": "` + strings.Repeat("a", 501) + `"`, "note", "La nota admite como máximo 500 caracteres."},
	} {
		rec := f.serve(t, f.user, http.MethodPost, "/", fmt.Sprintf(`{"account_id": %q, %s}`, card.ID, tt.body))
		if got := errorFields(t, rec); rec.Code != http.StatusUnprocessableEntity || got[tt.field] != tt.message {
			t.Errorf("POST %s = %d %s; want %s: %s", tt.body, rec.Code, rec.Body, tt.field, tt.message)
		}
	}
	rec = f.serve(t, f.user, http.MethodPost, "/", fmt.Sprintf(`{"account_id": %q, "amount": "5", "merchant": "x"}`, brokerage.ID))
	if rec.Code != http.StatusUnprocessableEntity || errorMessage(t, rec) != "En una cuenta de inversión no se anotan compras." {
		t.Errorf("POST to a brokerage account = %d %s", rec.Code, rec.Body)
	}

	// Noted: 201, as the movement it reads as.
	rec = f.serve(t, f.user, http.MethodPost, "/",
		fmt.Sprintf(`{"account_id": %q, "amount": "9.50", "merchant": " Café ", "day": %q, "note": "con Ana"}`, card.ID, day))
	noted, err := decodeView(rec)
	if err != nil || rec.Code != http.StatusCreated ||
		noted.State != store.CapturePending || noted.Origin != store.OriginCapture || noted.CaptureSource != store.CaptureSourceManual ||
		noted.Kind != store.KindExpense || noted.Description != "Café" || noted.Note != "con Ana" ||
		noted.BookedOn != day.String() || noted.Amount.Cents != -950 || noted.Line != nil {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	first := noted.ID
	second := f.add(t, card, day, 1000, "Otra").ID

	// Linking: the line must be free, of the same account and a purchase.
	line := f.line(t, card, day, -950, store.KindExpense, "CAFE")
	rec = f.serve(t, f.user, http.MethodPost, "/"+first.String()+"/link", entry(line.ID))
	if noted, err = decodeView(rec); err != nil || rec.Code != http.StatusOK ||
		noted.State != store.CaptureLinked || noted.Line == nil || noted.Line.ID != line.ID {
		t.Fatalf("link = %d %s", rec.Code, rec.Body)
	}
	payment := f.line(t, card, day, 10000, store.KindTransfer, "GRACIAS POR SU PAGO")
	for _, tt := range []struct {
		method, target, body string
		status               int
		message              string
	}{
		{http.MethodPost, "/" + second.String() + "/link", entry(line.ID), http.StatusConflict,
			"Esa línea ya está unida a otra compra."},
		{http.MethodPost, "/" + second.String() + "/link", entry(f.line(t, checking, day, -1000, store.KindExpense, "X").ID),
			http.StatusUnprocessableEntity, "Esa línea es de otra cuenta."},
		{http.MethodPost, "/" + second.String() + "/link", entry(payment.ID), http.StatusUnprocessableEntity,
			"Esa línea no es un gasto: no puede ser esta compra."},
		{http.MethodPost, "/" + second.String() + "/link", entry(uuid.New()), http.StatusNotFound, "Esa línea no existe."},
		{http.MethodPost, "/" + second.String() + "/link", `{}`, http.StatusUnprocessableEntity, "Revisa los datos marcados."},
		{http.MethodPost, "/" + uuid.NewString() + "/link", entry(line.ID), http.StatusNotFound, "Esa compra no existe."},
		{http.MethodPatch, "/not-an-id", `{"merchant": "x"}`, http.StatusNotFound, "Esa compra no existe."},
		{http.MethodPatch, "/" + first.String(), `{"amount": "1.00"}`, http.StatusConflict,
			"Esta compra ya está unida a una línea de tu estado."},
		{http.MethodPut, "/" + first.String() + "/discard", "", http.StatusConflict,
			"Esta compra ya está unida a una línea de tu estado."},
		{http.MethodDelete, "/" + second.String() + "/link", "", http.StatusConflict,
			"Esta compra no está unida a ninguna línea."},
		{http.MethodPatch, "/" + second.String(), `{"amount": "0"}`, http.StatusUnprocessableEntity, "Revisa los datos marcados."},
		{http.MethodPatch, "/" + second.String(), `{"colour": "red"}`, http.StatusBadRequest,
			"No pudimos procesar lo que enviaste. Intenta otra vez."},
	} {
		rec := f.serve(t, f.user, tt.method, tt.target, tt.body)
		if rec.Code != tt.status || errorMessage(t, rec) != tt.message {
			t.Errorf("%s %s %s = %d %s; want %d %q", tt.method, tt.target, tt.body, rec.Code, rec.Body, tt.status, tt.message)
		}
	}

	// Discarded, it waits to be restored.
	rec = f.serve(t, f.user, http.MethodPut, "/"+second.String()+"/discard", "")
	if noted, err = decodeView(rec); err != nil || rec.Code != http.StatusOK || noted.State != store.CaptureDiscarded {
		t.Errorf("discard = %d %s", rec.Code, rec.Body)
	}
	rec = f.serve(t, f.user, http.MethodPatch, "/"+second.String(), `{"merchant": "y"}`)
	if rec.Code != http.StatusConflict || errorMessage(t, rec) != "Esta compra está descartada. Restáurala primero." {
		t.Errorf("PATCH a discarded purchase = %d %s", rec.Code, rec.Body)
	}
	rec = f.serve(t, f.user, http.MethodDelete, "/"+second.String()+"/discard", "")
	if noted, err = decodeView(rec); err != nil || rec.Code != http.StatusOK || noted.State != store.CapturePending {
		t.Errorf("restore = %d %s", rec.Code, rec.Body)
	}
	rec = f.serve(t, f.user, http.MethodPatch, "/"+second.String(), `{"amount": "12.00", "merchant": "Super"}`)
	if noted, err = decodeView(rec); err != nil || rec.Code != http.StatusOK ||
		noted.Amount.Cents != -1200 || noted.Description != "Super" {
		t.Errorf("PATCH = %d %s", rec.Code, rec.Body)
	}

	// Undone: the link is gone and the line is remembered as not this one.
	rec = f.serve(t, f.user, http.MethodDelete, "/"+first.String()+"/link", "")
	if noted, err = decodeView(rec); err != nil || rec.Code != http.StatusOK ||
		noted.State != store.CapturePending || noted.Line != nil {
		t.Errorf("unlink = %d %s", rec.Code, rec.Body)
	}
	rec = f.serve(t, f.user, http.MethodPost, "/"+second.String()+"/reject", entry(line.ID))
	if rec.Code != http.StatusOK {
		t.Errorf("reject = %d %s", rec.Code, rec.Body)
	}

	// Review always carries both lists, empty or not.
	f.statement(t, card, day.AddDays(3))
	rec = f.serve(t, f.user, http.MethodGet, "/review", "")
	var review struct {
		Captures []map[string]json.RawMessage `json:"captures"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &review); err != nil || rec.Code != http.StatusOK || len(review.Captures) != 2 {
		t.Fatalf("review = %d %s", rec.Code, rec.Body)
	}
	for _, item := range review.Captures {
		if string(item["state"]) != `"missing"` || string(item["candidates"]) != "[]" || item["near"] == nil {
			t.Errorf("review item = %v", item)
		}
	}
}

// --- fixtures ----------------------------------------------------------------

// viewBody is the part of a captures.View these tests read.
type viewBody struct {
	ID            uuid.UUID `json:"id"`
	State         string    `json:"state"`
	Origin        string    `json:"origin"`
	CaptureSource string    `json:"capture_source"`
	Kind          string    `json:"kind"`
	Description   string    `json:"description"`
	Note          string    `json:"note"`
	BookedOn      string    `json:"booked_on"`
	Amount        struct {
		Cents int64 `json:"cents"`
	} `json:"amount"`
	Line *struct {
		ID uuid.UUID `json:"id"`
	} `json:"line"`
}

func decodeView(rec *httptest.ResponseRecorder) (viewBody, error) {
	var v viewBody
	err := json.Unmarshal(rec.Body.Bytes(), &v)
	return v, err
}

// serve sends one request to the captures router as user.
func (f fixture) serve(t *testing.T, user uuid.UUID, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(identity.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	captures.NewHandler(f.svc).Routes().ServeHTTP(rec, req)
	return rec
}

func entry(id uuid.UUID) string { return fmt.Sprintf(`{"entry_id": %q}`, id) }

type errorBody struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	return body
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decodeError(t, rec).Error.Code
}

func errorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decodeError(t, rec).Error.Message
}

func errorFields(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	return decodeError(t, rec).Error.Fields
}
