package plan_test

import (
	"errors"
	"testing"
	"time"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
)

func TestDay31FallsOnTheLastDayOfAShortMonth(t *testing.T) {
	cases := []struct {
		month plan.Month
		day   int
		want  civil.Date
	}{
		{plan.Month{Year: 2026, Month: time.February}, 31, civil.Date{Year: 2026, Month: time.February, Day: 28}},
		{plan.Month{Year: 2028, Month: time.February}, 31, civil.Date{Year: 2028, Month: time.February, Day: 29}},
		{plan.Month{Year: 2026, Month: time.February}, 30, civil.Date{Year: 2026, Month: time.February, Day: 28}},
		{plan.Month{Year: 2026, Month: time.November}, 31, civil.Date{Year: 2026, Month: time.November, Day: 30}},
		{plan.Month{Year: 2026, Month: time.December}, 31, civil.Date{Year: 2026, Month: time.December, Day: 31}},
		{plan.Month{Year: 2026, Month: time.November}, 16, civil.Date{Year: 2026, Month: time.November, Day: 16}},
		{plan.Month{Year: 2026, Month: time.November}, 0, civil.Date{Year: 2026, Month: time.November, Day: 1}},
	}
	for _, c := range cases {
		if got := c.month.DayIn(c.day); got != c.want {
			t.Errorf("%s DayIn(%d) = %s, want %s", c.month, c.day, got, c.want)
		}
	}
}

func TestAMonthTravelsAsYYYYMM(t *testing.T) {
	m, err := plan.ParseMonth("2026-11")
	if err != nil || m != (plan.Month{Year: 2026, Month: time.November}) {
		t.Fatalf(`ParseMonth("2026-11") = %v, %v`, m, err)
	}
	if got := m.String(); got != "2026-11" {
		t.Errorf("String() = %q, want 2026-11", got)
	}
	if got := (plan.Month{}).String(); got != "" {
		t.Errorf("the zero Month prints %q, want nothing", got)
	}
	for _, bad := range []string{"", "2026-1", "2026-13", "2026-00", "26-11", "2026-11-01", "2026/11", " 2026-11", "noviembre", "0000-12"} {
		if _, err := plan.ParseMonth(bad); !errors.Is(err, plan.ErrInvalidMonth) {
			t.Errorf("ParseMonth(%q) = %v, want ErrInvalidMonth", bad, err)
		}
	}
}

func TestMonthsCountAcrossTheEndOfTheYear(t *testing.T) {
	nov := plan.Month{Year: 2026, Month: time.November}
	jan := plan.Month{Year: 2027, Month: time.January}
	if got := nov.AddMonths(2); got != jan {
		t.Errorf("November plus 2 = %s, want 2027-01", got)
	}
	if got := jan.AddMonths(-13); got != (plan.Month{Year: 2025, Month: time.December}) {
		t.Errorf("January 2027 minus 13 = %s, want 2025-12", got)
	}
	if got := nov.AddMonths(24); got != (plan.Month{Year: 2028, Month: time.November}) {
		t.Errorf("November plus 24 = %s, want 2028-11", got)
	}
	if d := jan.Sub(nov); d != 2 {
		t.Errorf("January 2027 is %d months after November 2026, want 2", d)
	}
	if d := nov.Sub(jan); d != -2 {
		t.Errorf("November 2026 is %d months after January 2027, want -2", d)
	}
	if !nov.Before(jan) || nov.After(jan) || !jan.After(nov) || nov.Compare(jan) != -1 || jan.Compare(nov) != 1 ||
		nov.Compare(nov) != 0 {
		t.Error("November 2026 does not come before January 2027")
	}
	if got := plan.MonthOf(civil.Date{Year: 2026, Month: time.November, Day: 30}); got != nov {
		t.Errorf("the 30th of November is in %s", got)
	}
	if nov.First() != (civil.Date{Year: 2026, Month: time.November, Day: 1}) ||
		nov.Last() != (civil.Date{Year: 2026, Month: time.November, Day: 30}) || nov.Days() != 30 {
		t.Errorf("November runs %s to %s, %d days", nov.First(), nov.Last(), nov.Days())
	}
	if feb := (plan.Month{Year: 2028, Month: time.February}); feb.Days() != 29 || feb.Last().Day != 29 {
		t.Errorf("February 2028 has %d days", feb.Days())
	}
}
