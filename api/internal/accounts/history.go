package accounts

import (
	"context"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

const (
	// DefaultHistoryMonths is how far back a net worth's history goes unless
	// asked otherwise, and MaxHistoryMonths as far as it may be asked.
	DefaultHistoryMonths = 12
	MaxHistoryMonths     = 36
)

// HistoryPoint is the net worth at the end of a day, split the way NetWorth
// splits today's. Partial says some account could not be read that day: it
// had no statement yet, nothing anchors it, or its positions were never synced
// by then. Such an account is left out of the day rather than made up.
type HistoryPoint struct {
	Day         civil.Date
	Assets      money.Cents
	Liabilities money.Cents
	Holdings    money.Cents
	Partial     bool
}

// Total is assets plus liabilities (negative) plus holdings.
func (p HistoryPoint) Total() money.Cents { return p.Assets + p.Liabilities + p.Holdings }

// History is the owner's net worth at the end of each of the last months
// months, oldest first, and today last. With only set, it is that one
// account's instead, the way its page shows it.
//
// A past day's balance is read the way today's is: the opening the anchor
// gives plus every line that reached the balance through that day. Today's
// point is the net worth Cuentas shows, figure for figure. A day before any of
// the accounts has data is left out, so the history starts where the owner's
// statements do.
func (s *Service) History(ctx context.Context, userID uuid.UUID, months int, only *uuid.UUID) ([]HistoryPoint, error) {
	if months < 1 || months > MaxHistoryMonths {
		months = DefaultHistoryMonths
	}
	if only != nil {
		if _, err := s.resolve(ctx, userID, *only); err != nil {
			return nil, err
		}
	}
	list, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	q := s.db.Q()
	sums, err := q.DailySums(ctx, userID)
	if err != nil {
		return nil, err
	}
	checkpoints, err := q.CheckpointsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	worth, err := q.HoldingsDaysByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	var reads []accountReader
	var current HistoryPoint
	for _, a := range list {
		if a.PaidByOther || (only != nil && a.ID != *only) {
			continue
		}
		r := newReader(a, sums[a.ID], worth[a.ID])
		for _, c := range checkpoints {
			if c.AccountID == a.ID && (r.first.IsZero() || c.AsOf.Before(r.first)) {
				r.first = c.AsOf
			}
		}
		reads = append(reads, r)
		// Today is what List says, as NetWorth adds it up.
		if a.Class == "liability" {
			current.Liabilities += a.Balance
		} else {
			current.Assets += a.Balance
		}
		current.Holdings += a.Holdings
		current.Partial = current.Partial || r.unanchored
	}

	today := civil.Of(s.now().In(civil.Panama))
	out := make([]HistoryPoint, 0, months+1)
	for _, d := range monthEnds(today, months) {
		p, ok := pointAt(reads, d)
		if ok {
			out = append(out, p)
		}
	}
	current.Day = today
	return append(out, current), nil
}

// monthEnds are the last days of the months months before today's, oldest
// first.
func monthEnds(today civil.Date, months int) []civil.Date {
	out := make([]civil.Date, 0, months)
	firstOfThisMonth := civil.Date{Year: today.Year, Month: today.Month, Day: 1}
	for i := months; i >= 1; i-- {
		// The day before the first of the month after the one i back.
		next := civil.Of(firstOfThisMonth.Time(civil.Panama).AddDate(0, -i+1, 0))
		out = append(out, next.AddDays(-1))
	}
	return out
}

// accountReader reads one account's balance and positions on a past day.
type accountReader struct {
	class      string
	brokerage  bool
	opening    money.Cents
	unanchored bool
	steps      []store.DaySum
	worth      []store.HoldingsDay
	// first is the first day there is anything to read: its first line or the
	// first balance anybody stated, whichever came first. Zero when it has
	// neither.
	first civil.Date
	// hasPositions says the account holds, or once held, positions someone
	// synced.
	hasPositions bool
}

func newReader(a Account, steps []store.DaySum, worth []store.HoldingsDay) accountReader {
	var total money.Cents
	for _, s := range steps {
		total += s.Total
	}
	r := accountReader{
		class: a.Class, brokerage: a.Type == "brokerage", steps: steps, worth: worth,
		// The balance List gave is the opening plus every line, so the
		// opening is what is left once the lines are taken back out.
		opening:      a.Balance - total,
		unanchored:   !a.Anchored && a.Movements > 0,
		hasPositions: len(worth) > 0 || a.Holdings != 0,
	}
	if len(steps) > 0 {
		r.first = steps[0].Day
	}
	if len(worth) > 0 && (r.first.IsZero() || worth[0].Day.Before(r.first)) {
		r.first = worth[0].Day
	}
	return r
}

// pointAt adds up the accounts on day d. ok is false when none of them had
// anything to read yet.
func pointAt(reads []accountReader, d civil.Date) (p HistoryPoint, ok bool) {
	p.Day = d
	for _, r := range reads {
		if r.first.IsZero() && !r.hasPositions {
			// Nothing was ever imported or synced: it adds nothing, and
			// there is nothing missing either.
			continue
		}
		if r.first.IsZero() || d.Before(r.first) {
			p.Partial = true
			continue
		}
		ok = true
		balance := r.opening
		for _, s := range r.steps {
			if s.Day.After(d) {
				break
			}
			balance += s.Total
		}
		if r.class == "liability" {
			p.Liabilities += balance
		} else {
			p.Assets += balance
		}
		p.Partial = p.Partial || r.unanchored
		if r.brokerage && r.hasPositions {
			v, found := worthOn(r.worth, d)
			p.Holdings += v
			p.Partial = p.Partial || !found
		}
	}
	return p, ok
}

// worthOn is the latest figure on or before d. found is false when the
// positions had not been synced by then.
func worthOn(worth []store.HoldingsDay, d civil.Date) (v money.Cents, found bool) {
	for _, w := range worth {
		if w.Day.After(d) {
			break
		}
		v, found = w.Value, true
	}
	return v, found
}
