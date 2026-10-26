package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// Entry kinds. Reports read the effective kind: UserKind when set, Kind
// otherwise.
const (
	KindIncome   = "income"
	KindExpense  = "expense"
	KindRefund   = "refund"
	KindFee      = "fee"
	KindInterest = "interest"
	KindTransfer = "transfer"
	KindTrade    = "trade"
)

// SpendKinds are what counts as spending. A refund is positive and so reduces
// the total; transfers and trades never count.
var SpendKinds = []string{KindExpense, KindFee, KindInterest, KindRefund}

// IncomeKinds are what counts as income.
var IncomeKinds = []string{KindIncome}

// Where a movement came from: a line a statement or IBKR printed, or a
// purchase the owner noted that no statement has printed yet (ADR-0002).
const (
	OriginStatement = "statement"
	OriginCapture   = "capture"
)

// Entry is one movement a statement or IBKR printed. Lists, searches and
// reports also read a pending capture in this shape (Origin capture), so that
// a purchase counts from the moment it is noted; it never becomes a row of
// entries and nothing that builds a balance reads it.
type Entry struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	AccountID   uuid.UUID
	StatementID *uuid.UUID
	RunID       *uuid.UUID
	BookedOn    civil.Date
	BookedAt    *time.Time
	PostedOn    *civil.Date
	BalanceOn   civil.Date
	Seq         int
	// Amount is signed from the holder's view: positive makes them richer.
	Amount         money.Cents
	Kind           string
	UserKind       *string
	Description    string
	BankRef        string
	BankCategory   string
	BankBalance    *money.Cents
	CategoryID     *uuid.UUID
	CategorySource *string
	Note           string
	DedupKey       string
	Raw            map[string]string
	CreatedAt      time.Time
	// Origin is OriginStatement for a printed line and OriginCapture for a
	// noted purchase. A capture is always an expense, dated by the day it was
	// noted, with no statement, posting day or bank fields; its Description is
	// the merchant and its BookedAt when the shortcut sent it.
	Origin string
	// CaptureSource is how a capture was noted (shortcut | manual); empty for
	// a printed line.
	CaptureSource string
}

// EffectiveKind is what reports treat the entry as.
func (e Entry) EffectiveKind() string {
	if e.UserKind != nil {
		return *e.UserKind
	}
	return e.Kind
}

const entryColumns = `id, user_id, account_id, statement_id, run_id, booked_on, booked_at, posted_on,
	balance_on, seq, amount_cents, kind, user_kind, description, bank_ref, bank_category,
	bank_balance_cents, category_id, category_source, note, dedup_key, raw, created_at`

// movementColumns are entryColumns plus where the movement came from: what
// movementSource yields, and what scanMovement reads.
const movementColumns = entryColumns + `, origin, capture_source`

// captureAsEntryColumns maps a capture onto movementColumns, column by column. A
// capture is an expense on the day it was noted, which is also the day it
// would reach the balance; it has no statement, run, posting day or bank
// fields, and its time is only known when the shortcut sent it the moment the
// card was tapped. It reads from captures or capture_states alike.
const captureAsEntryColumns = `id, user_id, account_id, NULL::uuid, NULL::uuid, booked_on,
	CASE WHEN source = 'shortcut' THEN received_at END, NULL::date, booked_on, 0, amount_cents,
	'expense'::entry_kind, NULL::entry_kind, merchant, '', '', NULL::bigint, category_id, category_source,
	note, '', raw, created_at, 'capture', source`

// movementSource is every movement that counts, as one relation aliased e
// with movementColumns: each printed line, and each capture still pending —
// one no line could be yet and no statement has passed (ADR-0002). A printed,
// missing, linked or discarded capture is not here, so a purchase is never
// counted twice when its line has the same amount: once a line could be it,
// the line counts instead. A line the bank charged at a different amount, such
// as a tip, counts alongside it until the owner links them or a statement
// passes its day.
//
// Lists, search, the CSV and the reports read it, so EntryFilter's where and
// its (booked_on, id) cursor work across both. Nothing that builds a balance
// may: those read entries alone.
const movementSource = `(SELECT ` + entryColumns + `, 'statement' AS origin, '' AS capture_source FROM entries
	UNION ALL
	SELECT ` + captureAsEntryColumns + ` FROM capture_states WHERE state = 'pending') e`

// InsertEntryIfNew stores e unless the account already holds a line with its
// dedup key, in which case nothing changes and inserted is false.
func (q *Queries) InsertEntryIfNew(ctx context.Context, e Entry) (inserted bool, err error) {
	raw, err := json.Marshal(e.Raw)
	if err != nil {
		return false, fmt.Errorf("store.InsertEntryIfNew: %w", err)
	}
	var bankBalance *int64
	if e.BankBalance != nil {
		v := int64(*e.BankBalance)
		bankBalance = &v
	}
	const query = `
		INSERT INTO entries (id, user_id, account_id, statement_id, run_id, booked_on, booked_at, posted_on,
		                     seq, amount_cents, kind, description, bank_ref, bank_category,
		                     bank_balance_cents, category_id, category_source, dedup_key, raw, user_kind)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		ON CONFLICT (account_id, dedup_key) DO NOTHING`
	tag, err := q.q.Exec(ctx, query, e.ID, e.UserID, e.AccountID, e.StatementID, e.RunID, e.BookedOn,
		e.BookedAt, e.PostedOn, e.Seq, int64(e.Amount), e.Kind, e.Description, e.BankRef, e.BankCategory,
		bankBalance, e.CategoryID, e.CategorySource, e.DedupKey, raw, e.UserKind)
	if err != nil {
		return false, wrap("store.InsertEntryIfNew", err)
	}
	return tag.RowsAffected() == 1, nil
}

// EntryIDByDedupKey finds the entry an account holds for a dedup key.
func (q *Queries) EntryIDByDedupKey(ctx context.Context, accountID uuid.UUID, key string) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.q.QueryRow(ctx, `SELECT id FROM entries WHERE account_id = $1 AND dedup_key = $2`, accountID, key).Scan(&id)
	return id, wrap("store.EntryIDByDedupKey", err)
}

// EntryFilter selects movements for history, exports and reports: printed
// lines and pending captures alike (movementSource). UserID is always
// required.
type EntryFilter struct {
	UserID     uuid.UUID
	AccountIDs []uuid.UUID
	// From and To bound booked_on, both inclusive; zero means unbounded.
	From, To civil.Date
	// Kinds restricts to these effective kinds.
	Kinds         []string
	CategoryIDs   []uuid.UUID
	Uncategorized bool
	Search        string
	// Before continues a newest-first listing after this cursor.
	Before *EntryCursor
	Limit  int
}

// EntryCursor is a position in the newest-first order.
type EntryCursor struct {
	BookedOn civil.Date
	ID       uuid.UUID
}

func (f EntryFilter) where() (string, []any, error) {
	if f.UserID == uuid.Nil {
		return "", nil, fmt.Errorf("store: an entry query needs a user")
	}
	conds := []string{"e.user_id = $1"}
	args := []any{f.UserID}
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	if len(f.AccountIDs) > 0 {
		add("e.account_id = ANY(?)", f.AccountIDs)
	}
	if !f.From.IsZero() {
		add("e.booked_on >= ?", f.From)
	}
	if !f.To.IsZero() {
		add("e.booked_on <= ?", f.To)
	}
	if len(f.Kinds) > 0 {
		add("COALESCE(e.user_kind, e.kind)::text = ANY(?)", f.Kinds)
	}
	switch {
	case len(f.CategoryIDs) > 0 && f.Uncategorized:
		add("(e.category_id = ANY(?) OR e.category_id IS NULL)", f.CategoryIDs)
	case len(f.CategoryIDs) > 0:
		add("e.category_id = ANY(?)", f.CategoryIDs)
	case f.Uncategorized:
		conds = append(conds, "e.category_id IS NULL")
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		args = append(args, "%"+escapeLike(s)+"%")
		match := fmt.Sprintf("e.description ILIKE $%[1]d OR e.note ILIKE $%[1]d OR e.bank_category ILIKE $%[1]d", len(args))
		// Text that reads as an amount also finds the movement of that size,
		// in or out: "$25.00" finds both a 25.00 charge and a 25.00 deposit.
		if cents, ok := searchAmount(s); ok {
			args = append(args, int64(cents))
			match += fmt.Sprintf(" OR abs(e.amount_cents) = $%d", len(args))
		}
		conds = append(conds, "("+match+")")
	}
	if f.Before != nil {
		args = append(args, f.Before.BookedOn, f.Before.ID)
		conds = append(conds, fmt.Sprintf("(e.booked_on, e.id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	return strings.Join(conds, " AND "), args, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// searchAmount reads search text as an amount written the way the app shows
// one: "$25.00", "−$1,234.50" (with the typographic minus), "+25" or plain
// "25". The sign and "$" may only lead, so text like "25-30" is not an amount;
// commas are dropped as thousands separators. The result is the size of the
// amount, never negative.
func searchAmount(s string) (money.Cents, bool) {
	s = strings.TrimLeft(strings.TrimSpace(s), "+-−$ ")
	s = strings.ReplaceAll(s, ",", "")
	if s == "" || s[0] == '+' || s[0] == '-' {
		return 0, false
	}
	c, err := money.Parse(s)
	if err != nil {
		return 0, false
	}
	return c, true
}

// Entries lists movements newest first: printed lines and pending captures.
func (q *Queries) Entries(ctx context.Context, f EntryFilter) ([]Entry, error) {
	where, args, err := f.where()
	if err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	query := `SELECT ` + prefixed("e.", movementColumns) + ` FROM ` + movementSource + ` WHERE ` + where +
		fmt.Sprintf(` ORDER BY e.booked_on DESC, e.id DESC LIMIT %d`, limit)
	return q.queryEntries(ctx, "store.Entries", scanMovement, query, args...)
}

// EachEntry streams every movement matching f, printed lines and pending
// captures, oldest first, for an export that must not load the whole history
// into memory at once.
func (q *Queries) EachEntry(ctx context.Context, f EntryFilter, fn func(Entry) error) error {
	where, args, err := f.where()
	if err != nil {
		return err
	}
	query := `SELECT ` + prefixed("e.", movementColumns) + ` FROM ` + movementSource + ` WHERE ` + where +
		` ORDER BY e.booked_on, e.booked_at NULLS FIRST, e.seq, e.id`
	rows, err := q.q.Query(ctx, query, args...)
	if err != nil {
		return wrap("store.EachEntry", err)
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanMovement(rows)
		if err != nil {
			return wrap("store.EachEntry", err)
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return wrap("store.EachEntry", rows.Err())
}

// AccountEntriesInOrder lists one account's entries in balance order, oldest
// first: the order a running balance is computed in. Printed lines only, never
// captures (ADR-0002): reconciliation walks this list against the bank's
// balances.
func (q *Queries) AccountEntriesInOrder(ctx context.Context, userID, accountID uuid.UUID) ([]Entry, error) {
	query := `SELECT ` + entryColumns + ` FROM entries WHERE user_id = $1 AND account_id = $2
		ORDER BY ` + entryOrder
	return q.queryEntries(ctx, "store.AccountEntriesInOrder", scanEntry, query, userID, accountID)
}

// entryOrder is the order movements reach an account's balance. booked_at
// orders lines of the same day when the source gives a time; seq keeps the
// source's own order otherwise.
const entryOrder = `balance_on, booked_at NULLS FIRST, seq, id`

// EntryByID returns one of the user's entries: a printed line, never a
// capture (ADR-0002).
func (q *Queries) EntryByID(ctx context.Context, userID, id uuid.UUID) (Entry, error) {
	rows, err := q.queryEntries(ctx, "store.EntryByID", scanEntry,
		`SELECT `+entryColumns+` FROM entries WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return Entry{}, err
	}
	if len(rows) == 0 {
		return Entry{}, wrap("store.EntryByID", ErrNotFound)
	}
	return rows[0], nil
}

// CaptureAsEntry returns one of the user's open captures — not linked to its
// line, not discarded — as a movement with Origin capture, whatever it is now:
// pending, printed or missing. It is how a movement id from a list, or one the
// assistant was given, finds a noted purchase. Anything else, a linked or a
// discarded capture included, is ErrNotFound: its line is the movement now, or
// there is none.
func (q *Queries) CaptureAsEntry(ctx context.Context, userID, id uuid.UUID) (Entry, error) {
	rows, err := q.queryEntries(ctx, "store.CaptureAsEntry", scanMovement,
		`SELECT `+captureAsEntryColumns+` FROM captures
		WHERE id = $1 AND user_id = $2 AND entry_id IS NULL AND discarded_at IS NULL`, id, userID)
	if err != nil {
		return Entry{}, err
	}
	if len(rows) == 0 {
		return Entry{}, wrap("store.CaptureAsEntry", ErrNotFound)
	}
	return rows[0], nil
}

// SetEntryCategory files an entry under a category, or clears it with nil.
func (q *Queries) SetEntryCategory(ctx context.Context, userID, id uuid.UUID, categoryID *uuid.UUID, source string) error {
	var src *string
	if categoryID != nil {
		src = &source
	}
	return q.execOne(ctx, "store.SetEntryCategory",
		`UPDATE entries SET category_id = $3, category_source = $4 WHERE id = $1 AND user_id = $2`,
		id, userID, categoryID, src)
}

// SetEntryNote sets an entry's note.
func (q *Queries) SetEntryNote(ctx context.Context, userID, id uuid.UUID, note string) error {
	return q.execOne(ctx, "store.SetEntryNote",
		`UPDATE entries SET note = $3 WHERE id = $1 AND user_id = $2`, id, userID, note)
}

// SetEntryUserKind overrides what an entry counts as, or restores the
// importer's classification with nil.
func (q *Queries) SetEntryUserKind(ctx context.Context, userID, id uuid.UUID, kind *string) error {
	return q.execOne(ctx, "store.SetEntryUserKind",
		`UPDATE entries SET user_kind = $3::entry_kind WHERE id = $1 AND user_id = $2`, id, userID, kind)
}

// AccountSums is one account's total and the date span of its movements.
type AccountSums struct {
	Total       money.Cents
	Count       int
	FirstDay    civil.Date
	LastDay     civil.Date
	LastBooking civil.Date
}

// SumsByAccount totals every entry of each of the user's accounts. Printed
// lines only, never captures (ADR-0002): a purchase the owner noted must not
// move a balance.
func (q *Queries) SumsByAccount(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]AccountSums, error) {
	rows, err := q.q.Query(ctx, `
		SELECT account_id, COALESCE(SUM(amount_cents), 0), count(*), MIN(balance_on), MAX(balance_on), MAX(booked_on)
		FROM entries WHERE user_id = $1 GROUP BY account_id`, userID)
	if err != nil {
		return nil, wrap("store.SumsByAccount", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]AccountSums{}
	for rows.Next() {
		var id uuid.UUID
		var s AccountSums
		var total int64
		if err := rows.Scan(&id, &total, &s.Count, &s.FirstDay, &s.LastDay, &s.LastBooking); err != nil {
			return nil, wrap("store.SumsByAccount", err)
		}
		s.Total = money.Cents(total)
		out[id] = s
	}
	return out, wrap("store.SumsByAccount", rows.Err())
}

// SumThroughDay totals an account's entries that reached the balance on or
// before day. Printed lines only, never captures (ADR-0002).
func (q *Queries) SumThroughDay(ctx context.Context, accountID uuid.UUID, day civil.Date) (money.Cents, error) {
	var total int64
	err := q.q.QueryRow(ctx, `SELECT COALESCE(SUM(amount_cents), 0) FROM entries
		WHERE account_id = $1 AND balance_on <= $2`, accountID, day).Scan(&total)
	return money.Cents(total), wrap("store.SumThroughDay", err)
}

// SumBefore totals an account's entries that come strictly before the given
// entry in balance order. Printed lines only, never captures (ADR-0002).
func (q *Queries) SumBefore(ctx context.Context, accountID, entryID uuid.UUID) (money.Cents, error) {
	var total int64
	err := q.q.QueryRow(ctx, `
		SELECT COALESCE(SUM(e.amount_cents), 0) FROM entries e, entries x
		WHERE x.id = $2 AND e.account_id = $1
		  AND (e.balance_on, COALESCE(e.booked_at, '-infinity'::timestamptz), e.seq, e.id)
		    < (x.balance_on, COALESCE(x.booked_at, '-infinity'::timestamptz), x.seq, x.id)`,
		accountID, entryID).Scan(&total)
	return money.Cents(total), wrap("store.SumBefore", err)
}

// queryEntries runs a query whose rows scan reads: scanEntry for entryColumns,
// scanMovement for movementColumns.
func (q *Queries) queryEntries(ctx context.Context, op string, scan func(scanner) (Entry, error),
	query string, args ...any) ([]Entry, error) {
	rows, err := q.q.Query(ctx, query, args...)
	if err != nil {
		return nil, wrap(op, err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, wrap(op, err)
		}
		out = append(out, e)
	}
	return out, wrap(op, rows.Err())
}

// scanEntry reads entryColumns: a printed line.
func scanEntry(s scanner) (Entry, error) {
	return scanEntryWith(s)
}

// scanMovement reads movementColumns: a printed line or a pending capture.
func scanMovement(s scanner) (Entry, error) {
	var origin, source string
	e, err := scanEntryWith(s, &origin, &source)
	if err != nil {
		return Entry{}, err
	}
	e.Origin, e.CaptureSource = origin, source
	return e, nil
}

// scanEntryWith reads entryColumns and then whatever columns extra points at.
func scanEntryWith(s scanner, extra ...any) (Entry, error) {
	var (
		e           Entry
		amount      int64
		bankBalance *int64
		raw         []byte
	)
	dest := []any{&e.ID, &e.UserID, &e.AccountID, &e.StatementID, &e.RunID, &e.BookedOn, &e.BookedAt,
		&e.PostedOn, &e.BalanceOn, &e.Seq, &amount, &e.Kind, &e.UserKind, &e.Description, &e.BankRef,
		&e.BankCategory, &bankBalance, &e.CategoryID, &e.CategorySource, &e.Note, &e.DedupKey, &raw,
		&e.CreatedAt}
	if err := s.Scan(append(dest, extra...)...); err != nil {
		return Entry{}, err
	}
	finish(&e, amount, bankBalance, raw)
	return e, nil
}

// finish converts the scanned raw columns into an Entry's typed fields. A row
// of entries is a printed line; scanMovement says otherwise when it is not.
func finish(e *Entry, amount int64, bankBalance *int64, raw []byte) {
	e.Origin = OriginStatement
	e.Amount = money.Cents(amount)
	if bankBalance != nil {
		b := money.Cents(*bankBalance)
		e.BankBalance = &b
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &e.Raw)
	}
}

// prefixed qualifies a comma-separated column list with a table alias.
func prefixed(alias, columns string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = alias + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
