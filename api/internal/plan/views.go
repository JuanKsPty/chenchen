package plan

import (
	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// View is a month's plan on the wire. Money is an Amount, a month YYYY-MM
// and a day YYYY-MM-DD; every list is present, empty when there is nothing
// in it.
type View struct {
	Month    Month        `json:"month"`
	Today    civil.Date   `json:"today"`
	State    State        `json:"state"`
	Planned  bool         `json:"planned"`
	Editable bool         `json:"editable"`
	Summary  SummaryView  `json:"summary"`
	Income   IncomeView   `json:"income"`
	Fixed    []FixedView  `json:"fixed"`
	Savings  []SavingView `json:"savings"`
	// Purchases are planned figures, never a balance: what the plan sets
	// aside for each, which no account is said to hold.
	Purchases     []PurchaseView    `json:"purchases"`
	Caps          []CapLineView     `json:"caps"`
	Uncategorized UncategorizedView `json:"uncategorized"`
	Quincenas     [2]QuincenaView   `json:"quincenas"`
	// Cards are shown beside the month and never subtracted from it.
	Cards  []CardView  `json:"cards"`
	Alerts []AlertView `json:"alerts"`
}

// SummaryView is the month's account on the wire. PerDay is null when
// nothing is left or no day is.
type SummaryView struct {
	Income    money.Amount  `json:"income"`
	Fixed     money.Amount  `json:"fixed"`
	Savings   money.Amount  `json:"savings"`
	Purchases money.Amount  `json:"purchases"`
	Free      money.Amount  `json:"free"`
	Spent     money.Amount  `json:"spent"`
	Left      money.Amount  `json:"left"`
	DaysLeft  int           `json:"days_left"`
	PerDay    *money.Amount `json:"per_day"`
	Level     Level         `json:"level"`
}

// IncomeView is what the month expected to come in beside what did.
type IncomeView struct {
	Expected  money.Amount     `json:"expected"`
	Actual    money.Amount     `json:"actual"`
	Unplanned money.Amount     `json:"unplanned"`
	Items     []IncomeLineView `json:"items"`
}

// ItemView is an item on the wire, with the values that hold for the month
// read: its amount, its day and the run of months they hold for (To is null
// when it has no end).
//
// A planned purchase's are those of all of it, the way it is saved: Amount
// is its price, From the month it is set aside from (StartMonth) and To the
// month it is due (TargetMonth).
type ItemView struct {
	ID          uuid.UUID     `json:"id"`
	Kind        string        `json:"kind"`
	Name        string        `json:"name"`
	Amount      money.Amount  `json:"amount"`
	Day         int           `json:"day,omitempty"`
	CategoryID  *uuid.UUID    `json:"category_id,omitempty"`
	MatchText   string        `json:"match_text"`
	From        Month         `json:"from"`
	To          Month         `json:"to"`
	Target      *money.Amount `json:"target,omitempty"`
	TargetMonth Month         `json:"target_month,omitzero"`
	StartMonth  Month         `json:"start_month,omitzero"`
	Funding     string        `json:"funding,omitempty"`
	AccountID   *uuid.UUID    `json:"account_id,omitempty"`
}

// NewItemView builds an item's month on the wire.
func NewItemView(i Instance) ItemView {
	v := ItemView{
		ID: i.Item.ID, Kind: i.Item.Kind, Name: i.Item.Name, Amount: i.Planned().Amount(), Day: i.Version.Day,
		CategoryID: i.Item.CategoryID, MatchText: i.Item.MatchText, From: i.Version.From, To: i.Version.To,
		TargetMonth: monthOf(i.Item.TargetMonth), StartMonth: monthOf(i.Item.StartMonth), Funding: i.Item.Funding,
		AccountID: i.Item.AccountID,
	}
	// A purchase's target is its price, which is its amount already.
	if i.Item.Target != nil && i.Item.Kind != KindPurchase {
		t := i.Item.Target.Amount()
		v.Target = &t
	}
	return v
}

// MovementView is a movement on the wire. Of one that paid an item, How says
// who said so: "auto" when the plan recognised it, "assigned" when the owner
// did, with the link that says it, which undoing it deletes. A movement only
// offered has neither.
type MovementView struct {
	ID          uuid.UUID    `json:"id"`
	Origin      string       `json:"origin"`
	BookedOn    civil.Date   `json:"booked_on"`
	Amount      money.Amount `json:"amount"`
	Description string       `json:"description"`
	How         string       `json:"how,omitempty"`
	LinkID      *uuid.UUID   `json:"link_id,omitempty"`
}

func movementView(m store.PlanMovement) MovementView {
	return MovementView{ID: m.ID, Origin: m.Origin, BookedOn: m.BookedOn, Amount: m.Amount.Amount(),
		Description: m.Description}
}

// paidByViews are the movements that paid a line.
func paidByViews(movements []LineMovement) []MovementView {
	out := make([]MovementView, 0, len(movements))
	for _, m := range movements {
		v := movementView(m.PlanMovement)
		v.How = "auto"
		if m.Link != uuid.Nil {
			link := m.Link
			v.How, v.LinkID = "assigned", &link
		}
		out = append(out, v)
	}
	return out
}

func candidateViews(movements []store.PlanMovement) []MovementView {
	out := make([]MovementView, 0, len(movements))
	for _, m := range movements {
		out = append(out, movementView(m))
	}
	return out
}

// HintView is a movement pointed at, and why.
type HintView struct {
	Movement MovementView `json:"movement"`
	Reason   HintReason   `json:"reason"`
}

// linkID is a link's id on the wire: null for none.
func linkID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// IncomeLineView is a paycheck's month: when it was due, whether it arrived
// and what it arrived as. MarkedPaid says the owner said it arrived where the
// app cannot see, by the link PaidLinkID, which undoing it deletes.
// Candidates are what it could be while it has not arrived.
type IncomeLineView struct {
	ItemView
	DueOn      civil.Date     `json:"due_on"`
	Status     Status         `json:"status"`
	MarkedPaid bool           `json:"marked_paid"`
	PaidLinkID *uuid.UUID     `json:"paid_link_id,omitempty"`
	Movements  []MovementView `json:"movements"`
	Candidates []MovementView `json:"candidates"`
}

// FixedView is a payment's month. Paid is what went out for it, null until
// a movement says. MarkedPaid says the owner said it was paid where the app
// cannot see, by the link PaidLinkID. While it is still to be paid,
// Candidates are what it could be, and Hint a transfer it could be, or null.
type FixedView struct {
	ItemView
	DueOn      civil.Date     `json:"due_on"`
	Status     Status         `json:"status"`
	Paid       *money.Amount  `json:"paid"`
	MarkedPaid bool           `json:"marked_paid"`
	PaidLinkID *uuid.UUID     `json:"paid_link_id,omitempty"`
	Movements  []MovementView `json:"movements"`
	Candidates []MovementView `json:"candidates"`
	Hint       *HintView      `json:"hint"`
}

// SavingView is what the month sets aside. Of a saving tied to an account,
// Progress is what the account holds, as Cuentas shows it, and SavedMonth
// what reached it as transfers this month, net; both are null for one tied
// to none. NeededPerMonth is what each month to the goal's date would have to
// set aside to reach it, null without a goal and a date, or once reached.
// Status is done or partial by what reached the account, pending while the
// month lasts with nothing yet, missed once it ended with nothing, as a
// payment's is, and unlinked with no account.
type SavingView struct {
	ItemView
	Status         Status        `json:"status"`
	SavedMonth     *money.Amount `json:"saved_month"`
	Progress       *money.Amount `json:"progress"`
	NeededPerMonth *money.Amount `json:"needed_per_month"`
}

// PurchaseView is a planned purchase in the month. Share is what the month
// sets aside for it and SetAsideBefore what the months before did: plan
// figures, never a balance. Paid and BoughtOn are what it cost and when, in
// the month it was bought, and null otherwise. Movements are what bought it;
// Candidates what could be it, while it was not bought.
type PurchaseView struct {
	ItemView
	Status         Status         `json:"status"`
	Share          money.Amount   `json:"share"`
	SetAsideBefore money.Amount   `json:"set_aside_before"`
	Paid           *money.Amount  `json:"paid"`
	BoughtOn       civil.Date     `json:"bought_on"`
	Movements      []MovementView `json:"movements"`
	Candidates     []MovementView `json:"candidates"`
}

// amountOf is an amount on the wire, or null for none.
func amountOf(c *money.Cents) *money.Amount {
	if c == nil {
		return nil
	}
	a := c.Amount()
	return &a
}

// CapView is a cap on the wire: its amount and the run of months it holds
// for (To is null when it has no end).
type CapView struct {
	CategoryID uuid.UUID    `json:"category_id"`
	Amount     money.Amount `json:"amount"`
	From       Month        `json:"from"`
	To         Month        `json:"to"`
}

// NewCapView builds a cap's wire shape.
func NewCapView(c Cap) CapView {
	return CapView{CategoryID: c.CategoryID, Amount: c.Version.Amount.Amount(), From: c.Version.From, To: c.Version.To}
}

// CapLineView is a cap in a month: what its category's free spending took of
// it.
type CapLineView struct {
	CapView
	Spent money.Amount `json:"spent"`
	Level Level        `json:"level"`
}

// UncategorizedView is the month's free spending with no category.
type UncategorizedView struct {
	Spent money.Amount `json:"spent"`
	Count int          `json:"count"`
}

// QuincenaView is half the month.
type QuincenaView struct {
	From           civil.Date   `json:"from"`
	To             civil.Date   `json:"to"`
	Free           money.Amount `json:"free"`
	Spent          money.Amount `json:"spent"`
	IncomeExpected money.Amount `json:"income_expected"`
	FixedDue       money.Amount `json:"fixed_due"`
}

// CardView is a credit card's latest statement beside the month. Paid says
// what reached the card since it closed covers the minimum payment, or, with
// none, that something did.
type CardView struct {
	AccountID      uuid.UUID     `json:"account_id"`
	ClosingOn      civil.Date    `json:"closing_on"`
	DueOn          civil.Date    `json:"due_on"`
	MinimumPayment *money.Amount `json:"minimum_payment"`
	Paid           bool          `json:"paid"`
}

// AlertView is an alert on the wire: a cap's names its category, a payment's
// its item and day, a card's its account and day.
type AlertView struct {
	Kind       AlertKind  `json:"kind"`
	Level      Level      `json:"level"`
	CategoryID *uuid.UUID `json:"category_id,omitempty"`
	ItemID     *uuid.UUID `json:"item_id,omitempty"`
	AccountID  *uuid.UUID `json:"account_id,omitempty"`
	DueOn      civil.Date `json:"due_on,omitzero"`
}

// NewView builds a month's plan on the wire.
func NewView(p Plan) View {
	s := p.Summary
	v := View{
		Month: p.Month, Today: p.Today, State: p.State, Planned: p.Planned, Editable: p.Editable,
		Summary: SummaryView{
			Income: s.Income.Amount(), Fixed: s.Fixed.Amount(), Savings: s.Savings.Amount(),
			Purchases: s.Purchases.Amount(), Free: s.Free.Amount(), Spent: s.Spent.Amount(), Left: s.Left.Amount(),
			DaysLeft: s.DaysLeft, Level: s.Level,
		},
		Income: IncomeView{Expected: p.Income.Expected.Amount(), Actual: p.Income.Actual.Amount(),
			Unplanned: p.Income.Unplanned.Amount(), Items: make([]IncomeLineView, 0, len(p.Income.Items))},
		Fixed:         make([]FixedView, 0, len(p.Fixed)),
		Savings:       make([]SavingView, 0, len(p.Savings)),
		Purchases:     make([]PurchaseView, 0, len(p.Purchases)),
		Caps:          make([]CapLineView, 0, len(p.Caps)),
		Uncategorized: UncategorizedView{Spent: p.Uncategorized.Spent.Amount(), Count: p.Uncategorized.Count},
		Cards:         make([]CardView, 0, len(p.Cards)),
		Alerts:        make([]AlertView, 0, len(p.Alerts)),
	}
	if s.PerDay != nil {
		perDay := s.PerDay.Amount()
		v.Summary.PerDay = &perDay
	}
	for _, l := range p.Income.Items {
		v.Income.Items = append(v.Income.Items, IncomeLineView{ItemView: NewItemView(l.Instance), DueOn: l.Due(),
			Status: l.Status, MarkedPaid: l.MarkedPaid != uuid.Nil, PaidLinkID: linkID(l.MarkedPaid),
			Movements: paidByViews(l.Movements), Candidates: candidateViews(l.Candidates)})
	}
	for _, l := range p.Fixed {
		f := FixedView{ItemView: NewItemView(l.Instance), DueOn: l.Due(), Status: l.Status,
			MarkedPaid: l.MarkedPaid != uuid.Nil, PaidLinkID: linkID(l.MarkedPaid),
			Movements: paidByViews(l.Movements), Candidates: candidateViews(l.Candidates)}
		if l.Paid != nil {
			paid := l.Paid.Amount()
			f.Paid = &paid
		}
		if l.Hint != nil {
			f.Hint = &HintView{Movement: movementView(l.Hint.Movement), Reason: l.Hint.Reason}
		}
		v.Fixed = append(v.Fixed, f)
	}
	for _, l := range p.Savings {
		v.Savings = append(v.Savings, SavingView{ItemView: NewItemView(l.Instance), Status: l.Status,
			SavedMonth: amountOf(l.SavedMonth), Progress: amountOf(l.Progress), NeededPerMonth: amountOf(l.NeededPerMonth)})
	}
	for _, l := range p.Purchases {
		v.Purchases = append(v.Purchases, PurchaseView{ItemView: NewItemView(l.Instance), Status: l.Status,
			Share: l.Share.Amount(), SetAsideBefore: l.SetAsideBefore.Amount(), Paid: amountOf(l.Paid),
			BoughtOn: l.BoughtOn, Movements: paidByViews(l.Movements), Candidates: candidateViews(l.Candidates)})
	}
	for _, c := range p.Caps {
		v.Caps = append(v.Caps, CapLineView{CapView: NewCapView(c.Cap), Spent: c.Spent.Amount(), Level: c.Level})
	}
	for i, q := range p.Quincenas {
		v.Quincenas[i] = QuincenaView{From: q.From, To: q.To, Free: q.Free.Amount(), Spent: q.Spent.Amount(),
			IncomeExpected: q.IncomeExpected.Amount(), FixedDue: q.FixedDue.Amount()}
	}
	for _, c := range p.Cards {
		card := CardView{AccountID: c.AccountID, ClosingOn: c.ClosingOn, DueOn: c.DueOn, Paid: c.Paid}
		if c.MinimumPayment != nil {
			minimum := c.MinimumPayment.Amount()
			card.MinimumPayment = &minimum
		}
		v.Cards = append(v.Cards, card)
	}
	for _, a := range p.Alerts {
		v.Alerts = append(v.Alerts, AlertView{Kind: a.Kind, Level: a.Level, CategoryID: a.CategoryID,
			ItemID: a.ItemID, AccountID: a.AccountID, DueOn: a.DueOn})
	}
	return v
}
