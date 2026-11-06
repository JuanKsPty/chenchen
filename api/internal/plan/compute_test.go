package plan_test

import (
	"fmt"
	"math/rand/v2"
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

// These need no database: Compute and Match are pure, so their promises are
// checked over many made-up months.

// texts are what made-up movements say, and items look for: two that share a
// prefix, a person, an insurer and its tax, and a shop no item names.
var texts = []string{"ACH EMPRESA X", "ACH EMPRESA X BONO", "YAPPY A PERSONA UNO", "SEGURO X", "ITBMS", "SUPER Ñandú"}

var kindsOut = []string{store.KindExpense, store.KindFee, store.KindInterest}

// made is a made-up month: items around November 2026 and the movements of
// October to December, on two accounts and two categories (one the other's
// child), with whatever today is.
type made struct {
	today     civil.Date
	items     []store.PlanItem
	versions  []store.PlanVersion
	movements []store.PlanMovement
	caps      []plan.Cap
	parents   map[uuid.UUID]uuid.UUID
}

func makeMonth(r *rand.Rand) made {
	var m made
	m.today = civil.Date{Year: 2026, Month: time.October, Day: 1}.AddDays(r.IntN(92))
	parent, child := uuid.New(), uuid.New()
	m.parents = map[uuid.UUID]uuid.UUID{child: parent}
	categories := []*uuid.UUID{nil, &parent, &child}
	accounts := []uuid.UUID{uuid.New(), uuid.New()}
	if r.IntN(2) == 0 {
		m.caps = append(m.caps, plan.Cap{CategoryID: parent, Version: plan.Version{From: nov26, Values: plan.Values{Amount: money.Cents(1 + r.IntN(20000))}}})
	}
	for i := range r.IntN(6) {
		item := store.PlanItem{ID: uuid.New(), Kind: []string{plan.KindIncome, plan.KindFixed, plan.KindSaving}[r.IntN(3)],
			Name: fmt.Sprintf("item %d", i), CreatedAt: time.Unix(int64(i), 0)}
		if item.Kind != plan.KindSaving && r.IntN(4) > 0 {
			item.MatchText = strings.ToLower(texts[r.IntN(len(texts))])
		}
		m.items = append(m.items, item)
		// One or two versions over October to December.
		from := oct26.AddMonths(r.IntN(3))
		to := plan.Month{}
		if r.IntN(2) == 0 {
			to = from.AddMonths(r.IntN(2))
		}
		day := 0
		if item.Kind != plan.KindSaving || r.IntN(2) == 0 {
			day = 1 + r.IntN(31)
		}
		m.versions = append(m.versions, store.PlanVersion{ID: uuid.New(), ItemID: item.ID, Kind: item.Kind,
			From: from.First(), To: firstOrZero(to), Amount: money.Cents(100 + r.IntN(100000)), Day: day})
		if !to.IsZero() && r.IntN(2) == 0 {
			m.versions = append(m.versions, store.PlanVersion{ID: uuid.New(), ItemID: item.ID, Kind: item.Kind,
				From: to.AddMonths(1).First(), Amount: money.Cents(100 + r.IntN(100000)), Day: max(day, 1)})
		}
	}
	for range r.IntN(40) {
		mv := store.PlanMovement{ID: uuid.New(), Origin: store.OriginStatement, AccountID: accounts[r.IntN(2)],
			BookedOn:     civil.Date{Year: 2026, Month: time.October, Day: 1}.AddDays(r.IntN(92)),
			Description:  texts[r.IntN(len(texts))],
			BankCategory: []string{"", "COMPRA"}[r.IntN(2)], CategoryID: categories[r.IntN(3)]}
		switch r.IntN(4) {
		case 0:
			mv.Kind, mv.Amount = store.KindIncome, money.Cents(1+r.IntN(100000))
		case 1:
			mv.Kind, mv.Amount = store.KindRefund, money.Cents(1+r.IntN(5000))
		default:
			mv.Kind, mv.Amount = kindsOut[r.IntN(3)], -money.Cents(1+r.IntN(100000))
		}
		if r.IntN(5) == 0 {
			mv.Origin, mv.BankCategory = store.OriginCapture, ""
			mv.Kind, mv.Amount = store.KindExpense, -money.Cents(1+r.IntN(5000))
		}
		m.movements = append(m.movements, mv)
	}
	return m
}

func firstOrZero(m plan.Month) civil.Date {
	if m.IsZero() {
		return civil.Date{}
	}
	return m.First()
}

func (m made) input(rec plan.Recognition) plan.Input {
	instances := plan.Instances(m.items, m.versions, oct26, dec26)
	return plan.Input{Month: nov26, Today: m.today, Instances: instances, Caps: m.caps, Parents: m.parents,
		Movements: m.movements, Recognition: rec}
}

func TestComputeSplitsTheMonthsSpendingWithoutLosingOrAddingACent(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 2026))
	for n := range 2000 {
		m := makeMonth(r)
		instances := plan.Instances(m.items, m.versions, oct26, dec26)
		// Any recognition at all, even one Match would never make: a
		// movement paying a month next door, a refund paying alone.
		rec := plan.Recognition{Paid: make([]int, len(m.movements))}
		for i := range rec.Paid {
			rec.Paid[i] = r.IntN(len(instances)+1) - 1
		}
		if r.IntN(3) == 0 {
			rec = plan.Match(instances, m.movements)
		}
		p := plan.Compute(m.input(rec))
		s := p.Summary

		var spending, income money.Cents
		for _, mv := range m.movements {
			if plan.MonthOf(mv.BookedOn) != nov26 {
				continue
			}
			switch {
			case slices.Contains(store.SpendKinds, mv.Kind):
				spending -= mv.Amount
			case mv.Kind == store.KindIncome:
				income += mv.Amount
			}
		}
		var fixed, paidFixed, expected, fixedDue, incomeDue money.Cents
		for _, l := range p.Fixed {
			fixed += l.Committed
			if l.Paid != nil {
				paidFixed += *l.Paid
				if l.Committed != *l.Paid || l.Status != plan.StatusOut {
					t.Fatalf("#%d: a payment that went out commits %s, is %s; paid %s", n, l.Committed, l.Status, *l.Paid)
				}
			} else if l.Committed != l.Planned() {
				t.Fatalf("#%d: a payment still to go out commits %s, planned %s", n, l.Committed, l.Planned())
			}
		}
		for _, l := range p.Income.Items {
			expected += l.Planned()
		}
		for _, q := range p.Quincenas {
			fixedDue += q.FixedDue
			incomeDue += q.IncomeExpected
		}
		for _, check := range []struct {
			what      string
			got, want money.Cents
		}{
			{"the spending, against Gastos", p.Spending, spending},
			{"what was free plus what items took, against the spending", s.Spent + p.Charged, p.Spending},
			{"the quincenas' spending, against what was spent", p.Quincenas[0].Spent + p.Quincenas[1].Spent, s.Spent},
			{"the income, against the month's flow", p.Income.Actual, income},
			{"the payments, against their lines", s.Fixed, fixed},
			{"the payments, against their quincenas", fixedDue, s.Fixed},
			{"the income planned, against its lines", s.Income, expected},
			{"the income planned, against its quincenas", incomeDue, s.Income},
			{"what is free", s.Free, s.Income - s.Fixed - s.Savings - s.Purchases},
			{"what is left", s.Left, s.Free - s.Spent},
			{"the quincenas' share of what is free", p.Quincenas[0].Free + p.Quincenas[1].Free, max(s.Free, 0)},
		} {
			if check.got != check.want {
				t.Fatalf("#%d: %s: %s, want %s", n, check.what, check.got, check.want)
			}
		}
		if s.Purchases != 0 {
			t.Fatalf("#%d: planned purchases set aside %s before they are kept", n, s.Purchases)
		}
		if s.PerDay != nil && (s.Left <= 0 || *s.PerDay*money.Cents(s.DaysLeft) > s.Left ||
			(*s.PerDay+1)*money.Cents(s.DaysLeft) <= s.Left) {
			t.Fatalf("#%d: %s a day over %d days, with %s left", n, *s.PerDay, s.DaysLeft, s.Left)
		}
		if (s.PerDay == nil) != (s.Left <= 0 || s.DaysLeft == 0) {
			t.Fatalf("#%d: per day is %v with %s left over %d days", n, s.PerDay, s.Left, s.DaysLeft)
		}
		if p.State != plan.StateCurrent && len(p.Alerts) > 0 {
			t.Fatalf("#%d: a %s month alerts %+v", n, p.State, p.Alerts)
		}
		if s.Free <= 0 && s.Level != plan.LevelNoFree {
			t.Fatalf("#%d: nothing free is %s", n, s.Level)
		}
		// The one cap covers both categories, a parent and its child, so it
		// and what has no category hold every free movement once.
		if len(p.Caps) > 0 && p.Uncategorized.Spent+p.Caps[0].Spent != s.Spent {
			t.Fatalf("#%d: the cap and the uncategorized hold %s+%s of %s spent", n, p.Caps[0].Spent, p.Uncategorized.Spent, s.Spent)
		}
	}
}

func TestMatchOnlyRecognisesWhatTheItemCouldBe(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 2026))
	for n := range 2000 {
		m := makeMonth(r)
		instances := plan.Instances(m.items, m.versions, oct26, dec26)
		rec := plan.Match(instances, m.movements)
		paidBy := map[int][]store.PlanMovement{}
		for i, k := range rec.Paid {
			if k != plan.Unpaid {
				paidBy[k] = append(paidBy[k], m.movements[i])
			}
		}
		// Each instance was paid by one movement that has its text and fits
		// it, and, for a payment, by that movement's companion lines.
		for k, paid := range paidBy {
			inst := instances[k]
			if !slices.ContainsFunc(paid, func(own store.PlanMovement) bool {
				return inst.Item.MatchText != "" && hasText(own, inst) && fitsByAmountAndDay(own, inst) &&
					!slices.ContainsFunc(paid, func(other store.PlanMovement) bool {
						return other.ID != own.ID && (inst.Item.Kind != plan.KindFixed || !isCompanion(own, other, inst))
					})
			}) {
				t.Fatalf("#%d: %s %q of %s planned at %s on %s was paid by %+v",
					n, inst.Item.Kind, inst.Item.MatchText, inst.Month, inst.Planned(), inst.Due(), paid)
			}
		}

		// The order the movements arrive in changes nothing.
		shuffled := slices.Clone(m.movements)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		again := plan.Match(instances, shuffled)
		paid := map[uuid.UUID]int{}
		for i, k := range rec.Paid {
			paid[m.movements[i].ID] = k
		}
		for i, k := range again.Paid {
			if paid[shuffled[i].ID] != k {
				t.Fatalf("#%d: in another order %q paid %d, not %d", n, shuffled[i].Description, k, paid[shuffled[i].ID])
			}
		}
	}
}

// fitsByAmountAndDay is the rule an item's own movement keeps, besides its
// text: a quarter of the plan (a dollar at least) and, for a paycheck, a week
// of its day; for a payment, its own month.
func fitsByAmountAndDay(mv store.PlanMovement, inst plan.Instance) bool {
	margin := max(100, inst.Planned()*25/100)
	switch inst.Item.Kind {
	case plan.KindIncome:
		days := mv.BookedOn.Time(time.UTC).Sub(inst.Due().Time(time.UTC)).Hours() / 24
		return mv.Kind == store.KindIncome && mv.Amount > 0 && days >= -7 && days <= 7 &&
			mv.Amount-inst.Planned() <= margin && inst.Planned()-mv.Amount <= margin
	case plan.KindFixed:
		return slices.Contains(kindsOut, mv.Kind) && mv.Amount < 0 && plan.MonthOf(mv.BookedOn) == inst.Month &&
			-mv.Amount-inst.Planned() <= margin && inst.Planned()+mv.Amount <= margin
	}
	return false
}

// hasText reports whether a movement says what the item looks for, as a rule
// compares it.
func hasText(mv store.PlanMovement, inst plan.Instance) bool {
	return strings.Contains(store.FoldRuleText(mv.BankCategory+" "+mv.Description), store.FoldRuleText(inst.Item.MatchText))
}

// isCompanion reports whether line could go along with payment as the small
// charge the bank made beside it.
func isCompanion(payment, line store.PlanMovement, inst plan.Instance) bool {
	return (line.Kind == store.KindFee || line.Kind == store.KindInterest) && line.Amount < 0 &&
		-line.Amount <= max(100, inst.Planned()/10) && line.AccountID == payment.AccountID && line.BookedOn == payment.BookedOn
}

func TestTwoInstancesTakeTheMovementsClosestToTheirDayFirst(t *testing.T) {
	item := func() store.PlanItem {
		return store.PlanItem{ID: uuid.New(), Kind: plan.KindIncome, Name: "Sueldo", MatchText: "ACH EMPRESA X"}
	}
	first, second := item(), item()
	versions := []store.PlanVersion{
		{ItemID: first.ID, Kind: plan.KindIncome, From: nov26.First(), Amount: 65000, Day: 10},
		{ItemID: second.ID, Kind: plan.KindIncome, From: nov26.First(), Amount: 65000, Day: 13},
	}
	instances := plan.Instances([]store.PlanItem{first, second}, versions, nov26, nov26)
	line := func(day int) store.PlanMovement {
		return store.PlanMovement{ID: uuid.New(), Kind: store.KindIncome, Amount: 65000, Description: "ACH EMPRESA X",
			BookedOn: nov26.DayIn(day)}
	}
	// The 12th is a day from the 13th and two from the 10th, so it goes to
	// the 13th, and the 16th, the last one free, to the 10th.
	movements := []store.PlanMovement{line(16), line(12)}
	rec := plan.Match(instances, movements)
	got := []int{rec.Paid[0], rec.Paid[1]}
	if instances[got[0]].Item.ID != first.ID || instances[got[1]].Item.ID != second.ID {
		t.Errorf("the 16th paid %v and the 12th %v", instances[got[0]].Due(), instances[got[1]].Due())
	}
}

func TestEachChargeOnTheSameCardAndDayKeepsItsOwnTax(t *testing.T) {
	card := uuid.New()
	line := func(cents money.Cents, text string) store.PlanMovement {
		return store.PlanMovement{ID: uuid.New(), AccountID: card, BookedOn: nov26.DayIn(18), Amount: -cents,
			Kind: store.KindFee, Description: text}
	}
	seguro, proteccion := line(350, "SEGURO X"), line(250, "PROTECCION Y")
	seguroTax, proteccionTax := line(18, "ITBMS"), line(13, "ITBMS")
	// However each was planned, with its tax or without, each charge takes
	// its own: the one that brings it to its plan, or else the bigger tax
	// with the bigger charge.
	for _, tt := range []struct {
		what               string
		seguro, proteccion money.Cents
	}{
		{"both with their tax", 368, 263},
		{"both without it", 350, 250},
		{"the bigger with it", 368, 250},
		{"the smaller with it", 350, 263},
	} {
		items := []store.PlanItem{
			{ID: uuid.New(), Kind: plan.KindFixed, Name: "Seguro", MatchText: "SEGURO X"},
			{ID: uuid.New(), Kind: plan.KindFixed, Name: "Protección", MatchText: "PROTECCION Y"},
		}
		instances := plan.Instances(items, []store.PlanVersion{
			{ItemID: items[0].ID, Kind: plan.KindFixed, From: nov26.First(), Amount: tt.seguro, Day: 18},
			{ItemID: items[1].ID, Kind: plan.KindFixed, From: nov26.First(), Amount: tt.proteccion, Day: 18},
		}, nov26, nov26)
		want := map[uuid.UUID]uuid.UUID{seguro.ID: items[0].ID, seguroTax.ID: items[0].ID,
			proteccion.ID: items[1].ID, proteccionTax.ID: items[1].ID}
		movements := []store.PlanMovement{seguro, seguroTax, proteccion, proteccionTax}
		for range 2 {
			rec := plan.Match(instances, movements)
			for i, k := range rec.Paid {
				if k == plan.Unpaid || instances[k].Item.ID != want[movements[i].ID] {
					t.Errorf("%s: %s %s went with %v", tt.what, movements[i].Description, -movements[i].Amount, k)
				}
			}
			slices.Reverse(movements)
		}
	}
}
