package plan

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/transfers"
)

// maxKeyRunes matches the CHECK on plan_dismissals.key in 00021_plan.sql.
const maxKeyRunes = 120

// Suggestions works out what the owner's history suggests planning, as
// Detect does, from today in Panama. It reads only, in a handful of queries:
// the plan's versions from last month on, with their items and links when
// there are any; the movements of historyMonths complete months, this month
// and the next (the months either side of this one are what recognising this
// month's plan reads, as Month does); the transfers not yet decided among
// them; the dismissals; and what reached savings and brokerage accounts in
// the recent months.
func (s *Service) Suggestions(ctx context.Context, userID uuid.UUID) ([]Suggestion, error) {
	today := civil.Of(s.now().In(civil.Panama))
	m := MonthOf(today)
	before, after, from := m.AddMonths(-1), m.AddMonths(1), m.AddMonths(-historyMonths)
	q := s.db.Q()
	// Every version from last month on: last month to next for recognising
	// this month, as Month reads them, and this month on for what the plan
	// holds now or will.
	versions, err := q.PlanVersionsBetween(ctx, userID, before.First(), m.AddMonths(MonthsAhead).First())
	if err != nil {
		return nil, err
	}
	var items []store.PlanItem
	var said []Link
	if len(versions) > 0 {
		if items, err = q.PlanItemsByUser(ctx, userID); err != nil {
			return nil, err
		}
		if said, err = links(ctx, q, userID, before, after); err != nil {
			return nil, err
		}
	}
	movements, err := q.PlanMovements(ctx, userID, from.First(), after.Last())
	if err != nil {
		return nil, err
	}
	recognised, err := recognisedIn(ctx, q, userID, m, items, versions, movements, said)
	if err != nil {
		return nil, err
	}
	pairs, err := q.TransferCandidates(ctx, userID, transfers.Window, from.First(), m.Last())
	if err != nil {
		return nil, err
	}
	inPair := make(map[uuid.UUID]bool, 2*len(pairs))
	for _, p := range pairs {
		inPair[p.Out.ID], inPair[p.In.ID] = true, true
	}
	dismissed, err := q.PlanDismissals(ctx, userID)
	if err != nil {
		return nil, err
	}
	inflows, err := q.PlanInflows(ctx, userID, m.AddMonths(-recentMonths).First(), before.Last())
	if err != nil {
		return nil, err
	}
	return Detect(DetectInput{
		Today: today, Lines: movements, InOpenPair: inPair, Active: activeItems(items, versions, m),
		Recognised: recognised, Dismissed: dismissed, Inflows: inflows,
	}), nil
}

// recognisedIn maps each movement the plan recognised in month m to the item
// it paid: one that paid an instance of m, wherever it was booked, or one
// booked in m that paid any. It recognises them the way Month does, from the
// same months of movements and links, so a suggestion never contradicts what
// the plan shows.
func recognisedIn(ctx context.Context, q *store.Queries, userID uuid.UUID, m Month, items []store.PlanItem,
	versions []store.PlanVersion, movements []store.PlanMovement, said []Link) (map[uuid.UUID]uuid.UUID, error) {
	if len(versions) == 0 {
		return nil, nil
	}
	before, after := m.AddMonths(-1), m.AddMonths(1)
	var near []store.PlanMovement
	for _, mv := range movements {
		if !MonthOf(mv.BookedOn).Before(before) {
			near = append(near, mv)
		}
	}
	near, err := withLinked(ctx, q, userID, near, said)
	if err != nil {
		return nil, err
	}
	instances := Instances(items, versions, before, after)
	rec := Match(instances, near, said, nil)
	out := map[uuid.UUID]uuid.UUID{}
	for i, k := range rec.Paid {
		if k != Unpaid && (instances[k].Month == m || MonthOf(near[i].BookedOn) == m) {
			out[near[i].ID] = instances[k].Item.ID
		}
	}
	return out, nil
}

// activeItems are the items that hold for month m or a later one, each with
// its day in m or, if it starts later, in its first month. versions come by
// item and in month order, so an item's first version that reaches m is the
// one that holds for it, or the next.
func activeItems(items []store.PlanItem, versions []store.PlanVersion, m Month) []ActiveItem {
	byID := make(map[uuid.UUID]*store.PlanItem, len(items))
	for i := range items {
		byID[items[i].ID] = &items[i]
	}
	seen := map[uuid.UUID]bool{}
	var out []ActiveItem
	for _, v := range versions {
		item, ok := byID[v.ItemID]
		if !ok || seen[v.ItemID] || !v.To.IsZero() && MonthOf(v.To).Before(m) {
			continue
		}
		seen[v.ItemID] = true
		out = append(out, ActiveItem{ID: item.ID, Kind: item.Kind, MatchText: item.MatchText,
			AccountID: item.AccountID, Day: v.Day})
	}
	return out
}

// Dismissal is a suggestion the owner turns down, or takes back: its kind and
// its key, as Suggestions gave them.
type Dismissal struct {
	Kind string
	Key  string
}

// check cleans up a dismissal and reports every field that is wrong at once.
func (d *Dismissal) check() error {
	var errs []error
	switch d.Kind {
	case KindIncome, KindFixed, KindSaving:
	default:
		errs = append(errs, ErrKind)
	}
	d.Key = strings.TrimSpace(d.Key)
	if d.Key == "" || utf8.RuneCountInString(d.Key) > maxKeyRunes {
		errs = append(errs, ErrSuggestionKey)
	}
	return errors.Join(errs...)
}

// Dismiss turns a suggestion down, so it is not offered again until the
// owner takes that back. Turning it down twice is turning it down once.
func (s *Service) Dismiss(ctx context.Context, userID uuid.UUID, d Dismissal) error {
	if err := d.check(); err != nil {
		return err
	}
	return s.db.Q().InsertPlanDismissal(ctx, userID, store.PlanDismissal{Kind: d.Kind, Key: d.Key})
}

// Undismiss takes back turning a suggestion down, which is how the owner
// undoes it: it is offered again whenever the history still suggests it.
// Taking back what is not there changes nothing.
func (s *Service) Undismiss(ctx context.Context, userID uuid.UUID, d Dismissal) error {
	if err := d.check(); err != nil {
		return err
	}
	return s.db.Q().DeletePlanDismissal(ctx, userID, store.PlanDismissal{Kind: d.Kind, Key: d.Key})
}

// SuggestionView is a suggestion on the wire. Day is left out for a saving,
// which has none; AccountID is a saving's account.
type SuggestionView struct {
	Key        string       `json:"key"`
	Kind       string       `json:"kind"`
	Name       string       `json:"name"`
	Amount     money.Amount `json:"amount"`
	Day        int          `json:"day,omitempty"`
	MatchText  string       `json:"match_text"`
	CategoryID *uuid.UUID   `json:"category_id,omitempty"`
	MonthsSeen int          `json:"months_seen"`
	LastOn     civil.Date   `json:"last_on"`
	Person     bool         `json:"person"`
	AccountID  *uuid.UUID   `json:"account_id,omitempty"`
}

// SuggestionsView is what the history suggests, by kind, each list in the
// order Detect gives and present even when empty.
type SuggestionsView struct {
	Income  []SuggestionView `json:"income"`
	Fixed   []SuggestionView `json:"fixed"`
	Savings []SuggestionView `json:"savings"`
}

// NewSuggestionsView builds the suggestions' wire shape.
func NewSuggestionsView(suggestions []Suggestion) SuggestionsView {
	v := SuggestionsView{Income: []SuggestionView{}, Fixed: []SuggestionView{}, Savings: []SuggestionView{}}
	for _, s := range suggestions {
		sv := SuggestionView{Key: s.Key, Kind: s.Kind, Name: s.Name, Amount: s.Amount.Amount(), Day: s.Day,
			MatchText: s.MatchText, CategoryID: s.CategoryID, MonthsSeen: s.MonthsSeen, LastOn: s.LastOn,
			Person: s.Person, AccountID: s.AccountID}
		switch s.Kind {
		case KindIncome:
			v.Income = append(v.Income, sv)
		case KindFixed:
			v.Fixed = append(v.Fixed, sv)
		case KindSaving:
			v.Savings = append(v.Savings, sv)
		}
	}
	return v
}
