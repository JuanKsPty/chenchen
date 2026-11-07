package plan

import (
	"bytes"
	"cmp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// A planned purchase is something the owner means to buy by a month, with the
// money for it set aside in the plan beforehand. Set aside means only that:
// a share of the price comes out of what is free each month, as a figure of
// the plan. No balance moves, and no account is said to hold it (ADR-0005).

// How a planned purchase is set aside (plan_items.funding).
const (
	// FundingMonthly sets a share of the price aside every month, from the
	// month it is planned for to the month it is bought in.
	FundingMonthly = "monthly"
	// FundingThatMonth sets the whole price aside in the month it is bought.
	FundingThatMonth = "that_month"
	// FundingSavings pays it with what the owner already saved: it never
	// takes anything of what is free.
	FundingSavings = "savings"
)

// purchasePercent is how far an outflow may be from a purchase's price, in
// percent of it, and still be the purchase: a price that changed a little by
// the day it was bought.
const purchasePercent = 10

// Shares edits the shares of a planned purchase, its versions, from month m
// on, so that they come to price by month target: the shares of the months
// before m stay as they were, since they were set aside already, and what is
// left of the price is spread from m on.
//
//   - FundingMonthly spreads it over every month from m to target, the k-th
//     of n months taking ⌊left·(k+1)/n⌋ − ⌊left·k/n⌋, so the shares differ by
//     a cent at most and add up to exactly what was left.
//   - FundingThatMonth sets all of it aside in target.
//   - FundingSavings sets none of it aside.
//
// A month whose share comes to nothing has no version, as when what was set
// aside already covers the price. It returns the versions as they end up and
// the writes that get them there, as Split does.
func Shares(versions []Version, m Month, price money.Cents, target Month, funding string) (after []Version, ops []Op) {
	stored := slices.Clone(versions)
	slices.SortFunc(stored, func(a, b Version) int { return a.From.Compare(b.From) })
	var already money.Cents
	for _, v := range stored {
		if !v.From.Before(m) {
			continue
		}
		if v.Open() || !v.To.Before(m) {
			v.To = m.AddMonths(-1)
		}
		already += v.Amount * money.Cents(v.To.Sub(v.From)+1)
		after = append(after, v)
	}
	left := price - already
	switch {
	case left <= 0:
	case funding == FundingMonthly:
		n := money.Cents(target.Sub(m) + 1)
		for k := money.Cents(0); k < n; k++ {
			if share := left*(k+1)/n - left*k/n; share > 0 {
				month := m.AddMonths(int(k))
				after = append(after, Version{From: month, To: month, Values: Values{Amount: share}})
			}
		}
	case funding == FundingThatMonth && !target.Before(m):
		after = append(after, Version{From: target, To: target, Values: Values{Amount: left}})
	}
	return pair(stored, merge(after))
}

// Purchase is a planned purchase as the plan reads it: the item, and its
// shares, what each month sets aside of its price.
type Purchase struct {
	Item   *store.PlanItem
	Shares []Version
}

// Price is what it was planned to cost.
func (p Purchase) Price() money.Cents {
	if p.Item.Target == nil {
		return 0
	}
	return *p.Item.Target
}

// Start is the month it was first planned for, from which it is set aside
// and can be bought.
func (p Purchase) Start() Month { return monthOf(p.Item.StartMonth) }

// Target is the month it is to be bought in.
func (p Purchase) Target() Month { return monthOf(p.Item.TargetMonth) }

// last is the last month the plan shows it in, but for the month it is
// bought in: the month it is due, or, when the owner took it out of the plan
// before that, the last month they left it in.
func (p Purchase) last() Month {
	if end := monthOf(p.Item.EndMonth); !end.IsZero() && end.Before(p.Target()) {
		return end
	}
	return p.Target()
}

// lastBuyable is the last month it can be bought in: the one after it was
// due, since a purchase made at the end of its month may print in the next.
// Taken out of the plan, it can be bought in no month after the last the
// owner left it in: they no longer plan it, so an outflow then is something
// else, which goes to what was free.
func (p Purchase) lastBuyable() Month {
	last := p.Target().AddMonths(1)
	if end := monthOf(p.Item.EndMonth); !end.IsZero() && end.Before(last) {
		return end
	}
	return last
}

// Share is what month m sets aside for it.
func (p Purchase) Share(m Month) money.Cents {
	for _, v := range p.Shares {
		if v.Holds(m) {
			return v.Amount
		}
	}
	return 0
}

// SetAsideBefore is what the months before m set aside for it.
func (p Purchase) SetAsideBefore(m Month) money.Cents {
	var sum money.Cents
	for _, v := range p.Shares {
		if !v.From.Before(m) {
			continue
		}
		last := v.To
		if v.Open() || !last.Before(m) {
			last = m.AddMonths(-1)
		}
		sum += v.Amount * money.Cents(last.Sub(v.From)+1)
	}
	return sum
}

// canBeBoughtIn reports whether a movement booked in month m can have bought
// it: from the month it was planned for to lastBuyable.
func (p Purchase) canBeBoughtIn(m Month) bool {
	return !m.Before(p.Start()) && !m.After(p.lastBuyable())
}

// instance is the purchase in month m, with the values that hold for all of
// it: its price, and the months from when it was planned to when it is due.
func (p Purchase) instance(m Month) Instance {
	return Instance{Item: p.Item, Month: m,
		Version: Version{From: p.Start(), To: p.Target(), Values: Values{Amount: p.Price()}}}
}

// Purchases are the planned purchases among items, in the order the items
// were made, each with its shares among versions.
func Purchases(items []store.PlanItem, versions []store.PlanVersion) []Purchase {
	var out []Purchase
	for i := range items {
		if items[i].Kind != KindPurchase {
			continue
		}
		p := Purchase{Item: &items[i]}
		for _, v := range versions {
			if v.ItemID == p.Item.ID {
				p.Shares = append(p.Shares, Version{ID: v.ID, From: MonthOf(v.From), To: monthOf(v.To),
					Values: Values{Amount: v.Amount, Day: v.Day}})
			}
		}
		out = append(out, p)
	}
	slices.SortStableFunc(out, func(a, b Purchase) int {
		return cmp.Or(a.Item.CreatedAt.Compare(b.Item.CreatedAt), bytes.Compare(a.Item.ID[:], b.Item.ID[:]))
	})
	return out
}

// Bought is how a planned purchase was recognised.
type Bought struct {
	// Month is the month it was bought in, or the zero Month while it was
	// not: the month the owner said it was bought in, or the one the outflow
	// the plan recognised was booked in.
	Month Month
	// On is the day the first outflow that bought it was booked.
	On civil.Date
	// Paid is what went out for it, a refund lowering it.
	Paid money.Cents
	// Movements are the ones that bought it, by day. Candidates are, while it
	// was not bought, the outflows it could be, closest to its price first.
	Movements  []LineMovement
	Candidates []store.PlanMovement
}

// RecognisePurchases works out, for each of purchases, whether it was bought,
// when and for how much, from movements: the outflows near the price of each
// over the months it can be bought in, and any movement a link names. What
// the owner said comes first:
//
//   - A movement they said bought it did, whatever its text or amount, if it
//     is spending booked within linkMonths of the month they said, and that
//     month is one it can be bought in. The purchase was bought in the
//     earliest such month that has an outflow; a refund on its own buys
//     nothing. A noted purchase is followed to its line, or to the first
//     line it could be that is free.
//   - A movement they said did not buy it never is it, in any month, unless
//     they said it did afterwards: the newer word holds.
//
// Then a purchase nobody said anything about is recognised by itself: an
// expense booked in a month it can be bought in, within purchasePercent of
// its price, whose text contains the item's or whose category is the item's
// or a child of it (any, when the item has neither). It is taken only when it
// is the one outflow that could be the purchase; otherwise up to
// maxCandidates are offered.
//
// The result is the same whichever month is read, so a purchase bought in
// one month is bought in the eyes of every other. That is why a movement that
// pays something else is never one: one the owner said paid another item is
// never, and one that a payment of its month (instances) would take for its
// text and amount, which every month's plan recognises the same way, is
// never recognised by itself; the owner's word still takes it, and that
// payment then waits for another (MatchBeside). Each movement buys one
// purchase, the earliest made first. That is also why purchases are all the
// owner's, never only those around the month read: two purchases can be the
// same movement, and with one of them left out the other would take it.
func RecognisePurchases(purchases []Purchase, instances []Instance, movements []store.PlanMovement, links []Link,
	parents map[uuid.UUID]uuid.UUID) []Bought {
	out := make([]Bought, len(purchases))
	byItem := make(map[uuid.UUID]int, len(purchases))
	for k, p := range purchases {
		byItem[p.Item.ID] = k
	}
	index := make(map[uuid.UUID]int, len(movements))
	texts := make([]string, len(movements))
	for i, m := range movements {
		index[m.ID] = i
		texts[i] = store.FoldRuleText(m.BankCategory + " " + m.Description)
	}

	// saidElsewhere are the movements the owner said pay another item, and
	// paysElsewhere those a payment of their month would take by itself.
	saidElsewhere := make([]bool, len(movements))
	for _, l := range links {
		if _, ours := byItem[l.ItemID]; ours || l.Decision != DecisionAssign {
			continue
		}
		for _, id := range l.names() {
			if i, ok := index[id]; ok {
				saidElsewhere[i] = true
			}
		}
	}
	paysElsewhere := make([]bool, len(movements))
	for i, m := range movements {
		for _, inst := range instances {
			if inst.Item.Kind != KindFixed || MonthOf(m.BookedOn) != inst.Month {
				continue
			}
			if needle := needleOf(inst); needle != "" && strings.Contains(texts[i], needle) {
				if _, _, ok := fits(inst, m); ok {
					paysElsewhere[i] = true
				}
			}
		}
	}

	type refused struct {
		purchase int
		mov      uuid.UUID
	}
	// rejectedAt is the latest link by which the owner said a movement is not
	// a purchase.
	rejectedAt := map[refused]int{}
	for n, l := range links {
		if k, ours := byItem[l.ItemID]; ours && l.Decision == DecisionReject {
			for _, id := range l.names() {
				rejectedAt[refused{k, id}] = n
			}
		}
	}
	boughtBy := make([]int, len(movements))
	for i := range boughtBy {
		boughtBy[i] = Unpaid
	}
	free := func(i int) bool { return boughtBy[i] == Unpaid && !saidElsewhere[i] }

	linked := make([]bool, len(purchases))
	for n, l := range links {
		k, ours := byItem[l.ItemID]
		if !ours || l.Decision != DecisionAssign || !purchases[k].canBeBoughtIn(l.Month) {
			continue
		}
		usable := func(id uuid.UUID) int {
			i, ok := index[id]
			if !ok || !free(i) || !slices.Contains(store.SpendKinds, movements[i].Kind) ||
				abs(MonthOf(movements[i].BookedOn).Sub(l.Month)) > linkMonths {
				return Unpaid
			}
			if at, no := rejectedAt[refused{k, id}]; no && at > n {
				return Unpaid
			}
			return i
		}
		i := Unpaid
		for _, id := range l.names() {
			if i == Unpaid {
				i = usable(id)
			}
		}
		if i == Unpaid {
			continue
		}
		boughtBy[i], linked[k] = k, true
		b := &out[k]
		b.Movements = append(b.Movements, LineMovement{PlanMovement: movements[i], Link: l.ID})
		if movements[i].Amount < 0 && (b.Month.IsZero() || l.Month.Before(b.Month)) {
			b.Month = l.Month
		}
	}

	// The earliest made first, whatever order they came in.
	order := make([]int, len(purchases))
	for k := range order {
		order[k] = k
	}
	slices.SortStableFunc(order, func(a, b int) int {
		x, y := purchases[a].Item, purchases[b].Item
		return cmp.Or(x.CreatedAt.Compare(y.CreatedAt), bytes.Compare(x.ID[:], y.ID[:]))
	})
	for _, k := range order {
		p := purchases[k]
		if linked[k] {
			continue
		}
		price, needle := p.Price(), store.FoldRuleText(strings.TrimSpace(p.Item.MatchText))
		type found struct {
			i   int
			off money.Cents
		}
		var could []found
		for i, m := range movements {
			if !free(i) || paysElsewhere[i] || m.Kind != store.KindExpense || m.Amount >= 0 ||
				!p.canBeBoughtIn(MonthOf(m.BookedOn)) {
				continue
			}
			if _, no := rejectedAt[refused{k, m.ID}]; no {
				continue
			}
			off := abs(-m.Amount - price)
			if off > price*purchasePercent/100 || !p.looksLike(m, texts[i], needle, parents) {
				continue
			}
			could = append(could, found{i, off})
		}
		if len(could) == 1 {
			i := could[0].i
			boughtBy[i] = k
			out[k].Movements = []LineMovement{{PlanMovement: movements[i]}}
			out[k].Month = MonthOf(movements[i].BookedOn)
			continue
		}
		slices.SortFunc(could, func(a, b found) int {
			ma, mb := movements[a.i], movements[b.i]
			return cmp.Or(cmp.Compare(a.off, b.off), ma.BookedOn.Compare(mb.BookedOn), bytes.Compare(ma.ID[:], mb.ID[:]))
		})
		for _, c := range could[:min(len(could), maxCandidates)] {
			out[k].Candidates = append(out[k].Candidates, movements[c.i])
		}
	}

	for k := range out {
		b := &out[k]
		slices.SortFunc(b.Movements, func(x, y LineMovement) int {
			return cmp.Or(x.BookedOn.Compare(y.BookedOn), bytes.Compare(x.ID[:], y.ID[:]))
		})
		for _, m := range b.Movements {
			if slices.Contains(store.SpendKinds, m.Kind) {
				b.Paid -= m.Amount
			}
			if m.Amount < 0 && b.On.IsZero() {
				b.On = m.BookedOn
			}
		}
		if b.Month.IsZero() {
			// Refunds alone: nothing was bought, and nothing was paid.
			b.On, b.Paid = civil.Date{}, 0
		}
	}
	return out
}

// looksLike reports whether outflow m, whose folded text is text, looks like
// the purchase: it contains the item's text, or is filed under the item's
// category or one of its children. An item with neither looks like anything.
func (p Purchase) looksLike(m store.PlanMovement, text, needle string, parents map[uuid.UUID]uuid.UUID) bool {
	category := p.Item.CategoryID
	if needle == "" && category == nil {
		return true
	}
	if needle != "" && strings.Contains(text, needle) {
		return true
	}
	return category != nil && m.CategoryID != nil &&
		(*m.CategoryID == *category || parents[*m.CategoryID] == *category)
}

// PurchaseLine is a planned purchase in a month of the plan.
type PurchaseLine struct {
	// Instance holds the purchase's price and the months from when it was
	// planned to when it is due.
	Instance
	Status Status
	// Share is what the month sets aside for it, and SetAsideBefore what the
	// months before did.
	Share          money.Cents
	SetAsideBefore money.Cents
	// Committed is what it takes of the month: its share until the month it
	// is bought, and in that month what was paid beyond what was set aside
	// before, or nothing when it is paid with savings.
	Committed money.Cents
	// Paid is what went out for it, in the month it was bought; BoughtOn the
	// day.
	Paid     *money.Cents
	BoughtOn civil.Date
	// Movements are the ones that bought it, in the month it was bought.
	// Candidates are what it could be, while it was not bought.
	Movements  []LineMovement
	Candidates []store.PlanMovement
}

// purchaseLine is purchase p in month m, as it was recognised (b), and false
// when m does not show it. A purchase shows from the month it was planned for
// to the month it is due, and in the month it was bought; once bought, the
// months after show it no more, as they set nothing aside for it. Taken out
// of the plan, it shows to the last month the owner left it in, each of those
// as it always did: where it stands is said against the month it was due.
func purchaseLine(p Purchase, b Bought, m Month, state State) (PurchaseLine, bool) {
	bought := !b.Month.IsZero()
	planned := !m.Before(p.Start()) && !m.After(p.last())
	if m != b.Month && (!planned || bought && b.Month.Before(m)) {
		return PurchaseLine{}, false
	}
	line := PurchaseLine{Instance: p.instance(m), Share: p.Share(m), SetAsideBefore: p.SetAsideBefore(m)}
	switch {
	case m == b.Month:
		line.Status = StatusBought
		paid := b.Paid
		line.Paid, line.BoughtOn, line.Movements = &paid, b.On, b.Movements
		// Bought early, or dearer, it takes the rest of the price that month;
		// bought cheaper, it frees what was set aside beyond what it cost.
		if p.Item.Funding != FundingSavings {
			line.Committed = max(0, b.Paid-line.SetAsideBefore)
		}
		return line, true
	case m.Before(p.Target()):
		line.Status = StatusSettingAside
	case state == StatePast && !bought:
		line.Status = StatusMissed
	default:
		line.Status = StatusThisMonth
	}
	line.Committed = line.Share
	if !bought {
		// A refund said to be it, alone, is shown with it; it bought nothing.
		line.Movements, line.Candidates = b.Movements, b.Candidates
	}
	return line, true
}
