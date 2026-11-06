package plan_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
)

// mo is a month of 2026 by its number; past 12 it runs into 2027.
func mo(n int) plan.Month { return plan.Month{Year: 2026, Month: time.January}.AddMonths(n - 1) }

// ver is a stored version with id n, from month from to month to (0: no
// end), of amount cents.
func ver(n, from, to int, amount money.Cents) plan.Version {
	v := plan.Version{From: mo(from), Values: plan.Values{Amount: amount}}
	if n > 0 {
		v.ID = idN(n)
	}
	if to > 0 {
		v.To = mo(to)
	}
	return v
}

// fresh is a version not stored yet.
func fresh(from, to int, amount money.Cents) plan.Version { return ver(0, from, to, amount) }

func idN(n int) uuid.UUID { return uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", n)) }

func del(v plan.Version) plan.Op { return plan.Op{Kind: plan.OpDelete, Version: v} }
func upd(v plan.Version) plan.Op { return plan.Op{Kind: plan.OpUpdate, Version: v} }
func ins(v plan.Version) plan.Op { return plan.Op{Kind: plan.OpInsert, Version: v} }

func TestSplitTurnsEachEditIntoTheWritesItNeeds(t *testing.T) {
	const a, b, c = 1000, 2000, 3000
	cases := []struct {
		name   string
		before []plan.Version
		month  int
		scope  plan.Scope
		values *plan.Values // nil: Remove
		after  []plan.Version
		ops    []plan.Op
	}{
		{
			name:   "from a later month closes the version the month before",
			before: []plan.Version{ver(1, 1, 0, a)},
			month:  3, scope: plan.ScopeFrom, values: &plan.Values{Amount: b},
			after: []plan.Version{ver(1, 1, 2, a), fresh(3, 0, b)},
			ops:   []plan.Op{upd(ver(1, 1, 2, a)), ins(fresh(3, 0, b))},
		},
		{
			name:   "from its first month changes it in place",
			before: []plan.Version{ver(1, 1, 0, a)},
			month:  1, scope: plan.ScopeFrom, values: &plan.Values{Amount: b},
			after: []plan.Version{ver(1, 1, 0, b)},
			ops:   []plan.Op{upd(ver(1, 1, 0, b))},
		},
		{
			name:   "from a month, what was planned after it gives way",
			before: []plan.Version{ver(1, 1, 2, a), ver(2, 3, 4, b), ver(3, 5, 0, a)},
			month:  2, scope: plan.ScopeFrom, values: &plan.Values{Amount: c},
			after: []plan.Version{ver(1, 1, 1, a), fresh(2, 0, c)},
			ops:   []plan.Op{del(ver(2, 3, 4, b)), del(ver(3, 5, 0, a)), upd(ver(1, 1, 1, a)), ins(fresh(2, 0, c))},
		},
		{
			name:   "only one month splits the version around it",
			before: []plan.Version{ver(1, 1, 0, a)},
			month:  3, scope: plan.ScopeOnly, values: &plan.Values{Amount: b},
			after: []plan.Version{ver(1, 1, 2, a), fresh(3, 3, b), fresh(4, 0, a)},
			ops:   []plan.Op{upd(ver(1, 1, 2, a)), ins(fresh(3, 3, b)), ins(fresh(4, 0, a))},
		},
		{
			name:   "only a version's first month changes it there and carries on the rest",
			before: []plan.Version{ver(1, 1, 0, a)},
			month:  1, scope: plan.ScopeOnly, values: &plan.Values{Amount: b},
			after: []plan.Version{ver(1, 1, 1, b), fresh(2, 0, a)},
			ops:   []plan.Op{upd(ver(1, 1, 1, b)), ins(fresh(2, 0, a))},
		},
		{
			name:   "only a month that ends a version",
			before: []plan.Version{ver(1, 1, 3, a), ver(2, 4, 0, b)},
			month:  3, scope: plan.ScopeOnly, values: &plan.Values{Amount: c},
			after: []plan.Version{ver(1, 1, 2, a), fresh(3, 3, c), ver(2, 4, 0, b)},
			ops:   []plan.Op{upd(ver(1, 1, 2, a)), ins(fresh(3, 3, c))},
		},
		{
			name:   "the same values from a month change nothing",
			before: []plan.Version{ver(1, 1, 0, a)},
			month:  3, scope: plan.ScopeFrom, values: &plan.Values{Amount: a},
			after: []plan.Version{ver(1, 1, 0, a)},
		},
		{
			name:   "the same values for one month change nothing",
			before: []plan.Version{ver(1, 1, 2, a), ver(2, 3, 0, b)},
			month:  4, scope: plan.ScopeOnly, values: &plan.Values{Amount: b},
			after: []plan.Version{ver(1, 1, 2, a), ver(2, 3, 0, b)},
		},
		{
			name:   "a month set back to what its neighbours hold joins them",
			before: []plan.Version{ver(1, 1, 2, a), ver(2, 3, 3, b), ver(3, 4, 0, a)},
			month:  3, scope: plan.ScopeOnly, values: &plan.Values{Amount: a},
			after: []plan.Version{ver(1, 1, 0, a)},
			ops:   []plan.Op{del(ver(2, 3, 3, b)), del(ver(3, 4, 0, a)), upd(ver(1, 1, 0, a))},
		},
		{
			name:   "from a month, the same values as the month before join it",
			before: []plan.Version{ver(1, 1, 2, a), ver(2, 3, 0, b)},
			month:  3, scope: plan.ScopeFrom, values: &plan.Values{Amount: a},
			after: []plan.Version{ver(1, 1, 0, a)},
			ops:   []plan.Op{del(ver(2, 3, 0, b)), upd(ver(1, 1, 0, a))},
		},
		{
			name:  "a new item starts from its month",
			month: 3, scope: plan.ScopeFrom, values: &plan.Values{Amount: a},
			after: []plan.Version{fresh(3, 0, a)},
			ops:   []plan.Op{ins(fresh(3, 0, a))},
		},
		{
			name:  "a new item for one month holds for that month",
			month: 3, scope: plan.ScopeOnly, values: &plan.Values{Amount: a},
			after: []plan.Version{fresh(3, 3, a)},
			ops:   []plan.Op{ins(fresh(3, 3, a))},
		},
		{
			name:   "a month between two runs of the same values joins the one before",
			before: []plan.Version{ver(1, 1, 1, a), ver(2, 4, 0, a)},
			month:  2, scope: plan.ScopeOnly, values: &plan.Values{Amount: a},
			after: []plan.Version{ver(1, 1, 2, a), ver(2, 4, 0, a)},
			ops:   []plan.Op{upd(ver(1, 1, 2, a))},
		},
		{
			name:   "a month that fills the gap joins both sides",
			before: []plan.Version{ver(1, 1, 1, a), ver(2, 3, 0, a)},
			month:  2, scope: plan.ScopeOnly, values: &plan.Values{Amount: a},
			after: []plan.Version{ver(1, 1, 0, a)},
			ops:   []plan.Op{del(ver(2, 3, 0, a)), upd(ver(1, 1, 0, a))},
		},
		{
			name:   "removing from a month ends the item the month before",
			before: []plan.Version{ver(1, 1, 0, a)},
			month:  3, scope: plan.ScopeFrom,
			after: []plan.Version{ver(1, 1, 2, a)},
			ops:   []plan.Op{upd(ver(1, 1, 2, a))},
		},
		{
			name:   "removing from its first month leaves nothing",
			before: []plan.Version{ver(1, 3, 0, a)},
			month:  3, scope: plan.ScopeFrom,
			ops: []plan.Op{del(ver(1, 3, 0, a))},
		},
		{
			name:   "removing one month leaves a gap there",
			before: []plan.Version{ver(1, 1, 0, a)},
			month:  3, scope: plan.ScopeOnly,
			after: []plan.Version{ver(1, 1, 2, a), fresh(4, 0, a)},
			ops:   []plan.Op{upd(ver(1, 1, 2, a)), ins(fresh(4, 0, a))},
		},
		{
			name:   "removing a month with no plan changes nothing",
			before: []plan.Version{ver(1, 1, 2, a)},
			month:  4, scope: plan.ScopeOnly,
			after: []plan.Version{ver(1, 1, 2, a)},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var after []plan.Version
			var ops []plan.Op
			if c.values != nil {
				after, ops = plan.Split(c.before, mo(c.month), c.scope, *c.values)
			} else {
				after, ops = plan.Remove(c.before, mo(c.month), c.scope)
			}
			if !slices.Equal(after, c.after) {
				t.Errorf("after =\n\t%v\nwant\n\t%v", after, c.after)
			}
			if !slices.Equal(ops, c.ops) {
				t.Errorf("ops =\n\t%v\nwant\n\t%v", ops, c.ops)
			}
		})
	}
}

func TestVersionsOverlapWhenTheyShareAMonth(t *testing.T) {
	cases := []struct {
		name string
		a, b plan.Version
		want bool
	}{
		{"one ends the month before the other starts", ver(1, 1, 2, 100), ver(2, 3, 0, 100), false},
		{"one ends the month the other starts", ver(1, 1, 3, 100), ver(2, 3, 4, 100), true},
		{"one holds inside the other", ver(1, 1, 6, 100), ver(2, 3, 3, 100), true},
		{"both have no end", ver(1, 1, 0, 100), ver(2, 9, 0, 100), true},
		{"an open one starts after the other ends", ver(1, 4, 0, 100), ver(2, 1, 3, 100), false},
		{"an open one started before the other", ver(1, 1, 0, 100), ver(2, 5, 6, 100), true},
	}
	for _, c := range cases {
		if got := c.a.Overlaps(c.b); got != c.want {
			t.Errorf("%s: %v overlaps %v = %v, want %v", c.name, c.a, c.b, got, c.want)
		}
		if got := c.b.Overlaps(c.a); got != c.want {
			t.Errorf("%s, the other way: %v overlaps %v = %v, want %v", c.name, c.b, c.a, got, c.want)
		}
	}
}

func TestADifferentDayIsAnEditToo(t *testing.T) {
	before := []plan.Version{{ID: idN(1), From: mo(1), Values: plan.Values{Amount: 350, Day: 4}}}
	after, ops := plan.Split(before, mo(1), plan.ScopeFrom, plan.Values{Amount: 350, Day: 5})
	want := plan.Version{ID: idN(1), From: mo(1), Values: plan.Values{Amount: 350, Day: 5}}
	if !slices.Equal(after, []plan.Version{want}) || !slices.Equal(ops, []plan.Op{upd(want)}) {
		t.Errorf("moving the day = %v, %v; want the version changed in place", after, ops)
	}
}

// Every edit of every small plan: a run of up to five months, each holding
// nothing, a or b, with the last run open or not; an edit from or for each
// month, to a, b, a on another day, or nothing. Whatever the plan and the
// edit, the months come out as the edit says, and the writes, made one at a
// time as the database makes them, never overlap two versions on the way.
func TestEverySplitHoldsWhatItWasAskedAndNeverOverlapsOnTheWay(t *testing.T) {
	const window = 5
	values := []*plan.Values{nil, {Amount: 1000}, {Amount: 2000}}
	edits := []*plan.Values{{Amount: 1000}, {Amount: 2000}, {Amount: 1000, Day: 9}, nil}
	cases := 0
	for code := range pow(len(values), window) {
		months := make([]*plan.Values, window)
		for i, n := 0, code; i < window; i, n = i+1, n/len(values) {
			months[i] = values[n%len(values)]
		}
		for _, open := range []bool{false, true} {
			if open && months[window-1] == nil {
				continue
			}
			before := timeline(months, open)
			for m := 1; m <= window+1; m++ {
				for _, scope := range []plan.Scope{plan.ScopeFrom, plan.ScopeOnly} {
					for _, edit := range edits {
						cases++
						name := fmt.Sprintf("%v, %s %s, to %v", before, scope, mo(m), edit)
						var after []plan.Version
						var ops []plan.Op
						if edit != nil {
							after, ops = plan.Split(before, mo(m), scope, *edit)
						} else {
							after, ops = plan.Remove(before, mo(m), scope)
						}
						checkSplit(t, name, before, after, ops, mo(m), scope, edit)
					}
				}
			}
		}
	}
	if cases < 10000 {
		t.Fatalf("only %d edits tried", cases)
	}
}

func pow(b, e int) int {
	n := 1
	for range e {
		n *= b
	}
	return n
}

// timeline turns what each month from mo(1) holds into stored versions, one
// per run of equal values, the last one open when open says so.
func timeline(months []*plan.Values, open bool) []plan.Version {
	var out []plan.Version
	for i := 0; i < len(months); {
		if months[i] == nil {
			i++
			continue
		}
		j := i
		for j+1 < len(months) && months[j+1] != nil && *months[j+1] == *months[i] {
			j++
		}
		v := plan.Version{ID: idN(len(out) + 1), From: mo(i + 1), To: mo(j + 1), Values: *months[i]}
		if open && j == len(months)-1 {
			v.To = plan.Month{}
		}
		out = append(out, v)
		i = j + 1
	}
	return out
}

// holding is what versions hold for month m, or nil.
func holding(versions []plan.Version, m plan.Month) *plan.Values {
	for _, v := range versions {
		if v.Holds(m) {
			return &v.Values
		}
	}
	return nil
}

func checkSplit(t *testing.T, name string, before, after []plan.Version, ops []plan.Op, m plan.Month, scope plan.Scope, edit *plan.Values) {
	t.Helper()
	// Every month holds what the edit says, and the months it does not
	// reach hold what they did.
	for x := mo(0); !x.After(mo(10)); x = x.AddMonths(1) {
		want := holding(before, x)
		if x == m || (scope == plan.ScopeFrom && x.After(m)) {
			want = edit
		}
		got := holding(after, x)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Fatalf("%s: %s holds %v, want %v", name, x, got, want)
		}
	}
	// The versions are in order, apart, and joined where they hold the same.
	for i := 1; i < len(after); i++ {
		prev, v := after[i-1], after[i]
		if prev.Open() || !prev.To.Before(v.From) {
			t.Fatalf("%s: after %v overlaps or is out of order", name, after)
		}
		if prev.To.AddMonths(1) == v.From && prev.Values == v.Values {
			t.Fatalf("%s: after %v has two equal neighbours", name, after)
		}
	}
	// Made one at a time, the writes never overlap two versions or start
	// two in the same month, touch each stored version once, never move one's
	// first month, and end where after says.
	rows := slices.Clone(before)
	touched := map[uuid.UUID]bool{}
	for i, op := range ops {
		switch op.Kind {
		case plan.OpDelete, plan.OpUpdate:
			at := slices.IndexFunc(rows, func(v plan.Version) bool { return v.ID == op.Version.ID })
			if at < 0 || op.Version.ID == uuid.Nil || touched[op.Version.ID] {
				t.Fatalf("%s: op %d %v is not of a stored version, or the second for it", name, i, op)
			}
			touched[op.Version.ID] = true
			if op.Kind == plan.OpDelete {
				rows = slices.Delete(rows, at, at+1)
			} else {
				if rows[at].From != op.Version.From {
					t.Fatalf("%s: op %d %v moves a version's first month", name, i, op)
				}
				rows[at] = op.Version
			}
		case plan.OpInsert:
			if op.Version.ID != uuid.Nil {
				t.Fatalf("%s: op %d %v inserts a version with an id", name, i, op)
			}
			rows = append(rows, op.Version)
		}
		for j := range rows {
			for k := j + 1; k < len(rows); k++ {
				if rows[j].Overlaps(rows[k]) || rows[j].From == rows[k].From {
					t.Fatalf("%s: after op %d %v, %v and %v overlap", name, i, op, rows[j], rows[k])
				}
			}
		}
	}
	slices.SortFunc(rows, func(a, b plan.Version) int { return a.From.Compare(b.From) })
	if len(rows) != len(after) {
		t.Fatalf("%s: the writes leave %v, want %v", name, rows, after)
	}
	for i := range rows {
		if rows[i] != after[i] {
			t.Fatalf("%s: the writes leave %v, want %v", name, rows, after)
		}
	}
	// And nothing is written when nothing changes.
	if slices.Equal(after, before) && len(ops) > 0 {
		t.Fatalf("%s: nothing changes, yet %v", name, ops)
	}
}
