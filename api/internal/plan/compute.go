package plan

import (
	"bytes"
	"cmp"
	"slices"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// Instance is one month of an item: what was planned for it that month.
type Instance struct {
	Item  *store.PlanItem
	Month Month
	// Version is the item's version that holds for Month.
	Version Version
}

// Planned is what the instance was planned to amount to.
func (i Instance) Planned() money.Cents { return i.Version.Amount }

// Due is the day the instance was expected, fitted to its month; the zero
// Date for a saving with no day.
func (i Instance) Due() civil.Date {
	if i.Version.Day == 0 {
		return civil.Date{}
	}
	return i.Month.DayIn(i.Version.Day)
}

// Instances are the months from from to to of the items, from their stored
// versions: one per item and month a version holds for. They come by month,
// then by the day they are due, then in the order the items were made.
//
// A planned purchase has none yet. How much of its price a month sets aside,
// and whether it was bought, is worked out once purchases are kept; until
// then SaveItem refuses one, so none is ever stored.
func Instances(items []store.PlanItem, versions []store.PlanVersion, from, to Month) []Instance {
	byID := make(map[uuid.UUID]*store.PlanItem, len(items))
	for i := range items {
		byID[items[i].ID] = &items[i]
	}
	var out []Instance
	for _, v := range versions {
		item, ok := byID[v.ItemID]
		if !ok || item.Kind == KindPurchase {
			continue
		}
		version := Version{ID: v.ID, From: MonthOf(v.From), To: monthOf(v.To), Values: Values{Amount: v.Amount, Day: v.Day}}
		for m := from; !m.After(to); m = m.AddMonths(1) {
			if version.Holds(m) {
				out = append(out, Instance{Item: item, Month: m, Version: version})
			}
		}
	}
	slices.SortFunc(out, func(a, b Instance) int {
		return cmp.Or(a.Month.Compare(b.Month), a.Due().Compare(b.Due()),
			a.Item.CreatedAt.Compare(b.Item.CreatedAt), bytes.Compare(a.Item.ID[:], b.Item.ID[:]))
	})
	return out
}

// Cap is a cap on a category for a run of months.
type Cap struct {
	CategoryID uuid.UUID
	Version    Version
}

// Input is everything a month's plan is worked out from.
type Input struct {
	Month Month
	// Today is the day it is in Panama, which decides whether Month is
	// current, past or ahead, and what is late.
	Today civil.Date
	// Instances are the items' months from the month before Month to the one
	// after, so a paycheck that arrives across the turn of a month is
	// recognised the same whichever of the two is read.
	Instances []Instance
	// Caps are the caps that hold for Month.
	Caps []Cap
	// Parents maps a category to its parent, so a cap counts its children.
	Parents map[uuid.UUID]uuid.UUID
	// Movements are the spending and income booked from the month before
	// Month to the one after, with any the owner said paid one of Instances
	// booked further away, and Recognition which instance each paid.
	Movements   []store.PlanMovement
	Recognition Recognition
	// Cards are the owner's credit cards with a statement due, shown beside
	// the month and never subtracted.
	Cards []store.PlanCard
}

// State says where a month stands against today.
type State string

const (
	StateCurrent State = "current"
	StatePast    State = "past"
	StateFuture  State = "future"
)

// Level says how much of what was free, or of a cap, is spent.
type Level string

const (
	LevelOK   Level = "ok"
	LevelNear Level = "near"
	LevelOver Level = "over"
	// LevelNoFree says the plan itself leaves nothing free: what goes out and
	// what is set aside come to the income or more. It is the plan's shape,
	// not a warning about spending.
	LevelNoFree Level = "no_free"
	// LevelSoon is a payment's alert: it goes out within alertDays.
	LevelSoon Level = "soon"
)

// nearPercent is how much of what is free, or of a cap, is near it.
const nearPercent = 80

// alertDays is how many days ahead a payment still to go out is said to go
// out soon.
const alertDays = 3

// Status is where an instance stands.
type Status string

const (
	// StatusDone: a paycheck that arrived.
	StatusDone Status = "done"
	// StatusOut: a payment that went out.
	StatusOut Status = "out"
	// StatusUpcoming: its day has not passed, or its month is ahead.
	StatusUpcoming Status = "upcoming"
	// StatusLate: its day passed and it can still come: a payment within its
	// month, a paycheck within incomeWindow days.
	StatusLate Status = "late"
	// StatusMissed: it can no longer come, as far as the plan can tell.
	StatusMissed Status = "missed"
)

// AlertKind is what an alert is about.
type AlertKind string

const (
	AlertFree    AlertKind = "free"
	AlertCap     AlertKind = "cap"
	AlertPayment AlertKind = "payment"
)

// Plan is a month's plan beside what happened in it.
type Plan struct {
	Month Month
	Today civil.Date
	State State
	// Planned says the month has any item or cap.
	Planned bool
	// Editable says the month can still be written: it is the current one or
	// up to MonthsAhead after it.
	Editable bool
	Summary  Summary
	Income   Income
	// Fixed are the month's payments, Savings what it sets aside.
	Fixed   []Line
	Savings []Line
	// Caps are the month's caps, in the order they were given.
	Caps          []CapLine
	Uncategorized Uncategorized
	Quincenas     [2]Quincena
	// Cards are the credit cards whose latest statement is due in the month,
	// or, in the current month, after it.
	Cards []CardLine
	// Alerts are said only for the current month.
	Alerts []Alert
	// Spending is everything spent in the month, exactly what Gastos adds up,
	// and Charged the part of it planned items took. The rest is
	// Summary.Spent: Spending = Summary.Spent + Charged, always.
	Spending money.Cents
	Charged  money.Cents
}

// Summary is the month's account:
//
//	Free = Income − Fixed − Savings − Purchases
//	Left = Free − Spent
type Summary struct {
	// Income is what was planned to come in, not what did: extra income never
	// changes what is free by itself.
	Income money.Cents
	// Fixed is what the month's payments take: what went out for each, or,
	// until it does, what was planned.
	Fixed money.Cents
	// Savings is what the month sets aside, always: it reaches its account as
	// a transfer, which is never spending.
	Savings money.Cents
	// Purchases is what planned purchases set aside, nothing until they are
	// kept.
	Purchases money.Cents
	Free      money.Cents
	// Spent is what went out of what was free: the month's spending that no
	// planned item took.
	Spent money.Cents
	Left  money.Cents
	// DaysLeft counts today in the current month; it is every day of a month
	// ahead and none of a past one.
	DaysLeft int
	// PerDay is Left spread over DaysLeft, rounded down; nil when nothing is
	// left or no day is.
	PerDay *money.Cents
	Level  Level
}

// Income is what the month expected to come in beside what did.
type Income struct {
	// Expected is the sum of the paychecks planned.
	Expected money.Cents
	// Actual is every income booked in the month, as the month's flow counts
	// it; Unplanned is the part of it no paycheck of any month took.
	Actual    money.Cents
	Unplanned money.Cents
	Items     []Line
}

// Line is one item's month in the plan.
type Line struct {
	Instance
	Status Status
	// Movements are the ones that paid it, by day.
	Movements []LineMovement
	// Paid is, for a payment that went out, what went out for it.
	Paid *money.Cents
	// Committed is what it takes of the month: a payment's Paid once it went
	// out, what was planned otherwise.
	Committed money.Cents
	// MarkedPaid is the link by which the owner said it was paid where the
	// app cannot see; uuid.Nil when they did not.
	MarkedPaid uuid.UUID
	// Candidates are the movements it could be, while it is still to be
	// paid, closest first; Hint a transfer it could be, or nil.
	Candidates []store.PlanMovement
	Hint       *Hint
}

// LineMovement is a movement that paid a line.
type LineMovement struct {
	store.PlanMovement
	// Link is the link by which the owner said it paid the line; uuid.Nil
	// when the plan recognised it by itself.
	Link uuid.UUID
}

// CardLine is a credit card's latest statement beside the month. It is only
// shown: the card's purchases already count as spending, and paying it is a
// transfer.
type CardLine struct {
	store.PlanCard
	// Paid says what reached the card since the statement closed covers its
	// minimum payment, or, with no minimum, that something did.
	Paid bool
}

// CapLine is a cap in a month: what the free spending of its category and
// the category's children took of it. A refund gives back to it.
type CapLine struct {
	Cap
	Spent money.Cents
	Level Level
}

// Uncategorized is the free spending of the month with no category: it counts
// in Spent, under no cap.
type Uncategorized struct {
	Spent money.Cents
	Count int
}

// Quincena is half a month: the 1st to the 15th, or the 16th to its end.
type Quincena struct {
	From, To civil.Date
	// Free is its share of what is free, by its days.
	Free money.Cents
	// Spent is the free spending booked in it.
	Spent money.Cents
	// IncomeExpected and FixedDue are what was planned to come in, and what
	// the payments take, by the day each is due.
	IncomeExpected money.Cents
	FixedDue       money.Cents
}

// Alert is something in the current month the owner may want to look at.
type Alert struct {
	Kind  AlertKind
	Level Level
	// CategoryID is a cap's; ItemID and DueOn a payment's; AccountID and
	// DueOn a card's statement.
	CategoryID *uuid.UUID
	ItemID     *uuid.UUID
	AccountID  *uuid.UUID
	DueOn      civil.Date
}

// Compute works out a month's plan beside what happened in it. It is pure:
// the same input gives the same plan.
//
// It never counts spending its own way. The month's spending is the same
// movements Gastos adds up, and every one of them lands in exactly one
// bucket: the payment it paid, whichever month that payment is of, or what
// was free. A payment that went out takes what was paid, not what was
// planned, and one still to go out keeps what was planned aside. A refund on
// its own never settles a payment. A month the owner said was paid outside
// keeps what was planned, which is how it counts, once, until a movement
// recognised as it says what it cost.
func Compute(in Input) Plan {
	m := in.Month
	current := MonthOf(in.Today)
	p := Plan{
		Month: m, Today: in.Today, State: stateOf(m, current),
		Editable: !m.Before(current) && m.Sub(current) <= MonthsAhead,
		Planned:  len(in.Caps) > 0,
	}
	p.Quincenas = quincenas(m)

	// paidTo is the instance movement i paid, or Unpaid; paidKind is that
	// instance's kind, or "" for none.
	paidTo := func(i int) int {
		if k := in.Recognition.paid(i); k < len(in.Instances) {
			return k
		}
		return Unpaid
	}
	paidKind := func(i int) string {
		if k := paidTo(i); k != Unpaid {
			return in.Instances[k].Item.Kind
		}
		return ""
	}
	paidBy := make([][]int, len(in.Instances))
	for i := range in.Movements {
		if k := paidTo(i); k != Unpaid {
			paidBy[k] = append(paidBy[k], i)
		}
	}

	for k, inst := range in.Instances {
		if inst.Month != m {
			continue
		}
		p.Planned = true
		line := Line{Instance: inst, Committed: inst.Planned(), MarkedPaid: in.Recognition.markedPaid(k),
			Hint: in.Recognition.hint(k)}
		for _, i := range paidBy[k] {
			line.Movements = append(line.Movements,
				LineMovement{PlanMovement: in.Movements[i], Link: in.Recognition.assigned(i)})
		}
		for _, i := range in.Recognition.candidates(k) {
			line.Candidates = append(line.Candidates, in.Movements[i])
		}
		marked := line.MarkedPaid != uuid.Nil
		due := inst.Due()
		switch inst.Item.Kind {
		case KindIncome:
			line.Status = incomeStatus(p.State, in.Today, due, len(line.Movements) > 0 || marked)
			p.Income.Expected += inst.Planned()
			p.Quincenas[half(due)].IncomeExpected += inst.Planned()
			p.Income.Items = append(p.Income.Items, line)
		case KindFixed:
			if paid, out := paidFor(line.Movements); out {
				line.Paid, line.Committed = &paid, paid
			}
			line.Status = fixedStatus(p.State, in.Today, due, line.Paid != nil || marked)
			p.Summary.Fixed += line.Committed
			p.Quincenas[half(due)].FixedDue += line.Committed
			p.Fixed = append(p.Fixed, line)
		case KindSaving:
			p.Summary.Savings += inst.Planned()
			p.Savings = append(p.Savings, line)
		}
	}
	p.Summary.Income = p.Income.Expected
	// Planned purchases set nothing aside until they are kept (Instances).
	p.Summary.Purchases = 0
	p.Summary.Free = p.Summary.Income - p.Summary.Fixed - p.Summary.Savings - p.Summary.Purchases

	p.Caps = make([]CapLine, 0, len(in.Caps))
	for _, c := range in.Caps {
		p.Caps = append(p.Caps, CapLine{Cap: c})
	}
	for i, mv := range in.Movements {
		if MonthOf(mv.BookedOn) != m {
			continue
		}
		switch {
		case mv.Kind == store.KindIncome:
			p.Income.Actual += mv.Amount
			if paidKind(i) != KindIncome {
				p.Income.Unplanned += mv.Amount
			}
		case slices.Contains(store.SpendKinds, mv.Kind):
			p.Spending -= mv.Amount
			if paidKind(i) == KindFixed {
				p.Charged -= mv.Amount
				continue
			}
			p.Summary.Spent -= mv.Amount
			p.Quincenas[half(mv.BookedOn)].Spent -= mv.Amount
			if mv.CategoryID == nil {
				p.Uncategorized.Spent -= mv.Amount
				p.Uncategorized.Count++
				continue
			}
			for j := range p.Caps {
				if c := p.Caps[j].CategoryID; *mv.CategoryID == c || in.Parents[*mv.CategoryID] == c {
					p.Caps[j].Spent -= mv.Amount
				}
			}
		}
	}

	s := &p.Summary
	s.Left = s.Free - s.Spent
	s.Level = levelOf(s.Spent, s.Free)
	switch p.State {
	case StateCurrent:
		s.DaysLeft = m.Last().Day - in.Today.Day + 1
	case StateFuture:
		s.DaysLeft = m.Days()
	}
	if s.Left > 0 && s.DaysLeft > 0 {
		perDay := s.Left / money.Cents(s.DaysLeft)
		s.PerDay = &perDay
	}
	if s.Free > 0 {
		// By days, so a quincena's share agrees with what is free per day;
		// the second takes what rounding leaves.
		p.Quincenas[0].Free = s.Free * 15 / money.Cents(m.Days())
		p.Quincenas[1].Free = s.Free - p.Quincenas[0].Free
	}
	for j := range p.Caps {
		c := &p.Caps[j]
		c.Level = capLevelOf(c.Spent, c.Version.Amount)
	}
	for _, c := range in.Cards {
		if due := MonthOf(c.DueOn); due == m || p.State == StateCurrent && due.After(m) {
			p.Cards = append(p.Cards, CardLine{PlanCard: c, Paid: cardPaid(c)})
		}
	}
	if p.State == StateCurrent {
		p.Alerts = alerts(p)
	}
	return p
}

// cardPaid says a card's statement is paid: what reached it since it closed
// covers the minimum, or, when the statement gives none, something did.
func cardPaid(c store.PlanCard) bool {
	if c.MinimumPayment == nil {
		return c.PaidSince > 0
	}
	return c.PaidSince >= *c.MinimumPayment
}

// paidFor is what went out for a payment: the spending among the movements
// that paid it, refunds lowering it. out says one of them is an outflow; a
// refund on its own gives money back, it does not pay.
func paidFor(movements []LineMovement) (paid money.Cents, out bool) {
	for _, mv := range movements {
		if !slices.Contains(store.SpendKinds, mv.Kind) {
			continue
		}
		paid -= mv.Amount
		out = out || mv.Amount < 0
	}
	return paid, out
}

func stateOf(m, current Month) State {
	switch {
	case m.Before(current):
		return StatePast
	case m.After(current):
		return StateFuture
	default:
		return StateCurrent
	}
}

// levelOf says how much of what is free went out. Exactly what was free is
// still near; one cent more is over.
func levelOf(spent, free money.Cents) Level {
	if free <= 0 {
		return LevelNoFree
	}
	return capLevelOf(spent, free)
}

func capLevelOf(spent, limit money.Cents) Level {
	switch {
	case spent > limit:
		return LevelOver
	case spent*100 >= limit*nearPercent:
		return LevelNear
	default:
		return LevelOK
	}
}

func incomeStatus(state State, today, due civil.Date, arrived bool) Status {
	switch {
	case arrived:
		return StatusDone
	case state == StateFuture || !today.After(due):
		return StatusUpcoming
	case !today.After(due.AddDays(incomeWindow)):
		return StatusLate
	default:
		return StatusMissed
	}
}

// fixedStatus is where a payment stands. It is only looked for in its own
// month, so once the month is over it is missed.
func fixedStatus(state State, today, due civil.Date, out bool) Status {
	switch {
	case out:
		return StatusOut
	case state == StatePast:
		return StatusMissed
	case state == StateFuture || !today.After(due):
		return StatusUpcoming
	default:
		return StatusLate
	}
}

// quincenas are m's two halves, with nothing in them yet.
func quincenas(m Month) [2]Quincena {
	first := m.First()
	return [2]Quincena{
		{From: first, To: first.AddDays(14)},
		{From: first.AddDays(15), To: m.Last()},
	}
}

// half is the quincena day falls in.
func half(day civil.Date) int {
	if day.Day <= 15 {
		return 0
	}
	return 1
}

// alerts are what the current month has to say: what is free or a cap near
// or over, and the payments and card statements still to pay that are due
// within alertDays. A plan that leaves nothing free says so in its level
// instead.
func alerts(p Plan) []Alert {
	var out []Alert
	if l := p.Summary.Level; l == LevelNear || l == LevelOver {
		out = append(out, Alert{Kind: AlertFree, Level: l})
	}
	for _, c := range p.Caps {
		if c.Level == LevelNear || c.Level == LevelOver {
			id := c.CategoryID
			out = append(out, Alert{Kind: AlertCap, Level: c.Level, CategoryID: &id})
		}
	}
	soon := p.Today.AddDays(alertDays)
	for _, f := range p.Fixed {
		if due := f.Due(); f.Status == StatusUpcoming && !due.After(soon) {
			id := f.Item.ID
			out = append(out, Alert{Kind: AlertPayment, Level: LevelSoon, ItemID: &id, DueOn: due})
		}
	}
	for _, c := range p.Cards {
		if !c.Paid && !c.DueOn.Before(p.Today) && !c.DueOn.After(soon) {
			id := c.AccountID
			out = append(out, Alert{Kind: AlertPayment, Level: LevelSoon, AccountID: &id, DueOn: c.DueOn})
		}
	}
	return out
}
