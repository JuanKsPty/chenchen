package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// Where a capture came from.
const (
	CaptureSourceShortcut = "shortcut"
	CaptureSourceManual   = "manual"
)

// What a capture is now, as capture_states derives it when read. Only a
// pending capture counts as spending; in every other state either a printed
// line counts instead or nothing does.
const (
	CapturePending   = "pending"
	CapturePrinted   = "printed"
	CaptureMissing   = "missing"
	CaptureLinked    = "linked"
	CaptureDiscarded = "discarded"
)

// The window a capture's line may be booked in, and how far a statement must
// reach past a capture before its absence means something. Migration 00014
// writes the same numbers into capture_candidates and capture_states, which
// cannot read Go constants; a test pins the two together.
const (
	// CaptureDaysBefore covers a tap late at night that reaches the server
	// after midnight, dated a day after its line.
	CaptureDaysBefore = 1
	// CaptureDaysAfter is how long a purchase can take to reach a statement
	// as a line, the same reasoning as transfers.Window.
	CaptureDaysAfter = 5
	// CaptureSettleDays is how long purchases can stay on hold after the day a
	// statement ends; until then a statement that lacks one says nothing.
	CaptureSettleDays = 3
)

// Capture is a purchase the owner noted before a statement printed it: sent
// by the phone's shortcut, or typed by hand. It is never an entry and never
// reaches a balance (ADR-0002).
type Capture struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	AccountID uuid.UUID
	Source    string // shortcut | manual
	// KeyID is the shortcut key that sent it; nil when typed by hand.
	KeyID      *uuid.UUID
	ReceivedAt time.Time
	// BookedOn is the purchase's day: the day in Panama the shortcut's call
	// arrived, or the day the owner typed.
	BookedOn civil.Date
	// Amount is signed from the holder's view, like an entry's: always negative.
	Amount         money.Cents
	Merchant       string
	CategoryID     *uuid.UUID
	CategorySource *string
	Note           string
	// EntryID is the printed line the purchase turned out to be, and LinkedBy
	// who said so (auto | user). Both are set or neither is.
	EntryID     *uuid.UUID
	LinkedBy    *string
	DiscardedAt *time.Time
	Raw         map[string]string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// State is derived when read, never stored: one of the Capture* states.
	State string
}

const captureColumns = `id, user_id, account_id, source, key_id, received_at, booked_on, amount_cents,
	merchant, category_id, category_source, note, entry_id, linked_by, discarded_at, raw,
	created_at, updated_at, state`

// InsertCapture stores a new, unlinked capture. A zero ReceivedAt means now.
// Linking and discarding happen afterwards, never on insert.
func (q *Queries) InsertCapture(ctx context.Context, c Capture) error {
	raw := []byte("{}")
	if c.Raw != nil {
		var err error
		if raw, err = json.Marshal(c.Raw); err != nil {
			return fmt.Errorf("store.InsertCapture: %w", err)
		}
	}
	var receivedAt *time.Time
	if !c.ReceivedAt.IsZero() {
		receivedAt = &c.ReceivedAt
	}
	_, err := q.q.Exec(ctx, `
		INSERT INTO captures (id, user_id, account_id, source, key_id, received_at, booked_on, amount_cents,
		                      merchant, category_id, category_source, note, raw)
		VALUES ($1, $2, $3, $4, $5, COALESCE($6, now()), $7, $8, $9, $10, $11, $12, $13)`,
		c.ID, c.UserID, c.AccountID, c.Source, c.KeyID, receivedAt, c.BookedOn, int64(c.Amount),
		c.Merchant, c.CategoryID, c.CategorySource, c.Note, raw)
	return wrap("store.InsertCapture", err)
}

// CaptureByID returns one of the user's captures with its current state. A
// capture of someone else's is ErrNotFound, like one that does not exist.
func (q *Queries) CaptureByID(ctx context.Context, userID, id uuid.UUID) (Capture, error) {
	c, err := scanCapture(q.q.QueryRow(ctx,
		`SELECT `+captureColumns+` FROM capture_states WHERE id = $1 AND user_id = $2`, id, userID))
	return c, wrap("store.CaptureByID", err)
}

// CapturesByAccount lists one of the user's accounts' captures with their
// current states, newest first.
func (q *Queries) CapturesByAccount(ctx context.Context, userID, accountID uuid.UUID) ([]Capture, error) {
	rows, err := q.q.Query(ctx, `SELECT `+captureColumns+` FROM capture_states
		WHERE user_id = $1 AND account_id = $2
		ORDER BY booked_on DESC, received_at DESC, id DESC`, userID, accountID)
	if err != nil {
		return nil, wrap("store.CapturesByAccount", err)
	}
	defer rows.Close()
	var out []Capture
	for rows.Next() {
		c, err := scanCapture(rows)
		if err != nil {
			return nil, wrap("store.CapturesByAccount", err)
		}
		out = append(out, c)
	}
	return out, wrap("store.CapturesByAccount", rows.Err())
}

func scanCapture(s scanner) (Capture, error) {
	var (
		c      Capture
		amount int64
		raw    []byte
	)
	err := s.Scan(&c.ID, &c.UserID, &c.AccountID, &c.Source, &c.KeyID, &c.ReceivedAt, &c.BookedOn, &amount,
		&c.Merchant, &c.CategoryID, &c.CategorySource, &c.Note, &c.EntryID, &c.LinkedBy, &c.DiscardedAt, &raw,
		&c.CreatedAt, &c.UpdatedAt, &c.State)
	if err != nil {
		return Capture{}, err
	}
	c.Amount = money.Cents(amount)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &c.Raw)
	}
	return c, nil
}

// SetCaptureCategory files an open capture under a category, or clears it with
// nil, as SetEntryCategory does for a line. A capture linked to its line or
// discarded is ErrNotFound: the line carries the category now, or nothing
// counts.
func (q *Queries) SetCaptureCategory(ctx context.Context, userID, id uuid.UUID, categoryID *uuid.UUID, source string) error {
	var src *string
	if categoryID != nil {
		src = &source
	}
	return q.execOne(ctx, "store.SetCaptureCategory",
		`UPDATE captures SET category_id = $3, category_source = $4, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND entry_id IS NULL AND discarded_at IS NULL`,
		id, userID, categoryID, src)
}

// SetCaptureNote sets an open capture's note, as SetEntryNote does for a
// line; a linked or discarded capture is ErrNotFound.
func (q *Queries) SetCaptureNote(ctx context.Context, userID, id uuid.UUID, note string) error {
	return q.execOne(ctx, "store.SetCaptureNote",
		`UPDATE captures SET note = $3, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND entry_id IS NULL AND discarded_at IS NULL`, id, userID, note)
}

// CaptureSummary is what one account's open captures come to now.
type CaptureSummary struct {
	// PendingTotal is what the pending captures add up to, signed like their
	// amounts (negative), and PendingCount how many there are. They count as
	// spending and never reach the balance.
	PendingTotal money.Cents
	PendingCount int
	// ToReview counts the captures the owner should look at: a printed line
	// could be them but none is linked, or a statement passed their day
	// without one.
	ToReview int
}

// CaptureSummaries sums each of the user's accounts' open captures by state.
// An account with none is absent.
func (q *Queries) CaptureSummaries(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]CaptureSummary, error) {
	rows, err := q.q.Query(ctx, `
		SELECT account_id,
		       COALESCE(SUM(amount_cents) FILTER (WHERE state = 'pending'), 0),
		       count(*) FILTER (WHERE state = 'pending'),
		       count(*) FILTER (WHERE state IN ('printed', 'missing'))
		FROM capture_states
		WHERE user_id = $1 AND entry_id IS NULL AND discarded_at IS NULL
		GROUP BY account_id`, userID)
	if err != nil {
		return nil, wrap("store.CaptureSummaries", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]CaptureSummary{}
	for rows.Next() {
		var id uuid.UUID
		var s CaptureSummary
		var total int64
		if err := rows.Scan(&id, &total, &s.PendingCount, &s.ToReview); err != nil {
			return nil, wrap("store.CaptureSummaries", err)
		}
		s.PendingTotal = money.Cents(total)
		out[id] = s
	}
	return out, wrap("store.CaptureSummaries", rows.Err())
}
