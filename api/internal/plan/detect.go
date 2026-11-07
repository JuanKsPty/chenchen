package plan

import (
	"bytes"
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/rules"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// What the owner's history suggests planning is worked out each time it is
// asked for, never stored (ADR-0005): only what the owner turned down is
// (plan_dismissals). Nothing is planned until the owner says so; a suggestion
// is what a form starts from.

const (
	// historyMonths is how many complete months before the current one are
	// read: enough for a paycheck that comes twice a month to show its two
	// days, and for a payment to show it comes most months.
	historyMonths = 4
	// recentMonths are the complete months that say whether something still
	// comes every month: it came in at least seenMonths of them, and either in
	// the last one or already this month, so a charge that stopped is not
	// suggested.
	recentMonths = 3
	seenMonths   = 2
	// samePercent is how far a recent month's amount may be from the median
	// of those months and still be the same payment; sameFloor keeps that
	// margin from vanishing on a small one. It is tighter than what
	// recognition allows: the plan can be generous telling which movement paid
	// what the owner planned, but should not offer what changes every month,
	// like a utility bill, as fixed.
	samePercent             = 10
	sameFloor   money.Cents = 100
	// companionBelow is the amount, a dollar, under which a group is the tax
	// or fee a bank charges beside a payment, or else nothing worth planning.
	companionBelow money.Cents = 100
	// spreadDays is how far, at most, a group's days land from their median
	// for the group to come on one day of the month: a payday on a weekend or
	// a holiday moves a few days, and one at the month's end may slip into the
	// next month.
	spreadDays = 5
	// apartDays is how far apart, at least, the usual days of a month's two
	// paychecks are: about half a month.
	apartDays = 10
	// firstWeek is the last day of a month's first week, which counts as the
	// end of the month before when a month's two paychecks are put in order.
	firstWeek = 7
	// maxSuggestions is how many suggestions are offered at most: more than
	// that is a list nobody reads.
	maxSuggestions = 20
)

// PersonName is what a phone payment to or from a person, the kind
// rules.SuggestText tells apart from a shop, is called when it is suggested.
// The statement prints their name, and the text the plan looks for keeps it,
// since that is what the statement says and only its owner sees it; but the
// name chenchén offers does not, and the owner can call it what they like. A
// bank transfer to a person prints like one to a company, so nothing tells it
// apart: it is named by its text, like any other payment.
const PersonName = "Yappy de cada mes"

// AccountKeyPrefix starts a saving suggestion's key, followed by the
// account's id: a saving is found by the account it reaches, not by text.
const AccountKeyPrefix = "account:"

// Suggestion is something the owner's history suggests planning: a paycheck,
// a payment that goes out every month, or what they set aside in an account.
type Suggestion struct {
	// Key names it among the suggestions, the way a dismissal names it: the
	// text it was found by, folded as a rule folds it, with "#1" for the
	// paycheck in the middle of the month and "#2" for the one at its end
	// while both still come twice a month; for a saving, AccountKeyPrefix and
	// the account's id.
	Key  string
	Kind string
	Name string
	// Amount is the latest one seen, with the small lines charged beside it;
	// for a saving, what reached the account in a month, on average, in whole
	// dollars.
	Amount money.Cents
	// Day is the usual day of the month it comes on, the median; 0 for a
	// saving.
	Day int
	// MatchText is the text, as printed, the plan would recognise it by; empty
	// for a saving.
	MatchText string
	// CategoryID is the category its lines are filed under most often, if any.
	CategoryID *uuid.UUID
	// MonthsSeen is in how many months it was seen: of the complete months
	// read and the current one, or, for a saving, of the recent months that
	// money was set aside in.
	MonthsSeen int
	// LastOn is the last day it was seen.
	LastOn civil.Date
	// Person says it is paid to, or by, a person by phone, which is why its
	// name is PersonName.
	Person bool
	// AccountID is a saving's account.
	AccountID *uuid.UUID
}

// ActiveItem is an item of the plan that holds for the current month or a
// later one, as detection compares it with what it finds.
type ActiveItem struct {
	ID        uuid.UUID
	Kind      string
	MatchText string
	AccountID *uuid.UUID
	// Day is its day in the current month or, if it starts later, in its
	// first month.
	Day int
}

// DetectInput is everything detection works from.
type DetectInput struct {
	// Today is the day it is in Panama; it decides which months are complete.
	Today civil.Date
	// Lines are the movements of the historyMonths complete months before
	// Today's and of Today's month. Only printed lines are read: a noted
	// purchase's text is typed by the owner.
	Lines []store.PlanMovement
	// InOpenPair are the lines that are one side of a transfer between the
	// owner's accounts not yet decided: mostly a card's payment from checking,
	// which the bank prints as spending until it is paired.
	InOpenPair map[uuid.UUID]bool
	// Active are the plan's items that hold for this month or later.
	Active []ActiveItem
	// Recognised maps the lines the plan recognised this month to the item
	// each paid.
	Recognised map[uuid.UUID]uuid.UUID
	// Dismissed are the suggestions the owner turned down.
	Dismissed []store.PlanDismissal
	// Inflows are what reached the owner's savings and brokerage accounts in
	// each of the recentMonths complete months.
	Inflows []store.PlanInflow
}

// Detect finds in the owner's history the paychecks and payments that come
// every month, and what they set aside every month, that the plan does not
// have yet:
//
//   - Movements are grouped by the shop or person their text names, the text
//     rules.SuggestText finds, as long as the line's text contains it, so the
//     plan can recognise the next one by it; otherwise by their whole
//     description, so only lines that print the same thing go together.
//     Money in and money out are apart. Transfers, trades and refunds are
//     never grouped, nor a line that may be half of a transfer.
//   - A group comes every month when it came in most months once, in at
//     least two of the last three complete months, and in the last one or
//     already this month. A line counts for the month it stands for, read
//     from the day its group's run of days begins: a paycheck of the 30th
//     that slips to the 2nd of the next month stands for the month it pays.
//     A group whose days come mostly at the start of a month is read as
//     booked: a payment of the 4th booked once on the 31st is still the
//     4th's. A month is complete for a group once its line came or can no
//     longer come: on the 1st, a paycheck of the 30th that has come as late
//     as the 2nd is still on its way, not missing.
//   - A group whose days fall in two tight runs about half a month apart,
//     each coming once in most months and both in at least two of the same
//     months, like a paycheck in the middle of the month and another at its
//     end, is two suggestions, one for each, as long as both still come.
//     They are told apart by the day they land on, read around the month's
//     end, not by their place in a calendar month, so a month that held
//     three of them, or one, changes nothing. A line on neither run's days,
//     something else the same employer paid once, is left out, as long as
//     fewer than one in four are. A group that came exactly twice in at
//     least two months on other days, like two charges of one shop on the
//     same day, is two suggestions too: the month's first and second.
//   - Its recent complete months come to the same amount, give or take a
//     tenth (a dollar at least), and this month's line counts only if it
//     does too. It is suggested at the latest amount, on the median day; a
//     median past the month's end, as when a paycheck of the 30th came late
//     in most months, is the month's last day.
//   - A group under a dollar, charged on the same account and day as a
//     bigger one every time, is that one's tax: its amount is added to it.
//     Any other group under a dollar is not worth planning.
//   - What the plan has is not suggested again: a group whose text the text
//     of an item of the same kind contains, or that contains it, or with a
//     line the plan recognised this month. Of a pair of paychecks, each item
//     takes the one nearest its day, and the other is still suggested.
//   - Neither is what the owner turned down.
//
// Each savings or brokerage account that money was set aside in, net of what
// left it, in at least two of the last three complete months suggests a
// saving of what reached it on average, unless the plan already sets aside
// in it.
//
// Suggestions come paychecks first, then payments, then savings, each by how
// many months they were seen in and then by amount, at most maxSuggestions.
func Detect(in DetectInput) []Suggestion {
	now := MonthOf(in.Today)
	d := detector{in: in, now: now, from: now.AddMonths(-historyMonths),
		dismissed: make(map[store.PlanDismissal]bool, len(in.Dismissed))}
	for _, dm := range in.Dismissed {
		d.dismissed[dm] = true
	}
	d.active = slices.Clone(in.Active)
	slices.SortFunc(d.active, func(a, b ActiveItem) int { return bytes.Compare(a.ID[:], b.ID[:]) })

	var groups [][]*candidate
	var small []*pile
	for _, p := range d.piles() {
		if median(magnitudes(p.lines)) < companionBelow {
			small = append(small, p)
			continue
		}
		var found []*candidate
		for _, run := range d.split(p) {
			if c, ok := d.evaluate(p, run); ok {
				found = append(found, c)
			}
		}
		// The month's two are told apart by "#1" and "#2" only when both still
		// come. When one does, it is the pile's payment by the pile's own key:
		// whether the months read hold two a month changes as they roll (a
		// second payment to the same person, once or twice), and the key, which
		// a dismissal names, must not change with them.
		if len(found) == 2 {
			for i, c := range found {
				c.key += "#" + strconv.Itoa(i+1)
			}
		}
		groups = append(groups, found)
	}
	d.fold(small, slices.Concat(groups...))

	var out []Suggestion
	for _, found := range groups {
		for _, c := range d.unplanned(found) {
			out = append(out, c.suggestion())
		}
	}
	out = append(out, d.savings()...)
	slices.SortFunc(out, func(a, b Suggestion) int {
		return cmp.Or(cmp.Compare(kindRank(a.Kind), kindRank(b.Kind)), cmp.Compare(b.MonthsSeen, a.MonthsSeen),
			cmp.Compare(b.Amount, a.Amount), strings.Compare(a.Key, b.Key))
	})
	return out[:min(len(out), maxSuggestions)]
}

func kindRank(kind string) int {
	switch kind {
	case KindIncome:
		return 0
	case KindFixed:
		return 1
	default:
		return 2
	}
}

type detector struct {
	in        DetectInput
	now, from Month
	dismissed map[store.PlanDismissal]bool
	active    []ActiveItem
}

// pile is the lines grouped under one text, money in or money out, oldest
// first.
type pile struct {
	income bool
	// key is the text folded as a rule folds it; text is how the latest line
	// prints it, and person whether it names a person.
	key, text string
	person    bool
	lines     []store.PlanMovement
}

func (p *pile) kind() string {
	if p.income {
		return KindIncome
	}
	return KindFixed
}

// candidate is one suggestion of a pile: all of it, or one of the month's two
// paychecks.
type candidate struct {
	pile *pile
	key  string
	// lines are its own lines; chosen, one for each month read they stand
	// for, oldest first.
	lines, chosen []store.PlanMovement
	amount        money.Cents
	day           int
}

// cycle is the lines of what may be one of a pile's suggestions, with the
// month each stands for. Their days are read from start, the first day of the
// run they fall in, across the month's end when the run straddles it: a line
// booked before start came after the end of the month before, and stands for
// that month, as a paycheck of the 30th paid on the 2nd does. start is 0 when
// the days are read as booked, each line standing for the month it was booked
// in: when they are too spread to be one run, or when most of them come at
// the start of a month, so the one booked at the end of the month before came
// early.
type cycle struct {
	start int
	lines []store.PlanMovement
}

// day is the day of the month l is read on: past 31 when it came after the
// end of the month it stands for.
func (c cycle) day(l store.PlanMovement) int {
	if l.BookedOn.Day < c.start {
		return l.BookedOn.Day + 31
	}
	return l.BookedOn.Day
}

// month is the month l stands for.
func (c cycle) month(l store.PlanMovement) Month {
	if l.BookedOn.Day < c.start {
		return MonthOf(l.BookedOn).AddMonths(-1)
	}
	return MonthOf(l.BookedOn)
}

// spread is the median of the days c's lines are read on, how far the
// farthest of them is from it, and how far they all are, added up.
func (c cycle) spread() (mid, far, total int) {
	days := make([]int, 0, len(c.lines))
	for _, l := range c.lines {
		days = append(days, c.day(l))
	}
	mid = median(days)
	for _, day := range days {
		far, total = max(far, abs(day-mid)), total+abs(day-mid)
	}
	return mid, far, total
}

// usualDay is the day of the month days, read as c reads them, usually come
// on: their median, the month's last day when that falls past its end, so a
// paycheck of the 30th that came late in most months is still of the
// month's end, never of its 2nd. Days read as booked are left to usualDay.
func (c cycle) usualDay(days []int) int {
	if c.start == 0 {
		return usualDay(days)
	}
	return min(median(days), 31)
}

// piles groups the lines read into piles by their text, money in apart from
// money out. A line is left out when it is not a printed line, falls outside
// the months read, may be half of a transfer, or is neither income nor a
// payment: a transfer, a trade, or anything that gives money back.
func (d detector) piles() []*pile {
	type id struct {
		income bool
		key    string
	}
	// A day's lines come smaller first, not in the order of their ids, so two
	// charges with the same text on the same day (two subscriptions to one
	// shop) keep their places month after month, and each of the two a month
	// split finds keeps its own amount.
	lines := slices.Clone(d.in.Lines)
	slices.SortFunc(lines, func(a, b store.PlanMovement) int {
		return cmp.Or(a.BookedOn.Compare(b.BookedOn), cmp.Compare(abs(a.Amount), abs(b.Amount)),
			bytes.Compare(a.ID[:], b.ID[:]))
	})
	byID := map[id]*pile{}
	var out []*pile
	for _, l := range lines {
		if l.Origin != store.OriginStatement || d.in.InOpenPair[l.ID] {
			continue
		}
		if m := MonthOf(l.BookedOn); m.Before(d.from) || m.After(d.now) {
			continue
		}
		var income bool
		switch {
		case l.Kind == store.KindIncome && l.Amount > 0:
			income = true
		case isPayment(l.Kind) && l.Amount < 0:
		default:
			continue
		}
		text, person := textOf(l)
		k := id{income, store.FoldRuleText(text)}
		if k.key == "" {
			continue
		}
		p := byID[k]
		if p == nil {
			p = &pile{income: income, key: k.key}
			byID[k] = p
			out = append(out, p)
		}
		p.lines = append(p.lines, l)
		p.text, p.person = text, person
	}
	return out
}

// textOf is the text a line is grouped by, as printed: the shop or person
// rules.SuggestText finds in it, when the line's text, compared as the plan
// compares it, contains it, so the plan can look for it; otherwise its whole
// description, as long as an item's text may be.
func textOf(l store.PlanMovement) (text string, person bool) {
	text, person = rules.SuggestText(l.Description)
	folded := store.FoldRuleText(text)
	if folded == "" || !strings.Contains(store.FoldRuleText(l.BankCategory+" "+l.Description), folded) {
		text = clampRunes(l.Description, maxMatchRunes)
	}
	return text, person
}

// split says whether a pile comes once a month, twice, or neither, and
// returns its lines for each of the month's occurrences, with the months they
// stand for.
//
// It is twice when the pile's days fall in two runs, as pair finds them: a
// paycheck in the middle of the month and another at its end. They are told
// apart by the day they land on, not by their place in the calendar month,
// since a paycheck of the 30th that slips into the next month makes that
// month hold three, and the month it left hold one.
//
// Otherwise the pile is one run, read across the month's end when its days
// are close enough to be one and most of them come before that end, and the
// complete months read say the rest:
// once, when most months it came in it came once; twice, when it came
// exactly twice in at least two months, such as two charges of one shop on
// the same day, the first and second of each month apart. A month that had
// one of those two, or three, gives each to the one whose usual day is
// nearer. The two come in the month's order, as pair puts them, so a
// paycheck pair is not told apart by "#1" and "#2" the other way round in
// the months pair cannot read it, as when what else the employer paid lands
// on a payday in half of them.
func (d detector) split(p *pile) []cycle {
	if two, ok := d.pair(p.lines); ok {
		return two
	}
	whole := cycle{start: arc(p.lines), lines: p.lines}
	seen, once, twice := d.often(whole)
	switch {
	case once*2 > seen:
		return []cycle{whole}
	case twice < 2:
		return nil
	}
	byMonth := map[Month][]store.PlanMovement{}
	for _, l := range p.lines {
		m := whole.month(l)
		byMonth[m] = append(byMonth[m], l)
	}
	var firsts, seconds []int
	for _, ls := range byMonth {
		if len(ls) == 2 {
			firsts, seconds = append(firsts, whole.day(ls[0])), append(seconds, whole.day(ls[1]))
		}
	}
	days := [2]int{median(firsts), median(seconds)}
	halves := []cycle{{start: whole.start}, {start: whole.start}}
	for _, l := range p.lines {
		ls := byMonth[whole.month(l)]
		k := 0
		switch {
		case len(ls) == 2:
			if ls[1].ID == l.ID {
				k = 1
			}
		case dayGap(whole.day(l), days[1]) < dayGap(whole.day(l), days[0]):
			k = 1
		}
		halves[k].lines = append(halves[k].lines, l)
	}
	if inMonth(days[1]) < inMonth(days[0]) {
		halves[0], halves[1] = halves[1], halves[0]
	}
	return halves
}

// pair finds a month's two paychecks in lines: the month cut in two runs of
// days, each within spreadDays of its median and coming once in most of the
// complete months read it came in, with medians at least apartDays apart,
// that both came in at least two of the same complete months. A run that
// straddles the month's end is read from its first day, so a paycheck of the
// 30th paid on the 2nd stands for the month before. A line farther than
// spreadDays from the medians of both runs is something else the same payer
// paid once, a bonus or a refund of expenses, and is left out of both, as
// long as fewer than one in four of the lines are: a pile paid every week is
// not two paychecks with the other weeks left out. Of the cuts that do, it
// takes the one that leaves out the fewest lines, then the one whose days are
// closest to their medians. A payment made once a month, some months early
// and some late, never came twice in a month: it is not two.
//
// The two come in the month's order, a day of its first week counting as
// the end of the month before: the paycheck of the middle of the month
// first, then the one at its end, even in months it was paid on the 2nd.
func (d detector) pair(lines []store.PlanMovement) ([]cycle, bool) {
	days := distinctDays(lines)
	var best []cycle
	var mids [2]int
	bestOut, bestTotal := 0, 0
	for i := range days {
		for j := i + 1; j < len(days); j++ {
			// The first run is the days from the i-th up to the j-th; the
			// second, the rest, read from the j-th across the month's end.
			two := []cycle{{start: days[i]}, {start: days[j]}}
			for _, l := range lines {
				k := 1
				if day := l.BookedOn.Day; days[i] <= day && day < days[j] {
					k = 0
				}
				two[k].lines = append(two[k].lines, l)
			}
			var first, at [2]int
			for k := range two {
				first[k], _, _ = two[k].spread()
			}
			out, total, ok := 0, 0, true
			for k := range two {
				var odd int
				if two[k], odd, ok = two[k].tight(first[k], first[1-k]); !ok {
					break
				}
				mid, far, sum := two[k].spread()
				seen, once, _ := d.often(two[k])
				if len(two[k].lines) == 0 || far > spreadDays || once*2 <= seen {
					ok = false
					break
				}
				at[k], out, total = mid, out+odd, total+sum
			}
			if ok && out*4 < len(lines) && dayGap(at[0], at[1]) >= apartDays && d.together(two[0], two[1]) >= 2 &&
				(best == nil || out < bestOut || out == bestOut && total < bestTotal) {
				best, mids, bestOut, bestTotal = two, at, out, total
			}
		}
	}
	if best == nil {
		return nil, false
	}
	if inMonth(mids[1]) < inMonth(mids[0]) {
		best[0], best[1] = best[1], best[0]
	}
	return best, true
}

// inMonth is where a day falls in the month for putting a month's two
// paychecks in order: a day of the first week counts as past the end of the
// month before.
func inMonth(day int) int {
	if day <= firstWeek {
		return day + 31
	}
	return day
}

// arc is the first day of the run lines' days make when read from the day
// after the longest stretch of the month none of them falls on, across the
// month's end when the run straddles it, so a charge of the 30th that some
// months lands on the 1st is one run of days. It is 0, the days read as
// booked, when one of them is more than spreadDays from their median read so,
// since they are not one run, and when that median falls past the month's
// end: then most of them come at the start of a month, and one booked at the
// end of the month before came early for the next, as a payment of the 4th
// booked once on the 31st did. Reading it across the month's end would move
// every other line to the month before and the payment to the 31st.
func arc(lines []store.PlanMovement) int {
	days := distinctDays(lines)
	if len(days) == 0 {
		return 0
	}
	// The stretch across the month's end comes first, so a tie leaves the
	// days as they are.
	start, longest := days[0], days[0]+31-days[len(days)-1]
	for i := 1; i < len(days); i++ {
		if gap := days[i] - days[i-1]; gap > longest {
			start, longest = days[i], gap
		}
	}
	if mid, far, _ := (cycle{start: start, lines: lines}).spread(); far > spreadDays || mid > 31 {
		return 0
	}
	return start
}

// tight is c without the lines farther than spreadDays from mid, the median
// of the days its lines are read on, and how many it left out. ok is false
// when one of those is within spreadDays of other, the other run's median,
// counting across the month's end: it is that run's, and the month is not
// cut there.
func (c cycle) tight(mid, other int) (kept cycle, out int, ok bool) {
	other = (other-1)%31 + 1
	kept.start = c.start
	for _, l := range c.lines {
		switch {
		case abs(c.day(l)-mid) <= spreadDays:
			kept.lines = append(kept.lines, l)
		case dayGap(l.BookedOn.Day, other) <= spreadDays:
			return cycle{}, 0, false
		default:
			out++
		}
	}
	return kept, out, true
}

// distinctDays are the days of the month lines were booked on, each once,
// in order.
func distinctDays(lines []store.PlanMovement) []int {
	days := make([]int, 0, len(lines))
	for _, l := range lines {
		days = append(days, l.BookedOn.Day)
	}
	slices.Sort(days)
	return slices.Compact(days)
}

// count is how many of c's lines stand for each month.
func (c cycle) count() map[Month]int {
	n := map[Month]int{}
	for _, l := range c.lines {
		n[c.month(l)]++
	}
	return n
}

// often counts, of the complete months read, in how many c came, and in how
// many of those it came once and twice, by the months its lines stand for.
func (d detector) often(c cycle) (seen, once, twice int) {
	count := c.count()
	for m := d.from; m.Before(d.now); m = m.AddMonths(1) {
		switch count[m] {
		case 0:
			continue
		case 1:
			once++
		case 2:
			twice++
		}
		seen++
	}
	return seen, once, twice
}

// together counts the complete months read that both a and b came in.
func (d detector) together(a, b cycle) int {
	inA, inB := a.count(), b.count()
	n := 0
	for m := d.from; m.Before(d.now); m = m.AddMonths(1) {
		if inA[m] > 0 && inB[m] > 0 {
			n++
		}
	}
	return n
}

// current is the month whose line c waits for now: this month, unless
// neither its line nor last month's has come and last month's still may.
// Read across the month's end, a line comes as late as spreadDays past the
// usual day of its run, so a paycheck of the 30th that some months comes on
// the 2nd may still come on the 1st: until then last month is not complete
// for it, and it is awaited, not missing.
func (d detector) current(c cycle) Month {
	before := d.now.AddMonths(-1)
	if count := c.count(); c.start == 0 || count[before] > 0 || count[d.now] > 0 {
		return d.now
	}
	if mid, _, _ := c.spread(); d.in.Today.Day+31 > mid+spreadDays {
		return d.now
	}
	return before
}

// evaluate checks that c, a pile or one of its two paychecks, comes every
// month for the same amount, and if so makes it a candidate. Its lines count
// for the months they stand for, and one that stands for a month before the
// ones read, as the paycheck of the 30th of the month before paid on the 2nd
// of the first, is left out. A month with more than one counts the one
// closest to what they usually come to: a second payment to the same person
// in one month is something else. The months it is judged by are the
// complete ones before the month it waits for now, as current says.
func (d detector) evaluate(p *pile, c cycle) (*candidate, bool) {
	if len(c.lines) == 0 {
		return nil, false
	}
	now := d.current(c)
	usual := median(magnitudes(c.lines))
	var chosen []store.PlanMovement
	for _, l := range c.lines {
		m := c.month(l)
		if m.Before(d.from) {
			continue
		}
		n := len(chosen)
		if n == 0 || c.month(chosen[n-1]) != m {
			chosen = append(chosen, l)
			continue
		}
		if abs(abs(l.Amount)-usual) < abs(abs(chosen[n-1].Amount)-usual) {
			chosen[n-1] = l
		}
	}
	// Only the recent complete months are held to one amount, so a price that
	// went up a while ago is suggested at what it is now. This month's line,
	// often the only one so far, counts only when it comes to what they did:
	// another payment to the same person early in the month is not this one,
	// which may still come.
	var recent []money.Cents
	for _, l := range chosen {
		if m := c.month(l); m.Before(now) && !m.Before(now.AddMonths(-recentMonths)) {
			recent = append(recent, abs(l.Amount))
		}
	}
	mid := median(recent)
	margin := max(sameFloor, mid*samePercent/100)
	for _, a := range recent {
		if abs(a-mid) > margin {
			return nil, false
		}
	}
	if n := len(chosen); n > 0 && c.month(chosen[n-1]) == now &&
		abs(abs(chosen[n-1].Amount)-mid) > margin {
		chosen = chosen[:n-1]
	}
	came := map[Month]bool{}
	var days []int
	for _, l := range chosen {
		came[c.month(l)] = true
		days = append(days, c.day(l))
	}
	seen := 0
	for k := 1; k <= recentMonths; k++ {
		if came[now.AddMonths(-k)] {
			seen++
		}
	}
	if seen < seenMonths || !came[now.AddMonths(-1)] && !came[now] {
		return nil, false
	}
	latest := chosen[len(chosen)-1]
	return &candidate{pile: p, key: p.key, lines: c.lines, chosen: chosen, amount: abs(latest.Amount),
		day: c.usualDay(days)}, true
}

// fold adds each pile under a dollar to the candidate it was charged beside,
// on the same account and day every time: the tax on an insurance payment.
// The candidate takes what the small lines on the day of its latest one came
// to. With more than one such candidate, the one seen in more months takes
// it, then the bigger. A small pile no candidate takes is dropped.
func (d detector) fold(small []*pile, candidates []*candidate) {
	for _, s := range small {
		var best *candidate
		for _, c := range candidates {
			if c.pile.income != s.income || !beside(s.lines, c.lines) {
				continue
			}
			if best == nil || stronger(c, best) {
				best = c
			}
		}
		if best == nil {
			continue
		}
		latest := best.chosen[len(best.chosen)-1]
		for _, l := range s.lines {
			if l.AccountID == latest.AccountID && l.BookedOn == latest.BookedOn {
				best.amount += abs(l.Amount)
			}
		}
	}
}

// beside reports whether every one of small was charged on the account and
// day of one of lines.
func beside(small, lines []store.PlanMovement) bool {
	for _, s := range small {
		if !slices.ContainsFunc(lines, func(l store.PlanMovement) bool {
			return l.AccountID == s.AccountID && l.BookedOn == s.BookedOn
		}) {
			return false
		}
	}
	return true
}

func stronger(a, b *candidate) bool {
	return cmp.Or(cmp.Compare(len(b.chosen), len(a.chosen)), cmp.Compare(b.amount, a.amount),
		strings.Compare(a.key, b.key)) < 0
}

// unplanned leaves out, of a pile's candidates, what the plan already has or
// the owner turned down. An item that paid one of the pile's lines this
// month plans the pile, and so does each other item of the same kind whose
// text overlaps the pile's (one contains the other, compared as the plan
// compares them). Each such item takes the candidate nearest its day that no
// item took yet, not the one its line happened to land in, so one paycheck
// planned leaves the other suggested; an item no longer in the plan takes
// the candidate of its line. A candidate is turned down when its key is, or
// its pile's: turning down a payment covers the two it becomes if it starts
// to come twice a month.
func (d detector) unplanned(found []*candidate) []*candidate {
	if len(found) == 0 {
		return nil
	}
	p := found[0].pile
	taken := make([]bool, len(found))
	take := func(day int) {
		nearest := -1
		for i, c := range found {
			if !taken[i] && (nearest < 0 || dayGap(c.day, day) < dayGap(found[nearest].day, day)) {
				nearest = i
			}
		}
		if nearest >= 0 {
			taken[nearest] = true
		}
	}
	dayOf := make(map[uuid.UUID]int, len(d.active))
	for _, it := range d.active {
		dayOf[it.ID] = it.Day
	}
	used := map[uuid.UUID]bool{}
	for i, c := range found {
		for _, l := range c.lines {
			item, ok := d.in.Recognised[l.ID]
			if !ok || used[item] {
				continue
			}
			used[item] = true
			if day, ok := dayOf[item]; ok {
				take(day)
			} else {
				taken[i] = true
			}
		}
	}
	for _, it := range d.active {
		if it.Kind == p.kind() && !used[it.ID] && overlaps(it.MatchText, p.key) {
			take(it.Day)
		}
	}
	var out []*candidate
	for i, c := range found {
		if !taken[i] && !d.dismissed[store.PlanDismissal{Kind: p.kind(), Key: c.key}] &&
			!d.dismissed[store.PlanDismissal{Kind: p.kind(), Key: p.key}] {
			out = append(out, c)
		}
	}
	return out
}

// overlaps reports whether an item's text and a pile's key, both folded, are
// one inside the other. An item with no text overlaps nothing.
func overlaps(matchText, key string) bool {
	mt := store.FoldRuleText(strings.TrimSpace(matchText))
	return mt != "" && (strings.Contains(key, mt) || strings.Contains(mt, key))
}

func (c *candidate) suggestion() Suggestion {
	latest := c.chosen[len(c.chosen)-1]
	s := Suggestion{Key: c.key, Kind: c.pile.kind(), Name: title(c.pile.text), Amount: c.amount, Day: c.day,
		MatchText: c.pile.text, CategoryID: usualCategory(c.chosen), MonthsSeen: len(c.chosen),
		LastOn: latest.BookedOn, Person: c.pile.person}
	if s.Person {
		s.Name = PersonName
	}
	return s
}

// usualCategory is the category lines are filed under most often, the latest
// of those filed as often; nil when none is filed.
func usualCategory(lines []store.PlanMovement) *uuid.UUID {
	count := map[uuid.UUID]int{}
	var best *uuid.UUID
	for i := len(lines) - 1; i >= 0; i-- {
		id := lines[i].CategoryID
		if id == nil {
			continue
		}
		count[*id]++
		if best == nil || count[*id] > count[*best] {
			best = id
		}
	}
	if best == nil {
		return nil
	}
	id := *best
	return &id
}

// savings suggests, for each savings or brokerage account money was set
// aside in, net, in at least seenMonths of the recentMonths complete months,
// what reached it in a month on average over those months, in whole dollars:
// a saving is planned in round amounts, and an average of three months says
// no more than that. An account a saving of the plan already uses, or that the
// owner turned down, is left out.
func (d detector) savings() []Suggestion {
	type account struct {
		id    uuid.UUID
		alias string
		net   map[Month]money.Cents
		last  civil.Date
	}
	var accounts []*account
	byID := map[uuid.UUID]*account{}
	for _, in := range d.in.Inflows {
		m := MonthOf(in.Month)
		if !m.Before(d.now) || m.Before(d.now.AddMonths(-recentMonths)) {
			continue
		}
		a := byID[in.AccountID]
		if a == nil {
			a = &account{id: in.AccountID, alias: in.Alias, net: map[Month]money.Cents{}}
			byID[in.AccountID] = a
			accounts = append(accounts, a)
		}
		a.net[m] += in.Net
		if in.Net > 0 && in.LastIn.After(a.last) {
			a.last = in.LastIn
		}
	}
	saved := map[uuid.UUID]bool{}
	for _, it := range d.in.Active {
		if it.Kind == KindSaving && it.AccountID != nil {
			saved[*it.AccountID] = true
		}
	}
	var out []Suggestion
	for _, a := range accounts {
		var sum money.Cents
		positive := 0
		for k := 1; k <= recentMonths; k++ {
			net := a.net[d.now.AddMonths(-k)]
			sum += net
			if net > 0 {
				positive++
			}
		}
		amount := (sum/recentMonths + 50) / 100 * 100
		key := AccountKeyPrefix + a.id.String()
		if positive < seenMonths || amount <= 0 || saved[a.id] ||
			d.dismissed[store.PlanDismissal{Kind: KindSaving, Key: key}] {
			continue
		}
		name := "Ahorro de cada mes"
		if alias := strings.TrimSpace(a.alias); alias != "" {
			name = clampRunes("Ahorro en "+alias, maxNameRunes)
		}
		id := a.id
		out = append(out, Suggestion{Key: key, Kind: KindSaving, Name: name, Amount: amount, MonthsSeen: positive,
			LastOn: a.last, AccountID: &id})
	}
	return out
}

// smallWords stay in lower case inside a name, as Spanish writes them.
var smallWords = map[string]bool{
	"a": true, "al": true, "con": true, "de": true, "del": true, "el": true, "en": true, "la": true,
	"las": true, "los": true, "o": true, "para": true, "por": true, "y": true,
}

// title makes a statement's text, printed in capitals, read as a name:
// "SEGURO DE FRAUDE MENSUAL" is "Seguro de Fraude Mensual". A word that is not
// only letters, such as "S.A." or a shop's web address, stays as printed.
func title(text string) string {
	words := strings.Fields(text)
	for i, w := range words {
		lower := strings.ToLower(w)
		switch {
		case strings.ContainsFunc(w, func(r rune) bool { return !unicode.IsLetter(r) }):
		case i > 0 && smallWords[lower]:
			words[i] = lower
		default:
			r, size := utf8.DecodeRuneInString(lower)
			words[i] = string(unicode.ToUpper(r)) + lower[size:]
		}
	}
	return clampRunes(strings.Join(words, " "), maxNameRunes)
}

// clampRunes trims s and cuts it to at most n characters.
func clampRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n]))
	}
	return s
}

func magnitudes(lines []store.PlanMovement) []money.Cents {
	out := make([]money.Cents, 0, len(lines))
	for _, l := range lines {
		out = append(out, abs(l.Amount))
	}
	return out
}

// dayGap is how many days apart two days of the month are, counting across
// the month's end: the 30th and the 2nd are three apart.
func dayGap(a, b int) int {
	g := abs(a - b)
	return min(g, 31-g)
}

// usualDay is the median of days of the month, read across the month's end:
// a payment of the 30th that some months lands on the 1st or the 2nd is
// still of the 30th, not of the 15th. Each day is read as the one nearest
// the others, this month or the one either side, and the median is brought
// back into the month; 0 for none.
func usualDay(days []int) int {
	if len(days) == 0 {
		return 0
	}
	best, spread := 0, -1
	for _, pivot := range days {
		near := make([]int, len(days))
		for i, d := range days {
			switch {
			case d-pivot > 15:
				near[i] = d - 31
			case pivot-d > 15:
				near[i] = d + 31
			default:
				near[i] = d
			}
		}
		if s := slices.Max(near) - slices.Min(near); spread < 0 || s < spread {
			best, spread = median(near), s
		}
	}
	switch {
	case best < 1:
		best += 31
	case best > 31:
		best -= 31
	}
	return best
}

// median is the middle of xs, or the mean of the two middles, rounded down;
// 0 for none.
func median[T ~int | ~int64](xs []T) T {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
