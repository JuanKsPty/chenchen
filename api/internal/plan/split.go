package plan

import (
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// Scope says which months an edit holds for.
type Scope string

const (
	// ScopeFrom: the month and every one after it ("desde este mes").
	ScopeFrom Scope = "from"
	// ScopeOnly: that month alone; the months around it keep what they had
	// ("solo este mes").
	ScopeOnly Scope = "only"
)

// ErrInvalidScope means an edit said neither from a month nor only for it.
var ErrInvalidScope = errors.New("plan: an edit holds from a month on or only for it")

// ParseScope reads a scope as it travels: "from" or "only".
func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeFrom, ScopeOnly:
		return Scope(s), nil
	default:
		return "", ErrInvalidScope
	}
}

// Values are what a version holds for each of its months.
type Values struct {
	Amount money.Cents
	// Day is the day of the month it is expected, 1 to 31, or 0 for none.
	Day int
}

// Version is a run of months that hold the same values: an item's amount and
// day, or a category's cap.
type Version struct {
	// ID is the stored version's, or uuid.Nil for one not written yet.
	ID   uuid.UUID
	From Month
	// To is the last month it holds for; the zero Month means it has no end.
	To Month
	Values
}

// Open reports whether v holds for every month from its first on.
func (v Version) Open() bool { return v.To.IsZero() }

// Holds reports whether v holds for month m.
func (v Version) Holds(m Month) bool {
	return !m.Before(v.From) && (v.Open() || !m.After(v.To))
}

// Overlaps reports whether v and w hold for at least one month in common.
func (v Version) Overlaps(w Version) bool {
	vEndsFirst := !v.Open() && v.To.Before(w.From)
	wEndsFirst := !w.Open() && w.To.Before(v.From)
	return !vEndsFirst && !wEndsFirst
}

// endsAfter reports whether v holds for a month after w's last one.
func (v Version) endsAfter(w Version) bool {
	switch {
	case w.Open():
		return false
	case v.Open():
		return true
	default:
		return v.To.After(w.To)
	}
}

// OpKind is what one write does to a stored version.
type OpKind int

const (
	OpDelete OpKind = iota + 1
	OpUpdate
	OpInsert
)

func (k OpKind) String() string {
	switch k {
	case OpDelete:
		return "delete"
	case OpUpdate:
		return "update"
	case OpInsert:
		return "insert"
	default:
		return "unknown"
	}
}

// Op is one write that takes the stored versions toward the edited ones.
type Op struct {
	Kind OpKind
	// Version is, for OpDelete, the version as it is stored; for OpUpdate,
	// what it becomes, with the same ID and the same first month; for
	// OpInsert, the new one, with no ID yet.
	Version Version
}

// Split edits the versions of one item, or the caps of one category, so that
// they hold values from month m on (ScopeFrom) or for m alone (ScopeOnly). The
// months before m keep what they had and, for ScopeOnly, so do the ones after
// it.
//
// It returns the versions as they end up and the writes that get them there,
// in an order the database accepts one at a time: no write ever leaves two
// versions over the same month, or two starting in the same one. The version
// that covered m is closed the month before, or changed in place when it
// starts in m; for ScopeFrom, whatever was planned after m gives way.
// Neighbouring versions with the same values become one, and an edit that
// changes nothing writes nothing, so repeating one is harmless.
func Split(versions []Version, m Month, scope Scope, values Values) (after []Version, ops []Op) {
	return rewrite(versions, m, scope, &values)
}

// Remove takes month m on (ScopeFrom), or m alone (ScopeOnly), out of the
// versions of one item or the caps of one category, the way Split edits them.
// When nothing is left, the item has no month it holds for.
func Remove(versions []Version, m Month, scope Scope) (after []Version, ops []Op) {
	return rewrite(versions, m, scope, nil)
}

// rewrite lays the edit over the stored versions month by month, merges what
// came out the same, and pairs the result with what is stored. values nil
// takes the months out instead.
func rewrite(stored []Version, m Month, scope Scope, values *Values) ([]Version, []Op) {
	stored = slices.Clone(stored)
	slices.SortFunc(stored, func(a, b Version) int { return a.From.Compare(b.From) })

	var after []Version
	// The months before m, as they were.
	for _, v := range stored {
		if v.From.Before(m) {
			if v.Open() || !v.To.Before(m) {
				v.To = m.AddMonths(-1)
			}
			after = append(after, v)
		}
	}
	if values != nil {
		edited := Version{From: m, Values: *values}
		if scope == ScopeOnly {
			edited.To = m
		}
		after = append(after, edited)
	}
	// The months after m, as they were, when the edit is for m alone.
	if scope == ScopeOnly {
		next := m.AddMonths(1)
		for _, v := range stored {
			if v.Open() || v.To.After(m) {
				if v.From.Before(next) {
					v.From = next
				}
				after = append(after, v)
			}
		}
	}
	return pair(stored, merge(after))
}

// merge joins neighbouring versions that hold the same values: nothing points
// at a version, so one run of months is one version.
func merge(versions []Version) []Version {
	var out []Version
	for _, v := range versions {
		if n := len(out); n > 0 {
			last := &out[n-1]
			if !last.Open() && last.To.AddMonths(1) == v.From && last.Values == v.Values {
				last.To = v.To
				continue
			}
		}
		out = append(out, v)
	}
	return out
}

// pair turns the stored versions into the edited ones with the fewest
// writes, in an order that never overlaps two versions along the way.
//
// A version keeps its row when one of the edited versions starts in the same
// month, so an update never moves a version's first month and the unique
// (item, first month) never collides; every other stored version is deleted
// and every other edited one inserted. Deletes go first, then the updates that
// end no later than before, then the inserts, and last the updates that end
// later: at each step every row is where it ends up or inside it, and where
// they end up no two overlap.
func pair(stored, after []Version) ([]Version, []Op) {
	byFrom := make(map[Month]Version, len(stored))
	for _, v := range stored {
		byFrom[v.From] = v
	}
	var deletes, shrinks, inserts, grows []Op
	for i := range after {
		edited := &after[i]
		was, ok := byFrom[edited.From]
		if !ok {
			edited.ID = uuid.Nil
			inserts = append(inserts, Op{Kind: OpInsert, Version: *edited})
			continue
		}
		delete(byFrom, edited.From)
		edited.ID = was.ID
		switch {
		case edited.To == was.To && edited.Values == was.Values:
		case edited.endsAfter(was):
			grows = append(grows, Op{Kind: OpUpdate, Version: *edited})
		default:
			shrinks = append(shrinks, Op{Kind: OpUpdate, Version: *edited})
		}
	}
	for _, v := range stored {
		if _, unpaired := byFrom[v.From]; unpaired {
			deletes = append(deletes, Op{Kind: OpDelete, Version: v})
		}
	}
	return after, slices.Concat(deletes, shrinks, inserts, grows)
}
