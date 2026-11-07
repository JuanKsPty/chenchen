package plan_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/identity"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

// serve sends a request to /api/plan as user.
func (f fixture) serve(t *testing.T, user uuid.UUID, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(identity.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	plan.NewHandler(f.svc).Routes().ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decoding %d %s: %v", rec.Code, rec.Body, err)
	}
}

type errorBody struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields"`
	} `json:"error"`
}

func failure(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var e errorBody
	decode(t, rec, &e)
	return e
}

// item is the body of a PUT that saves an item, with fields added to it.
func item(kind, month, fields string) string {
	body := fmt.Sprintf(`{"kind": %q, "month": %q, "scope": "from", "name": "Sueldo", "amount": "650.00", "day": 15`, kind, month)
	if fields != "" {
		body += ", " + fields
	}
	return body + "}"
}

func TestTheHandlerSaysWhatIsWrong(t *testing.T) {
	f := newFixture(t)
	income := f.category(t, f.user, "Salario", "income")
	food := f.category(t, f.user, "Comida", "expense")
	id := uuid.NewString()

	for _, tt := range []struct {
		method, target, body string
		status               int
		code, message        string
	}{
		{"GET", "/?month=2026-13", "", 400, "invalid_month", "Revisa el mes."},
		{"GET", "/?month=noviembre", "", 400, "invalid_month", "Revisa el mes."},
		{"GET", "/?month=0000-01", "", 400, "invalid_month", "Revisa el mes."},
		{"PUT", "/items/" + id, item("income", "2026-1", ""), 400, "invalid_month", "Revisa el mes."},
		{"PUT", "/items/" + id, item("saving", "2026-11", `"target_month": "junio"`), 400, "invalid_month", "Revisa el mes."},
		{"PUT", "/items/" + id, item("income", "2026-11", `"color": "verde"`), 400, "unknown_field", ""},
		{"PUT", "/items/" + id, item("income", "2026-10", ""), 422, "plan_month_past", "Ese mes ya pasó; su plan queda como estaba."},
		{"PUT", "/items/" + id, item("income", "2028-12", ""), 422, "plan_month_too_far", "Puedes planear hasta 24 meses adelante."},
		{"DELETE", "/items/" + id + "?month=2026-11&scope=from", "", 404, "plan_item_not_found", "Eso ya no está en tu plan."},
		{"DELETE", "/items/no-es-un-id?month=2026-11&scope=from", "", 404, "plan_item_not_found", "Eso ya no está en tu plan."},
		{"DELETE", "/items/" + id + "?scope=from", "", 400, "invalid_month", "Revisa el mes."},
		{"PUT", "/caps/" + uuid.NewString(), `{"month": "2026-11", "scope": "from", "amount": "10"}`, 404, "category_not_found", "La categoría no existe."},
		{"DELETE", "/caps/" + food.String() + "?month=2026-11", "", 422, "validation_failed", "Revisa los datos marcados."},
	} {
		rec := f.serve(t, f.user, tt.method, tt.target, tt.body)
		e := failure(t, rec)
		if rec.Code != tt.status || e.Error.Code != tt.code || tt.message != "" && e.Error.Message != tt.message {
			t.Errorf("%s %s %s = %d %s; want %d %s", tt.method, tt.target, tt.body, rec.Code, rec.Body, tt.status, tt.code)
		}
	}

	// Everything wrong with a form is said at once, field by field.
	for _, tt := range []struct {
		body string
		want map[string]string
	}{
		{`{"kind": "fixed", "month": "2026-11", "amount": "abc"}`, map[string]string{
			"scope":  "Elige si vale desde este mes o solo para este mes.",
			"name":   "Ponle un nombre de hasta 60 caracteres.",
			"amount": "El monto no tiene un formato válido. Usa por ejemplo «12.50».",
			"day":    "Elige un día del 1 al 31.",
		}},
		{`{"kind": "income", "month": "2026-11", "scope": "from", "name": "Sueldo", "amount": "1.234", "day": 15}`,
			map[string]string{"amount": "El monto admite como máximo dos decimales."}},
		{`{"kind": "income", "month": "2026-11", "scope": "from", "name": "Sueldo", "amount": "-5", "day": 15}`,
			map[string]string{"amount": "Indica un monto mayor que cero."}},
		{item("purchase", "2026-11", `"start_month": "2026-12", "funding": "monthly"`), map[string]string{
			"day":          "Una compra planeada no lleva día.",
			"start_month":  "Una compra se empieza a apartar el mes en que la planeas.",
			"target_month": "Elige para cuándo, de aquí a 24 meses como mucho.",
		}},
		{item("income", "2026-11", fmt.Sprintf(`"category_id": %q`, food)),
			map[string]string{"category_id": "Elige una categoría de ingresos."}},
		{item("saving", "2026-11", `"target": "cero"`), map[string]string{
			"target": "El monto no tiene un formato válido. Usa por ejemplo «12.50».",
		}},
		// A paycheck can have no goal, whatever was typed in it.
		{item("income", "2026-11", `"target": "cero"`), map[string]string{
			"target": "Solo un ahorro tiene una meta.",
		}},
	} {
		rec := f.serve(t, f.user, "PUT", "/items/"+id, tt.body)
		e := failure(t, rec)
		if rec.Code != http.StatusUnprocessableEntity || e.Error.Code != "validation_failed" || len(e.Error.Fields) != len(tt.want) {
			t.Errorf("PUT %s = %d %s; want %v", tt.body, rec.Code, rec.Body, tt.want)
			continue
		}
		for field, message := range tt.want {
			if e.Error.Fields[field] != message {
				t.Errorf("PUT %s: %s = %q, want %q", tt.body, field, e.Error.Fields[field], message)
			}
		}
	}
	if n := storetest.Count(t, f.db, "plan_items"); n != 0 {
		t.Fatalf("a refused save wrote %d items", n)
	}

	// Saved: 201 and the month it was saved for; the same again, 200.
	body := item("income", "2026-11", fmt.Sprintf(`"match_text": " ACH EMPRESA X ", "category_id": %q`, income))
	var created, again map[string]any
	rec := f.serve(t, f.user, "PUT", "/items/"+id, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	decode(t, rec, &created)
	want := map[string]any{"id": id, "kind": "income", "name": "Sueldo", "day": float64(15),
		"match_text": "ACH EMPRESA X", "category_id": income.String(), "from": "2026-11", "to": nil,
		"amount": map[string]any{"cents": float64(65000), "formatted": "650.00", "currency": "USD"}}
	for field, value := range want {
		if fmt.Sprint(created[field]) != fmt.Sprint(value) {
			t.Errorf("created %s = %v, want %v", field, created[field], value)
		}
	}
	rec = f.serve(t, f.user, "PUT", "/items/"+id, body)
	if decode(t, rec, &again); rec.Code != http.StatusOK || fmt.Sprint(again) != fmt.Sprint(created) {
		t.Errorf("the same PUT again = %d %s", rec.Code, rec.Body)
	}

	// A paycheck stays a paycheck.
	rec = f.serve(t, f.user, "PUT", "/items/"+id, item("fixed", "2026-11", ""))
	if e := failure(t, rec); rec.Code != http.StatusUnprocessableEntity || e.Error.Code != "plan_kind_fixed" {
		t.Errorf("PUT as a payment = %d %s", rec.Code, rec.Body)
	}
	rec = f.serve(t, f.user, "DELETE", "/items/"+id+"?month=2026-11&scope=sometimes", "")
	if e := failure(t, rec); rec.Code != http.StatusUnprocessableEntity ||
		e.Error.Fields["scope"] != "Elige si vale desde este mes o solo para este mes." {
		t.Errorf("DELETE with a scope that is neither = %d %s", rec.Code, rec.Body)
	}

	// Caps: only on an expense category, more than zero.
	for _, tt := range []struct {
		target, body, field, message string
	}{
		{"/caps/" + income.String(), `{"month": "2026-11", "scope": "from", "amount": "100"}`, "category_id", "Elige una categoría de gastos."},
		{"/caps/" + food.String(), `{"month": "2026-11", "scope": "from", "amount": "0"}`, "amount", "Indica un monto mayor que cero."},
		{"/caps/" + food.String(), `{"month": "2026-11", "scope": "", "amount": "1.001"}`, "amount", "El monto admite como máximo dos decimales."},
	} {
		rec := f.serve(t, f.user, "PUT", tt.target, tt.body)
		if e := failure(t, rec); rec.Code != http.StatusUnprocessableEntity || e.Error.Fields[tt.field] != tt.message {
			t.Errorf("PUT %s %s = %d %s; want %s: %s", tt.target, tt.body, rec.Code, rec.Body, tt.field, tt.message)
		}
	}
	rec = f.serve(t, f.user, "PUT", "/caps/"+food.String(), `{"month": "2026-12", "scope": "from", "amount": "300"}`)
	var capView map[string]any
	if decode(t, rec, &capView); rec.Code != http.StatusOK || capView["category_id"] != food.String() ||
		capView["from"] != "2026-12" || capView["to"] != nil || fmt.Sprint(capView["amount"]) != fmt.Sprint(map[string]any{
		"cents": float64(30000), "formatted": "300.00", "currency": "USD"}) {
		t.Errorf("PUT a cap = %d %s", rec.Code, rec.Body)
	}
	if rec := f.serve(t, f.user, "DELETE", "/caps/"+food.String()+"?month=2026-12&scope=from", ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE the cap = %d %s", rec.Code, rec.Body)
	}
	if rec := f.serve(t, f.user, "DELETE", "/items/"+id+"?month=2026-11&scope=from", ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE the paycheck = %d %s", rec.Code, rec.Body)
	}
	if n := storetest.Count(t, f.db, "plan_items"); n != 0 {
		t.Errorf("after removing it from its first month on, %d items are left", n)
	}

	// A goal left blank is no goal: a saving may have none, and a paycheck
	// sent from the same form is not told to add one.
	for _, kind := range []string{"saving", "income"} {
		var saved map[string]any
		rec = f.serve(t, f.user, "PUT", "/items/"+uuid.NewString(), item(kind, "2026-11", `"target": "", "target_month": ""`))
		if decode(t, rec, &saved); rec.Code != http.StatusCreated || saved["target"] != nil {
			t.Errorf("PUT a %s with its goal left blank = %d %s", kind, rec.Code, rec.Body)
		}
	}
}

func TestCreatingAnItemTwiceWithTheSameIDCreatesItOnce(t *testing.T) {
	f := newFixture(t)
	id := uuid.NewString()
	body := item("income", "2026-11", `"match_text": "ACH EMPRESA X"`)
	for i, want := range []int{http.StatusCreated, http.StatusOK} {
		if rec := f.serve(t, f.user, "PUT", "/items/"+id, body); rec.Code != want {
			t.Fatalf("PUT #%d = %d %s, want %d", i+1, rec.Code, rec.Body, want)
		}
	}
	if items, versions := storetest.Count(t, f.db, "plan_items"), storetest.Count(t, f.db, "plan_item_versions"); items != 1 || versions != 1 {
		t.Errorf("%d items with %d versions, want one of each", items, versions)
	}
	p := f.month(t, nov26)
	if len(p.Income.Items) != 1 || p.Summary.Income != 65000 {
		t.Errorf("the plan expects %s from %d paychecks", p.Summary.Income, len(p.Income.Items))
	}
}

func TestAPastMonthCannotBeEdited(t *testing.T) {
	f := newFixture(t)
	food := f.category(t, f.user, "Comida", "expense")
	id := uuid.NewString()
	if rec := f.serve(t, f.user, "PUT", "/items/"+id, item("income", "2026-11", "")); rec.Code != http.StatusCreated {
		t.Fatalf("PUT for November = %d %s", rec.Code, rec.Body)
	}
	for _, tt := range []struct{ method, target, body string }{
		{"PUT", "/items/" + id, item("income", "2026-10", "")},
		{"PUT", "/items/" + uuid.NewString(), item("income", "2026-10", "")},
		{"DELETE", "/items/" + id + "?month=2026-10&scope=from", ""},
		{"DELETE", "/items/" + id + "?month=2026-10&scope=only", ""},
		{"PUT", "/caps/" + food.String(), `{"month": "2026-10", "scope": "from", "amount": "300"}`},
		{"DELETE", "/caps/" + food.String() + "?month=2026-10&scope=from", ""},
	} {
		rec := f.serve(t, f.user, tt.method, tt.target, tt.body)
		if e := failure(t, rec); rec.Code != http.StatusUnprocessableEntity || e.Error.Code != "plan_month_past" ||
			e.Error.Message != "Ese mes ya pasó; su plan queda como estaba." {
			t.Errorf("%s %s = %d %s", tt.method, tt.target, rec.Code, rec.Body)
		}
	}
	// Read, it says so.
	rec := f.serve(t, f.user, "GET", "/?month=2026-10", "")
	var october struct {
		Month    string `json:"month"`
		State    string `json:"state"`
		Planned  bool   `json:"planned"`
		Editable bool   `json:"editable"`
	}
	if decode(t, rec, &october); rec.Code != http.StatusOK || october.Month != "2026-10" || october.State != "past" ||
		october.Planned || october.Editable {
		t.Errorf("GET October = %d %s", rec.Code, rec.Body)
	}
	if n := storetest.Count(t, f.db, "plan_item_versions WHERE from_month < '2026-11-01'"); n != 0 {
		t.Errorf("%d versions reach into October", n)
	}
}

func TestOneOwnerNeverReachesAnothersPlan(t *testing.T) {
	f := newFixture(t)
	a := f.user
	b := newUser(t, f.db, "b@example.com")
	checking := f.account(t, a, "checking", "0001")
	savings := f.account(t, a, "savings", "0002")
	food := f.category(t, a, "Comida", "expense")
	paycheckID := uuid.NewString()
	for _, req := range []struct{ target, body string }{
		{"/items/" + paycheckID, item("income", "2026-11", `"match_text": "ACH EMPRESA X"`)},
		{"/caps/" + food.String(), `{"month": "2026-11", "scope": "from", "amount": "300"}`},
	} {
		if rec := f.serve(t, a, "PUT", req.target, req.body); rec.Code >= 300 {
			t.Fatalf("A: PUT %s = %d %s", req.target, rec.Code, rec.Body)
		}
	}
	f.filed(t, checking, on(nov26, 2), -2000, store.KindExpense, "SUPER X", &food)
	f.line(t, checking, on(nov26, 14), 65000, store.KindIncome, "ACH EMPRESA X")
	aBefore := f.serve(t, a, "GET", "/", "").Body.String()

	for _, tt := range []struct {
		method, target, body string
		status               int
		code                 string
	}{
		{"PUT", "/items/" + paycheckID, item("income", "2026-11", ""), http.StatusConflict, "plan_item_id_taken"},
		{"DELETE", "/items/" + paycheckID + "?month=2026-11&scope=from", "", http.StatusNotFound, "plan_item_not_found"},
		{"PUT", "/caps/" + food.String(), `{"month": "2026-11", "scope": "from", "amount": "1"}`, http.StatusNotFound, "category_not_found"},
		{"DELETE", "/caps/" + food.String() + "?month=2026-11&scope=from", "", http.StatusNotFound, "category_not_found"},
		{"PUT", "/items/" + uuid.NewString(), item("fixed", "2026-11", fmt.Sprintf(`"category_id": %q`, food)),
			http.StatusNotFound, "category_not_found"},
		{"PUT", "/items/" + uuid.NewString(), `{"kind": "saving", "month": "2026-11", "scope": "from", "name": "Fondo", "amount": "100", "account_id": "` + savings.String() + `"}`,
			http.StatusNotFound, "account_not_found"},
	} {
		rec := f.serve(t, b, tt.method, tt.target, tt.body)
		if e := failure(t, rec); rec.Code != tt.status || e.Error.Code != tt.code {
			t.Errorf("B: %s %s = %d %s; want %d %s", tt.method, tt.target, rec.Code, rec.Body, tt.status, tt.code)
		}
	}

	// B's plan has none of A's items, caps or movements.
	rec := f.serve(t, b, "GET", "/", "")
	var theirs struct {
		Planned bool `json:"planned"`
		Summary struct {
			Income struct {
				Cents int64 `json:"cents"`
			} `json:"income"`
			Spent struct {
				Cents int64 `json:"cents"`
			} `json:"spent"`
		} `json:"summary"`
		Income struct {
			Actual struct {
				Cents int64 `json:"cents"`
			} `json:"actual"`
			Items []json.RawMessage `json:"items"`
		} `json:"income"`
		Caps []json.RawMessage `json:"caps"`
	}
	if decode(t, rec, &theirs); rec.Code != http.StatusOK || theirs.Planned || theirs.Summary.Income.Cents != 0 ||
		theirs.Summary.Spent.Cents != 0 || theirs.Income.Actual.Cents != 0 || len(theirs.Income.Items) != 0 || len(theirs.Caps) != 0 {
		t.Errorf("B's plan = %d %s", rec.Code, rec.Body)
	}
	// And A's is as it was.
	if after := f.serve(t, a, "GET", "/", "").Body.String(); after != aBefore {
		t.Errorf("A's plan changed:\n%s\n%s", aBefore, after)
	}
}
