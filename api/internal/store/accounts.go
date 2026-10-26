package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InstitutionManual is the institution of an account the owner keeps by hand:
// a card someone else pays, whose statements never reach the app. Nothing a
// statement or a sync printed can land in one (migration 00015).
const InstitutionManual = "manual"

// Account is one real-world account: a bank account, a card or a brokerage
// account, keyed the way its institution keys it.
type Account struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	Class          string // asset | liability
	Type           string // checking | savings | credit_card | brokerage
	Institution    string // banco_general | bac | ibkr | manual
	ExternalNumber string
	Currency       string
	DisplayName    string
	Alias          string
	// PaidByOther marks a card someone else pays: its purchases are the
	// owner's spending, its debt is not theirs.
	PaidByOther bool
	CreatedAt   time.Time
}

// KeptByHand reports whether the account lives only on the purchases the
// owner notes, with nothing printed in it.
func (a Account) KeptByHand() bool { return a.Institution == InstitutionManual }

const accountColumns = `id, user_id, class, type, institution, external_number, currency, display_name, alias,
	paid_by_other, created_at`

// EnsureAccount returns the user's account with a's identity, creating it on
// first sight. created reports which happened. An existing account keeps its
// alias and type: the owner may have relabelled it.
func (q *Queries) EnsureAccount(ctx context.Context, a Account) (Account, bool, error) {
	const insert = `
		INSERT INTO accounts (id, user_id, class, type, institution, external_number, currency, display_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, institution, external_number, currency) DO NOTHING
		RETURNING ` + accountColumns

	created, err := scanAccount(q.q.QueryRow(ctx, insert, a.ID, a.UserID, a.Class, a.Type,
		a.Institution, a.ExternalNumber, a.Currency, a.DisplayName))
	if err == nil {
		return created, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Account{}, false, wrap("store.EnsureAccount", err)
	}

	const existing = `SELECT ` + accountColumns + ` FROM accounts
		WHERE user_id = $1 AND institution = $2 AND external_number = $3 AND currency = $4`
	found, err := scanAccount(q.q.QueryRow(ctx, existing, a.UserID, a.Institution, a.ExternalNumber, a.Currency))
	if err != nil {
		return Account{}, false, wrap("store.EnsureAccount", err)
	}
	return found, false, nil
}

// CreateHandCard adds a card someone else pays, ending in last4, that the
// owner calls alias. It is a card and a liability like any other, but kept by
// hand and paid by another: no statement will ever reach it, and its debt is
// not the owner's. Its number is written as a statement prints a card's, with
// only the digits the owner knows, and it has no bank's name for it. A second
// one ending in the same digits is a ConstraintError on
// accounts_user_id_institution_external_number_currency_key.
func (q *Queries) CreateHandCard(ctx context.Context, userID, id uuid.UUID, alias, last4 string) (Account, error) {
	a, err := scanAccount(q.q.QueryRow(ctx, `
		INSERT INTO accounts (id, user_id, class, type, institution, external_number, display_name, alias, paid_by_other)
		VALUES ($1, $2, 'liability', 'credit_card', $3, '**** ' || $4, '', $5, true)
		RETURNING `+accountColumns, id, userID, InstitutionManual, last4, alias))
	return a, wrap("store.CreateHandCard", err)
}

// LastFour is the last four digits of a card's number however it is written —
// '**** 4321', '4321' — or "" when it shows fewer than four.
func LastFour(number string) string {
	digits := make([]byte, 0, len(number))
	for i := 0; i < len(number); i++ {
		if c := number[i]; c >= '0' && c <= '9' {
			digits = append(digits, c)
		}
	}
	if len(digits) < 4 {
		return ""
	}
	return string(digits[len(digits)-4:])
}

// PrintedCardEndingIn reports whether a statement the user imported printed a
// card ending in last4: as the number of a card account, or as the card one of
// its lines was made with (one account's statement can carry several cards).
func (q *Queries) PrintedCardEndingIn(ctx context.Context, userID uuid.UUID, last4 string) (bool, error) {
	var printed bool
	err := q.q.QueryRow(ctx, `
		SELECT EXISTS (
		           SELECT 1 FROM accounts
		           WHERE user_id = $1 AND type = 'credit_card' AND institution <> $3
		             AND right(regexp_replace(external_number, '[^0-9]', '', 'g'), 4) = $2)
		    OR EXISTS (
		           SELECT 1 FROM entries e JOIN accounts a ON a.id = e.account_id
		           WHERE e.user_id = $1 AND a.type = 'credit_card'
		             AND right(regexp_replace(e.raw->>'Tarjeta', '[^0-9]', '', 'g'), 4) = $2)`,
		userID, last4, InstitutionManual).Scan(&printed)
	return printed, wrap("store.PrintedCardEndingIn", err)
}

// AccountsByUser lists a user's accounts: bank accounts, then cards, then
// brokerage, each by name.
func (q *Queries) AccountsByUser(ctx context.Context, userID uuid.UUID) ([]Account, error) {
	const query = `SELECT ` + accountColumns + ` FROM accounts WHERE user_id = $1
		ORDER BY CASE type WHEN 'checking' THEN 0 WHEN 'savings' THEN 0 WHEN 'credit_card' THEN 1 ELSE 2 END,
		         created_at, id`
	rows, err := q.q.Query(ctx, query, userID)
	if err != nil {
		return nil, wrap("store.AccountsByUser", err)
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, wrap("store.AccountsByUser", err)
		}
		out = append(out, a)
	}
	return out, wrap("store.AccountsByUser", rows.Err())
}

// LastSyncsByUser returns when each of the user's linked brokerage accounts
// last synced with IBKR, in one query for the whole account list. Only a sync
// that succeeded counts: ibkr_links stamps last_synced_at on a failed attempt
// too, and "synced two hours ago" must not describe a sync that failed. Only
// these two columns are read; the list never needs the token.
func (q *Queries) LastSyncsByUser(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]time.Time, error) {
	const query = `SELECT account_id, last_synced_at FROM ibkr_links
		WHERE user_id = $1 AND last_sync_status = 'ok' AND last_synced_at IS NOT NULL`
	rows, err := q.q.Query(ctx, query, userID)
	if err != nil {
		return nil, wrap("store.LastSyncsByUser", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]time.Time{}
	for rows.Next() {
		var id uuid.UUID
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, wrap("store.LastSyncsByUser", err)
		}
		out[id] = at
	}
	return out, wrap("store.LastSyncsByUser", rows.Err())
}

// AccountByID returns one of the user's accounts. An account that exists but
// belongs to someone else is ErrNotFound, like one that does not exist.
func (q *Queries) AccountByID(ctx context.Context, userID, id uuid.UUID) (Account, error) {
	const query = `SELECT ` + accountColumns + ` FROM accounts WHERE id = $1 AND user_id = $2`
	a, err := scanAccount(q.q.QueryRow(ctx, query, id, userID))
	return a, wrap("store.AccountByID", err)
}

// SetAccountAlias renames one of the user's accounts.
func (q *Queries) SetAccountAlias(ctx context.Context, userID, id uuid.UUID, alias string) error {
	return q.execOne(ctx, "store.SetAccountAlias",
		`UPDATE accounts SET alias = $3 WHERE id = $1 AND user_id = $2`, id, userID, alias)
}

// SetAccountType relabels a bank account as checking or savings. A card or a
// brokerage account cannot change type; the WHERE clause refuses it.
func (q *Queries) SetAccountType(ctx context.Context, userID, id uuid.UUID, typ string) error {
	return q.execOne(ctx, "store.SetAccountType",
		`UPDATE accounts SET type = $3 WHERE id = $1 AND user_id = $2 AND type IN ('checking', 'savings')`,
		id, userID, typ)
}

// DeleteAccount removes an account and, by cascade, everything imported into
// it: movements, statements, checkpoints, runs, holdings and its IBKR link.
func (q *Queries) DeleteAccount(ctx context.Context, userID, id uuid.UUID) error {
	return q.execOne(ctx, "store.DeleteAccount",
		`DELETE FROM accounts WHERE id = $1 AND user_id = $2`, id, userID)
}

// LockUser serialises every write that touches one user's money. Imports, IBKR
// syncs and checkpoint changes each take it first, so two of them can never
// interleave over the same accounts.
func (q *Queries) LockUser(ctx context.Context, userID uuid.UUID) error {
	var one int
	err := q.q.QueryRow(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&one)
	return wrap("store.LockUser", err)
}

// execOne runs a statement expected to touch exactly one row.
func (q *Queries) execOne(ctx context.Context, op, sql string, args ...any) error {
	tag, err := q.q.Exec(ctx, sql, args...)
	if err != nil {
		return wrap(op, err)
	}
	if tag.RowsAffected() == 0 {
		return wrap(op, pgx.ErrNoRows)
	}
	return nil
}

// scanner is satisfied by both pgx.Row and pgx.Rows, so one scan body serves the
// single-row and multi-row queries alike.
type scanner interface {
	Scan(dest ...any) error
}

func scanAccount(s scanner) (Account, error) {
	var a Account
	err := s.Scan(&a.ID, &a.UserID, &a.Class, &a.Type, &a.Institution, &a.ExternalNumber,
		&a.Currency, &a.DisplayName, &a.Alias, &a.PaidByOther, &a.CreatedAt)
	return a, err
}
