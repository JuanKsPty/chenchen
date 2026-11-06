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
	Month    Month       `json:"month"`
	Today    civil.Date  `json:"today"`
	State    State       `json:"state"`
	Planned  bool        `json:"planned"`
	Editable bool        `json:"editable"`
	Summary  SummaryView `json:"summary"`
	Income   IncomeView  `json:"income"`
	Fixed    []FixedView `json:"fixed"`
	// Savings say nothing yet about what reached their account: Progress,
	// SavedMonth and NeededPerMonth are null.
	Savings []SavingView `json:"savings"`
	// Purchases is empty until planned purchases are kept.
	Purchases     []struct{}        `json:"purchases"`
	Caps          []CapLineView     `json:"caps"`
	Uncategorized UncategorizedView `json:"uncategorized"`
	Quincenas     [2]QuincenaView   `json:"quincenas"`
	Alerts        []AlertView       `json:"alerts"`
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
	AccountID   *uuid.UUID    `json:"account_id,omitempty"`
}

// NewItemView builds an item's month on the wire.
func NewItemView(i Instance) ItemView {
	v := ItemView{
		ID: i.Item.ID, Kind: i.Item.Kind, Name: i.Item.Name, Amount: i.Planned().Amount(), Day: i.Version.Day,
		CategoryID: i.Item.CategoryID, MatchText: i.Item.MatchText, From: i.Version.From, To: i.Version.To,
		TargetMonth: monthOf(i.Item.TargetMonth), AccountID: i.Item.AccountID,
	}
	if i.Item.Target != nil {
		t := i.Item.Target.Amount()
		v.Target = &t
	}
	return v
}

// MovementView is a movement that paid an item. How is "auto" when the plan
// recognised it.
type MovementView struct {
	ID          uuid.UUID    `json:"id"`
	Origin      string       `json:"origin"`
	BookedOn    civil.Date   `json:"booked_on"`
	Amount      money.Amount `json:"amount"`
	Description string       `json:"description"`
	How         string       `json:"how"`
}

func movementViews(movements []store.PlanMovement) []MovementView {
	out := make([]MovementView, 0, len(movements))
	for _, m := range movements {
		out = append(out, MovementView{ID: m.ID, Origin: m.Origin, BookedOn: m.BookedOn, Amount: m.Amount.Amount(),
			Description: m.Description, How: "auto"})
	}
	return out
}

// IncomeLineView is a paycheck's month: when it was due, whether it arrived
// and what it arrived as.
type IncomeLineView struct {
	ItemView
	DueOn     civil.Date     `json:"due_on"`
	Status    Status         `json:"status"`
	Movements []MovementView `json:"movements"`
}

// FixedView is a payment's month. Paid is what went out for it, null until
// it does.
type FixedView struct {
	ItemView
	DueOn     civil.Date     `json:"due_on"`
	Status    Status         `json:"status"`
	Paid      *money.Amount  `json:"paid"`
	Movements []MovementView `json:"movements"`
}

// SavingView is what the month sets aside.
type SavingView struct {
	ItemView
	Status         *string       `json:"status"`
	SavedMonth     *money.Amount `json:"saved_month"`
	Progress       *money.Amount `json:"progress"`
	NeededPerMonth *money.Amount `json:"needed_per_month"`
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

// AlertView is an alert on the wire: a cap's names its category, a payment's
// its item and day.
type AlertView struct {
	Kind       AlertKind  `json:"kind"`
	Level      Level      `json:"level"`
	CategoryID *uuid.UUID `json:"category_id,omitempty"`
	ItemID     *uuid.UUID `json:"item_id,omitempty"`
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
		Purchases:     []struct{}{},
		Caps:          make([]CapLineView, 0, len(p.Caps)),
		Uncategorized: UncategorizedView{Spent: p.Uncategorized.Spent.Amount(), Count: p.Uncategorized.Count},
		Alerts:        make([]AlertView, 0, len(p.Alerts)),
	}
	if s.PerDay != nil {
		perDay := s.PerDay.Amount()
		v.Summary.PerDay = &perDay
	}
	for _, l := range p.Income.Items {
		v.Income.Items = append(v.Income.Items, IncomeLineView{ItemView: NewItemView(l.Instance), DueOn: l.Due(),
			Status: l.Status, Movements: movementViews(l.Movements)})
	}
	for _, l := range p.Fixed {
		f := FixedView{ItemView: NewItemView(l.Instance), DueOn: l.Due(), Status: l.Status,
			Movements: movementViews(l.Movements)}
		if l.Paid != nil {
			paid := l.Paid.Amount()
			f.Paid = &paid
		}
		v.Fixed = append(v.Fixed, f)
	}
	for _, l := range p.Savings {
		v.Savings = append(v.Savings, SavingView{ItemView: NewItemView(l.Instance)})
	}
	for _, c := range p.Caps {
		v.Caps = append(v.Caps, CapLineView{CapView: NewCapView(c.Cap), Spent: c.Spent.Amount(), Level: c.Level})
	}
	for i, q := range p.Quincenas {
		v.Quincenas[i] = QuincenaView{From: q.From, To: q.To, Free: q.Free.Amount(), Spent: q.Spent.Amount(),
			IncomeExpected: q.IncomeExpected.Amount(), FixedDue: q.FixedDue.Amount()}
	}
	for _, a := range p.Alerts {
		v.Alerts = append(v.Alerts, AlertView{Kind: a.Kind, Level: a.Level, CategoryID: a.CategoryID,
			ItemID: a.ItemID, DueOn: a.DueOn})
	}
	return v
}
