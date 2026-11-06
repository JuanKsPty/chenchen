package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CaptureKey is the key a phone's shortcut notes purchases with, in one
// account and nothing else. Only its hash is stored, for the same reason only
// a refresh or device token's is: a database dump must not be replayable as a
// working shortcut. The owner sees its last four characters, never the key
// again.
type CaptureKey struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	AccountID uuid.UUID
	Hint      string
	// CardLabel is the first card name the shortcut sent, so a different one
	// later can be noticed; empty until one arrives.
	CardLabel string
	CreatedAt time.Time
	// TestedAt is the last time the shortcut was run by hand, with nothing to
	// note; LastUsedAt the last purchase it noted.
	TestedAt   *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

const captureKeyColumns = `id, user_id, account_id, hint, card_label, created_at, tested_at, last_used_at, revoked_at`

// CreateCaptureKey stores a new key for one of the user's accounts, revoking
// the one it had: an account has at most one key that works, so replacing a
// key is how a leaked one is shut off. It runs two statements and belongs in
// the caller's transaction; two creations racing outside one would have the
// second refused by capture_keys_one_live_idx.
func (q *Queries) CreateCaptureKey(ctx context.Context, userID, accountID uuid.UUID, hash, hint string) (CaptureKey, error) {
	if err := q.RevokeCaptureKey(ctx, userID, accountID); err != nil {
		return CaptureKey{}, err
	}
	k, err := scanCaptureKey(q.q.QueryRow(ctx, `
		INSERT INTO capture_keys (id, user_id, account_id, key_hash, hint)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+captureKeyColumns, uuid.New(), userID, accountID, hash, hint))
	return k, wrap("store.CreateCaptureKey", err)
}

// LiveCaptureKeyByHash looks up the key a shortcut presented. A revoked key is
// ErrNotFound, the same as one that never existed: the two look identical to
// whoever is asking. It is the one capture key lookup not scoped to a user,
// because the key is what says which user it is.
func (q *Queries) LiveCaptureKeyByHash(ctx context.Context, hash string) (CaptureKey, error) {
	k, err := scanCaptureKey(q.q.QueryRow(ctx, `SELECT `+captureKeyColumns+` FROM capture_keys
		WHERE key_hash = $1 AND revoked_at IS NULL`, hash))
	return k, wrap("store.LiveCaptureKeyByHash", err)
}

// TouchCaptureKey records a purchase the key noted, and the card name it came
// from the first time one arrives. A key revoked meanwhile is ErrNotFound, so
// the caller's transaction, and the purchase in it, can be undone: once the
// owner revokes a key, nothing it sent is kept, however close the race.
func (q *Queries) TouchCaptureKey(ctx context.Context, userID, id uuid.UUID, now time.Time, cardLabel string) error {
	return q.execOne(ctx, "store.TouchCaptureKey", `
		UPDATE capture_keys SET last_used_at = $3,
		       card_label = CASE WHEN card_label = '' THEN $4 ELSE card_label END
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID, now, cardLabel)
}

// MarkCaptureKeyTested records the shortcut being run by hand, which sends
// nothing to note: the owner checking that it is connected. A revoked key is
// ErrNotFound, as in TouchCaptureKey.
func (q *Queries) MarkCaptureKeyTested(ctx context.Context, userID, id uuid.UUID, now time.Time) error {
	return q.execOne(ctx, "store.MarkCaptureKeyTested",
		`UPDATE capture_keys SET tested_at = $3 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		id, userID, now)
}

// RevokeCaptureKey shuts off the key of one of the user's accounts. Idempotent
// and silent about whether there was one, like RevokeTrustedDevice: the owner
// switching it off never needs to know. The row stays, revoked, so the
// purchases it noted still say which key sent them.
func (q *Queries) RevokeCaptureKey(ctx context.Context, userID, accountID uuid.UUID) error {
	_, err := q.q.Exec(ctx, `UPDATE capture_keys SET revoked_at = now()
		WHERE user_id = $1 AND account_id = $2 AND revoked_at IS NULL`, userID, accountID)
	return wrap("store.RevokeCaptureKey", err)
}

// CaptureKeysByUser returns the live key of each of the user's accounts that
// has one, in one query for the whole account list. The hash is never read.
func (q *Queries) CaptureKeysByUser(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]CaptureKey, error) {
	rows, err := q.q.Query(ctx, `SELECT `+captureKeyColumns+` FROM capture_keys
		WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return nil, wrap("store.CaptureKeysByUser", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]CaptureKey{}
	for rows.Next() {
		k, err := scanCaptureKey(rows)
		if err != nil {
			return nil, wrap("store.CaptureKeysByUser", err)
		}
		out[k.AccountID] = k
	}
	return out, wrap("store.CaptureKeysByUser", rows.Err())
}

func scanCaptureKey(s scanner) (CaptureKey, error) {
	var k CaptureKey
	err := s.Scan(&k.ID, &k.UserID, &k.AccountID, &k.Hint, &k.CardLabel, &k.CreatedAt, &k.TestedAt,
		&k.LastUsedAt, &k.RevokedAt)
	return k, err
}
