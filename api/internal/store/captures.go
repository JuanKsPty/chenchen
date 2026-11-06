package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// The ways a purchase reaches chenchén. Each capture is told of by one or more
// of them, its signals, and by each at most once: the phone's shortcut, the
// owner typing it, the assistant noting what it read, and, once the bank's
// alerts are read, an email or a notification. Migration 00017 lists them in
// capture_signals.
const (
	ChannelShortcut     = "shortcut"
	ChannelManual       = "manual"
	ChannelAgent        = "agent"
	ChannelEmail        = "email"
	ChannelNotification = "notification"
)

// timedChannels hear of a purchase the moment it happens, so the time of
// their signal is the purchase's time. Typed by hand or noted by the
// assistant, a purchase's time is only when someone wrote it down.
// captureAsEntryColumns spells the same list in SQL as timedChannelsSQL; a
// test pins the two together.
var timedChannels = []string{ChannelShortcut, ChannelEmail, ChannelNotification}

const timedChannelsSQL = `'shortcut', 'email', 'notification'`

// IsTimedChannel reports whether a channel hears of a purchase as it happens.
func IsTimedChannel(channel string) bool {
	for _, c := range timedChannels {
		if c == channel {
			return true
		}
	}
	return false
}

// Where a capture came from: the channel of its first signal.
const (
	CaptureSourceShortcut = ChannelShortcut
	CaptureSourceManual   = ChannelManual
	CaptureSourceAgent    = ChannelAgent
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

// Who linked a capture to its line: AutoLink, when only one line could be it
// and it could be no other purchase, or the owner.
const (
	LinkedByAuto = "auto"
	LinkedByUser = "user"
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
	// Source is the channel that noted it first: shortcut, manual or agent.
	Source string
	// KeyID is the shortcut key that sent it; nil unless the shortcut noted it.
	KeyID      *uuid.UUID
	ReceivedAt time.Time
	// BookedOn is the purchase's day: the day in Panama the shortcut's call
	// arrived, or the day the owner or the assistant gave.
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

// AsEntry is the capture in a movement's shape, whatever its state: the same
// mapping captureAsEntryColumns makes in SQL, for a capture already read. It
// has a time of day only when a timed channel noted it.
func (c Capture) AsEntry() Entry {
	e := Entry{ID: c.ID, UserID: c.UserID, AccountID: c.AccountID, BookedOn: c.BookedOn, BalanceOn: c.BookedOn,
		Amount: c.Amount, Kind: KindExpense, Description: c.Merchant, CategoryID: c.CategoryID,
		CategorySource: c.CategorySource, Note: c.Note, Raw: c.Raw, CreatedAt: c.CreatedAt,
		Origin: OriginCapture, CaptureSource: c.Source}
	if IsTimedChannel(c.Source) {
		at := c.ReceivedAt
		e.BookedAt = &at
	}
	return e
}

const captureColumns = `id, user_id, account_id, source, key_id, received_at, booked_on, amount_cents,
	merchant, category_id, category_source, note, entry_id, linked_by, discarded_at, raw,
	created_at, updated_at, state`

// InsertCapture stores a new, unlinked capture with its first signal: the
// channel it came from (its Source), at the moment it was received, with what
// that channel sent (its Raw). Both are one statement, so no capture ever
// lacks a signal. A zero ReceivedAt means now. Linking and discarding happen
// afterwards, never on insert.
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
		WITH c AS (
			INSERT INTO captures (id, user_id, account_id, source, key_id, received_at, booked_on, amount_cents,
			                      merchant, category_id, category_source, note, raw)
			VALUES ($1, $2, $3, $4, $5, COALESCE($6, now()), $7, $8, $9, $10, $11, $12, $13)
			RETURNING id, user_id, source, received_at, raw)
		INSERT INTO capture_signals (capture_id, user_id, channel, at, raw)
		SELECT id, user_id, source, received_at, raw FROM c`,
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

// RecentDuplicateCapture finds the capture of one of the user's accounts
// that a channel most recently brought a purchase of this amount and merchant
// to, at or after since, in any state: the same tap sent twice — a terminal
// repeating its signal, an automation that runs on two devices — which must
// count once. It reads the channel's signals rather than the captures, so a
// repeat is found whether the first copy noted a purchase or joined one
// another channel had noted. merchant is compared with the one the channel
// sent, as it sent it, ignoring case and runs of whitespace. ok is false when
// there is none.
func (q *Queries) RecentDuplicateCapture(ctx context.Context, userID, accountID uuid.UUID, channel string,
	amount money.Cents, merchant string, since time.Time) (c Capture, ok bool, err error) {
	c, err = scanCapture(q.q.QueryRow(ctx, `SELECT `+prefixed("c.", captureColumns)+`
		FROM capture_states c JOIN capture_signals s ON s.capture_id = c.id
		WHERE c.user_id = $1 AND c.account_id = $2 AND c.amount_cents = $3 AND s.channel = $4 AND s.at >= $6
		  AND lower(btrim(regexp_replace(s.raw->>'merchant', '\s+', ' ', 'g'))) = lower(btrim(regexp_replace($5, '\s+', ' ', 'g')))
		ORDER BY s.at DESC, c.id DESC LIMIT 1`, userID, accountID, int64(amount), channel, merchant, since))
	return maybeCapture("store.RecentDuplicateCapture", c, err)
}

// CaptureToldBy finds the capture of one of the user's accounts that a
// channel already told of, in any state: the same purchase told again. With a
// reference — the bank's own for the purchase — a signal that kept one is that
// purchase only if the references are equal, so two identical purchases of a
// day stay two. Otherwise it is the purchase told with this amount, merchant
// and day: what the channel sent (its signal's raw amount, merchant and day),
// falling back to the capture's own for a signal that kept none, so a purchase
// the owner corrected since is still found by what was first said of it.
// merchant compares ignoring case and runs of whitespace, as
// RecentDuplicateCapture does. ok is false when there is none.
//
// The raw values are cast only when they have the shape the assistant's
// signals give them: another channel's raw keeps what it was sent, as sent,
// and nothing guarantees the channel is compared first.
func (q *Queries) CaptureToldBy(ctx context.Context, userID, accountID uuid.UUID, channel string,
	paid money.Cents, merchant string, day civil.Date, reference string) (c Capture, ok bool, err error) {
	c, err = scanCapture(q.q.QueryRow(ctx, `SELECT `+prefixed("c.", captureColumns)+`
		FROM capture_states c JOIN capture_signals s ON s.capture_id = c.id
		WHERE c.user_id = $1 AND c.account_id = $2 AND s.channel = $3
		  AND CASE WHEN $7::text <> '' AND s.raw->>'reference' IS NOT NULL THEN s.raw->>'reference' = $7::text
		      ELSE CASE WHEN s.raw->>'amount' ~ '^[0-9]+(\.[0-9]+)?$'
		                THEN round((s.raw->>'amount')::numeric * 100)::bigint ELSE -c.amount_cents END = $4
		       AND CASE WHEN s.raw->>'day' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
		                THEN (s.raw->>'day')::date ELSE c.booked_on END = $5
		       AND lower(btrim(regexp_replace(COALESCE(s.raw->>'merchant', c.merchant), '\s+', ' ', 'g')))
		         = lower(btrim(regexp_replace($6, '\s+', ' ', 'g')))
		      END
		ORDER BY s.at DESC, c.id DESC LIMIT 1`, userID, accountID, channel, int64(paid), day, merchant, reference))
	return maybeCapture("store.CaptureToldBy", c, err)
}

// CaptureSignal is one way a purchase reached chenchén: a capture's first
// signal is the channel that noted it, and every other is a channel that told
// of the same purchase later (ADR-0002).
type CaptureSignal struct {
	CaptureID uuid.UUID
	UserID    uuid.UUID
	Channel   string
	// At is when the channel heard of the purchase: its time for a timed
	// channel, when it was noted for any other.
	At time.Time
	// AlertID is the bank alert the signal was read from, if any.
	AlertID *uuid.UUID
	Raw     map[string]string
	// CallerID is the id a caller chose for a new purchase when this signal
	// joined one already noted instead (CaptureJoinedAs); nil otherwise. One
	// per user.
	CallerID *uuid.UUID
}

// InsertCaptureSignal records that another channel told of one of the user's
// captures. A channel it already has is a ConstraintError on
// capture_signals_pkey, a caller id the user already gave another signal one
// on capture_signals_caller_idx, and someone else's capture one on the
// foreign key. The capture itself is never touched.
func (q *Queries) InsertCaptureSignal(ctx context.Context, s CaptureSignal) error {
	raw := []byte("{}")
	if s.Raw != nil {
		var err error
		if raw, err = json.Marshal(s.Raw); err != nil {
			return fmt.Errorf("store.InsertCaptureSignal: %w", err)
		}
	}
	_, err := q.q.Exec(ctx, `INSERT INTO capture_signals (capture_id, user_id, channel, at, alert_id, raw, caller_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, s.CaptureID, s.UserID, s.Channel, s.At, s.AlertID, raw, s.CallerID)
	return wrap("store.InsertCaptureSignal", err)
}

// CaptureSignals lists the signals of one of the user's captures, the first
// one first. Someone else's capture has none.
func (q *Queries) CaptureSignals(ctx context.Context, userID, captureID uuid.UUID) ([]CaptureSignal, error) {
	rows, err := q.q.Query(ctx, `SELECT capture_id, user_id, channel, at, alert_id, raw, caller_id FROM capture_signals
		WHERE capture_id = $1 AND user_id = $2 ORDER BY at, channel`, captureID, userID)
	if err != nil {
		return nil, wrap("store.CaptureSignals", err)
	}
	defer rows.Close()
	var out []CaptureSignal
	for rows.Next() {
		var (
			s   CaptureSignal
			raw []byte
		)
		if err := rows.Scan(&s.CaptureID, &s.UserID, &s.Channel, &s.At, &s.AlertID, &raw, &s.CallerID); err != nil {
			return nil, wrap("store.CaptureSignals", err)
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &s.Raw)
		}
		out = append(out, s)
	}
	return out, wrap("store.CaptureSignals", rows.Err())
}

// CaptureToJoinNear finds the capture of one of the user's accounts that a
// new signal of a timed channel, heard at at, most likely tells of: one with
// this amount, in any state, with no signal of this channel yet, that a timed
// channel heard of within window of at. The closest in time wins. ok is false
// when there is none.
func (q *Queries) CaptureToJoinNear(ctx context.Context, userID, accountID uuid.UUID, amount money.Cents,
	channel string, at time.Time, window time.Duration) (c Capture, ok bool, err error) {
	c, err = scanCapture(q.q.QueryRow(ctx, `SELECT `+prefixed("c.", captureColumns)+`
		FROM capture_states c
		CROSS JOIN LATERAL (SELECT min(abs(extract(epoch FROM s.at - $5::timestamptz))) AS gap
		                      FROM capture_signals s WHERE s.capture_id = c.id AND s.channel = ANY($7)) t
		WHERE c.user_id = $1 AND c.account_id = $2 AND c.amount_cents = $3 AND t.gap <= $6::numeric
		  AND NOT EXISTS (SELECT 1 FROM capture_signals s WHERE s.capture_id = c.id AND s.channel = $4)
		ORDER BY t.gap, c.created_at, c.id LIMIT 1`,
		userID, accountID, int64(amount), channel, at, int64(window/time.Second), timedChannels))
	return maybeCapture("store.CaptureToJoinNear", c, err)
}

// CaptureToJoinOn finds the capture of one of the user's accounts that a new
// signal most likely tells of by its day: one with this amount, with no signal
// of this channel yet, booked from days before day to days after, in any state
// but discarded unless withDiscarded. The closest day wins, then the signal
// closest in time to at. ok is false when there is none.
func (q *Queries) CaptureToJoinOn(ctx context.Context, userID, accountID uuid.UUID, amount money.Cents,
	channel string, day civil.Date, days int, at time.Time, withDiscarded bool) (c Capture, ok bool, err error) {
	c, err = scanCapture(q.q.QueryRow(ctx, `SELECT `+prefixed("c.", captureColumns)+`
		FROM capture_states c
		CROSS JOIN LATERAL (SELECT min(abs(extract(epoch FROM s.at - $7::timestamptz))) AS gap
		                      FROM capture_signals s WHERE s.capture_id = c.id) t
		WHERE c.user_id = $1 AND c.account_id = $2 AND c.amount_cents = $3
		  AND c.booked_on BETWEEN $5::date - $6::int AND $5::date + $6::int
		  AND ($8 OR c.discarded_at IS NULL)
		  AND NOT EXISTS (SELECT 1 FROM capture_signals s WHERE s.capture_id = c.id AND s.channel = $4)
		ORDER BY abs(c.booked_on - $5::date), t.gap NULLS LAST, c.created_at, c.id LIMIT 1`,
		userID, accountID, int64(amount), channel, day, days, at, withDiscarded))
	return maybeCapture("store.CaptureToJoinOn", c, err)
}

// CaptureJoinedAs finds the user's capture that a signal sent with this id —
// the id its caller chose for a new purchase — joined instead of noting one:
// a repeat of that call must find the same purchase, not note it after all.
// The id is the signal's caller_id, which names one purchase of the user's
// whatever account the repeat names, as a purchase noted under it would. ok
// is false when there is none.
func (q *Queries) CaptureJoinedAs(ctx context.Context, userID, id uuid.UUID) (c Capture, ok bool, err error) {
	c, err = scanCapture(q.q.QueryRow(ctx, `SELECT `+prefixed("c.", captureColumns)+`
		FROM capture_signals s JOIN capture_states c ON c.id = s.capture_id
		WHERE s.user_id = $1 AND s.caller_id = $2`, userID, id))
	return maybeCapture("store.CaptureJoinedAs", c, err)
}

// maybeCapture reads a lookup that may find nothing: ok is false then, with
// no error.
func maybeCapture(op string, c Capture, err error) (Capture, bool, error) {
	err = wrap(op, err)
	if errors.Is(err, ErrNotFound) {
		return Capture{}, false, nil
	}
	return c, err == nil, err
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

// FileCaptureByRule files an open capture under the category the user's rules
// give it, or clears it with nil, unless the owner or the assistant chose its
// category: the check is in the statement, so a choice made a moment earlier
// is never overwritten. A capture it leaves alone, or that is not open, is no
// error.
func (q *Queries) FileCaptureByRule(ctx context.Context, userID, id uuid.UUID, categoryID *uuid.UUID) error {
	var src *string
	if categoryID != nil {
		rule := "rule"
		src = &rule
	}
	_, err := q.q.Exec(ctx, `UPDATE captures SET category_id = $3, category_source = $4, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND entry_id IS NULL AND discarded_at IS NULL
		  AND (category_source IS NULL OR category_source = 'rule')`, id, userID, categoryID, src)
	return wrap("store.FileCaptureByRule", err)
}

// FillCapture gives one of the user's open captures what a channel that
// joined it typed and it lacks: note, when it has none, and categoryID,
// chosen by source, when its own is empty or a rule's and source is someone's
// choice — the precedence ApplyCategoryRule and LinkCapture keep, where a
// choice outranks a rule and a rule outranks nothing. Its amount, day,
// merchant and state are never touched. The checks are in the statement, so a
// choice made a moment earlier is never overwritten. A capture it leaves
// alone, or that is not open — a linked one is its line now, a discarded one
// counts for nothing — is no error.
func (q *Queries) FillCapture(ctx context.Context, userID, id uuid.UUID, categoryID *uuid.UUID, source, note string) error {
	// Spelled once for SET and WHERE, both of which read the row as updated.
	const fileIt = `($3::uuid IS NOT NULL AND $4 IN ('user', 'assistant')
		AND (category_source IS NULL OR category_source = 'rule'))`
	_, err := q.q.Exec(ctx, `
		UPDATE captures
		   SET category_id = CASE WHEN `+fileIt+` THEN $3 ELSE category_id END,
		       category_source = CASE WHEN `+fileIt+` THEN $4 ELSE category_source END,
		       note = CASE WHEN note = '' THEN $5 ELSE note END,
		       updated_at = now()
		 WHERE id = $1 AND user_id = $2 AND entry_id IS NULL AND discarded_at IS NULL
		   AND ((note = '' AND $5 <> '') OR `+fileIt+`)`,
		id, userID, categoryID, source, note)
	return wrap("store.FillCapture", err)
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
	// MonthTotal is what the pending captures booked in the month asked for
	// add up to, signed like PendingTotal: the month's spending on a card
	// someone else pays, whose captures are all it has.
	MonthTotal money.Cents
}

// CaptureSummaries sums each of the user's accounts' open captures by state,
// and the pending ones booked in the calendar month that starts on month. An
// account with none is absent.
func (q *Queries) CaptureSummaries(ctx context.Context, userID uuid.UUID, month civil.Date) (map[uuid.UUID]CaptureSummary, error) {
	rows, err := q.q.Query(ctx, `
		SELECT account_id,
		       COALESCE(SUM(amount_cents) FILTER (WHERE state = 'pending'), 0),
		       count(*) FILTER (WHERE state = 'pending'),
		       count(*) FILTER (WHERE state IN ('printed', 'missing')),
		       COALESCE(SUM(amount_cents) FILTER (WHERE state = 'pending' AND booked_on >= $2::date
		                                          AND booked_on < ($2::date + interval '1 month')), 0)
		FROM capture_states
		WHERE user_id = $1 AND entry_id IS NULL AND discarded_at IS NULL
		GROUP BY account_id`, userID, month)
	if err != nil {
		return nil, wrap("store.CaptureSummaries", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]CaptureSummary{}
	for rows.Next() {
		var id uuid.UUID
		var s CaptureSummary
		var total, monthTotal int64
		if err := rows.Scan(&id, &total, &s.PendingCount, &s.ToReview, &monthTotal); err != nil {
			return nil, wrap("store.CaptureSummaries", err)
		}
		s.PendingTotal, s.MonthTotal = money.Cents(total), money.Cents(monthTotal)
		out[id] = s
	}
	return out, wrap("store.CaptureSummaries", rows.Err())
}

// MissingCaptures returns the account of each of the user's captures that a
// statement passed without printing: an import compares them before and after
// to say which ones it is the first to miss.
func (q *Queries) MissingCaptures(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := q.q.Query(ctx, `SELECT id, account_id FROM capture_states WHERE user_id = $1 AND state = 'missing'`, userID)
	if err != nil {
		return nil, wrap("store.MissingCaptures", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var id, account uuid.UUID
		if err := rows.Scan(&id, &account); err != nil {
			return nil, wrap("store.MissingCaptures", err)
		}
		out[id] = account
	}
	return out, wrap("store.MissingCaptures", rows.Err())
}

// CaptureCandidate is a printed line an open capture could be, as
// capture_candidates finds it.
type CaptureCandidate struct {
	CaptureID uuid.UUID
	Line      Entry
}

// CaptureCandidates lists every line each of the user's open captures could
// be, in the given accounts or, with none given, in all of them: oldest line
// first.
func (q *Queries) CaptureCandidates(ctx context.Context, userID uuid.UUID, accountIDs []uuid.UUID) ([]CaptureCandidate, error) {
	if len(accountIDs) == 0 {
		accountIDs = nil
	}
	rows, err := q.q.Query(ctx, `
		SELECT `+prefixed("e.", entryColumns)+`, k.capture_id
		FROM capture_candidates k JOIN entries e ON e.id = k.entry_id
		WHERE k.user_id = $1 AND ($2::uuid[] IS NULL OR k.account_id = ANY($2))
		ORDER BY e.booked_on, e.id, k.capture_id`, userID, accountIDs)
	if err != nil {
		return nil, wrap("store.CaptureCandidates", err)
	}
	defer rows.Close()
	var out []CaptureCandidate
	for rows.Next() {
		var c CaptureCandidate
		if c.Line, err = scanEntryWith(rows, &c.CaptureID); err != nil {
			return nil, wrap("store.CaptureCandidates", err)
		}
		out = append(out, c)
	}
	return out, wrap("store.CaptureCandidates", rows.Err())
}

// LinkCapture joins one of the user's open captures to the printed line it
// turned out to be, and carries over to the line what the owner said about the
// purchase. Its category goes when the line has none, or has only one a rule
// gave it while the capture's was chosen by the owner or the assistant; a
// category someone chose for the line is never overwritten. Its note goes
// when the line has none. The line's amount, dates and description are never
// touched: entries_guard would refuse it, and the line counts as printed.
//
// The capture is linked and read in one statement, which locks it: a category
// set on it a moment earlier is the one carried, and one set a moment later
// finds it linked and is refused, never silently dropped.
//
// A capture that is not open is ErrNotFound. A line another capture has is a
// ConstraintError on captures_one_per_line_idx, and a line of another account
// one on the captures' foreign key to entries.
func (q *Queries) LinkCapture(ctx context.Context, userID, captureID, entryID uuid.UUID, by string) error {
	var (
		categoryID *uuid.UUID
		source     *string
		note       string
	)
	err := q.q.QueryRow(ctx, `
		UPDATE captures SET entry_id = $3, linked_by = $4, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND entry_id IS NULL AND discarded_at IS NULL
		RETURNING category_id, category_source, note`,
		captureID, userID, entryID, by).Scan(&categoryID, &source, &note)
	if err != nil {
		return wrap("store.LinkCapture", err)
	}
	if categoryID != nil && source != nil {
		if _, err := q.q.Exec(ctx, `
			UPDATE entries SET category_id = $3, category_source = $4
			WHERE id = $1 AND user_id = $2
			  AND (category_id IS NULL OR (category_source = 'rule' AND $4 IN ('user', 'assistant')))`,
			entryID, userID, *categoryID, *source); err != nil {
			return wrap("store.LinkCapture", err)
		}
	}
	if note != "" {
		if _, err := q.q.Exec(ctx, `UPDATE entries SET note = $3 WHERE id = $1 AND user_id = $2 AND note = ''`,
			entryID, userID, note); err != nil {
			return wrap("store.LinkCapture", err)
		}
	}
	return nil
}

// UnlinkCapture undoes a link the owner says is wrong ("No es esta compra"):
// the capture is open again, and its line is remembered as not this purchase,
// so it is never offered or linked to it again. A capture that is not linked
// is ErrNotFound.
//
// What the link carried to the line goes back while the line still holds it.
// Left there, the wrong purchase's category would read as chosen for the line,
// and neither a rule nor the purchase the line really is could file it again.
// A category that is still the capture's, with the same source, is filed by
// the user's rules again, as the importer filed the line, or cleared; a note
// that is still the capture's is cleared. Nothing records what the line had
// before, so one the owner had filed or noted exactly as the purchase before
// the link goes back too: the lesser loss, since it is the line's own text
// that files it again.
func (q *Queries) UnlinkCapture(ctx context.Context, userID, captureID uuid.UUID) error {
	var (
		entryID    uuid.UUID
		categoryID *uuid.UUID
		source     *string
		note       string
	)
	err := q.q.QueryRow(ctx, `SELECT entry_id, category_id, category_source, note FROM captures
		WHERE id = $1 AND user_id = $2 AND entry_id IS NOT NULL FOR UPDATE`, captureID, userID).
		Scan(&entryID, &categoryID, &source, &note)
	if err != nil {
		return wrap("store.UnlinkCapture", err)
	}
	if err := q.execOne(ctx, "store.UnlinkCapture", `
		UPDATE captures SET entry_id = NULL, linked_by = NULL, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND entry_id = $3`, captureID, userID, entryID); err != nil {
		return err
	}
	if categoryID != nil && source != nil {
		var text string
		if err := q.q.QueryRow(ctx, `SELECT bank_category || ' ' || description FROM entries
			WHERE id = $1 AND user_id = $2`, entryID, userID).Scan(&text); err != nil {
			return wrap("store.UnlinkCapture", err)
		}
		rules, err := q.CategoryRulesByUser(ctx, userID)
		if err != nil {
			return err
		}
		filed := MatchRule(text, rules).CategoryID
		var filedBy *string
		if filed != nil {
			rule := "rule"
			filedBy = &rule
		}
		if _, err := q.q.Exec(ctx, `
			UPDATE entries SET category_id = $3, category_source = $4
			WHERE id = $1 AND user_id = $2 AND category_id = $5 AND category_source = $6`,
			entryID, userID, filed, filedBy, *categoryID, *source); err != nil {
			return wrap("store.UnlinkCapture", err)
		}
	}
	if note != "" {
		if _, err := q.q.Exec(ctx, `UPDATE entries SET note = '' WHERE id = $1 AND user_id = $2 AND note = $3`,
			entryID, userID, note); err != nil {
			return wrap("store.UnlinkCapture", err)
		}
	}
	return q.RejectPair(ctx, userID, captureID, entryID)
}

// NotedPurchase is the purchase the owner noted that a printed line turned
// out to be: what they called it, how it was noted first (shortcut | manual |
// agent) and who linked the two (auto | user).
type NotedPurchase struct {
	CaptureID uuid.UUID
	Merchant  string
	Source    string
	LinkedBy  string
}

// NotedPurchases finds which of the given printed lines a capture of the user
// is linked to, keyed by line, so a line can say which purchase it is and the
// owner can undo a wrong link. A line with none is absent, and so is any line
// that is not the user's: only their own captures are read. Nothing that
// builds a balance calls it.
func (q *Queries) NotedPurchases(ctx context.Context, userID uuid.UUID, entryIDs []uuid.UUID) (map[uuid.UUID]NotedPurchase, error) {
	out := map[uuid.UUID]NotedPurchase{}
	if len(entryIDs) == 0 {
		return out, nil
	}
	rows, err := q.q.Query(ctx, `SELECT entry_id, id, merchant, source, linked_by FROM captures
		WHERE user_id = $1 AND entry_id = ANY($2)`, userID, entryIDs)
	if err != nil {
		return nil, wrap("store.NotedPurchases", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			line uuid.UUID
			n    NotedPurchase
		)
		if err := rows.Scan(&line, &n.CaptureID, &n.Merchant, &n.Source, &n.LinkedBy); err != nil {
			return nil, wrap("store.NotedPurchases", err)
		}
		out[line] = n
	}
	return out, wrap("store.NotedPurchases", rows.Err())
}

// RejectPair remembers that a line is not one of the user's captures, so it
// is never a candidate for it again and never suggested as close to it.
// Saying so twice is saying it once. Both must be the user's: the foreign keys
// refuse anything else.
func (q *Queries) RejectPair(ctx context.Context, userID, captureID, entryID uuid.UUID) error {
	_, err := q.q.Exec(ctx, `INSERT INTO capture_rejections (capture_id, entry_id, user_id) VALUES ($1, $2, $3)
		ON CONFLICT (capture_id, entry_id) DO NOTHING`, captureID, entryID, userID)
	return wrap("store.RejectPair", err)
}

// DiscardCapture records that one of the user's open captures did not happen
// — a declined tap, a purchase noted twice — so it never counts again. A
// capture linked to its line, or already discarded, is ErrNotFound.
func (q *Queries) DiscardCapture(ctx context.Context, userID, id uuid.UUID) error {
	return q.execOne(ctx, "store.DiscardCapture", `UPDATE captures SET discarded_at = now(), updated_at = now()
		WHERE id = $1 AND user_id = $2 AND entry_id IS NULL AND discarded_at IS NULL`, id, userID)
}

// RestoreCapture undoes DiscardCapture: whether the capture counts derives
// again, as for a new one. One that is not discarded is ErrNotFound.
func (q *Queries) RestoreCapture(ctx context.Context, userID, id uuid.UUID) error {
	return q.execOne(ctx, "store.RestoreCapture", `UPDATE captures SET discarded_at = NULL, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND discarded_at IS NOT NULL`, id, userID)
}

// UpdateCapture corrects what was noted about one of the user's open
// captures: its amount, signed like on insert, its merchant and its day.
// Whether it counts derives again from the new values. What the shortcut sent
// stays in raw. A linked or discarded capture is ErrNotFound: a linked one is
// its line now, which only a statement can change.
func (q *Queries) UpdateCapture(ctx context.Context, userID, id uuid.UUID, amount money.Cents, merchant string, bookedOn civil.Date) error {
	return q.execOne(ctx, "store.UpdateCapture", `
		UPDATE captures SET amount_cents = $3, merchant = $4, booked_on = $5, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND entry_id IS NULL AND discarded_at IS NULL`,
		id, userID, int64(amount), merchant, bookedOn)
}

// What review suggests as a purchase's line when none matches it exactly: a
// tip or a pre-authorization changes the amount, a slow merchant the day.
// These only suggest; nothing here decides what counts.
const (
	// NearDaysAfter is how many days after the purchase a close line may be
	// booked; it may be booked CaptureDaysBefore before it, as an exact one.
	NearDaysAfter = 10
	// NearPercent is how far a close line's amount may be from the
	// purchase's, as a share of the purchase.
	NearPercent = 30
	// NearLimit is how many close lines are suggested for one purchase.
	NearLimit = 5
)

// NearCandidates suggests, for each of the given open captures of the user,
// up to NearLimit printed lines that could be it although nothing pairs them
// exactly: a line of the same account that counts as an expense to the
// importer or the owner, as in capture_candidates, unclaimed, never rejected
// for it, booked from CaptureDaysBefore before to NearDaysAfter after, and
// within NearPercent of its amount. An exact amount comes first; then lines
// whose text shares a word with the merchant, the closest amount and the
// closest day. The merchant never decides, it only orders. The amounts are
// compared in numeric: in bigint, a share of an absurd amount — a card number
// pasted as one — would overflow and break review for good.
func (q *Queries) NearCandidates(ctx context.Context, userID uuid.UUID, captureIDs []uuid.UUID) (map[uuid.UUID][]Entry, error) {
	out := map[uuid.UUID][]Entry{}
	if len(captureIDs) == 0 {
		return out, nil
	}
	rows, err := q.q.Query(ctx, `
		SELECT `+prefixed("e.", entryColumns)+`, c.id, c.merchant, c.amount_cents, c.booked_on
		FROM captures c
		JOIN entries e ON e.account_id = c.account_id
		              AND e.booked_on BETWEEN c.booked_on - $3::int AND c.booked_on + $4::int
		              AND abs(e.amount_cents::numeric - c.amount_cents) * 100 <= abs(c.amount_cents::numeric) * $5
		WHERE c.user_id = $1 AND c.id = ANY($2) AND c.entry_id IS NULL AND c.discarded_at IS NULL
		  AND 'expense' IN (e.kind, e.user_kind)
		  AND NOT EXISTS (SELECT 1 FROM captures m WHERE m.entry_id = e.id)
		  AND NOT EXISTS (SELECT 1 FROM capture_rejections r WHERE r.capture_id = c.id AND r.entry_id = e.id)`,
		userID, captureIDs, CaptureDaysBefore, NearDaysAfter, NearPercent)
	if err != nil {
		return nil, wrap("store.NearCandidates", err)
	}
	defer rows.Close()
	type near struct {
		line     Entry
		exact    bool
		shared   int
		off      money.Cents
		daysAway int
	}
	found := map[uuid.UUID][]near{}
	for rows.Next() {
		var (
			captureID uuid.UUID
			merchant  string
			amount    int64
			day       civil.Date
		)
		line, err := scanEntryWith(rows, &captureID, &merchant, &amount, &day)
		if err != nil {
			return nil, wrap("store.NearCandidates", err)
		}
		off := line.Amount - money.Cents(amount)
		found[captureID] = append(found[captureID], near{line: line, exact: off == 0,
			shared: sharedWords(merchant, line.BankCategory+" "+line.Description), off: abs(off),
			daysAway: abs(daysBetween(day, line.BookedOn))})
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("store.NearCandidates", err)
	}
	for id, lines := range found {
		sort.Slice(lines, func(i, j int) bool {
			a, b := lines[i], lines[j]
			switch {
			case a.exact != b.exact:
				return a.exact
			case a.shared != b.shared:
				return a.shared > b.shared
			case a.off != b.off:
				return a.off < b.off
			case a.daysAway != b.daysAway:
				return a.daysAway < b.daysAway
			case a.line.BookedOn != b.line.BookedOn:
				return a.line.BookedOn.Before(b.line.BookedOn)
			default:
				return a.line.ID.String() < b.line.ID.String()
			}
		})
		if len(lines) > NearLimit {
			lines = lines[:NearLimit]
		}
		for _, n := range lines {
			out[id] = append(out[id], n.line)
		}
	}
	return out, nil
}

// CaptureReview is one of the user's captures that needs the owner. A printed
// one has the lines it could be (Candidates): none was linked because the
// choice was not obvious. A missing one has the lines that come close to it
// (Near), when any do: the bank may have charged a tip, or booked it late.
type CaptureReview struct {
	Capture    Capture
	Candidates []Entry
	Near       []Entry
}

// CapturesForReview lists the user's printed and missing captures, newest
// first, with what could be their line. Neither counts as spending, so this is
// about getting the record right, never about the totals.
func (q *Queries) CapturesForReview(ctx context.Context, userID uuid.UUID) ([]CaptureReview, error) {
	rows, err := q.q.Query(ctx, `SELECT `+captureColumns+` FROM capture_states
		WHERE user_id = $1 AND state IN ('printed', 'missing')
		ORDER BY booked_on DESC, received_at DESC, id DESC`, userID)
	if err != nil {
		return nil, wrap("store.CapturesForReview", err)
	}
	defer rows.Close()
	var (
		out     []CaptureReview
		missing []uuid.UUID
	)
	for rows.Next() {
		c, err := scanCapture(rows)
		if err != nil {
			return nil, wrap("store.CapturesForReview", err)
		}
		out = append(out, CaptureReview{Capture: c})
		if c.State == CaptureMissing {
			missing = append(missing, c.ID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("store.CapturesForReview", err)
	}
	rows.Close()
	if len(out) == 0 {
		return nil, nil
	}

	candidates, err := q.CaptureCandidates(ctx, userID, nil)
	if err != nil {
		return nil, err
	}
	byCapture := map[uuid.UUID][]Entry{}
	for _, c := range candidates {
		byCapture[c.CaptureID] = append(byCapture[c.CaptureID], c.Line)
	}
	near, err := q.NearCandidates(ctx, userID, missing)
	if err != nil {
		return nil, err
	}
	for i := range out {
		id := out[i].Capture.ID
		out[i].Candidates, out[i].Near = byCapture[id], near[id]
	}
	return out, nil
}

// sharedWords counts the words of three or more letters a merchant shares with
// a line's text, ignoring case and accents; a word that starts the other
// counts, so "Super" finds "SUPER99". The phone and the statement name a shop
// differently, so this only ever orders suggestions.
func sharedWords(merchant, text string) int {
	theirs := words(text)
	n := 0
	for _, w := range words(merchant) {
		for _, t := range theirs {
			if strings.HasPrefix(t, w) || strings.HasPrefix(w, t) {
				n++
				break
			}
		}
	}
	return n
}

var foldAccents = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")

func words(s string) []string {
	fields := strings.FieldsFunc(foldAccents.Replace(strings.ToLower(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := fields[:0]
	for _, f := range fields {
		if utf8.RuneCountInString(f) >= 3 {
			out = append(out, f)
		}
	}
	return out
}

func daysBetween(a, b civil.Date) int {
	return int(b.Time(time.UTC).Sub(a.Time(time.UTC)).Hours() / 24)
}

func abs[T ~int | ~int64](v T) T {
	if v < 0 {
		return -v
	}
	return v
}
