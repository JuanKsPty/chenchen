package plan

import (
	"sort"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// AlertCashShort says the projection runs the bank accounts below zero before
// the month ends: its DueOn is the day it is lowest.
const AlertCashShort AlertKind = "cash_short"

// What moves the projection on a day.
const (
	EventIncome  = "income"
	EventFixed   = "fixed"
	EventCard    = "card"
	EventSaving  = "saving"
	EventPending = "pending"
)

// Cash is what the projection starts from and what it needs to know about the
// owner's accounts. Only the bank accounts count: a card's purchases reach
// them when the card is paid, and a broker's money is not for this month.
type Cash struct {
	// Balance is the bank accounts' balance today, as Cuentas shows it.
	Balance money.Cents
	// Pending is what the purchases noted on them that no statement has
	// printed yet add up to, signed (negative): spent, but not in Balance.
	Pending money.Cents
	// InCash says an account is one of those bank accounts, and Card that it
	// is one of the owner's cards.
	InCash map[uuid.UUID]bool
	Card   map[uuid.UUID]bool
	// PaidFrom is the account each payment's latest movement went out of,
	// by item: one paid with a card goes out with the card, not on its day.
	PaidFrom map[uuid.UUID]uuid.UUID
}

// Projection is where the bank accounts are headed by the month's end: today's
// balance, then each day what the plan says comes in or goes out on it, and
// what is free to spend spread over the days left.
type Projection struct {
	Start  money.Cents
	End    money.Cents
	Lowest ProjectedDay
	// FreePerDay is what the plan leaves free to spend each day, which the
	// projection takes as spent; zero when nothing is left.
	FreePerDay money.Cents
	Events     []Event
	Days       []ProjectedDay
}

// ProjectedDay is the balance at the end of a day.
type ProjectedDay struct {
	Day     civil.Date
	Balance money.Cents
}

// Event is something the plan expects on a day. Amount is signed: what comes
// in is positive.
type Event struct {
	Day       civil.Date
	Kind      string
	Name      string
	Amount    money.Cents
	ItemID    *uuid.UUID
	AccountID *uuid.UUID
}

// Project works out the current month's projection. It is pure. What is late
// still counts, on today: a paycheck that has not arrived yet and a payment
// that has not gone out are both still coming.
//
//   - a paycheck still to arrive comes in on its day;
//   - a payment still to go out leaves on its day, unless it was last paid with
//     a card, which pays it;
//   - a card statement not yet paid in full leaves what is left of it on its
//     due date, if that is this month: paid in full, it charges no interest;
//   - a saving tied to an account outside the bank accounts leaves what is
//     still to reach it; one tied to a bank account stays in them;
//   - what is free leaves a day's share each day, today included;
//   - the purchases noted and not yet printed leave today.
func Project(p Plan, c Cash) Projection {
	today, last := p.Today, p.Month.Last()
	on := func(d civil.Date) civil.Date {
		if d.Before(today) {
			return today
		}
		return d
	}
	var events []Event
	add := func(e Event) {
		if e.Amount != 0 && !e.Day.After(last) {
			events = append(events, e)
		}
	}

	if c.Pending != 0 {
		add(Event{Day: today, Kind: EventPending, Name: "Compras anotadas", Amount: c.Pending})
	}
	for _, l := range p.Income.Items {
		if l.Status != StatusUpcoming && l.Status != StatusLate {
			continue
		}
		id := l.Item.ID
		add(Event{Day: on(l.Due()), Kind: EventIncome, Name: l.Item.Name, Amount: l.Planned(), ItemID: &id})
	}
	for _, l := range p.Fixed {
		if l.Status != StatusUpcoming && l.Status != StatusLate || l.MarkedPaid != uuid.Nil {
			continue
		}
		if from, ok := c.PaidFrom[l.Item.ID]; ok && c.Card[from] {
			continue
		}
		id := l.Item.ID
		add(Event{Day: on(l.Due()), Kind: EventFixed, Name: l.Item.Name, Amount: -l.Planned(), ItemID: &id})
	}
	for _, card := range p.Cards {
		// What the statement printed as owed, less what reached the card
		// since: paying it in full is what keeps it from charging interest.
		owed := -card.Statement - card.PaidSince
		if owed <= 0 {
			continue
		}
		id := card.AccountID
		add(Event{Day: on(card.DueOn), Kind: EventCard, Name: "Pago de la tarjeta", Amount: -owed, AccountID: &id})
	}
	for _, l := range p.Savings {
		account := l.Item.AccountID
		if account == nil || c.InCash[*account] {
			continue
		}
		left := l.Planned()
		if l.SavedMonth != nil {
			left -= *l.SavedMonth
		}
		if left <= 0 {
			continue
		}
		id := l.Item.ID
		day := last
		if due := l.Due(); !due.IsZero() {
			day = on(due)
		}
		add(Event{Day: day, Kind: EventSaving, Name: l.Item.Name, Amount: -left, ItemID: &id, AccountID: account})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Day.Before(events[j].Day) })

	var perDay money.Cents
	if p.Summary.PerDay != nil {
		perDay = *p.Summary.PerDay
	}
	out := Projection{Start: c.Balance, FreePerDay: perDay, Events: events}
	balance, next := c.Balance, 0
	for d := today; !d.After(last); d = d.AddDays(1) {
		for ; next < len(events) && !events[next].Day.After(d); next++ {
			balance += events[next].Amount
		}
		balance -= perDay
		out.Days = append(out.Days, ProjectedDay{Day: d, Balance: balance})
		if len(out.Days) == 1 || balance < out.Lowest.Balance {
			out.Lowest = ProjectedDay{Day: d, Balance: balance}
		}
	}
	out.End = balance
	return out
}
