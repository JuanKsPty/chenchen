// Package plan keeps the owner's plan for each month: what they expect to come
// in, the payments that go out every month, what they set aside, and an
// optional cap per category (ADR-0005).
//
// The plan is metadata. Nothing here writes a line, a noted purchase or a
// stated balance, and what was actually spent is never stored: it is worked
// out from the same movements Gastos adds up each time the plan is read.
//
// A plan value holds for a run of months, a version. A past month keeps the
// plan it had, next month is already planned because an open version carries
// forward, and an edit holds either from a month on or only for that month.
package plan

import (
	"errors"
	"fmt"
	"time"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
)

// ErrInvalidMonth means a month is not in the YYYY-MM form.
var ErrInvalidMonth = errors.New("plan: a month is written YYYY-MM")

// Month is a calendar month, the unit the plan is kept in. The zero value is
// "no month".
type Month struct {
	Year  int
	Month time.Month
}

// ParseMonth reads the YYYY-MM form months travel in, as the web's ?mes= does.
func ParseMonth(s string) (Month, error) {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return Month{}, fmt.Errorf("%w: %q", ErrInvalidMonth, s)
	}
	return Month{Year: t.Year(), Month: t.Month()}, nil
}

// MonthOf is the month day falls in.
func MonthOf(day civil.Date) Month { return Month{Year: day.Year, Month: day.Month} }

// IsZero reports whether m is the zero Month.
func (m Month) IsZero() bool { return m == Month{} }

// String is YYYY-MM, or "" for the zero Month.
func (m Month) String() string {
	if m.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d", m.Year, m.Month)
}

// First is the month's first day, the day the database stores a month as.
func (m Month) First() civil.Date { return civil.Date{Year: m.Year, Month: m.Month, Day: 1} }

// Last is the month's last day.
func (m Month) Last() civil.Date { return civil.Date{Year: m.Year, Month: m.Month, Day: m.Days()} }

// Days is how many days the month has.
func (m Month) Days() int {
	// Day 0 of the next month is this month's last day.
	return time.Date(m.Year, m.Month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// DayIn is the day of the month an item expected on day falls on. A day the
// month does not have is its last: what is due on the 31st is due on the 30th
// of November and the 28th of a February. A day before the 1st is the 1st.
func (m Month) DayIn(day int) civil.Date {
	day = max(1, min(day, m.Days()))
	return civil.Date{Year: m.Year, Month: m.Month, Day: day}
}

// AddMonths returns m moved by n months, which may be negative.
func (m Month) AddMonths(n int) Month {
	t := time.Date(m.Year, m.Month+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	return Month{Year: t.Year(), Month: t.Month()}
}

// Sub is how many months m comes after n: negative when it comes before.
func (m Month) Sub(n Month) int { return m.index() - n.index() }

// Compare returns -1, 0 or +1 as m is before, the same as or after n.
func (m Month) Compare(n Month) int {
	switch d := m.Sub(n); {
	case d < 0:
		return -1
	case d > 0:
		return 1
	default:
		return 0
	}
}

// Before reports whether m is strictly earlier than n.
func (m Month) Before(n Month) bool { return m.Sub(n) < 0 }

// After reports whether m is strictly later than n.
func (m Month) After(n Month) bool { return m.Sub(n) > 0 }

// index counts months from year 0, so two months compare and subtract as
// integers.
func (m Month) index() int { return m.Year*12 + int(m.Month) - 1 }
