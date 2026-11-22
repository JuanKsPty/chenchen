package plan_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// A November with 30 days, read on the 5th: 26 days left, today included.
var (
	novProjection = plan.Month{Year: 2026, Month: time.November}
	nov5          = civil.Date{Year: 2026, Month: time.November, Day: 5}
)

func projLine(kind, name string, amount money.Cents, day int, status plan.Status) plan.Line {
	item := &store.PlanItem{ID: uuid.New(), Kind: kind, Name: name}
	return plan.Line{Instance: plan.Instance{Item: item, Month: novProjection,
		Version: plan.Version{Values: plan.Values{Amount: amount, Day: day}}}, Status: status}
}

func projPlan() plan.Plan {
	return plan.Plan{Month: novProjection, Today: nov5, State: plan.StateCurrent}
}

func cash(balance money.Cents) plan.Cash {
	return plan.Cash{Balance: balance, InCash: map[uuid.UUID]bool{}, Card: map[uuid.UUID]bool{},
		PaidFrom: map[uuid.UUID]uuid.UUID{}}
}

func novDay(n int) civil.Date { return civil.Date{Year: 2026, Month: time.November, Day: n} }

func balanceOn(t *testing.T, p plan.Projection, d civil.Date) money.Cents {
	t.Helper()
	for _, x := range p.Days {
		if x.Day == d {
			return x.Balance
		}
	}
	t.Fatalf("no day %s in the projection", d)
	return 0
}

func TestProjectionGoesDownForAPaymentAndUpForThePaycheck(t *testing.T) {
	p := projPlan()
	p.Fixed = []plan.Line{projLine(plan.KindFixed, "Alquiler", 95000, 10, plan.StatusUpcoming)}
	p.Income.Items = []plan.Line{projLine(plan.KindIncome, "Planilla", 180000, 15, plan.StatusUpcoming)}

	got := plan.Project(p, cash(50000))
	if got.Start != 50000 || len(got.Days) != 26 || got.Days[0].Day != nov5 || got.Days[25].Day != novDay(30) {
		t.Fatalf("start %s, %d days from %s, want 500.00 and 26 days from the 5th", got.Start, len(got.Days), got.Days[0].Day)
	}
	// Short from the 10th until the paycheck on the 15th.
	if got.Lowest.Day != novDay(10) || got.Lowest.Balance != -45000 {
		t.Errorf("lowest = %s on %s, want −450.00 on the 10th", got.Lowest.Balance, got.Lowest.Day)
	}
	if b := balanceOn(t, got, novDay(14)); b != -45000 {
		t.Errorf("the 14th = %s, want −450.00", b)
	}
	if got.End != 135000 {
		t.Errorf("end = %s, want 1,350.00", got.End)
	}
}

func TestProjectionSpendsWhatIsFreeEachDay(t *testing.T) {
	p := projPlan()
	perDay := money.Cents(1000)
	p.Summary.PerDay = &perDay

	got := plan.Project(p, cash(100000))
	// 26 days of $10.00, today's included.
	if got.End != 100000-26*1000 || got.FreePerDay != 1000 {
		t.Errorf("end = %s spending %s a day, want 740.00 spending 10.00", got.End, got.FreePerDay)
	}
	if b := balanceOn(t, got, nov5); b != 99000 {
		t.Errorf("today = %s, want 990.00: today's share is spent today", b)
	}

	// With no plan there is nothing free to spread and nothing else to expect.
	if got := plan.Project(projPlan(), cash(100000)); got.End != 100000 || len(got.Events) != 0 {
		t.Errorf("no plan: end %s with %d events, want 1,000.00 and none", got.End, len(got.Events))
	}
}

func TestProjectionLeavesOutWhatIsDoneOrPaidElsewhere(t *testing.T) {
	p := projPlan()
	card := uuid.New()
	onCard := projLine(plan.KindFixed, "Netflix", 1500, 12, plan.StatusUpcoming)
	marked := projLine(plan.KindFixed, "Luz", 6000, 20, plan.StatusUpcoming)
	marked.MarkedPaid = uuid.New()
	p.Fixed = []plan.Line{
		projLine(plan.KindFixed, "Seguro", 4000, 3, plan.StatusOut),
		onCard, marked,
		// Late: its day passed, it has not gone out, so it goes out today.
		projLine(plan.KindFixed, "Internet", 3000, 2, plan.StatusLate),
	}
	p.Income.Items = []plan.Line{projLine(plan.KindIncome, "Planilla", 180000, 1, plan.StatusDone)}
	c := cash(100000)
	c.Card[card] = true
	c.PaidFrom[onCard.Item.ID] = card

	got := plan.Project(p, c)
	if len(got.Events) != 1 || got.Events[0].Name != "Internet" || got.Events[0].Day != nov5 || got.Events[0].Amount != -3000 {
		t.Fatalf("events = %+v, want only Internet, −30.00 today", got.Events)
	}
	if got.End != 97000 {
		t.Errorf("end = %s, want 970.00", got.End)
	}
}

func TestProjectionPaysWhatIsLeftOfACardStatement(t *testing.T) {
	p := projPlan()
	minimum := money.Cents(2500)
	owing, paid, next := uuid.New(), uuid.New(), uuid.New()
	p.Cards = []plan.CardLine{
		{PlanCard: store.PlanCard{AccountID: owing, DueOn: novDay(18), Statement: -65000, PaidSince: 15000, MinimumPayment: &minimum}},
		{PlanCard: store.PlanCard{AccountID: paid, DueOn: novDay(20), Statement: -30000, PaidSince: 30000}},
		// Due next month: not this month's.
		{PlanCard: store.PlanCard{AccountID: next, DueOn: civil.Date{Year: 2026, Month: time.December, Day: 3}, Statement: -10000}},
	}
	got := plan.Project(p, cash(100000))
	if len(got.Events) != 1 || got.Events[0].Kind != plan.EventCard || *got.Events[0].AccountID != owing ||
		got.Events[0].Amount != -50000 || got.Events[0].Day != novDay(18) {
		t.Fatalf("events = %+v, want the first card's 500.00 left, on the 18th", got.Events)
	}
	if got.End != 50000 {
		t.Errorf("end = %s, want 500.00", got.End)
	}
}

func TestProjectionMovesSavingsOnlyOutOfTheBankAccounts(t *testing.T) {
	p := projPlan()
	broker, bank := uuid.New(), uuid.New()
	toBroker := projLine(plan.KindSaving, "Inversión", 20000, 16, plan.StatusPending)
	toBroker.Item.AccountID = &broker
	saved := money.Cents(5000)
	toBroker.SavedMonth = &saved
	toBank := projLine(plan.KindSaving, "Fondo", 30000, 0, plan.StatusPending)
	toBank.Item.AccountID = &bank
	p.Savings = []plan.Line{toBroker, toBank}
	c := cash(100000)
	c.InCash[bank] = true
	c.Pending = -1200

	got := plan.Project(p, c)
	if got.End != 100000-1200-15000 {
		t.Errorf("end = %s, want 838.00: the noted purchases and what is left of the broker's saving", got.End)
	}
	if b := balanceOn(t, got, nov5); b != 98800 {
		t.Errorf("today = %s, want 988.00: noted purchases leave today", b)
	}
}
