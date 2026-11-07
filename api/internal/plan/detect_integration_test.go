package plan_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// history is what an owner's accounts printed from July to the 6th of
// November 2026, shaped like the history the plan was made for: two
// paychecks a month, a payment to a person, an insurance charged with its tax
// on the card, a theft cover, weekly groceries, a camera bought once, extra
// income twice, the card paid from checking every month without the pair
// confirmed, a saving every month, money to the brokerage once and a
// transfer to another person once.
type history struct {
	checking, card, savings, brokerage uuid.UUID
}

func (f fixture) history(t *testing.T) history {
	t.Helper()
	h := history{
		checking:  f.account(t, f.user, "checking", "0001"),
		card:      f.account(t, f.user, "credit_card", "4321"),
		savings:   f.account(t, f.user, "savings", "0002"),
		brokerage: f.account(t, f.user, "brokerage", "U0001"),
	}
	if err := f.db.Q().SetAccountAlias(context.Background(), f.user, h.savings, "Inversion"); err != nil {
		t.Fatal(err)
	}
	for _, d := range []struct {
		m             time.Month
		first, second int
	}{{time.July, 16, 30}, {time.August, 14, 29}, {time.September, 16, 30}, {time.October, 16, 30}} {
		f.line(t, h.checking, day(d.m, d.first), 65000, store.KindIncome, "ACH CRE EMPRESA X S.A.")
		f.line(t, h.checking, day(d.m, d.second), 65000, store.KindIncome, "ACH CRE EMPRESA X S.A.")
	}
	f.line(t, h.checking, day(time.July, 3), 20000, store.KindIncome, "DEPOSITO EN EFECTIVO")
	f.line(t, h.checking, day(time.September, 3), 15000, store.KindIncome, "DEPOSITO EN EFECTIVO")
	for _, at := range []civil.Date{day(time.July, 5), day(time.August, 4), day(time.September, 6),
		day(time.October, 5), day(time.November, 5)} {
		f.line(t, h.checking, at, -3267, store.KindExpense, "YAPPY BG A PERSONA UNO")
	}
	for _, m := range []time.Month{time.July, time.August, time.September, time.October, time.November} {
		f.line(t, h.checking, day(m, 4), -263, store.KindFee, "PROTECCION ROBO 4000-00**-****-00000")
	}
	for _, m := range []time.Month{time.August, time.September, time.October} {
		f.line(t, h.card, day(m, 18), -350, store.KindFee, "SEGURO DE FRAUDE MENSUAL")
		f.line(t, h.card, day(m, 18), -18, store.KindFee, "ITBMS CARGO POR SEGURO")
		f.line(t, h.checking, day(m, 17), -30000, store.KindTransfer, "TRANSFERENCIA A INVERSION")
		f.line(t, h.savings, day(m, 17), 30000, store.KindTransfer, "TRANSFERENCIA DE CORRIENTE")
	}
	for _, m := range []time.Month{time.July, time.August, time.September, time.October} {
		for i, d := range []int{3, 10, 17, 24} {
			f.line(t, h.card, day(m, d), -money.Cents(3800+i*450+int(m)), store.KindExpense, "SUPERMERCADO UNO")
		}
		// The card paid from checking: the bank prints it as spending until
		// the owner confirms the pair.
		f.line(t, h.checking, day(m, 10), -20000, store.KindExpense, "PAGO A TARJETA 4321")
		f.line(t, h.card, day(m, 11), 20000, store.KindTransfer, "PAGO RECIBIDO GRACIAS")
	}
	f.line(t, h.card, day(time.October, 12), -110200, store.KindExpense, "TIENDA DE CAMARAS")
	f.line(t, h.checking, day(time.September, 20), -95000, store.KindTransfer, "ENVIO A CORREDOR")
	f.line(t, h.brokerage, day(time.September, 20), 95000, store.KindTransfer, "DEPOSITO")
	f.line(t, h.checking, day(time.October, 8), -45500, store.KindTransfer, "TEF A PERSONA DOS")
	return h
}

// suggestionsView is what GET /api/plan/suggestions answers, as a client
// reads it.
type suggestionsView struct {
	Income  []suggestionView `json:"income"`
	Fixed   []suggestionView `json:"fixed"`
	Savings []suggestionView `json:"savings"`
}

type suggestionView struct {
	Key    string `json:"key"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Amount struct {
		Cents int64 `json:"cents"`
	} `json:"amount"`
	Day        int        `json:"day"`
	MatchText  string     `json:"match_text"`
	CategoryID *uuid.UUID `json:"category_id"`
	MonthsSeen int        `json:"months_seen"`
	LastOn     string     `json:"last_on"`
	Person     bool       `json:"person"`
	AccountID  *uuid.UUID `json:"account_id"`
}

// suggestionLine is a suggestion as a test compares it.
type suggestionLine struct {
	key, name string
	cents     int64
	day       int
}

func linesOf(views []suggestionView) []suggestionLine {
	out := make([]suggestionLine, 0, len(views))
	for _, v := range views {
		out = append(out, suggestionLine{v.Key, v.Name, v.Amount.Cents, v.Day})
	}
	return out
}

func (f fixture) suggestions(t *testing.T, user uuid.UUID) suggestionsView {
	t.Helper()
	rec := f.serve(t, user, "GET", "/suggestions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /suggestions = %d %s", rec.Code, rec.Body)
	}
	var v suggestionsView
	decode(t, rec, &v)
	return v
}

func wantLines(t *testing.T, what string, got []suggestionView, want ...suggestionLine) {
	t.Helper()
	if lines := linesOf(got); !slices.Equal(lines, want) {
		t.Errorf("%s = %+v, want %+v", what, lines, want)
	}
}

func TestTheSuggestionsAreWhatTheHistoryShows(t *testing.T) {
	f := newFixture(t)
	if rec := f.serve(t, f.user, "GET", "/suggestions", ""); rec.Code != http.StatusOK ||
		strings.TrimSpace(rec.Body.String()) != `{"income":[],"fixed":[],"savings":[]}` {
		t.Fatalf("with no history, GET /suggestions = %d %s", rec.Code, rec.Body)
	}
	h := f.history(t)

	got := f.suggestions(t, f.user)

	wantLines(t, "income", got.Income,
		suggestionLine{"ach cre empresa x s.a.#1", "Ach Cre Empresa X S.A.", 65000, 16},
		suggestionLine{"ach cre empresa x s.a.#2", "Ach Cre Empresa X S.A.", 65000, 30})
	wantLines(t, "fixed", got.Fixed,
		suggestionLine{"persona uno", plan.PersonName, 3267, 5},
		suggestionLine{"proteccion robo", "Proteccion Robo", 263, 4},
		suggestionLine{"seguro de fraude mensual", "Seguro de Fraude Mensual", 368, 18})
	wantLines(t, "savings", got.Savings,
		suggestionLine{"account:" + h.savings.String(), "Ahorro en Inversion", 30000, 0})

	yappy := got.Fixed[0]
	if !yappy.Person || yappy.MatchText != "PERSONA UNO" || yappy.MonthsSeen != 5 || yappy.LastOn != "2026-11-05" ||
		yappy.Kind != plan.KindFixed || yappy.AccountID != nil {
		t.Errorf("the payment to a person = %+v", yappy)
	}
	if s := got.Savings[0]; s.AccountID == nil || *s.AccountID != h.savings || s.MatchText != "" ||
		s.MonthsSeen != 3 || s.LastOn != "2026-10-17" {
		t.Errorf("the saving = %+v", s)
	}
}

func TestTwoPaychecksAMonthAreSuggestedHoweverTheMonthsHoldThem(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	// July holds June's late paycheck of the month's end, its two and
	// something else the same employer paid once; August only the 16th, its
	// statement missing a line, and something else on the 22nd; September
	// only the 15th, its paycheck of the month's end slipping to the 1st of
	// October, which holds three.
	for _, at := range []civil.Date{day(time.July, 2), day(time.July, 15), day(time.July, 30),
		day(time.August, 16), day(time.September, 15), day(time.October, 1), day(time.October, 16),
		day(time.October, 30)} {
		f.line(t, checking, at, 65000, store.KindIncome, salaryText)
	}
	f.line(t, checking, day(time.July, 15), 30000, store.KindIncome, salaryText)
	f.line(t, checking, day(time.August, 22), 8500, store.KindIncome, salaryText)
	first := suggestionLine{"ach cre empresa x s.a.#1", "Ach Cre Empresa X S.A.", 65000, 15}

	got := f.suggestions(t, f.user)

	wantLines(t, "income", got.Income, first,
		suggestionLine{"ach cre empresa x s.a.#2", "Ach Cre Empresa X S.A.", 65000, 30})
	if end := suggestedKey(got.Income, "ach cre empresa x s.a.#2"); end == nil || end.MonthsSeen != 3 ||
		end.LastOn != "2026-10-30" {
		t.Errorf("the paycheck of the month's end = %+v", end)
	}

	// Turning down the one at the month's end leaves the other.
	if rec := f.serve(t, f.user, "PUT", "/dismissals", `{"kind": "income", "key": "ach cre empresa x s.a.#2"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT /dismissals = %d %s", rec.Code, rec.Body)
	}
	wantLines(t, "income once the month's end one is turned down", f.suggestions(t, f.user).Income, first)
}

func TestTransfersAndCardPaymentsAreNeverSuggested(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	card := f.account(t, f.user, "credit_card", "4321")
	savings := f.account(t, f.user, "savings", "0002")
	for _, m := range []time.Month{time.July, time.August, time.September, time.October} {
		// Paid from checking the same every month, but not yet paired: the
		// line on checking is still spending, and half of an open pair.
		f.line(t, checking, day(m, 10), -20000, store.KindExpense, "PAGO A TARJETA 4321")
		f.line(t, card, day(m, 12), 20000, store.KindTransfer, "PAGO RECIBIDO GRACIAS")
		// A transfer both sides of which are transfers.
		f.line(t, checking, day(m, 1), -10000, store.KindTransfer, "TRANSFERENCIA A AHORROS")
		f.line(t, savings, day(m, 1), 10000, store.KindTransfer, "TRANSFERENCIA DE CORRIENTE")
		// A transfer out to someone else's account the bank called one.
		f.line(t, checking, day(m, 8), -45500, store.KindTransfer, "TEF A PERSONA DOS")
		// A refund every month is money back, not income.
		f.line(t, card, day(m, 20), 500, store.KindRefund, "DEVOLUCION TIENDA")
	}

	got := f.suggestions(t, f.user)

	wantLines(t, "income", got.Income)
	wantLines(t, "fixed", got.Fixed)
	// What reached savings is a saving, never a payment.
	wantLines(t, "savings", got.Savings, suggestionLine{"account:" + savings.String(), "Ahorro de cada mes", 10000, 0})
}

func TestAPlannedChargeIsNotSuggestedAgainButTheOtherPaycheckIs(t *testing.T) {
	f := newFixture(t)
	h := f.history(t)
	f.now(time.Date(2026, time.November, 20, 9, 0, 0, 0, civil.Panama))
	nov16 := f.line(t, h.checking, day(time.November, 16), 65000, store.KindIncome, "ACH CRE EMPRESA X S.A.")
	f.line(t, h.card, day(time.November, 18), -350, store.KindFee, "SEGURO DE FRAUDE MENSUAL")
	f.line(t, h.card, day(time.November, 18), -18, store.KindFee, "ITBMS CARGO POR SEGURO")
	before := f.suggestions(t, f.user)
	if len(before.Income) != 2 || suggestedKey(before.Fixed, "seguro de fraude mensual") == nil {
		t.Fatalf("before planning: %+v", before)
	}

	// The insurance, by a shorter text than the one suggested.
	f.save(t, uuid.New(), plan.ItemInput{Kind: plan.KindFixed, Month: nov26, Scope: plan.ScopeFrom,
		Name: "Seguro", Amount: 368, Day: 18, MatchText: "SEGURO DE FRAUDE"})
	// The paycheck of the 16th, with no text: the owner said which line it was.
	quincena := f.save(t, uuid.New(), plan.ItemInput{Kind: plan.KindIncome, Month: nov26, Scope: plan.ScopeFrom,
		Name: "Quincena", Amount: 65000, Day: 16})
	f.says(t, quincena.ID, nov26, nov16, plan.DecisionAssign)

	got := f.suggestions(t, f.user)

	wantLines(t, "income", got.Income, suggestionLine{"ach cre empresa x s.a.#2", "Ach Cre Empresa X S.A.", 65000, 30})
	wantLines(t, "fixed", got.Fixed,
		suggestionLine{"persona uno", plan.PersonName, 3267, 5},
		suggestionLine{"proteccion robo", "Proteccion Robo", 263, 4})
}

func suggestedKey(views []suggestionView, key string) *suggestionView {
	for i := range views {
		if views[i].Key == key {
			return &views[i]
		}
	}
	return nil
}

func TestADismissedSuggestionStaysDismissedUntilUndone(t *testing.T) {
	f := newFixture(t)
	f.history(t)
	undo := "/dismissals?" + url.Values{"kind": {"fixed"}, "key": {"persona uno"}}.Encode()
	if suggestedKey(f.suggestions(t, f.user).Fixed, "persona uno") == nil {
		t.Fatal("the payment to a person is not suggested to begin with")
	}

	for range 2 {
		if rec := f.serve(t, f.user, "PUT", "/dismissals", `{"kind": "fixed", "key": "persona uno"}`); rec.Code != http.StatusNoContent {
			t.Fatalf("PUT /dismissals = %d %s", rec.Code, rec.Body)
		}
		got := f.suggestions(t, f.user)
		if suggestedKey(got.Fixed, "persona uno") != nil {
			t.Fatal("a dismissed suggestion is still offered")
		}
		if len(got.Fixed) != 2 || len(got.Income) != 2 {
			t.Errorf("dismissing one took others with it: %+v", got)
		}
	}
	// Turning it down as income says nothing about the payment.
	if rec := f.serve(t, f.user, "DELETE", "/dismissals?kind=income&key=persona+uno", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE another kind = %d %s", rec.Code, rec.Body)
	}
	if suggestedKey(f.suggestions(t, f.user).Fixed, "persona uno") != nil {
		t.Fatal("undoing another kind's dismissal brought the payment back")
	}

	for range 2 {
		if rec := f.serve(t, f.user, "DELETE", undo, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("DELETE %s = %d %s", undo, rec.Code, rec.Body)
		}
		if suggestedKey(f.suggestions(t, f.user).Fixed, "persona uno") == nil {
			t.Fatal("an undone dismissal still hides the suggestion")
		}
	}
}

func TestASavingsHabitIsSuggestedForItsAccount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	checking := f.account(t, f.user, "checking", "0001")
	savings := f.account(t, f.user, "savings", "0002")
	other := f.account(t, f.user, "savings", "0003")
	brokerage := f.account(t, f.user, "brokerage", "U0001")
	if err := f.db.Q().SetAccountAlias(ctx, f.user, savings, "Inversion"); err != nil {
		t.Fatal(err)
	}
	for _, m := range []time.Month{time.August, time.September, time.October} {
		f.line(t, checking, day(m, 17), -30000, store.KindTransfer, "TRANSFERENCIA A INVERSION")
		f.line(t, savings, day(m, 17), 30000, store.KindTransfer, "TRANSFERENCIA DE CORRIENTE")
		// Interest is not something the owner set aside.
		f.line(t, savings, day(m, 30), 125, store.KindInterest, "INTERESES")
	}
	// A withdrawal is taken off what was set aside.
	f.line(t, savings, day(time.September, 25), -5000, store.KindTransfer, "TRANSFERENCIA A CORRIENTE")
	// Once is not a habit.
	f.line(t, brokerage, day(time.September, 20), 95000, store.KindTransfer, "DEPOSITO")
	f.line(t, other, day(time.October, 2), 10000, store.KindTransfer, "TRANSFERENCIA DE CORRIENTE")

	got := f.suggestions(t, f.user)

	// (300 + 250 + 300) / 3 is 283.33, in whole dollars 283.
	wantLines(t, "savings", got.Savings, suggestionLine{"account:" + savings.String(), "Ahorro en Inversion", 28300, 0})
	if s := got.Savings[0]; s.Kind != plan.KindSaving || s.AccountID == nil || *s.AccountID != savings ||
		s.MonthsSeen != 3 || s.LastOn != "2026-10-17" {
		t.Errorf("the saving = %+v", s)
	}

	// Once the plan sets aside in that account, it is not suggested again.
	f.save(t, uuid.New(), goal("Fondo", savings, nov26, plan.ScopeFrom))
	wantLines(t, "savings once planned", f.suggestions(t, f.user).Savings)
}

func TestTheSuggestionsHandlerSaysWhatIsWrong(t *testing.T) {
	f := newFixture(t)
	for _, tt := range []struct {
		method, target, body string
		status               int
		code                 string
		fields               map[string]string
	}{
		{"PUT", "/dismissals", `{"kind": "purchase", "key": " "}`, http.StatusUnprocessableEntity, "validation_failed",
			map[string]string{"kind": "Elige si es un ingreso, un pago fijo o un ahorro.", "key": "Elige una de las sugerencias."}},
		{"PUT", "/dismissals", `{"kind": "fixed", "key": "` + strings.Repeat("x", 121) + `"}`,
			http.StatusUnprocessableEntity, "validation_failed", map[string]string{"key": "Elige una de las sugerencias."}},
		{"PUT", "/dismissals", `{"kind": "fixed", "key": "x", "name": "x"}`, http.StatusBadRequest, "unknown_field", nil},
		{"DELETE", "/dismissals", "", http.StatusUnprocessableEntity, "validation_failed",
			map[string]string{"kind": "Elige si es un ingreso, un pago fijo o un ahorro.", "key": "Elige una de las sugerencias."}},
	} {
		rec := f.serve(t, f.user, tt.method, tt.target, tt.body)
		e := failure(t, rec)
		if rec.Code != tt.status || e.Error.Code != tt.code {
			t.Errorf("%s %s %s = %d %s; want %d %s", tt.method, tt.target, tt.body, rec.Code, rec.Body, tt.status, tt.code)
			continue
		}
		if tt.fields != nil && !mapsEqual(e.Error.Fields, tt.fields) {
			t.Errorf("%s %s %s fields = %v, want %v", tt.method, tt.target, tt.body, e.Error.Fields, tt.fields)
		}
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestSuggestionsAndDismissalsAreTheOwnersOwn(t *testing.T) {
	f := newFixture(t)
	f.history(t)
	other := newUser(t, f.db, "b@example.com")
	before := f.suggestions(t, f.user)

	if rec := f.serve(t, other, "PUT", "/dismissals", `{"kind": "fixed", "key": "persona uno"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("B: PUT /dismissals = %d %s", rec.Code, rec.Body)
	}

	if theirs := f.suggestions(t, other); len(theirs.Income)+len(theirs.Fixed)+len(theirs.Savings) != 0 {
		t.Errorf("B is suggested A's history: %+v", theirs)
	}
	if after := f.suggestions(t, f.user); !slices.Equal(linesOf(after.Fixed), linesOf(before.Fixed)) {
		t.Errorf("B's dismissal changed A's suggestions: %+v, was %+v", after.Fixed, before.Fixed)
	}
}
