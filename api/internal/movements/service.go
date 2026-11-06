// Package movements lists and annotates the owner's movements across every
// account: every line their statements and IBKR printed, and every purchase
// they noted — with the phone's shortcut or by hand — that no statement has
// printed yet (ADR-0002). A noted purchase is listed only while it is pending;
// once a printed line could be it, the line is the movement, and once the two
// are linked the line says which purchase it was.
//
// What a movement *is* — its amount, dates and account — came from a document,
// or from what the owner noted, and cannot be edited here. What it *means*
// can: its category, a note, and whether a printed line is a transfer between
// the owner's own accounts. A noted purchase is always spending, never a
// transfer.
package movements

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

var (
	// ErrNotFound means no such movement belongs to the caller.
	ErrNotFound = errors.New("movements: movement not found")
	// ErrNoteTooLong means the note is longer than 500 characters.
	ErrNoteTooLong = errors.New("movements: the note is too long")
	// ErrCaptureNotTransfer means the movement is a purchase the owner noted,
	// which is spending by definition: only a printed line can be a transfer.
	ErrCaptureNotTransfer = errors.New("movements: a noted purchase cannot be a transfer")
)

// Service lists and annotates movements.
type Service struct {
	db         *store.DB
	categories *categories.Service
}

func NewService(db *store.DB, cats *categories.Service) *Service {
	return &Service{db: db, categories: cats}
}

// Query selects movements. UserID comes from the caller's identity, never
// from the request.
type Query struct {
	AccountIDs    []uuid.UUID
	From, To      civil.Date
	Kinds         []string
	CategoryIDs   []uuid.UUID
	Uncategorized bool
	Search        string
	Before        *store.EntryCursor
	Limit         int
}

// Page is one page of movements, newest first.
type Page struct {
	Entries []store.Entry
	Next    *store.EntryCursor
}

// List returns one page of the user's movements.
func (s *Service) List(ctx context.Context, userID uuid.UUID, q Query) (Page, error) {
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Q().Entries(ctx, s.filter(userID, q, limit+1))
	if err != nil {
		return Page{}, err
	}
	page := Page{Entries: rows}
	if len(rows) > limit {
		page.Entries = rows[:limit]
		last := page.Entries[limit-1]
		page.Next = &store.EntryCursor{BookedOn: last.BookedOn, ID: last.ID}
	}
	if err := s.fillNoted(ctx, userID, page.Entries); err != nil {
		return Page{}, err
	}
	return page, nil
}

// fillNoted sets, on each printed line a purchase the owner noted was linked
// to, which purchase that is (Entry.Noted): one read of the user's own
// captures however long the page, so a line can say what the owner called it
// and the link can be undone from there.
func (s *Service) fillNoted(ctx context.Context, userID uuid.UUID, entries []store.Entry) error {
	var lines []uuid.UUID
	for _, e := range entries {
		if e.Origin != store.OriginCapture {
			lines = append(lines, e.ID)
		}
	}
	noted, err := s.db.Q().NotedPurchases(ctx, userID, lines)
	if err != nil {
		return err
	}
	for i := range entries {
		if n, ok := noted[entries[i].ID]; ok {
			entries[i].Noted = &n
		}
	}
	return nil
}

// Each streams every movement matching q, oldest first, for an export.
func (s *Service) Each(ctx context.Context, userID uuid.UUID, q Query, fn func(store.Entry) error) error {
	return s.db.Q().EachEntry(ctx, s.filter(userID, q, 0), fn)
}

func (s *Service) filter(userID uuid.UUID, q Query, limit int) store.EntryFilter {
	return store.EntryFilter{
		UserID: userID, AccountIDs: q.AccountIDs, From: q.From, To: q.To, Kinds: q.Kinds,
		CategoryIDs: q.CategoryIDs, Uncategorized: q.Uncategorized, Search: q.Search,
		Before: q.Before, Limit: limit,
	}
}

// Get returns one of the user's movements: a printed line, or a purchase the
// owner noted that is still open — pending, or one a line could be or a
// statement passed, which the lists no longer show but an id given out
// earlier may still name. A capture linked to its line or discarded is
// ErrNotFound. It leaves Entry.Noted unset: only the lists need it, and every
// write reads the movement through here, twice.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (store.Entry, error) {
	e, err := s.db.Q().EntryByID(ctx, userID, id)
	if errors.Is(err, store.ErrNotFound) {
		e, err = s.db.Q().CaptureAsEntry(ctx, userID, id)
	}
	return e, notFound(err)
}

// SetCategory files a movement under one of the user's categories, or clears
// it with nil. source records who decided: "user" or "assistant".
//
// Money in on a printed line is either spending given back or income, and the
// category chosen says which: an income category makes a refund income (a
// friend paying for something sold, not back for a dinner), a spending
// category makes income a refund. Reports follow the kind, so it moves from
// Gastos to Ingresos or back. Choosing what the importer said again clears
// the override.
func (s *Service) SetCategory(ctx context.Context, userID, id uuid.UUID, categoryID *uuid.UUID, source string) (store.Entry, error) {
	var chosen categories.Category
	if categoryID != nil {
		var err error
		if chosen, err = s.categories.Get(ctx, userID, *categoryID); err != nil {
			return store.Entry{}, err
		}
	}
	e, err := s.Get(ctx, userID, id)
	if err != nil {
		return store.Entry{}, err
	}
	if e.Origin == store.OriginCapture {
		if err := s.db.Q().SetCaptureCategory(ctx, userID, id, categoryID, source); err != nil {
			return store.Entry{}, notFound(err)
		}
		return s.Get(ctx, userID, id)
	}
	err = s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.SetEntryCategory(ctx, userID, id, categoryID, source); err != nil {
			return err
		}
		if kind, change := moneyInKind(e, chosen, categoryID != nil); change {
			return q.SetEntryUserKind(ctx, userID, id, kind)
		}
		return nil
	})
	if err != nil {
		return store.Entry{}, notFound(err)
	}
	return s.Get(ctx, userID, id)
}

// moneyInKind is the user_kind a line of money in takes with the category
// chosen for it, and whether that changes it: refund under spending, income
// under income, nil when that is what the importer said.
func moneyInKind(e store.Entry, chosen categories.Category, has bool) (*string, bool) {
	now := e.EffectiveKind()
	if !has || e.Amount <= 0 || (now != store.KindRefund && now != store.KindIncome) {
		return nil, false
	}
	want := store.KindRefund
	if chosen.Kind == categories.KindIncome {
		want = store.KindIncome
	}
	if want == now {
		return nil, false
	}
	if want == e.Kind {
		return nil, true
	}
	return &want, true
}

// SetNote sets a movement's note; an empty note clears it.
func (s *Service) SetNote(ctx context.Context, userID, id uuid.UUID, note string) (store.Entry, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 500 {
		return store.Entry{}, ErrNoteTooLong
	}
	e, err := s.Get(ctx, userID, id)
	if err != nil {
		return store.Entry{}, err
	}
	set := s.db.Q().SetEntryNote
	if e.Origin == store.OriginCapture {
		set = s.db.Q().SetCaptureNote
	}
	if err := set(ctx, userID, id, note); err != nil {
		return store.Entry{}, notFound(err)
	}
	return s.Get(ctx, userID, id)
}

// SetTransfer marks a printed line as a transfer between the owner's own
// accounts, so it never counts as spending or income — or, with false,
// restores what the importer decided. A noted purchase is spending either way:
// ErrCaptureNotTransfer.
func (s *Service) SetTransfer(ctx context.Context, userID, id uuid.UUID, transfer bool) (store.Entry, error) {
	e, err := s.Get(ctx, userID, id)
	if err != nil {
		return store.Entry{}, err
	}
	if e.Origin == store.OriginCapture {
		return store.Entry{}, ErrCaptureNotTransfer
	}
	var kind *string
	if transfer && e.Kind != store.KindTransfer {
		k := store.KindTransfer
		kind = &k
	}
	if !transfer && e.Kind == store.KindTransfer {
		// The importer said transfer; saying it is not one needs a kind of its
		// own, which the sign decides.
		k := store.KindExpense
		if e.Amount > 0 {
			k = store.KindIncome
		}
		kind = &k
	}
	if err := s.db.Q().SetEntryUserKind(ctx, userID, id, kind); err != nil {
		return store.Entry{}, notFound(err)
	}
	return s.Get(ctx, userID, id)
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return err
}

// categoryNames maps every category the user has to its display name, a child
// as "Padre › Hija".
func (s *Service) categoryNames(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]string, error) {
	tree, err := s.categories.Tree(ctx, userID)
	if err != nil {
		return nil, err
	}
	names := map[uuid.UUID]string{}
	for _, n := range tree {
		names[n.ID] = n.Name
		for _, c := range n.Children {
			names[c.ID] = n.Name + " › " + c.Name
		}
	}
	return names, nil
}
