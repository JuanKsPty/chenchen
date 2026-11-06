package plan

import (
	"bytes"
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// Which movement paid which month of which item is worked out each time the
// plan is read, never stored (ADR-0005): what the owner files, re-files or
// imports changes it the moment they look again, the way it changes Gastos.

const (
	// incomeWindow is how many days before or after its day a paycheck may
	// arrive and still be it. Enough for a payday that falls on a weekend or
	// a holiday; such a paycheck may land in the month next door and still
	// belong to the month it was expected in.
	incomeWindow = 7
	// matchPercent is how far a movement may be from what was planned, in
	// percent of it, and still be it: a payment that went up a little is the
	// same payment. matchFloor keeps that margin from vanishing on a small
	// amount.
	matchPercent             = 25
	matchFloor   money.Cents = 100
	// companionPercent is the most a companion line comes to, in percent of
	// what was planned: the tax a bank charges beside an insurance payment,
	// say. companionFloor keeps a small payment's tax from being left out.
	companionPercent             = 10
	companionFloor   money.Cents = 100
)

// Unpaid marks a movement that paid no month of any item.
const Unpaid = -1

// Recognition says which month of which item each movement paid.
type Recognition struct {
	// Paid[i] is the index, among the instances it was worked out from, of
	// the one movement i paid, or Unpaid.
	Paid []int
}

// paid is the instance movement i paid, or Unpaid.
func (r Recognition) paid(i int) int {
	if i < 0 || i >= len(r.Paid) {
		return Unpaid
	}
	return r.Paid[i]
}

// Match recognises, among movements, the paychecks and payments of
// instances, the way the owner would reading their statement:
//
//   - A paycheck is an income whose text contains the item's text, within a
//     quarter of what was planned (a dollar at least) and a week of its day.
//     It may arrive in the month before or after.
//   - A payment is an expense, a fee or interest, an outflow booked in its
//     month, whose text contains the item's text, within a quarter of what was
//     planned (a dollar at least).
//   - A payment takes along the small fee or interest line the bank charged
//     beside it, on the same account and day, such as the tax on an insurance
//     payment: at most a tenth of what was planned, a dollar at least. A line
//     two payments on that day could take goes to the one it brings to
//     exactly its plan, or else the bigger tax goes with the bigger charge.
//
// An item with no text is never recognised by itself: nothing says which of
// the month's movements it would be.
//
// Every month and movement goes to its closest partner first: the pairs are
// taken by how many days apart they are, then by how far the amount is from
// the plan, then by the movement's day and id, so two paychecks with the same
// text each go to the day they are nearest, and the order movements arrive
// in never changes who paid what. Each instance takes one movement besides
// its companion lines, and each movement pays one instance.
func Match(instances []Instance, movements []store.PlanMovement) Recognition {
	rec := Recognition{Paid: make([]int, len(movements))}
	for i := range rec.Paid {
		rec.Paid[i] = Unpaid
	}
	texts := make([]string, len(movements))
	for i, m := range movements {
		// The text a rule is matched against: a line's bank category and
		// description, or the merchant of a noted purchase.
		texts[i] = store.FoldRuleText(m.BankCategory + " " + m.Description)
	}

	type pair struct {
		inst, mov int
		days      int
		off       money.Cents
	}
	var pairs []pair
	for k, inst := range instances {
		needle := store.FoldRuleText(strings.TrimSpace(inst.Item.MatchText))
		if needle == "" {
			continue
		}
		for i, m := range movements {
			if !strings.Contains(texts[i], needle) {
				continue
			}
			if days, off, ok := fits(inst, m); ok {
				pairs = append(pairs, pair{inst: k, mov: i, days: days, off: off})
			}
		}
	}
	slices.SortFunc(pairs, func(a, b pair) int {
		ma, mb := movements[a.mov], movements[b.mov]
		ia, ib := instances[a.inst], instances[b.inst]
		return cmp.Or(
			cmp.Compare(a.days, b.days),
			cmp.Compare(a.off, b.off),
			ma.BookedOn.Compare(mb.BookedOn),
			bytes.Compare(ma.ID[:], mb.ID[:]),
			ia.Month.Compare(ib.Month),
			bytes.Compare(ia.Item.ID[:], ib.Item.ID[:]),
		)
	})

	taken := make([]bool, len(instances))
	var payments []pair
	for _, p := range pairs {
		if taken[p.inst] || rec.Paid[p.mov] != Unpaid {
			continue
		}
		taken[p.inst] = true
		rec.Paid[p.mov] = p.inst
		if instances[p.inst].Item.Kind == KindFixed {
			payments = append(payments, p)
		}
	}

	// The companion lines, once every payment has its own: a line another
	// payment recognised by its text stays with that one. A line that only
	// one payment could take goes with it. One that more could take, as when
	// two insurance payments are charged on the same card and day, each with
	// its tax, goes first to a payment it brings to exactly what was planned.
	// The rest go biggest first, each to the payment with the fewest lines so
	// far, since a charge comes with one tax, and then to the bigger charge,
	// which carries the bigger tax. Whichever payment a line joins, the month
	// spends it once.
	var lines []int
	for j, m := range movements {
		if rec.Paid[j] == Unpaid && (m.Kind == store.KindFee || m.Kind == store.KindInterest) && m.Amount < 0 {
			lines = append(lines, j)
		}
	}
	slices.SortFunc(lines, func(a, b int) int {
		return cmp.Or(cmp.Compare(movements[a].Amount, movements[b].Amount),
			bytes.Compare(movements[a].ID[:], movements[b].ID[:]))
	})
	outflow := make([]money.Cents, len(instances))
	joined := make([]int, len(instances))
	for _, p := range payments {
		outflow[p.inst] = -movements[p.mov].Amount
	}
	// before reports whether payment p takes a line ahead of payment q.
	before := func(p, q pair) bool {
		return cmp.Or(cmp.Compare(joined[q.inst], joined[p.inst]),
			cmp.Compare(movements[q.mov].Amount, movements[p.mov].Amount)) > 0
	}
	for _, exact := range []bool{true, false} {
		for _, j := range lines {
			line := movements[j]
			if rec.Paid[j] != Unpaid {
				continue
			}
			best := -1
			for n, p := range payments {
				charge := movements[p.mov]
				planned := instances[p.inst].Planned()
				if line.AccountID != charge.AccountID || line.BookedOn != charge.BookedOn ||
					-line.Amount > max(companionFloor, planned*companionPercent/100) ||
					exact && outflow[p.inst]-line.Amount != planned {
					continue
				}
				if best < 0 || before(p, payments[best]) {
					best = n
				}
			}
			if best >= 0 {
				k := payments[best].inst
				rec.Paid[j] = k
				outflow[k] -= line.Amount
				joined[k]++
			}
		}
	}
	return rec
}

// fits reports whether movement m could be instance inst by its kind, its
// amount and its day, with how many days and how much money apart they are.
// Its text is checked by the caller.
func fits(inst Instance, m store.PlanMovement) (days int, off money.Cents, ok bool) {
	planned := inst.Planned()
	margin := max(matchFloor, planned*matchPercent/100)
	days = abs(daysBetween(inst.Due(), m.BookedOn))
	switch inst.Item.Kind {
	case KindIncome:
		off = abs(m.Amount - planned)
		ok = m.Kind == store.KindIncome && m.Amount > 0 && days <= incomeWindow && off <= margin
	case KindFixed:
		off = abs(-m.Amount - planned)
		ok = isPayment(m.Kind) && m.Amount < 0 && MonthOf(m.BookedOn) == inst.Month && off <= margin
	}
	return days, off, ok
}

// isPayment reports whether a movement of this effective kind can pay a
// fixed payment: spending, but not a refund, which gives money back.
func isPayment(kind string) bool {
	return kind == store.KindExpense || kind == store.KindFee || kind == store.KindInterest
}

// daysBetween is how many days from a to b: negative when b comes first.
func daysBetween(a, b civil.Date) int {
	return int(b.Time(time.UTC).Sub(a.Time(time.UTC)) / (24 * time.Hour))
}

func abs[T ~int | ~int64](x T) T {
	if x < 0 {
		return -x
	}
	return x
}
