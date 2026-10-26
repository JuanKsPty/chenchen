package captures

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/auth"
	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/identity"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// NewKey is a shortcut key as it is made: the key itself, which the owner sees
// this once and never again, and what names it afterwards.
type NewKey struct {
	Key string
	// Hint is the key's last four characters, all the owner sees of it later.
	Hint      string
	CreatedAt time.Time
}

// CreateKey makes a shortcut key for one of the user's accounts, revoking the
// one it had in the same transaction: making another key is how a leaked one
// is shut off, and an account never has two that work. A brokerage account
// takes no purchases, so it gets no key.
func (s *Service) CreateKey(ctx context.Context, userID, accountID uuid.UUID) (NewKey, error) {
	key, hash, err := auth.NewCaptureKey()
	if err != nil {
		return NewKey{}, err
	}
	var out NewKey
	err = s.db.InTx(ctx, func(q *store.Queries) error {
		account, err := s.keyAccount(ctx, q, userID, accountID)
		if err != nil {
			return err
		}
		if account.Type == "brokerage" {
			return ErrBrokerage
		}
		k, err := q.CreateCaptureKey(ctx, userID, account.ID, hash, key[len(key)-4:])
		if err != nil {
			return err
		}
		out = NewKey{Key: key, Hint: k.Hint, CreatedAt: k.CreatedAt}
		return nil
	})
	return out, err
}

// RevokeKey shuts off the shortcut key of one of the user's accounts, if it
// has one: its shortcut stops noting purchases at once. What it already noted
// stays.
func (s *Service) RevokeKey(ctx context.Context, userID, accountID uuid.UUID) error {
	return s.db.InTx(ctx, func(q *store.Queries) error {
		if _, err := s.keyAccount(ctx, q, userID, accountID); err != nil {
			return err
		}
		return q.RevokeCaptureKey(ctx, userID, accountID)
	})
}

// keyAccount takes the user's lock, as Ingest does, so a key changes only
// between purchases, never during one, and reads the account it is for.
func (s *Service) keyAccount(ctx context.Context, q *store.Queries, userID, accountID uuid.UUID) (store.Account, error) {
	if err := q.LockUser(ctx, userID); err != nil {
		return store.Account{}, err
	}
	account, err := q.AccountByID(ctx, userID, accountID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Account{}, fmt.Errorf("%w: %v", ErrAccountNotFound, err)
	}
	return account, err
}

// KeyView is a key just made, on the wire: the only response that ever
// carries one.
type KeyView struct {
	Key       string    `json:"key"`
	Hint      string    `json:"hint"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *Handler) createKey(w http.ResponseWriter, r *http.Request) {
	id, err := keyAccountID(r)
	if err != nil {
		httpx.Fail(w, r, TranslateError(err))
		return
	}
	k, err := h.svc.CreateKey(r.Context(), identity.MustFromContext(r.Context()), id)
	if err != nil {
		httpx.Fail(w, r, TranslateError(err))
		return
	}
	httpx.JSON(w, r, http.StatusCreated, KeyView{Key: k.Key, Hint: k.Hint, CreatedAt: k.CreatedAt})
}

func (h *Handler) revokeKey(w http.ResponseWriter, r *http.Request) {
	id, err := keyAccountID(r)
	if err != nil {
		httpx.Fail(w, r, TranslateError(err))
		return
	}
	if err := h.svc.RevokeKey(r.Context(), identity.MustFromContext(r.Context()), id); err != nil {
		httpx.Fail(w, r, TranslateError(err))
		return
	}
	httpx.NoContent(w)
}

// keyAccountID reads the {accountID} URL parameter; a malformed id is a 404
// like a missing account.
func keyAccountID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "accountID"))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %q", ErrAccountNotFound, chi.URLParam(r, "accountID"))
	}
	return id, nil
}
