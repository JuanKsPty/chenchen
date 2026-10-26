package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// ErrCaptureKeyInvalid covers every way a capture key can fail: none sent, not
// a capture key at all, one never issued, or one revoked. They all mean the
// same thing to the shortcut — make another key — so none is told apart, and
// none of them ever carries the key itself into an error or a log.
var ErrCaptureKeyInvalid = errors.New("auth: capture key is invalid")

// captureKeyLen is the length of every key NewCaptureKey makes: the prefix
// and an opaque token's 32 bytes in unpadded base64url.
var captureKeyLen = len(CaptureKeyPrefix) + 43

// CaptureScope is everything a capture key grants: noting purchases in one
// account of one user. It is a value handed to the one handler that takes
// keys, never an identity in the request context: nothing that reads
// identity.FromContext can ever see a key's user, so a key cannot reach any
// other endpoint, read anything, or act as the person it belongs to.
type CaptureScope struct {
	UserID    uuid.UUID
	AccountID uuid.UUID
	KeyID     uuid.UUID
	// CardLabel is the first card name the shortcut sent with this key; empty
	// until one arrives.
	CardLabel string
}

// CaptureKeys checks the keys a phone's shortcut presents.
type CaptureKeys struct {
	db *store.DB
}

func NewCaptureKeys(db *store.DB) *CaptureKeys { return &CaptureKeys{db: db} }

// Authenticate reads an Authorization header — "Bearer chn_…", or the key
// alone, since a shortcut is built by hand and either is easy to get — and
// returns what the key may do. Anything not shaped like a capture key, an
// access token included, is refused before the database is asked, so a
// stranger probing the endpoint costs nothing but the rate limit. Only a live
// key's hash is looked up; the key itself is never stored or logged.
//
// It never calls identity.WithUser: see CaptureScope.
func (k *CaptureKeys) Authenticate(ctx context.Context, header string) (CaptureScope, error) {
	key := strings.TrimSpace(header)
	if scheme, rest, found := strings.Cut(key, " "); found && strings.EqualFold(scheme, "Bearer") {
		key = strings.TrimSpace(rest)
	}
	if !looksLikeCaptureKey(key) {
		return CaptureScope{}, ErrCaptureKeyInvalid
	}
	found, err := k.db.Q().LiveCaptureKeyByHash(ctx, HashCaptureKey(key))
	if errors.Is(err, store.ErrNotFound) {
		return CaptureScope{}, ErrCaptureKeyInvalid
	}
	if err != nil {
		return CaptureScope{}, err
	}
	return CaptureScope{UserID: found.UserID, AccountID: found.AccountID, KeyID: found.ID,
		CardLabel: found.CardLabel}, nil
}

// looksLikeCaptureKey reports whether key has the exact shape NewCaptureKey
// gives one: the prefix and 43 base64url characters.
func looksLikeCaptureKey(key string) bool {
	if len(key) != captureKeyLen || !strings.HasPrefix(key, CaptureKeyPrefix) {
		return false
	}
	for _, r := range key[len(CaptureKeyPrefix):] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
