package plan_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// savingLine is month m's line of a saving.
func savingLine(t *testing.T, p plan.Plan, id uuid.UUID) plan.Line {
	t.Helper()
	for _, l := range p.Savings {
		if l.Item.ID == id {
			return l
		}
	}
	t.Fatalf("%s has no line for %s: %+v", p.Month, id, p.Savings)
	return plan.Line{}
}

func wantSome(t *testing.T, what string, got *money.Cents, want money.Cents) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, want %s", what, got, want)
	}
}

func TestSavingsProgressIsWhatTheLinkedAccountHolds(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	q := f.db.Q()

	// A savings account its statement opened at $500.00, before its first
	// line.
	savings := f.account(t, f.user, "savings", "0001")
	first := f.line(t, savings, on(oct26, 1), 100000, store.KindTransfer, "TRASPASO DE CORRIENTE")
	f.line(t, savings, on(nov26, 3), 15000, store.KindTransfer, "TRASPASO DE CORRIENTE")
	f.line(t, savings, on(oct26, 31), 120, store.KindIncome, "INTERESES")
	if err := q.CreateCheckpoint(ctx, store.Checkpoint{ID: uuid.New(), UserID: f.user, AccountID: savings,
		AsOf: on(oct26, 1), BeforeEntryID: &first, Balance: 50000, Source: store.SourceStatementOpening}); err != nil {
		t.Fatal(err)
	}
	// A brokerage account: the cash the broker stated, and the positions on
	// top of it, the total Cuentas shows.
	brokerage := f.account(t, f.user, "brokerage", "U0001")
	f.line(t, brokerage, on(nov26, 4), 20000, store.KindTransfer, "DEPOSITO")
	f.line(t, brokerage, on(nov26, 5), -15000, store.KindTrade, "COMPRA DE VALORES")
	if err := q.CreateCheckpoint(ctx, store.Checkpoint{ID: uuid.New(), UserID: f.user, AccountID: brokerage,
		AsOf: on(nov26, 5), Balance: 7000, Source: store.SourceBroker}); err != nil {
		t.Fatal(err)
	}
	value := money.Cents(30000)
	if err := q.ReplacePositions(ctx, f.user, brokerage, []store.Position{{Symbol: "VT", Currency: "USD",
		AssetClass: "STK", Quantity: 3, MarketValue: &value, AsOf: on(nov26, 5)}}); err != nil {
		t.Fatal(err)
	}
	// One no stated balance anchors yet: Cuentas shows the sum of its lines.
	unanchored := f.account(t, f.user, "savings", "0002")
	f.line(t, unanchored, on(nov26, 2), 1000, store.KindTransfer, "TRASPASO DE CORRIENTE")

	fund, invest, trip := uuid.New(), uuid.New(), uuid.New()
	target := money.Cents(300001)
	in := goal("Fondo de emergencia", savings, nov26, plan.ScopeFrom)
	in.Target, in.TargetMonth = &target, mar27
	f.save(t, fund, in)
	f.save(t, invest, goal("Inversión", brokerage, nov26, plan.ScopeFrom))
	f.save(t, trip, goal("Viaje", unanchored, nov26, plan.ScopeFrom))

	list, err := accounts.NewService(f.db).List(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	shown := map[uuid.UUID]money.Cents{}
	for _, a := range list {
		shown[a.ID] = a.Balance
		if a.Type == "brokerage" {
			shown[a.ID] += a.Holdings
		}
	}
	p := f.month(t, nov26)
	for _, c := range []struct {
		what    string
		id      uuid.UUID
		account uuid.UUID
		want    money.Cents
	}{
		{"the savings account", fund, savings, 165120},
		{"the brokerage account", invest, brokerage, 37000},
		{"the account with no stated balance", trip, unanchored, 1000},
	} {
		if shown[c.account] != c.want {
			t.Fatalf("Cuentas shows %s for %s; the fixture is not what this test assumes", shown[c.account], c.what)
		}
		wantSome(t, "the progress of the goal in "+c.what, savingLine(t, p, c.id).Progress, c.want)
	}
	// $3,000.01 by March, with $1,651.20 there: November to March, five
	// months, $269.77 each, rounded up.
	wantSome(t, "what each month needs", savingLine(t, p, fund).NeededPerMonth, 26977)
	if l := savingLine(t, p, invest); l.NeededPerMonth != nil {
		t.Errorf("a goal with no target needs %s a month", *l.NeededPerMonth)
	}
	// Today's balance, in any month read: a month ahead shows the same.
	wantSome(t, "the progress read in December", savingLine(t, f.month(t, dec26), fund).Progress, 165120)

	// Reached, it needs nothing more.
	reached := money.Cents(100000)
	in.Target = &reached
	f.save(t, fund, in)
	if l := savingLine(t, f.month(t, nov26), fund); l.NeededPerMonth != nil {
		t.Errorf("a goal already reached needs %s a month", *l.NeededPerMonth)
	}
}

func TestThisMonthsSavingIsWhatReachedTheAccount(t *testing.T) {
	f := newFixture(t)
	savings := f.account(t, f.user, "savings", "0001")
	empty := f.account(t, f.user, "savings", "0002")
	fund, other, loose := uuid.New(), uuid.New(), uuid.New()
	f.save(t, fund, goal("Fondo de emergencia", savings, nov26, plan.ScopeFrom))
	f.save(t, other, goal("Viaje", empty, nov26, plan.ScopeFrom))
	f.save(t, loose, plan.ItemInput{Kind: plan.KindSaving, Month: nov26, Scope: plan.ScopeFrom, Name: "Alcancía", Amount: 5000})

	// A transfer in counts, interest does not, and taking money out takes it
	// back: $150.00 − $30.00 of the $150.00 planned.
	f.line(t, savings, on(nov26, 3), 15000, store.KindTransfer, "TRASPASO DE CORRIENTE")
	f.line(t, savings, on(nov26, 4), 500, store.KindIncome, "INTERESES")
	f.line(t, savings, on(nov26, 5), -3000, store.KindTransfer, "TRASPASO A CORRIENTE")
	// October's transfer is October's.
	f.line(t, savings, on(oct26, 30), 9000, store.KindTransfer, "TRASPASO DE CORRIENTE")

	p := f.month(t, nov26)
	l := savingLine(t, p, fund)
	wantSome(t, "what reached the account this month", l.SavedMonth, 12000)
	if l.Status != plan.StatusPartial {
		t.Errorf("$120.00 of $150.00 is %s", l.Status)
	}
	if l := savingLine(t, p, other); l.Status != plan.StatusPending || l.SavedMonth == nil || *l.SavedMonth != 0 {
		t.Errorf("nothing yet, with the month still going = %+v", l)
	}
	if l := savingLine(t, p, loose); l.Status != plan.StatusUnlinked || l.SavedMonth != nil || l.Progress != nil {
		t.Errorf("tied to no account = %+v", l)
	}
	// Whatever reached it, the saving is set aside in full and is never
	// spending.
	wantCents(t, "what the month sets aside", p.Summary.Savings, 15000+15000+5000)
	wantCents(t, "the spending", p.Spending, 0)

	f.line(t, savings, on(nov26, 20), 3000, store.KindTransfer, "TRASPASO DE CORRIENTE")
	if l := savingLine(t, f.month(t, nov26), fund); l.Status != plan.StatusDone {
		t.Errorf("$150.00 of $150.00 is %s", l.Status)
	}

	// Once the month is over, one where nothing reached the account missed.
	f.now(morning(dec26, 10))
	if l := savingLine(t, f.month(t, nov26), other); l.Status != plan.StatusMissed {
		t.Errorf("a month that passed with nothing set aside is %s", l.Status)
	}
	if l := savingLine(t, f.month(t, dec26), fund); l.Status != plan.StatusPending || *l.SavedMonth != 0 {
		t.Errorf("December, with nothing yet = %+v", l)
	}
}
