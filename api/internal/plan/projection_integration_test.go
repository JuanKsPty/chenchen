package plan_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

func (f fixture) stated(t *testing.T, account uuid.UUID, asOf civil.Date, balance money.Cents, due *civil.Date) {
	t.Helper()
	if err := f.db.Q().CreateCheckpoint(context.Background(), store.Checkpoint{ID: uuid.New(), UserID: f.user,
		AccountID: account, AsOf: asOf, Balance: balance, Source: store.SourceManual, DueOn: due}); err != nil {
		t.Fatal(err)
	}
}

func TestTheCurrentMonthSaysWhereTheBankAccountsAreHeaded(t *testing.T) {
	f := newFixture(t) // the 6th of November
	bank := f.account(t, f.user, "checking", "100")
	card := f.account(t, f.user, "credit_card", "**** 1111")
	broker := f.account(t, f.user, "brokerage", "U1")
	f.stated(t, bank, civil.Date{Year: 2026, Month: time.October, Day: 31}, 50000, nil)
	// Owed 650.00 on the statement, 150.00 of it paid since.
	f.stated(t, card, civil.Date{Year: 2026, Month: time.October, Day: 25}, -65000, &civil.Date{Year: 2026, Month: time.November, Day: 18})
	f.line(t, card, civil.Date{Year: 2026, Month: time.November, Day: 2}, 15000, store.KindTransfer, "PAGO")
	// The broker is money, but not this month's.
	f.stated(t, broker, civil.Date{Year: 2026, Month: time.October, Day: 31}, 900000, nil)
	f.noted(t, bank, civil.Date{Year: 2026, Month: time.November, Day: 5}, 1200, "Café")

	f.save(t, uuid.New(), plan.ItemInput{Kind: plan.KindFixed, Month: nov26, Scope: plan.ScopeFrom,
		Name: "Alquiler", Amount: 95000, Day: 10, MatchText: "ALQUILER"})
	f.save(t, uuid.New(), plan.ItemInput{Kind: plan.KindIncome, Month: nov26, Scope: plan.ScopeFrom,
		Name: "Planilla", Amount: 180000, Day: 15, MatchText: "PLANILLA"})

	p := f.month(t, nov26)
	got := p.Projection
	if got == nil {
		t.Fatal("the current month has no projection")
	}
	if got.Start != 50000 {
		t.Errorf("start = %s, want the bank account's 500.00 alone", got.Start)
	}
	want := []struct {
		day    int
		kind   string
		amount money.Cents
	}{{6, plan.EventPending, -1200}, {10, plan.EventFixed, -95000}, {15, plan.EventIncome, 180000}, {18, plan.EventCard, -50000}}
	if len(got.Events) != len(want) {
		t.Fatalf("events = %+v, want %d", got.Events, len(want))
	}
	for i, w := range want {
		if e := got.Events[i]; e.Day.Day != w.day || e.Kind != w.kind || e.Amount != w.amount {
			t.Errorf("event %d = %s %s %s, want the %dth %s %s", i, e.Day, e.Kind, e.Amount, w.day, w.kind, w.amount)
		}
	}
	// What is free is spread over the 25 days from today; the paycheck is the
	// 15th, so the 14th is the lowest, and below zero.
	perDay := *p.Summary.PerDay
	lowest := money.Cents(50000-1200-95000) - 9*perDay
	if got.Lowest.Day.Day != 14 || got.Lowest.Balance != lowest || got.FreePerDay != perDay {
		t.Errorf("lowest = %s on %s spending %s a day, want %s on the 14th spending %s",
			got.Lowest.Balance, got.Lowest.Day, got.FreePerDay, lowest, perDay)
	}
	short := false
	for _, a := range p.Alerts {
		short = short || a.Kind == plan.AlertCashShort && a.DueOn == got.Lowest.Day
	}
	if !short {
		t.Errorf("alerts = %+v, want cash_short on the 14th", p.Alerts)
	}

	// Only the current month looks ahead.
	if past := f.month(t, oct26); past.Projection != nil {
		t.Errorf("October has a projection: %+v", past.Projection)
	}
	if ahead := f.month(t, dec26); ahead.Projection != nil {
		t.Errorf("December has a projection: %+v", ahead.Projection)
	}
}

func TestNoProjectionWithoutABalanceToStartFrom(t *testing.T) {
	f := newFixture(t)
	f.save(t, uuid.New(), plan.ItemInput{Kind: plan.KindFixed, Month: nov26, Scope: plan.ScopeFrom,
		Name: "Alquiler", Amount: 95000, Day: 10, MatchText: "ALQUILER"})

	// No bank account at all.
	if p := f.month(t, nov26); p.Projection != nil {
		t.Errorf("with no bank account, projection = %+v", p.Projection)
	}

	// A bank account whose lines no stated balance anchors: its balance is
	// only their sum, so nothing is projected and nothing is said short.
	bank := f.account(t, f.user, "checking", "100")
	f.line(t, bank, civil.Date{Year: 2026, Month: time.November, Day: 2}, -2000, store.KindExpense, "SUPER")
	p := f.month(t, nov26)
	if p.Projection != nil {
		t.Errorf("with an unanchored bank account, projection = %+v", p.Projection)
	}
	for _, a := range p.Alerts {
		if a.Kind == plan.AlertCashShort {
			t.Errorf("alerts = %+v, want no cash_short", p.Alerts)
		}
	}
}
