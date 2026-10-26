package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

func TestACaptureKeyIsReplacedNeverDoubled(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "k@example.com")
	card, checking := newCard(t, db, u.ID), newChecking(t, db, u.ID)

	var first store.CaptureKey
	if err := db.InTx(ctx, func(q *store.Queries) error {
		var err error
		first, err = q.CreateCaptureKey(ctx, u.ID, card.ID, "hash-1", "aaaa")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if first.UserID != u.ID || first.AccountID != card.ID || first.Hint != "aaaa" || first.RevokedAt != nil ||
		first.CreatedAt.IsZero() || first.CardLabel != "" || first.TestedAt != nil || first.LastUsedAt != nil {
		t.Fatalf("CreateCaptureKey = %+v", first)
	}
	if got, err := db.Q().LiveCaptureKeyByHash(ctx, "hash-1"); err != nil || got.ID != first.ID {
		t.Fatalf("LiveCaptureKeyByHash = %+v, %v", got, err)
	}

	// Making another revokes the first in the same transaction: the old
	// hash finds nothing, and the account still has exactly one key.
	var second store.CaptureKey
	if err := db.InTx(ctx, func(q *store.Queries) error {
		var err error
		second, err = q.CreateCaptureKey(ctx, u.ID, card.ID, "hash-2", "bbbb")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Q().LiveCaptureKeyByHash(ctx, "hash-1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the replaced key = %v, want ErrNotFound", err)
	}
	if got, err := db.Q().LiveCaptureKeyByHash(ctx, "hash-2"); err != nil || got.ID != second.ID {
		t.Errorf("the new key = %+v, %v", got, err)
	}
	checkingKey, err := db.Q().CreateCaptureKey(ctx, u.ID, checking.ID, "hash-3", "cccc")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := db.Q().CaptureKeysByUser(ctx, u.ID)
	if err != nil || len(keys) != 2 || keys[card.ID].ID != second.ID || keys[checking.ID].ID != checkingKey.ID {
		t.Errorf("CaptureKeysByUser = %+v, %v", keys, err)
	}

	// Someone else can neither make a key for the account nor revoke its own.
	other := newUser(t, db, "other@example.com")
	if _, err := db.Q().CreateCaptureKey(ctx, other.ID, card.ID, "hash-4", "dddd"); err == nil {
		t.Error("another user made a key for the account")
	}
	if err := db.Q().RevokeCaptureKey(ctx, other.ID, card.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Q().LiveCaptureKeyByHash(ctx, "hash-2"); err != nil {
		t.Errorf("another user's revoke reached the key: %v", err)
	}
	if keys, err := db.Q().CaptureKeysByUser(ctx, other.ID); err != nil || len(keys) != 0 {
		t.Errorf("another user's keys = %+v, %v", keys, err)
	}

	// Revoking is idempotent, and only that account's key goes.
	for range 2 {
		if err := db.Q().RevokeCaptureKey(ctx, u.ID, card.ID); err != nil {
			t.Fatal(err)
		}
	}
	keys, err = db.Q().CaptureKeysByUser(ctx, u.ID)
	if err != nil || len(keys) != 1 || keys[checking.ID].ID != checkingKey.ID {
		t.Errorf("CaptureKeysByUser after revoking = %+v, %v", keys, err)
	}
}

func TestUsingACaptureKeyNeedsItLive(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "k@example.com")
	card := newCard(t, db, u.ID)
	key, err := db.Q().CreateCaptureKey(ctx, u.ID, card.ID, "hash", "aaaa")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.October, 15, 12, 41, 0, 0, time.UTC)

	if err := db.Q().MarkCaptureKeyTested(ctx, u.ID, key.ID, at); err != nil {
		t.Fatal(err)
	}
	// The first card name sticks; a later, different one never replaces it.
	if err := db.Q().TouchCaptureKey(ctx, u.ID, key.ID, at, "Visa Oro"); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().TouchCaptureKey(ctx, u.ID, key.ID, at.Add(time.Hour), "Otra"); err != nil {
		t.Fatal(err)
	}
	got, err := db.Q().LiveCaptureKeyByHash(ctx, "hash")
	if err != nil || got.CardLabel != "Visa Oro" || got.TestedAt == nil || !got.TestedAt.Equal(at) ||
		got.LastUsedAt == nil || !got.LastUsedAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("after use = %+v, %v", got, err)
	}

	// Someone else's id for it, or the key once revoked, writes nothing.
	other := newUser(t, db, "other@example.com")
	if err := db.Q().TouchCaptureKey(ctx, other.ID, key.ID, at, ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("TouchCaptureKey as another user = %v, want ErrNotFound", err)
	}
	if err := db.Q().RevokeCaptureKey(ctx, u.ID, card.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().TouchCaptureKey(ctx, u.ID, key.ID, at, ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("TouchCaptureKey on a revoked key = %v, want ErrNotFound", err)
	}
	if err := db.Q().MarkCaptureKeyTested(ctx, u.ID, key.ID, at); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("MarkCaptureKeyTested on a revoked key = %v, want ErrNotFound", err)
	}
}

func TestRecentDuplicateCapture(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "d@example.com")
	card, checking := newCard(t, db, u.ID), newChecking(t, db, u.ID)
	at := time.Date(2026, time.October, 15, 12, 41, 0, 0, civil.Panama)
	first := store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: card.ID, Source: store.CaptureSourceShortcut,
		ReceivedAt: at, BookedOn: day, Amount: -450, Merchant: "Café  Unido"}
	if err := db.Q().InsertCapture(ctx, first); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name     string
		account  store.Account
		amount   money.Cents
		merchant string
		since    time.Time
		want     bool
	}{
		{"the same purchase", card, -450, "Café  Unido", at, true},
		{"its merchant in another case and spacing", card, -450, " CAFÉ unido ", at.Add(-time.Minute), true},
		{"received before since", card, -450, "Café Unido", at.Add(time.Second), false},
		{"another amount", card, -451, "Café Unido", at, false},
		{"another merchant", card, -450, "Café Durán", at, false},
		{"another account", checking, -450, "Café Unido", at, false},
	} {
		got, found, err := db.Q().RecentDuplicateCapture(ctx, u.ID, tt.account.ID, tt.amount, tt.merchant, tt.since)
		if err != nil || found != tt.want || (found && got.ID != first.ID) {
			t.Errorf("%s: RecentDuplicateCapture = %v, %v, %v; want %v", tt.name, got.ID, found, err, tt.want)
		}
	}
	other := newUser(t, db, "other@example.com")
	if _, found, err := db.Q().RecentDuplicateCapture(ctx, other.ID, card.ID, -450, "Café Unido", at); err != nil || found {
		t.Errorf("another user found the purchase: %v, %v", found, err)
	}

	// In any state: a purchase discarded a moment ago was still sent.
	if err := discard(t, db, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := db.Q().RecentDuplicateCapture(ctx, u.ID, card.ID, -450, "Café Unido", at); err != nil || !found {
		t.Errorf("a discarded purchase: %v, %v; want found", found, err)
	}
}
