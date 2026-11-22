package accounts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// stated records a balance the owner read off a statement.
func (f fixture) stated(t *testing.T, accountID uuid.UUID, asOf civil.Date, balance money.Cents) {
	t.Helper()
	if _, err := f.svc.AddCheckpoint(context.Background(), f.user, accountID,
		accounts.CheckpointInput{AsOf: asOf, Balance: balance}); err != nil {
		t.Fatalf("AddCheckpoint(%s): %v", asOf, err)
	}
}

// historyFixture stands on 22 November 2026 in Panama.
func historyFixture(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t)
	f.svc.SetNow(func() time.Time { return time.Date(2026, time.November, 22, 15, 0, 0, 0, civil.Panama) })
	return f
}

type wantPoint struct {
	day     civil.Date
	total   money.Cents
	partial bool
}

func checkPoints(t *testing.T, got []accounts.HistoryPoint, want []wantPoint) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("points = %+v, want %d of them", got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Day != w.day || g.Total() != w.total || g.Partial != w.partial {
			t.Errorf("point %d = %s total %s partial %v, want %s total %s partial %v",
				i, g.Day, g.Total(), g.Partial, w.day, w.total, w.partial)
		}
	}
}

func TestHistoryReadsEachMonthsEndTheWayTodayIsRead(t *testing.T) {
	f := historyFixture(t)
	ctx := context.Background()
	bank := f.account(t, f.user, "checking", "100")
	card := f.account(t, f.user, "credit_card", "**** 1111")

	f.stated(t, bank.ID, day(2026, time.August, 31), 100000)
	f.line(t, bank, day(2026, time.September, 5), 50000)
	f.line(t, bank, day(2026, time.September, 20), -20000)
	f.line(t, bank, day(2026, time.October, 10), -10000)
	f.line(t, bank, day(2026, time.November, 5), 5000)

	f.stated(t, card.ID, day(2026, time.September, 30), -30000)
	f.line(t, card, day(2026, time.October, 2), -5000)
	f.line(t, card, day(2026, time.November, 1), 30000)

	got, err := f.svc.History(ctx, f.user, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkPoints(t, got, []wantPoint{
		// July has nothing to read in either account, so it is left out.
		// In August the card had no statement yet.
		{day(2026, time.August, 31), 100000, true},
		{day(2026, time.September, 30), 130000 - 30000, false},
		{day(2026, time.October, 31), 120000 - 35000, false},
		{day(2026, time.November, 22), 125000 - 5000, false},
	})

	// Today is the net worth Cuentas shows.
	list, err := f.svc.List(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	if nw := accounts.NewListView(list).NetWorth; got[len(got)-1].Total().Amount() != nw.Total {
		t.Errorf("today = %s, Cuentas says %s", got[len(got)-1].Total(), nw.Total.Formatted)
	}
	// A month's end is the balance a reconciliation computes for that day.
	cps, err := f.db.Q().CheckpointsByAccount(ctx, f.user, bank.ID)
	if err != nil {
		t.Fatal(err)
	}
	through, ok, err := accounts.BalanceThrough(ctx, f.db.Q(), bank.ID, cps, day(2026, time.October, 31))
	if err != nil || !ok {
		t.Fatalf("BalanceThrough = %v, %v", ok, err)
	}
	if got[2].Assets != through {
		t.Errorf("October's bank = %s, BalanceThrough says %s", got[2].Assets, through)
	}

	// One account alone, the way its page reads it.
	only, err := f.svc.History(ctx, f.user, 4, &card.ID)
	if err != nil {
		t.Fatal(err)
	}
	checkPoints(t, only, []wantPoint{
		{day(2026, time.September, 30), -30000, false},
		{day(2026, time.October, 31), -35000, false},
		{day(2026, time.November, 22), -5000, false},
	})
}

func TestHistoryMakesNothingUp(t *testing.T) {
	f := historyFixture(t)
	ctx := context.Background()
	bank := f.account(t, f.user, "checking", "100")
	f.stated(t, bank.ID, day(2026, time.September, 30), 10000)

	// A savings account no balance anchors is only the sum of its lines, as
	// Cuentas shows it: every day it is in is partial, today too.
	savings := f.account(t, f.user, "savings", "200")
	f.line(t, savings, day(2026, time.October, 15), 2500)

	// A card someone else pays is not the owner's debt.
	if _, err := f.svc.AddHandCard(ctx, f.user, uuid.Nil, "Mamá", "4321"); err != nil {
		t.Fatal(err)
	}

	// A broker whose positions were synced from mid-October on.
	broker := f.account(t, f.user, "brokerage", "U1")
	f.stated(t, broker.ID, day(2026, time.September, 30), 0)
	for _, d := range []struct {
		on    civil.Date
		worth money.Cents
	}{{day(2026, time.October, 15), 70000}, {day(2026, time.November, 20), 80000}} {
		if err := f.db.Q().UpsertHoldingsDay(ctx, f.user, broker.ID, d.on, d.worth); err != nil {
			t.Fatal(err)
		}
	}
	value := money.Cents(80000)
	if err := f.db.Q().ReplacePositions(ctx, f.user, broker.ID, []store.Position{{Symbol: "VT", Currency: "USD",
		Quantity: 1, MarketValue: &value, AsOf: day(2026, time.November, 20)}}); err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.History(ctx, f.user, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkPoints(t, got, []wantPoint{
		// September: the savings had no line yet and the positions no figure.
		{day(2026, time.September, 30), 10000, true},
		// October: the savings is unanchored; the broker is worth October 15's.
		{day(2026, time.October, 31), 10000 + 2500 + 70000, true},
		{day(2026, time.November, 22), 10000 + 2500 + 80000, true},
	})
	if got[1].Holdings != 70000 {
		t.Errorf("October's holdings = %s, want 700.00", got[1].Holdings)
	}

	list, err := f.svc.List(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	if nw := accounts.NewListView(list).NetWorth; got[2].Total().Amount() != nw.Total || got[2].Partial != nw.Incomplete {
		t.Errorf("today = %s partial %v, Cuentas says %s incomplete %v",
			got[2].Total(), got[2].Partial, nw.Total.Formatted, nw.Incomplete)
	}
}

func TestHistoryOfSomeoneElsesAccountIsNotFound(t *testing.T) {
	f := historyFixture(t)
	other := newUser(t, f.db, "x@example.com")
	theirs := f.account(t, other, "checking", "900")
	if _, err := f.svc.History(context.Background(), f.user, 12, &theirs.ID); !errors.Is(err, accounts.ErrNotFound) {
		t.Errorf("History(their account) = %v, want ErrNotFound", err)
	}
}
