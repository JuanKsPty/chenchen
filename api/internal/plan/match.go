package plan

import (
	"bytes"
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

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

// What the plan offers for a month it has not recognised yet, so the owner
// can say which movement it was with one tap.
const (
	// maxCandidates is how many movements are offered.
	maxCandidates = 3
	// candidateDays is how many days from its day a payment may be booked
	// and still be offered: late enough to fall in the month after.
	candidateDays = 5
	// candidatePercent is how far from what was planned, in percent of it, a
	// movement may be and be offered for a payment with no text to look for.
	candidatePercent = 10
)

// Unpaid marks a movement that paid no month of any item.
const Unpaid = -1

// HintReason says why a movement is pointed at.
type HintReason string

// HintTransfer: a transfer out with the payment's text. Banks print some
// payments, such as one to another person's account, as transfers, which the
// plan never counts; the owner can say it is not one, and it is then
// recognised and counted, once.
const HintTransfer HintReason = "transfer"

// Hint is a movement the plan cannot recognise as an instance, but that may
// be it.
type Hint struct {
	Movement store.PlanMovement
	Reason   HintReason
}

// Recognition says which month of which item each movement paid, and what
// the owner said about each month.
type Recognition struct {
	// Paid[i] is the index, among the instances it was worked out from, of
	// the one movement i paid, or Unpaid.
	Paid []int
	// Assigned[i] is the link by which the owner said movement i paid it;
	// uuid.Nil when the plan recognised it by itself.
	Assigned []uuid.UUID
	// MarkedPaid[k] is the link by which the owner said instance k was paid
	// where the app cannot see; uuid.Nil when they did not.
	MarkedPaid []uuid.UUID
	// Candidates[k] are the movements instance k could be, closest first, up
	// to maxCandidates, while it is still to be paid; Hints[k] a transfer it
	// could be, or nil.
	Candidates [][]int
	Hints      []*Hint

	// bought[i] says movement i bought a planned purchase, so it pays and is
	// offered for nothing else.
	bought []bool
}

// free reports whether movement i is still free to be recognised: it paid no
// instance yet and bought no purchase.
func (r Recognition) free(i int) bool { return r.Paid[i] == Unpaid && !at(r.bought, i) }

// paid is the instance movement i paid, or Unpaid.
func (r Recognition) paid(i int) int {
	if i < 0 || i >= len(r.Paid) {
		return Unpaid
	}
	return r.Paid[i]
}

// assigned is the link by which the owner said movement i paid, or uuid.Nil.
func (r Recognition) assigned(i int) uuid.UUID { return at(r.Assigned, i) }

// markedPaid is the link by which the owner said instance k was paid
// outside, or uuid.Nil.
func (r Recognition) markedPaid(k int) uuid.UUID { return at(r.MarkedPaid, k) }

func (r Recognition) candidates(k int) []int { return at(r.Candidates, k) }

func (r Recognition) hint(k int) *Hint { return at(r.Hints, k) }

// at is s[i], or the zero value when s does not reach i: a Recognition built
// by hand may leave out what it does not say.
func at[T any](s []T, i int) T {
	var zero T
	if i < 0 || i >= len(s) {
		return zero
	}
	return s[i]
}

// told is a pair the owner said is not a match: an instance and a movement.
type told struct {
	inst int
	mov  uuid.UUID
}

// Match recognises, among movements, the paychecks and payments of
// instances, the way the owner would reading their statement. What the owner
// said in links comes first:
//
//   - A movement they said paid a month pays it, whatever its text or
//     amount, if it is still a movement that can: income for a paycheck,
//     spending for a payment, booked within linkMonths of that month. A
//     noted purchase is followed to its line once printed; of the lines it
//     could be, it is the one another link of the same month names, or else
//     takes the first one free.
//   - A movement they said did not pay a month is never recognised as it.
//     When they said both about one movement, as when the purchase they
//     named turns out to be the line they refused, the newer word holds.
//   - A month they said was paid outside is paid, and keeps what was
//     planned; a movement may still be recognised as it.
//   - When two links say one movement paid two things, the one said first
//     holds.
//
// Then the plan recognises the rest by itself:
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
//
// Last, each month still to be paid is offered up to maxCandidates
// movements it could be, and a payment, a transfer out of its month with its
// text (transfers), which the bank may have printed it as.
func Match(instances []Instance, movements []store.PlanMovement, links []Link, transfers []store.PlanMovement) Recognition {
	return MatchBeside(instances, movements, links, transfers, nil)
}

// MatchBeside is Match in a plan with planned purchases: the movements that
// bought one (bought, as RecognisePurchases found them) are the purchase's,
// so no paycheck or payment is recognised as one of them, takes one along as
// a companion line, or is offered one, even when the owner said it paid it.
// RecognisePurchases never takes a movement that a payment of its month would
// recognise, nor one a link says pays another item, so this only keeps a
// payment from claiming what the owner said bought a purchase.
func MatchBeside(instances []Instance, movements []store.PlanMovement, links []Link, transfers []store.PlanMovement,
	bought []Bought) Recognition {
	rec := Recognition{
		Paid: make([]int, len(movements)), Assigned: make([]uuid.UUID, len(movements)),
		MarkedPaid: make([]uuid.UUID, len(instances)), Candidates: make([][]int, len(instances)),
		Hints: make([]*Hint, len(instances)), bought: make([]bool, len(movements)),
	}
	for i := range rec.Paid {
		rec.Paid[i] = Unpaid
	}
	purchased := map[uuid.UUID]bool{}
	for _, b := range bought {
		for _, mv := range b.Movements {
			purchased[mv.ID] = true
		}
	}
	for i, m := range movements {
		rec.bought[i] = purchased[m.ID]
	}
	texts := make([]string, len(movements))
	for i, m := range movements {
		// The text a rule is matched against: a line's bank category and
		// description, or the merchant of a noted purchase.
		texts[i] = store.FoldRuleText(m.BankCategory + " " + m.Description)
	}
	taken, rejected := rec.follow(instances, movements, links)

	type pair struct {
		inst, mov int
		days      int
		off       money.Cents
	}
	var pairs []pair
	for k, inst := range instances {
		needle := needleOf(inst)
		if needle == "" || taken[k] {
			continue
		}
		for i, m := range movements {
			if !rec.free(i) || rejected[told{k, m.ID}] || !strings.Contains(texts[i], needle) {
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
	for _, p := range pairs {
		if taken[p.inst] || rec.Paid[p.mov] != Unpaid {
			continue
		}
		taken[p.inst] = true
		rec.Paid[p.mov] = p.inst
	}

	// The companion lines, once every payment has its own, whether the owner
	// said which it was or the plan saw it: a line another payment recognised
	// by its text stays with that one. A line that only one payment could take
	// goes with it. One that more could take, as when two insurance payments
	// are charged on the same card and day, each with its tax, goes first to a
	// payment it brings to exactly what was planned. The rest go biggest
	// first, each to the payment with the fewest lines so far, since a charge
	// comes with one tax, and then to the bigger charge, which carries the
	// bigger tax. Whichever payment a line joins, the month spends it once.
	var payments []pair
	outflow := make([]money.Cents, len(instances))
	for i, k := range rec.Paid {
		if k == Unpaid || instances[k].Item.Kind != KindFixed {
			continue
		}
		outflow[k] -= movements[i].Amount
		if isPayment(movements[i].Kind) && movements[i].Amount < 0 {
			payments = append(payments, pair{inst: k, mov: i})
		}
	}
	var lines []int
	for j, m := range movements {
		if rec.free(j) && (m.Kind == store.KindFee || m.Kind == store.KindInterest) && m.Amount < 0 {
			lines = append(lines, j)
		}
	}
	slices.SortFunc(lines, func(a, b int) int {
		return cmp.Or(cmp.Compare(movements[a].Amount, movements[b].Amount),
			bytes.Compare(movements[a].ID[:], movements[b].ID[:]))
	})
	joined := make([]int, len(instances))
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
					rejected[told{p.inst, line.ID}] ||
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

	rec.offer(instances, movements, texts, transfers, rejected)
	return rec
}

// follow applies what the owner said in links, the earliest said first, and
// returns the instances a movement they named now pays, and the pairs they
// said are not a match. A link about a month not among instances says
// nothing here.
func (r *Recognition) follow(instances []Instance, movements []store.PlanMovement, links []Link) (taken []bool, rejected map[told]bool) {
	type key struct {
		item  uuid.UUID
		month Month
	}
	byMonth := make(map[key]int, len(instances))
	for k, inst := range instances {
		byMonth[key{inst.Item.ID, inst.Month}] = k
	}
	instanceOf := func(l Link) (int, bool) {
		k, ok := byMonth[key{l.ItemID, l.Month}]
		return k, ok
	}
	index := make(map[uuid.UUID]int, len(movements))
	for i, m := range movements {
		index[m.ID] = i
	}
	// rejectedAt is the latest link by which the owner said a movement is not
	// an instance. A noted purchase is followed to its line before this, so
	// "not this line" said after "this purchase" counts against the purchase:
	// the newer word holds.
	rejectedAt := map[told]int{}
	// namedFor are the instances whose links name a line themselves, Unpaid
	// for a month not among instances. A noted purchase that several lines
	// could be is the line named for its own instance, and leaves one named
	// for another to that one.
	namedFor := map[uuid.UUID][]int{}
	for n, l := range links {
		k, ok := instanceOf(l)
		switch {
		case l.Decision == DecisionReject && ok:
			for _, id := range l.names() {
				rejectedAt[told{k, id}] = n
			}
		case l.Decision == DecisionAssign && l.Movement != uuid.Nil:
			if !ok {
				k = Unpaid
			}
			namedFor[l.Movement] = append(namedFor[l.Movement], k)
		}
	}
	rejected = make(map[told]bool, len(rejectedAt))
	for p := range rejectedAt {
		rejected[p] = true
	}

	taken = make([]bool, len(instances))
	for n, l := range links {
		k, ok := instanceOf(l)
		if !ok {
			continue
		}
		switch l.Decision {
		case DecisionPaid:
			if r.MarkedPaid[k] == uuid.Nil {
				r.MarkedPaid[k] = l.ID
			}
		case DecisionAssign:
			// usable is movement id when it can pay the instance: it is free,
			// of a kind that pays it, booked within linkMonths of its month,
			// and the owner did not say after this link that it is not it.
			// Taken already, it stays with the link said first.
			usable := func(id uuid.UUID) int {
				i, ok := index[id]
				if !ok || !r.free(i) || !canPay(instances[k], movements[i]) ||
					abs(MonthOf(movements[i].BookedOn).Sub(l.Month)) > linkMonths {
					return Unpaid
				}
				if at, said := rejectedAt[told{k, id}]; said && at > n {
					return Unpaid
				}
				return i
			}
			i := Unpaid
			if l.Movement != uuid.Nil {
				i = usable(l.Movement)
			}
			// A noted purchase several lines could be is the one among them
			// that another link of the same month names: the owner said which
			// line it was, and the purchase never pays the month a second
			// time through another line. Otherwise it takes the first line
			// free that the owner did not refuse for the instance, one that no
			// other link names before any.
			if own := slices.IndexFunc(l.Lines, func(id uuid.UUID) bool {
				return slices.Contains(namedFor[id], k)
			}); own >= 0 {
				i = usable(l.Lines[own])
			} else {
				for _, others := range []bool{false, true} {
					for _, id := range l.Lines {
						if i == Unpaid && !rejected[told{k, id}] && (others || len(namedFor[id]) == 0) {
							i = usable(id)
						}
					}
				}
			}
			if i != Unpaid {
				r.Paid[i], r.Assigned[i] = k, l.ID
				taken[k] = true
			}
		}
	}
	return taken, rejected
}

// canPay reports whether movement m can pay instance inst at all: a paycheck
// is income, and a payment spending, a refund included, which lowers what it
// cost.
func canPay(inst Instance, m store.PlanMovement) bool {
	switch inst.Item.Kind {
	case KindIncome:
		return m.Kind == store.KindIncome
	case KindFixed:
		return slices.Contains(store.SpendKinds, m.Kind)
	}
	return false
}

// offer works out, for each paycheck and payment still to be paid, the
// movements it could be, which no item took and the owner did not say are
// not it, closest to its day first, then to its amount:
//
//   - a paycheck, any income within a quarter of what was planned (a dollar
//     at least) and a week of its day, whatever its text;
//   - a payment with a text to look for, any outflow with that text in its
//     month or within candidateDays of its day, whatever its amount;
//   - a payment with none, any outflow within candidatePercent of what was
//     planned and candidateDays of its day.
//
// A payment with a text also gets, as a hint, the transfer out of its month
// with that text closest to what was planned.
func (r *Recognition) offer(instances []Instance, movements []store.PlanMovement, texts []string,
	transfers []store.PlanMovement, rejected map[told]bool) {
	settled := make([]bool, len(instances))
	for i, k := range r.Paid {
		if k == Unpaid {
			continue
		}
		m := movements[i]
		settled[k] = settled[k] || instances[k].Item.Kind == KindIncome || m.Amount < 0
	}
	type offered struct {
		mov  store.PlanMovement
		i    int
		days int
		off  money.Cents
	}
	closest := func(a, b offered) int {
		return cmp.Or(cmp.Compare(a.days, b.days), cmp.Compare(a.off, b.off),
			a.mov.BookedOn.Compare(b.mov.BookedOn), bytes.Compare(a.mov.ID[:], b.mov.ID[:]))
	}
	for k, inst := range instances {
		if settled[k] || r.MarkedPaid[k] != uuid.Nil {
			continue
		}
		needle, planned, due := needleOf(inst), inst.Planned(), inst.Due()
		var found []offered
		for i, m := range movements {
			if !r.free(i) || rejected[told{k, m.ID}] {
				continue
			}
			days := abs(daysBetween(due, m.BookedOn))
			ok := false
			switch inst.Item.Kind {
			case KindIncome:
				ok = m.Kind == store.KindIncome && m.Amount > 0 && days <= incomeWindow &&
					abs(m.Amount-planned) <= max(matchFloor, planned*matchPercent/100)
			case KindFixed:
				switch {
				case !isPayment(m.Kind) || m.Amount >= 0:
				case needle != "":
					ok = strings.Contains(texts[i], needle) && (MonthOf(m.BookedOn) == inst.Month || days <= candidateDays)
				default:
					ok = days <= candidateDays && abs(-m.Amount-planned) <= planned*candidatePercent/100
				}
			}
			if ok {
				found = append(found, offered{mov: m, i: i, days: days, off: abs(-m.Amount - planned)})
			}
		}
		slices.SortFunc(found, closest)
		for _, o := range found[:min(len(found), maxCandidates)] {
			r.Candidates[k] = append(r.Candidates[k], o.i)
		}

		if inst.Item.Kind != KindFixed || needle == "" {
			continue
		}
		var best *offered
		for _, t := range transfers {
			if t.Amount >= 0 || MonthOf(t.BookedOn) != inst.Month || rejected[told{k, t.ID}] ||
				!strings.Contains(store.FoldRuleText(t.BankCategory+" "+t.Description), needle) {
				continue
			}
			o := offered{mov: t, days: abs(daysBetween(due, t.BookedOn)), off: abs(-t.Amount - planned)}
			// The amount first: a transfer with the text that is about what
			// was planned says more than one nearer its day.
			if best == nil || cmp.Or(cmp.Compare(o.off, best.off), closest(o, *best)) < 0 {
				best = &o
			}
		}
		if best != nil {
			r.Hints[k] = &Hint{Movement: best.mov, Reason: HintTransfer}
		}
	}
}

// needleOf is the text an instance's movements contain, folded as a rule
// folds it; "" when the item has none.
func needleOf(inst Instance) string {
	return store.FoldRuleText(strings.TrimSpace(inst.Item.MatchText))
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
