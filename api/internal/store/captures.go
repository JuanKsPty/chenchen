package store

import (
	"context"
	"encoding/json"
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

// AsEntry is the capture in a movement's shape, whatever its state: the same
// mapping captureAsEntryColumns makes in SQL, for a capture already read.
func (c Capture) AsEntry() Entry {
	e := Entry{ID: c.ID, UserID: c.UserID, AccountID: c.AccountID, BookedOn: c.BookedOn, BalanceOn: c.BookedOn,
		Amount: c.Amount, Kind: KindExpense, Description: c.Merchant, CategoryID: c.CategoryID,
		CategorySource: c.CategorySource, Note: c.Note, Raw: c.Raw, CreatedAt: c.CreatedAt,
		Origin: OriginCapture, CaptureSource: c.Source}
	if c.Source == CaptureSourceShortcut {
		at := c.ReceivedAt
		e.BookedAt = &at
	}
	return e
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
