package plan_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/reports"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// novNow is the moment the fixture stands at: the 6th of November 2026, in
// Panama.
var novNow = time.Date(2026, time.November, 6, 9, 0, 0, 0, civil.Panama)

func on(m plan.Month, day int) civil.Date { return civil.Date{Year: m.Year, Month: m.Month, Day: day} }

func (f fixture) now(at time.Time) { f.svc.SetNow(func() time.Time { return at }) }

// line is a line a statement printed in one of the owner's accounts, as an
// import stores it.
func (f fixture) line(t *testing.T, account uuid.UUID, day civil.Date, amount money.Cents, kind, text string) uuid.UUID {
	t.Helper()
	return f.filed(t, account, day, amount, kind, text, nil)
}

// filed is a line filed under category.
func (f fixture) filed(t *testing.T, account uuid.UUID, day civil.Date, amount money.Cents, kind, text string, category *uuid.UUID) uuid.UUID {
	t.Helper()
	e := store.Entry{ID: uuid.New(), UserID: f.user, AccountID: account, BookedOn: day, Amount: amount, Kind: kind,
		Description: text, CategoryID: category, DedupKey: uuid.NewString()}
	if ok, err := f.db.Q().InsertEntryIfNew(context.Background(), e); err != nil || !ok {
		t.Fatalf("InsertEntryIfNew = %v, %v", ok, err)
	}
	return e.ID
}

// noted is a purchase of amount the owner noted on day, that no line could be
// yet: it counts while it is pending.
func (f fixture) noted(t *testing.T, account uuid.UUID, day civil.Date, amount money.Cents, merchant string) uuid.UUID {
	t.Helper()
	c := store.Capture{ID: uuid.New(), UserID: f.user, AccountID: account, Source: store.CaptureSourceManual,
		BookedOn: day, Amount: -amount, Merchant: merchant}
	if err := f.db.Q().InsertCapture(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func (f fixture) child(t *testing.T, parent uuid.UUID, name string) uuid.UUID {
	t.Helper()
	c, err := f.db.Q().CreateCategory(context.Background(),
		store.Category{ID: uuid.New(), UserID: f.user, ParentID: &parent, Name: name, Kind: "expense"})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func (f fixture) capOn(t *testing.T, category uuid.UUID, amount money.Cents) {
	t.Helper()
	in := plan.CapInput{Month: nov26, Scope: plan.ScopeFrom, Amount: amount}
	if err := f.svc.SaveCap(context.Background(), f.user, category, in); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) month(t *testing.T, m plan.Month) plan.Plan {
	t.Helper()
	p, err := f.svc.Month(context.Background(), f.user, m)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// paycheck is income of amount on day, from m on, that arrives as a payroll
// deposit.
func paycheck(m plan.Month, amount money.Cents, day int) plan.ItemInput {
	return plan.ItemInput{Kind: plan.KindIncome, Month: m, Scope: plan.ScopeFrom, Name: "Sueldo",
		Amount: amount, Day: day, MatchText: "ACH EMPRESA X"}
}

func named(in plan.ItemInput, name, matchText string) plan.ItemInput {
	in.Name, in.MatchText = name, matchText
	return in
}

func wantCents(t *testing.T, what string, got, want money.Cents) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %s, want %s", what, got, want)
	}
}

func TestThePlanSpendsExactlyWhatGastosSpends(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	checking := f.account(t, f.user, "checking", "0001")
	card := f.account(t, f.user, "credit_card", "**** 1111")
	brokerage := f.account(t, f.user, "brokerage", "U0001")
	fathers, err := f.db.Q().CreateHandCard(ctx, f.user, uuid.New(), "Tarjeta de papá", "4321")
	if err != nil {
		t.Fatal(err)
	}
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	f.save(t, uuid.New(), payment(nov26, 3267, 5))

	f.line(t, checking, on(nov26, 5), -3267, store.KindExpense, "YAPPY A PERSONA UNO")
	f.line(t, card, on(nov26, 3), -2500, store.KindExpense, "SUPER X")
	f.line(t, card, on(nov26, 4), 500, store.KindRefund, "SUPER X DEVOLUCION")
	f.line(t, checking, on(nov26, 7), -150, store.KindFee, "COMISION")
	f.noted(t, card, on(nov26, 6), 1200, "Café")
	f.noted(t, fathers.ID, on(nov26, 2), 4000, "Farmacia")
	f.line(t, checking, on(nov26, 14), 65000, store.KindIncome, "ACH EMPRESA X")
	// Paying the card and buying shares are neither spending nor income.
	f.line(t, checking, on(nov26, 8), -20000, store.KindTransfer, "PAGO A TARJETA")
	f.line(t, card, on(nov26, 8), 20000, store.KindTransfer, "SU PAGO")
	f.line(t, brokerage, on(nov26, 9), -100000, store.KindTrade, "COMPRA DE VALORES")
	// Nor are the months either side November's.
	f.line(t, card, on(oct26, 31), -1000, store.KindExpense, "SUPER X")
	f.line(t, card, on(dec26, 1), -1000, store.KindExpense, "SUPER X")

	p := f.month(t, nov26)
	totals, err := reports.NewService(f.db).Spending(ctx, f.user, nov26.First(), nov26.Last(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var gastos money.Cents
	for _, c := range totals {
		gastos += c.Total
	}
	if gastos != 10617 {
		t.Fatalf("Gastos adds up %s; the fixture is not what this test assumes", gastos)
	}
	wantCents(t, "the plan's spending", p.Spending, gastos)
	wantCents(t, "what went out of what is free and what the payment took", p.Summary.Spent+p.Charged, gastos)
	wantCents(t, "what the payment took", p.Charged, 3267)
	wantCents(t, "what went out of what is free", p.Summary.Spent, 7350)

	flow, err := reports.NewService(f.db).Flow(ctx, f.user, nov26.First(), nov26.Last(), true)
	if err != nil || len(flow) != 1 {
		t.Fatalf("Flow = %v, %v", flow, err)
	}
	wantCents(t, "the income that came in", p.Income.Actual, flow[0].In)
	wantCents(t, "the month's flow out", flow[0].Out, gastos)
}

func TestEveryOutflowOfTheMonthLandsInExactlyOneBucket(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	card := f.account(t, f.user, "credit_card", "**** 1111")
	food := f.category(t, f.user, "Comida", "expense")
	groceries := f.child(t, food, "Súper")
	health := f.category(t, f.user, "Salud", "expense")
	f.capOn(t, food, 30000)
	f.capOn(t, health, 10000)
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	f.save(t, uuid.New(), payment(nov26, 3267, 5))
	f.save(t, uuid.New(), named(payment(nov26, 368, 18), "Seguro", "SEGURO X"))

	amounts := map[uuid.UUID]money.Cents{}
	out := func(account uuid.UUID, day int, amount money.Cents, kind, text string, category *uuid.UUID) {
		amounts[f.filed(t, account, on(nov26, day), amount, kind, text, category)] = amount
	}
	out(checking, 5, -3267, store.KindExpense, "YAPPY A PERSONA UNO", nil)
	out(card, 18, -350, store.KindExpense, "SEGURO X", nil)
	out(card, 18, -18, store.KindFee, "ITBMS", nil)
	out(card, 2, -4500, store.KindExpense, "SUPER X", &groceries)
	out(card, 20, -2000, store.KindExpense, "RESTAURANTE", &food)
	out(card, 21, 700, store.KindRefund, "SUPER X DEVOLUCION", &groceries)
	out(checking, 9, -1500, store.KindExpense, "FARMACIA", &health)
	out(checking, 16, -150, store.KindFee, "COMISION", nil)
	amounts[f.noted(t, card, on(nov26, 6), 999, "Kiosco")] = -999
	// December's payment is December's, even a day after November ends.
	f.line(t, checking, on(dec26, 1), -3267, store.KindExpense, "YAPPY A PERSONA UNO")
	f.line(t, card, on(oct26, 31), -1000, store.KindExpense, "SUPER X")

	p := f.month(t, nov26)
	charged := map[uuid.UUID]int{}
	for _, l := range p.Fixed {
		for _, mv := range l.Movements {
			charged[mv.ID]++
		}
	}
	var spending, free, paid money.Cents
	for id, amount := range amounts {
		spending -= amount
		switch charged[id] {
		case 0:
			free -= amount
		case 1:
			paid -= amount
		default:
			t.Errorf("%s of %s paid %d payments", id, amount, charged[id])
		}
	}
	if len(charged) != 3 {
		t.Errorf("the payments took %d movements, want the two payments and the tax beside one", len(charged))
	}
	wantCents(t, "the spending", p.Spending, spending)
	wantCents(t, "what went out of what is free", p.Summary.Spent, free)
	wantCents(t, "what the payments took", p.Charged, paid)
	wantCents(t, "what went out of what is free, by quincena", p.Quincenas[0].Spent+p.Quincenas[1].Spent, free)
	// Every category spent in has a cap, so the caps and what has no category
	// hold what is free between them, each movement once.
	if len(p.Caps) != 2 {
		t.Fatalf("caps = %+v", p.Caps)
	}
	wantCents(t, "the caps and the uncategorized", p.Caps[0].Spent+p.Caps[1].Spent+p.Uncategorized.Spent, free)
	wantCents(t, "what has no category", p.Uncategorized.Spent, 1149)
	if p.Uncategorized.Count != 2 {
		t.Errorf("uncategorized count = %d, want the fee and the noted purchase", p.Uncategorized.Count)
	}
}

func TestAFixedPaymentThatWentOutIsNotCountedTwice(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	f.save(t, uuid.New(), payment(nov26, 3267, 5))
	before := f.month(t, nov26).Summary

	f.line(t, checking, on(nov26, 5), -3267, store.KindExpense, "YAPPY A PERSONA UNO")
	f.line(t, checking, on(nov26, 5), -1000, store.KindExpense, "SUPER X")
	p := f.month(t, nov26)
	if len(p.Fixed) != 1 || p.Fixed[0].Status != plan.StatusOut || p.Fixed[0].Paid == nil || len(p.Fixed[0].Movements) != 1 {
		t.Fatalf("the payment = %+v", p.Fixed)
	}
	s := p.Summary
	wantCents(t, "the payments", s.Fixed, 3267)
	wantCents(t, "what went out of what is free", s.Spent, 1000)
	wantCents(t, "what is free", s.Free, 96733)
	wantCents(t, "what is left", s.Left, 95733)
	// The payment was set aside before it went out, so it going out lowers
	// what is left by nothing: only the purchase does.
	wantCents(t, "what is left, against before", before.Left-s.Left, 1000)
}

func TestAFixedPaymentPaidAtADifferentAmountCostsWhatWasPaid(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	f.save(t, uuid.New(), payment(nov26, 3267, 5))
	f.save(t, uuid.New(), named(payment(nov26, 3000, 3), "Seguro", "SEGURO X"))
	f.line(t, checking, on(nov26, 5), -3600, store.KindExpense, "YAPPY A PERSONA UNO")
	f.line(t, checking, on(nov26, 3), -2400, store.KindExpense, "SEGURO X")

	p := f.month(t, nov26)
	for i, want := range []money.Cents{2400, 3600} {
		if l := p.Fixed[i]; l.Paid == nil || *l.Paid != want || l.Committed != want || l.Status != plan.StatusOut {
			t.Errorf("%s = %+v, want it paid at %s", l.Item.Name, l, want)
		}
	}
	wantCents(t, "the payments", p.Summary.Fixed, 6000)
	wantCents(t, "what is free", p.Summary.Free, 94000)
	wantCents(t, "what went out of what is free", p.Summary.Spent, 0)
}

func TestAnUnpaidFixedPaymentKeepsItsMoneyAside(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	f.save(t, uuid.New(), payment(nov26, 3267, 5))
	insurance := uuid.New()
	f.save(t, insurance, named(payment(nov26, 3000, 8), "Seguro", "SEGURO X"))
	f.save(t, uuid.New(), named(payment(nov26, 5000, 20), "Luz", "LUZ X"))
	// A movement with the payment's text but nowhere near its amount is not
	// it: it is spending, and the payment keeps what it set aside until it
	// is seen.
	f.line(t, checking, on(nov26, 5), -10000, store.KindExpense, "YAPPY A PERSONA UNO")

	p := f.month(t, nov26)
	for i, want := range []plan.Status{plan.StatusLate, plan.StatusUpcoming, plan.StatusUpcoming} {
		if l := p.Fixed[i]; l.Status != want || l.Paid != nil || l.Committed != l.Planned() || len(l.Movements) != 0 {
			t.Errorf("%s = %s, %+v; want %s and its money aside", l.Item.Name, l.Status, l, want)
		}
	}
	wantCents(t, "the payments", p.Summary.Fixed, 11267)
	wantCents(t, "what went out of what is free", p.Summary.Spent, 10000)
	wantCents(t, "what is left", p.Summary.Left, 100000-11267-10000)
	// The insurance goes out on the 8th, two days from today.
	due := on(nov26, 8)
	if len(p.Alerts) != 1 || p.Alerts[0].Kind != plan.AlertPayment || p.Alerts[0].Level != plan.LevelSoon ||
		*p.Alerts[0].ItemID != insurance || p.Alerts[0].DueOn != due {
		t.Errorf("alerts = %+v, want the insurance going out soon", p.Alerts)
	}
}

func TestARefundAloneDoesNotSettleAPayment(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	f.save(t, uuid.New(), named(payment(nov26, 3000, 3), "Seguro", "SEGURO X"))
	f.line(t, checking, on(nov26, 4), 3000, store.KindRefund, "SEGURO X")

	p := f.month(t, nov26)
	if l := p.Fixed[0]; l.Status != plan.StatusLate || l.Paid != nil || l.Committed != 3000 || len(l.Movements) != 0 {
		t.Errorf("the insurance = %+v, want it still to go out", l)
	}
	// The refund gives back to what is free.
	wantCents(t, "what went out of what is free", p.Summary.Spent, -3000)
	wantCents(t, "what is left", p.Summary.Left, 100000-3000+3000)
}

func TestATaxLineGoesWithTheChargeItCameWith(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), named(payment(nov26, 368, 4), "Seguro", "SEGURO X"))
	charge := f.line(t, card, on(nov26, 4), -350, store.KindExpense, "SEGURO X")
	tax := f.line(t, card, on(nov26, 4), -18, store.KindFee, "ITBMS")
	// The same tax on another account, or another day, is not this one's,
	// and neither is a charge too big to be a tax on it.
	f.line(t, checking, on(nov26, 4), -18, store.KindFee, "ITBMS")
	f.line(t, card, on(nov26, 5), -18, store.KindFee, "ITBMS")
	f.line(t, card, on(nov26, 4), -200, store.KindFee, "COMISION")

	p := f.month(t, nov26)
	l := p.Fixed[0]
	if l.Status != plan.StatusOut || l.Paid == nil || *l.Paid != 368 || len(l.Movements) != 2 ||
		l.Movements[0].ID != charge && l.Movements[1].ID != charge || l.Movements[0].ID != tax && l.Movements[1].ID != tax {
		t.Errorf("the insurance = %+v, want it paid by the charge and its tax", l)
	}
	wantCents(t, "what went out of what is free", p.Summary.Spent, 236)
}

func TestTwoPaychecksWithTheSameTextGoToTheNearestExpectedDay(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), named(paycheck(nov26, 65000, 15), "Primera quincena", "ACH EMPRESA X"))
	f.save(t, uuid.New(), named(paycheck(nov26, 65000, 30), "Segunda quincena", "ACH EMPRESA X"))
	// A week from the 30th, the 23rd is the second; a week from the 15th,
	// the 22nd is the first. The one written first changes nothing.
	second := f.line(t, checking, on(nov26, 23), 65000, store.KindIncome, "ACH EMPRESA X PLANILLA")
	first := f.line(t, checking, on(nov26, 22), 65000, store.KindIncome, "ACH EMPRESA X PLANILLA")

	p := f.month(t, nov26)
	for i, want := range []uuid.UUID{first, second} {
		l := p.Income.Items[i]
		if l.Status != plan.StatusDone || len(l.Movements) != 1 || l.Movements[0].ID != want {
			t.Errorf("%s = %+v", l.Item.Name, l)
		}
	}
	wantCents(t, "the income that came in", p.Income.Actual, 130000)
	wantCents(t, "the income no paycheck took", p.Income.Unplanned, 0)
}

func TestALatePaycheckBelongsToTheMonthItWasExpectedIn(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.now(time.Date(2026, time.October, 10, 9, 0, 0, 0, civil.Panama))
	f.save(t, uuid.New(), paycheck(oct26, 65000, 30))

	// Until a week after the 30th it is late, then missed.
	for _, tt := range []struct {
		day  int
		want plan.Status
	}{{3, plan.StatusLate}, {6, plan.StatusLate}, {7, plan.StatusMissed}} {
		f.now(time.Date(2026, time.November, tt.day, 9, 0, 0, 0, civil.Panama))
		if got := f.month(t, oct26).Income.Items[0].Status; got != tt.want {
			t.Errorf("on the %d of November, October's paycheck is %s, want %s", tt.day, got, tt.want)
		}
	}

	f.now(novNow)
	paid := f.line(t, checking, on(nov26, 2), 65000, store.KindIncome, "ACH EMPRESA X")
	oct := f.month(t, oct26)
	if l := oct.Income.Items[0]; l.Status != plan.StatusDone || len(l.Movements) != 1 || l.Movements[0].ID != paid {
		t.Errorf("October's paycheck = %+v, want the one on the 2nd of November", l)
	}
	wantCents(t, "October's income that came in", oct.Income.Actual, 0)

	nov := f.month(t, nov26)
	if l := nov.Income.Items[0]; l.Status != plan.StatusUpcoming || len(l.Movements) != 0 {
		t.Errorf("November's paycheck = %+v, want it still to come", l)
	}
	// It came in in November, but it was October's paycheck: November did not
	// get anything it did not plan.
	wantCents(t, "November's income that came in", nov.Income.Actual, 65000)
	wantCents(t, "November's income no paycheck took", nov.Income.Unplanned, 0)
}

func TestExtraIncomeNeverChangesWhatIsFree(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 65000, 15))
	f.save(t, uuid.New(), payment(nov26, 3267, 5))
	f.line(t, checking, on(nov26, 14), 65000, store.KindIncome, "ACH EMPRESA X")
	before := f.month(t, nov26)

	f.line(t, checking, on(nov26, 10), 30000, store.KindIncome, "TRANSFERENCIA DE PERSONA DOS")
	// Ten days from the 15th: no paycheck is due near it.
	f.line(t, checking, on(nov26, 25), 65000, store.KindIncome, "ACH EMPRESA X")
	after := f.month(t, nov26)

	wantCents(t, "what is free", after.Summary.Free, before.Summary.Free)
	wantCents(t, "what is left", after.Summary.Left, before.Summary.Left)
	wantCents(t, "the income planned", after.Summary.Income, 65000)
	wantCents(t, "the income that came in", after.Income.Actual, 160000)
	wantCents(t, "the income no paycheck took", after.Income.Unplanned, 95000)
}

func TestSavingsAreSetAsideAndNeverCountAsSpending(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	savings := f.account(t, f.user, "savings", "0002")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	f.save(t, uuid.New(), goal("Fondo de emergencia", savings, nov26, plan.ScopeFrom))
	f.line(t, checking, on(nov26, 3), -15000, store.KindTransfer, "TRASPASO A AHORRO")
	f.line(t, savings, on(nov26, 3), 15000, store.KindTransfer, "TRASPASO DE CORRIENTE")

	p := f.month(t, nov26)
	s := p.Summary
	wantCents(t, "what is set aside", s.Savings, 15000)
	wantCents(t, "what is free", s.Free, 85000)
	wantCents(t, "what went out of what is free", s.Spent, 0)
	wantCents(t, "the spending", p.Spending, 0)
	wantCents(t, "what is left", s.Left, 85000)
	v := plan.NewView(p)
	if len(v.Savings) != 1 || v.Savings[0].Amount.Cents != 15000 || v.Savings[0].Status != plan.StatusDone ||
		v.Savings[0].SavedMonth == nil || v.Savings[0].SavedMonth.Cents != 15000 ||
		v.Savings[0].AccountID == nil || *v.Savings[0].AccountID != savings {
		t.Errorf("savings = %+v", v.Savings)
	}
}

func TestDaysLeftCountTodayInPanama(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	f.line(t, checking, on(nov26, 2), -40000, store.KindExpense, "SUPER X")

	// Three in the morning of the 1st of December in UTC is ten at night of
	// the 30th of November in Panama: November's last day, counted.
	f.now(time.Date(2026, time.December, 1, 3, 0, 0, 0, time.UTC))
	rec := f.serve(t, f.user, "GET", "/", "")
	var got struct {
		Month   string `json:"month"`
		Today   string `json:"today"`
		State   string `json:"state"`
		Summary struct {
			DaysLeft int `json:"days_left"`
			PerDay   *struct {
				Cents int64 `json:"cents"`
			} `json:"per_day"`
		} `json:"summary"`
	}
	decode(t, rec, &got)
	if got.Month != "2026-11" || got.Today != "2026-11-30" || got.State != "current" || got.Summary.DaysLeft != 1 ||
		got.Summary.PerDay == nil || got.Summary.PerDay.Cents != 60000 {
		t.Errorf("GET /api/plan at 03:00 UTC of the 1st = %s", rec.Body)
	}

	for _, tt := range []struct {
		now   time.Time
		month plan.Month
		days  int
	}{
		{novNow, plan.Month{}, 25},
		{time.Date(2026, time.December, 1, 5, 0, 0, 0, time.UTC), plan.Month{}, 31},
		{novNow, oct26, 0},
		{novNow, dec26, 31},
		{novNow, feb27, 28},
	} {
		f.now(tt.now)
		if p := f.month(t, tt.month); p.Summary.DaysLeft != tt.days {
			t.Errorf("at %s, %s has %d days left, want %d", tt.now, p.Month, p.Summary.DaysLeft, tt.days)
		}
	}
}

func TestPerDayIsNullOnceTheFreeMoneyIsGone(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 10000, 1))
	for _, tt := range []struct {
		spend  money.Cents
		perDay *money.Cents
		level  plan.Level
	}{
		// A cent over 25 days is less than a cent a day, but it is left.
		{9999, new(money.Cents(0)), plan.LevelNear},
		{1, nil, plan.LevelNear},
		{100, nil, plan.LevelOver},
	} {
		f.line(t, checking, on(nov26, 2), -tt.spend, store.KindExpense, "SUPER X")
		s := f.month(t, nov26).Summary
		if (s.PerDay == nil) != (tt.perDay == nil) || s.PerDay != nil && *s.PerDay != *tt.perDay || s.Level != tt.level {
			t.Errorf("with %s left, per day = %v and the level %s; want %v and %s", s.Left, s.PerDay, s.Level, tt.perDay, tt.level)
		}
	}
}

func TestAPlanThatLeavesNothingFreeSaysSoInsteadOfAlerting(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	savings := f.account(t, f.user, "savings", "0002")
	f.save(t, uuid.New(), paycheck(nov26, 50000, 15))
	f.save(t, uuid.New(), named(payment(nov26, 40000, 25), "Alquiler", "ALQUILER"))
	f.save(t, uuid.New(), goal("Fondo", savings, nov26, plan.ScopeFrom))
	f.line(t, checking, on(nov26, 2), -2000, store.KindExpense, "SUPER X")

	p := f.month(t, nov26)
	s := p.Summary
	wantCents(t, "what is free", s.Free, -5000)
	wantCents(t, "what is left", s.Left, -7000)
	if s.Level != plan.LevelNoFree || s.PerDay != nil || len(p.Alerts) != 0 ||
		p.Quincenas[0].Free != 0 || p.Quincenas[1].Free != 0 {
		t.Errorf("a plan with nothing free = %+v, alerts %+v, quincenas %+v", s, p.Alerts, p.Quincenas)
	}
}

func TestEightyPercentIsNearAndOneCentMoreIsOver(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	food := f.category(t, f.user, "Comida", "expense")
	f.save(t, uuid.New(), paycheck(nov26, 10000, 1))
	f.capOn(t, food, 5000)
	for _, tt := range []struct {
		spend     money.Cents
		category  *uuid.UUID
		free, cap plan.Level
	}{
		{3999, &food, plan.LevelOK, plan.LevelOK},
		{1, &food, plan.LevelOK, plan.LevelNear},
		{1000, &food, plan.LevelOK, plan.LevelNear},
		{1, &food, plan.LevelOK, plan.LevelOver},
		{2999, nil, plan.LevelNear, plan.LevelOver},
		{2000, nil, plan.LevelNear, plan.LevelOver},
		{1, nil, plan.LevelOver, plan.LevelOver},
	} {
		f.filed(t, checking, on(nov26, 2), -tt.spend, store.KindExpense, "SUPER X", tt.category)
		p := f.month(t, nov26)
		if p.Summary.Level != tt.free || p.Caps[0].Level != tt.cap {
			t.Errorf("with %s of %s spent and %s of the cap, the levels are %s and %s; want %s and %s",
				p.Summary.Spent, p.Summary.Free, p.Caps[0].Spent, p.Summary.Level, p.Caps[0].Level, tt.free, tt.cap)
		}
		var want []plan.Alert
		if tt.free != plan.LevelOK {
			want = append(want, plan.Alert{Kind: plan.AlertFree, Level: tt.free})
		}
		if tt.cap != plan.LevelOK {
			want = append(want, plan.Alert{Kind: plan.AlertCap, Level: tt.cap, CategoryID: &food})
		}
		if len(p.Alerts) != len(want) {
			t.Errorf("alerts = %+v, want %+v", p.Alerts, want)
			continue
		}
		for i, a := range p.Alerts {
			if a.Kind != want[i].Kind || a.Level != want[i].Level || (a.CategoryID == nil) != (want[i].CategoryID == nil) {
				t.Errorf("alert %d = %+v, want %+v", i, a, want[i])
			}
		}
	}
}

func TestACapCountsItsChildCategories(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	food := f.category(t, f.user, "Comida", "expense")
	groceries := f.child(t, food, "Súper")
	health := f.category(t, f.user, "Salud", "expense")
	f.capOn(t, food, 10000)
	f.capOn(t, groceries, 2000)
	f.filed(t, card, on(nov26, 2), -2000, store.KindExpense, "RESTAURANTE", &food)
	f.filed(t, card, on(nov26, 3), -3000, store.KindExpense, "SUPER X", &groceries)
	f.filed(t, card, on(nov26, 4), -4000, store.KindExpense, "FARMACIA", &health)
	f.line(t, card, on(nov26, 5), -500, store.KindExpense, "KIOSCO")

	p := f.month(t, nov26)
	spent := map[uuid.UUID]money.Cents{}
	for _, c := range p.Caps {
		spent[c.CategoryID] = c.Spent
	}
	wantCents(t, "Comida, with Súper", spent[food], 5000)
	wantCents(t, "Súper alone", spent[groceries], 3000)
	wantCents(t, "what has no category", p.Uncategorized.Spent, 500)
	if p.Uncategorized.Count != 1 || !p.Planned {
		t.Errorf("uncategorized = %+v, planned %v", p.Uncategorized, p.Planned)
	}
}

func TestACapIgnoresMovementsThePlanAlreadyCounts(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	utilities := f.category(t, f.user, "Servicios", "expense")
	f.capOn(t, utilities, 5000)
	electricity := named(payment(nov26, 4000, 3), "Luz", "LUZ X")
	electricity.CategoryID = &utilities
	f.save(t, uuid.New(), electricity)
	f.filed(t, checking, on(nov26, 3), -4000, store.KindExpense, "LUZ X", &utilities)
	f.filed(t, checking, on(nov26, 4), -1000, store.KindExpense, "AGUA X", &utilities)

	p := f.month(t, nov26)
	if p.Fixed[0].Status != plan.StatusOut {
		t.Fatalf("the electricity = %+v", p.Fixed[0])
	}
	wantCents(t, "the cap", p.Caps[0].Spent, 1000)
	if p.Caps[0].Level != plan.LevelOK {
		t.Errorf("the cap's level = %s", p.Caps[0].Level)
	}
}

func TestARefundLowersItsCategorysCap(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	food := f.category(t, f.user, "Comida", "expense")
	f.capOn(t, food, 10000)
	f.filed(t, card, on(nov26, 2), -3000, store.KindExpense, "SUPER X", &food)
	f.filed(t, card, on(nov26, 3), 1000, store.KindRefund, "SUPER X DEVOLUCION", &food)

	wantCents(t, "the cap", f.month(t, nov26).Caps[0].Spent, 2000)
}

func TestQuincenasSplitTheFreeMoneyByDaysInFebruaryToo(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), named(paycheck(nov26, 65000, 15), "Primera quincena", "ACH EMPRESA X"))
	f.save(t, uuid.New(), named(paycheck(nov26, 65000, 30), "Segunda quincena", "ACH EMPRESA X"))
	f.save(t, uuid.New(), named(payment(nov26, 10000, 20), "Luz", "LUZ X"))
	f.line(t, checking, on(nov26, 3), -1000, store.KindExpense, "SUPER X")
	f.line(t, checking, on(nov26, 16), -2000, store.KindExpense, "SUPER X")

	for _, tt := range []struct {
		month plan.Month
		want  [2]plan.Quincena
	}{
		{nov26, [2]plan.Quincena{
			{From: on(nov26, 1), To: on(nov26, 15), Free: 60000, Spent: 1000, IncomeExpected: 65000},
			{From: on(nov26, 16), To: on(nov26, 30), Free: 60000, Spent: 2000, IncomeExpected: 65000, FixedDue: 10000},
		}},
		// 15 of February's 28 days: 1,200.00·15/28 is 642.857…, rounded down,
		// and the second half takes the rest. The paycheck of the 30th is
		// due on the 28th.
		{feb27, [2]plan.Quincena{
			{From: on(feb27, 1), To: on(feb27, 15), Free: 64285, IncomeExpected: 65000},
			{From: on(feb27, 16), To: on(feb27, 28), Free: 55715, IncomeExpected: 65000, FixedDue: 10000},
		}},
	} {
		if got := f.month(t, tt.month).Quincenas; got != tt.want {
			t.Errorf("%s's quincenas = %+v, want %+v", tt.month, got, tt.want)
		}
	}
}

func TestPastMonthsKeepThePlanTheyHad(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	id := uuid.New()
	f.now(time.Date(2026, time.October, 10, 9, 0, 0, 0, civil.Panama))
	f.save(t, id, paycheck(oct26, 100000, 15))
	f.line(t, checking, on(oct26, 15), 100000, store.KindIncome, "ACH EMPRESA X")
	f.now(novNow)
	f.save(t, id, paycheck(nov26, 120000, 15))

	oct := f.month(t, oct26)
	if oct.State != plan.StatePast || oct.Editable || !oct.Planned || oct.Summary.DaysLeft != 0 || oct.Summary.PerDay != nil {
		t.Errorf("October = %s, editable %v, planned %v, %+v", oct.State, oct.Editable, oct.Planned, oct.Summary)
	}
	wantCents(t, "October's income planned", oct.Summary.Income, 100000)
	if l := oct.Income.Items[0]; l.Status != plan.StatusDone || l.Version.From != oct26 || l.Version.To != oct26 {
		t.Errorf("October's paycheck = %+v", l)
	}
	nov := f.month(t, nov26)
	if nov.State != plan.StateCurrent || !nov.Editable {
		t.Errorf("November = %s, editable %v", nov.State, nov.Editable)
	}
	wantCents(t, "November's income planned", nov.Summary.Income, 120000)
}

func TestNextMonthStartsFromThisMonthsPlan(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	food := f.category(t, f.user, "Comida", "expense")
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	f.save(t, uuid.New(), payment(nov26, 3267, 5))
	f.capOn(t, food, 30000)
	f.line(t, checking, on(nov26, 5), -3267, store.KindExpense, "YAPPY A PERSONA UNO")

	dec := f.month(t, dec26)
	s := dec.Summary
	if dec.State != plan.StateFuture || !dec.Editable || !dec.Planned || s.DaysLeft != 31 || len(dec.Alerts) != 0 {
		t.Errorf("December = %s, editable %v, planned %v, %+v, alerts %+v", dec.State, dec.Editable, dec.Planned, s, dec.Alerts)
	}
	wantCents(t, "December's income", s.Income, 130000)
	// November's payment went out in November; December's is still to go.
	wantCents(t, "December's payments", s.Fixed, 3267)
	wantCents(t, "December's spending", dec.Spending, 0)
	if s.PerDay == nil || *s.PerDay != (130000-3267)/31 {
		t.Errorf("December per day = %v", s.PerDay)
	}
	if len(dec.Fixed) != 1 || dec.Fixed[0].Status != plan.StatusUpcoming || len(dec.Caps) != 1 || dec.Caps[0].Version.Amount != 30000 {
		t.Errorf("December's payments %+v and caps %+v", dec.Fixed, dec.Caps)
	}
}

func TestThePlanNeverWritesEntriesCapturesOrCheckpoints(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	checking := f.account(t, f.user, "checking", "0001")
	savings := f.account(t, f.user, "savings", "0002")
	food := f.category(t, f.user, "Comida", "expense")
	f.filed(t, checking, on(nov26, 5), -3267, store.KindExpense, "YAPPY A PERSONA UNO", &food)
	f.line(t, checking, on(nov26, 14), 65000, store.KindIncome, "ACH EMPRESA X")
	f.noted(t, checking, on(nov26, 6), 1200, "Café")
	if err := f.db.Q().CreateCheckpoint(ctx, store.Checkpoint{ID: uuid.New(), UserID: f.user, AccountID: checking,
		AsOf: on(nov26, 1), Balance: 50000, Source: "manual"}); err != nil {
		t.Fatal(err)
	}
	tables := []string{"entries", "captures", "capture_signals", "balance_checkpoints", "statements", "import_runs",
		"transfer_decisions", "categories", "category_rules", "accounts"}
	snapshot := func() map[string]string {
		out := map[string]string{}
		for _, table := range tables {
			var sum string
			if err := f.db.QueryRow(ctx, `SELECT md5(COALESCE(string_agg(t::text, '|' ORDER BY t::text), '')) FROM `+
				table+` t`).Scan(&sum); err != nil {
				t.Fatal(err)
			}
			out[table] = sum
		}
		return out
	}
	before := snapshot()

	id, saving := uuid.New(), uuid.New()
	for _, req := range []struct{ method, target, body string }{
		{"PUT", "/items/" + id.String(), `{"kind":"fixed","month":"2026-11","scope":"from","name":"Pago","amount":"32.67","day":5,"match_text":"YAPPY A PERSONA UNO","category_id":"` + food.String() + `"}`},
		{"PUT", "/items/" + uuid.NewString(), `{"kind":"income","month":"2026-11","scope":"from","name":"Sueldo","amount":"650","day":15,"match_text":"ACH EMPRESA X"}`},
		{"PUT", "/items/" + saving.String(), `{"kind":"saving","month":"2026-11","scope":"from","name":"Fondo","amount":"100","account_id":"` + savings.String() + `","target":"1000","target_month":"2027-06"}`},
		{"PUT", "/caps/" + food.String(), `{"month":"2026-11","scope":"from","amount":"300"}`},
		{"GET", "/", ""},
		{"GET", "/?month=2026-10", ""},
		{"GET", "/?month=2026-12", ""},
		{"PUT", "/items/" + id.String(), `{"kind":"fixed","month":"2026-12","scope":"only","name":"Pago","amount":"40","day":5,"match_text":"YAPPY A PERSONA UNO"}`},
		{"DELETE", "/items/" + saving.String() + "?month=2026-12&scope=from", ""},
		{"DELETE", "/caps/" + food.String() + "?month=2026-11&scope=from", ""},
	} {
		if rec := f.serve(t, f.user, req.method, req.target, req.body); rec.Code >= 300 {
			t.Fatalf("%s %s = %d %s", req.method, req.target, rec.Code, rec.Body)
		}
	}
	after := snapshot()
	for _, table := range tables {
		if before[table] != after[table] {
			t.Errorf("the plan changed %s", table)
		}
	}
}
