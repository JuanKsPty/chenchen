package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

// day is the day every capture below was noted on.
var day = civil.Date{Year: 2026, Month: time.October, Day: 15}

func TestACaptureRoundTripsWithItsState(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	cat, err := db.Q().CreateCategory(ctx, store.Category{ID: uuid.New(), UserID: u.ID, Name: "Comida", Kind: "expense"})
	if err != nil {
		t.Fatal(err)
	}
	key := newKey(t, db, card)

	received := time.Date(2026, time.October, 15, 23, 30, 0, 0, civil.Panama)
	src := "rule"
	in := store.Capture{
		ID: uuid.New(), UserID: u.ID, AccountID: card.ID, Source: store.CaptureSourceShortcut, KeyID: &key,
		ReceivedAt: received, BookedOn: day, Amount: -450, Merchant: "Café Unido",
		CategoryID: &cat.ID, CategorySource: &src, Note: "con Ana",
		Raw: map[string]string{"amount": "US$4.50", "card": "Visa Oro"},
	}
	if err := db.Q().InsertCapture(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := db.Q().CaptureByID(ctx, u.ID, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.CapturePending || got.Amount != -450 || got.BookedOn != day || !got.ReceivedAt.Equal(received) ||
		got.Merchant != "Café Unido" || got.Note != "con Ana" || got.Source != store.CaptureSourceShortcut {
		t.Errorf("capture = %+v", got)
	}
	if got.KeyID == nil || *got.KeyID != key || got.CategoryID == nil || *got.CategoryID != cat.ID ||
		got.CategorySource == nil || *got.CategorySource != "rule" {
		t.Errorf("key/category = %v, %v, %v", got.KeyID, got.CategoryID, got.CategorySource)
	}
	if got.EntryID != nil || got.LinkedBy != nil || got.DiscardedAt != nil {
		t.Errorf("a new capture is already linked or discarded: %+v", got)
	}
	if got.Raw["amount"] != "US$4.50" || got.Raw["card"] != "Visa Oro" {
		t.Errorf("raw = %v", got.Raw)
	}

	// Typed by hand: no key, no raw, received now.
	before := time.Now().Add(-time.Minute)
	manual := store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: card.ID, Source: store.CaptureSourceManual,
		BookedOn: day.AddDays(-1), Amount: -1200, Merchant: "Super 99"}
	if err := db.Q().InsertCapture(ctx, manual); err != nil {
		t.Fatal(err)
	}
	m, err := db.Q().CaptureByID(ctx, u.ID, manual.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.KeyID != nil || m.ReceivedAt.Before(before) || len(m.Raw) != 0 {
		t.Errorf("manual capture = %+v", m)
	}

	list, err := db.Q().CapturesByAccount(ctx, u.ID, card.ID)
	if err != nil || len(list) != 2 || list[0].ID != in.ID || list[1].ID != manual.ID {
		t.Fatalf("CapturesByAccount = %+v, %v; want newest first", list, err)
	}

	// Somebody else sees none of it.
	other := newUser(t, db, "other@example.com")
	if _, err := db.Q().CaptureByID(ctx, other.ID, in.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("another user's CaptureByID = %v, want ErrNotFound", err)
	}
	if list, err := db.Q().CapturesByAccount(ctx, other.ID, card.ID); err != nil || len(list) != 0 {
		t.Errorf("another user's CapturesByAccount = %v, %v", list, err)
	}
}

func TestTheSchemaRefusesImpossibleCaptures(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	checking := newChecking(t, db, u.ID)

	t.Run("an amount that is not a purchase", func(t *testing.T) {
		for _, amount := range []money.Cents{0, 450} {
			err := db.Q().InsertCapture(ctx, store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: card.ID,
				Source: store.CaptureSourceManual, BookedOn: day, Amount: amount})
			refusedBy(t, err, "captures_amount_cents_check")
		}
	})

	t.Run("an unknown source", func(t *testing.T) {
		err := db.Q().InsertCapture(ctx, store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: card.ID,
			Source: "bank", BookedOn: day, Amount: -1})
		refusedBy(t, err, "captures_source_check")
	})

	t.Run("a line of another account", func(t *testing.T) {
		line := addLine(t, db, checking, day, -450, store.KindExpense, nil)
		c := addCapture(t, db, card, day, -450)
		refusedBy(t, link(t, db, c, line), "captures_entry_id_account_id_fkey")
	})

	t.Run("two captures for one line", func(t *testing.T) {
		line := addLine(t, db, card, day, -700, store.KindExpense, nil)
		first, second := addCapture(t, db, card, day, -700), addCapture(t, db, card, day, -700)
		if err := link(t, db, first, line); err != nil {
			t.Fatal(err)
		}
		refusedBy(t, link(t, db, second, line), "captures_one_per_line_idx")
	})

	t.Run("a line without who linked it, or the other way round", func(t *testing.T) {
		line := addLine(t, db, card, day, -800, store.KindExpense, nil)
		c := addCapture(t, db, card, day, -800)
		refusedBy(t, storetest.Exec(t, db, `UPDATE captures SET entry_id = $2 WHERE id = $1`, c, line),
			"captures_linked_has_who")
		refusedBy(t, storetest.Exec(t, db, `UPDATE captures SET linked_by = 'user' WHERE id = $1`, c),
			"captures_linked_has_who")
	})

	t.Run("linked and discarded at once", func(t *testing.T) {
		line := addLine(t, db, card, day, -900, store.KindExpense, nil)
		c := addCapture(t, db, card, day, -900)
		if err := link(t, db, c, line); err != nil {
			t.Fatal(err)
		}
		refusedBy(t, discard(t, db, c), "captures_linked_xor_discarded")
	})

	t.Run("two live keys for one account", func(t *testing.T) {
		account := newCard(t, db, u.ID)
		first := newKey(t, db, account)
		refusedBy(t, insertKey(t, db, account, uuid.New()), "capture_keys_one_live_idx")
		// Revoking the first frees the slot: replacing a key is revoke, then insert.
		if err := storetest.Exec(t, db, `UPDATE capture_keys SET revoked_at = now() WHERE id = $1`, first); err != nil {
			t.Fatal(err)
		}
		if err := insertKey(t, db, account, uuid.New()); err != nil {
			t.Errorf("a key after revoking the last one: %v", err)
		}
	})

	t.Run("a key of another account", func(t *testing.T) {
		key := newKey(t, db, checking)
		err := db.Q().InsertCapture(ctx, store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: card.ID,
			Source: store.CaptureSourceShortcut, KeyID: &key, BookedOn: day, Amount: -1})
		refusedBy(t, err, "captures_key_id_account_id_fkey")
	})

	other := newUser(t, db, "other@example.com")
	theirs := newCard(t, db, other.ID)

	t.Run("another user's account", func(t *testing.T) {
		err := db.Q().InsertCapture(ctx, store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: theirs.ID,
			Source: store.CaptureSourceManual, BookedOn: day, Amount: -1})
		refusedBy(t, err, "captures_account_id_user_id_fkey")
		err = storetest.Exec(t, db, `INSERT INTO capture_keys (id, user_id, account_id, key_hash, hint)
			VALUES ($1, $2, $3, $4, 'abcd')`, uuid.New(), u.ID, theirs.ID, uuid.NewString())
		refusedBy(t, err, "capture_keys_account_id_user_id_fkey")
	})

	t.Run("another user's category", func(t *testing.T) {
		cat, err := db.Q().CreateCategory(ctx, store.Category{ID: uuid.New(), UserID: other.ID, Name: "Suya", Kind: "expense"})
		if err != nil {
			t.Fatal(err)
		}
		src := "user"
		err = db.Q().InsertCapture(ctx, store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: card.ID,
			Source: store.CaptureSourceManual, BookedOn: day, Amount: -1, CategoryID: &cat.ID, CategorySource: &src})
		refusedBy(t, err, "captures_category_id_user_id_fkey")
	})

	t.Run("rejecting another user's line", func(t *testing.T) {
		line := addLine(t, db, theirs, day, -450, store.KindExpense, nil)
		c := addCapture(t, db, card, day, -450)
		refusedBy(t, reject(t, db, c, line), "capture_rejections_entry_id_user_id_fkey")
	})
}

// The window is pinned to store.CaptureDaysBefore/After: moving either
// constant without the view, or the view without the constant, fails here.
func TestALineInsideTheWindowPrintsACapture(t *testing.T) {
	db := storetest.New(t)
	u := newUser(t, db, "c@example.com")

	tests := []struct {
		name   string
		lineOn civil.Date
		amount money.Cents
		want   string
	}{
		{"a day before the window", day.AddDays(-store.CaptureDaysBefore - 1), -450, store.CapturePending},
		{"the window's first day", day.AddDays(-store.CaptureDaysBefore), -450, store.CapturePrinted},
		{"the same day", day, -450, store.CapturePrinted},
		{"the window's last day", day.AddDays(store.CaptureDaysAfter), -450, store.CapturePrinted},
		{"a day after the window", day.AddDays(store.CaptureDaysAfter + 1), -450, store.CapturePending},
		{"a cent apart", day, -451, store.CapturePending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := newCard(t, db, u.ID)
			addLine(t, db, card, tt.lineOn, tt.amount, store.KindExpense, nil)
			c := addCapture(t, db, card, day, -450)
			if got := stateOf(t, db, u.ID, c); got != tt.want {
				t.Errorf("line on %v for %v: state = %s, want %s", tt.lineOn, tt.amount, got, tt.want)
			}
		})
	}

	t.Run("the same line in another account", func(t *testing.T) {
		card, checking := newCard(t, db, u.ID), newChecking(t, db, u.ID)
		addLine(t, db, checking, day, -450, store.KindExpense, nil)
		c := addCapture(t, db, card, day, -450)
		if got := stateOf(t, db, u.ID, c); got != store.CapturePending {
			t.Errorf("state = %s, want pending", got)
		}
	})
}

func TestOnlyAnUnclaimedUnrejectedExpenseCanPrintACapture(t *testing.T) {
	db := storetest.New(t)
	u := newUser(t, db, "c@example.com")
	expense, fee, transfer := store.KindExpense, store.KindFee, store.KindTransfer

	kinds := []struct {
		name     string
		kind     string
		userKind *string
		want     string
	}{
		{"an expense", store.KindExpense, nil, store.CapturePrinted},
		{"a fee", store.KindFee, nil, store.CapturePending},
		{"a transfer", store.KindTransfer, nil, store.CapturePending},
		{"a fee the owner made an expense", store.KindFee, &expense, store.CapturePrinted},
		// Calling a printed expense something else never brings its purchase
		// back: as a transfer it would count while its line does not, and as a
		// fee it would count twice.
		{"an expense the owner made a transfer", store.KindExpense, &transfer, store.CapturePrinted},
		{"an expense the owner made a fee", store.KindExpense, &fee, store.CapturePrinted},
	}
	for _, tt := range kinds {
		t.Run(tt.name, func(t *testing.T) {
			card := newCard(t, db, u.ID)
			addLine(t, db, card, day, -450, tt.kind, tt.userKind)
			c := addCapture(t, db, card, day, -450)
			if got := stateOf(t, db, u.ID, c); got != tt.want {
				t.Errorf("state = %s, want %s", got, tt.want)
			}
		})
	}

	t.Run("a line another capture claimed", func(t *testing.T) {
		card := newCard(t, db, u.ID)
		line := addLine(t, db, card, day, -450, store.KindExpense, nil)
		mine, twin := addCapture(t, db, card, day, -450), addCapture(t, db, card, day, -450)
		// Unclaimed, the one line could be either: neither counts.
		for _, c := range []uuid.UUID{mine, twin} {
			if got := stateOf(t, db, u.ID, c); got != store.CapturePrinted {
				t.Errorf("before linking: state = %s, want printed", got)
			}
		}
		if err := link(t, db, mine, line); err != nil {
			t.Fatal(err)
		}
		if got := stateOf(t, db, u.ID, mine); got != store.CaptureLinked {
			t.Errorf("linked capture: state = %s, want linked", got)
		}
		if got := stateOf(t, db, u.ID, twin); got != store.CapturePending {
			t.Errorf("its twin: state = %s, want pending", got)
		}
	})

	t.Run("a line rejected for this capture", func(t *testing.T) {
		card := newCard(t, db, u.ID)
		line := addLine(t, db, card, day, -450, store.KindExpense, nil)
		rejected, other := addCapture(t, db, card, day, -450), addCapture(t, db, card, day, -450)
		if err := reject(t, db, rejected, line); err != nil {
			t.Fatal(err)
		}
		if got := stateOf(t, db, u.ID, rejected); got != store.CapturePending {
			t.Errorf("rejected pair: state = %s, want pending", got)
		}
		// The rejection is of one pair, not of the line.
		if got := stateOf(t, db, u.ID, other); got != store.CapturePrinted {
			t.Errorf("another capture of the same line: state = %s, want printed", got)
		}
	})
}

// The settle delay is pinned to store.CaptureSettleDays the same way.
func TestAStatementPastTheSettleDaysMakesACaptureMissing(t *testing.T) {
	db := storetest.New(t)
	u := newUser(t, db, "c@example.com")

	tests := []struct {
		name  string
		endOn civil.Date
		want  string
	}{
		{"a day short", day.AddDays(store.CaptureSettleDays - 1), store.CapturePending},
		{"exactly settled", day.AddDays(store.CaptureSettleDays), store.CaptureMissing},
		{"long past", day.AddDays(40), store.CaptureMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := newCard(t, db, u.ID)
			addStatement(t, db, card, tt.endOn)
			c := addCapture(t, db, card, day, -450)
			if got := stateOf(t, db, u.ID, c); got != tt.want {
				t.Errorf("statement to %v: state = %s, want %s", tt.endOn, got, tt.want)
			}
		})
	}

	t.Run("another account's statement", func(t *testing.T) {
		card, checking := newCard(t, db, u.ID), newChecking(t, db, u.ID)
		addStatement(t, db, checking, day.AddDays(40))
		c := addCapture(t, db, card, day, -450)
		if got := stateOf(t, db, u.ID, c); got != store.CapturePending {
			t.Errorf("state = %s, want pending", got)
		}
	})

	t.Run("a statement that prints it", func(t *testing.T) {
		card := newCard(t, db, u.ID)
		addStatement(t, db, card, day.AddDays(40))
		addLine(t, db, card, day.AddDays(2), -450, store.KindExpense, nil)
		c := addCapture(t, db, card, day, -450)
		if got := stateOf(t, db, u.ID, c); got != store.CapturePrinted {
			t.Errorf("state = %s, want printed", got)
		}
	})
}

func TestLinkedAndDiscardedCapturesStaySo(t *testing.T) {
	db := storetest.New(t)
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	addStatement(t, db, card, day.AddDays(40))
	line := addLine(t, db, card, day, -450, store.KindExpense, nil)
	addLine(t, db, card, day.AddDays(1), -450, store.KindExpense, nil)

	linked := addCapture(t, db, card, day, -450)
	if err := link(t, db, linked, line); err != nil {
		t.Fatal(err)
	}
	discarded := addCapture(t, db, card, day, -450)
	if err := discard(t, db, discarded); err != nil {
		t.Fatal(err)
	}
	// A second line it could be, and a statement long past it, change neither.
	c, err := db.Q().CaptureByID(context.Background(), u.ID, linked)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != store.CaptureLinked || c.EntryID == nil || *c.EntryID != line || c.LinkedBy == nil || *c.LinkedBy != "auto" {
		t.Errorf("linked capture = %+v", c)
	}
	c, err = db.Q().CaptureByID(context.Background(), u.ID, discarded)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != store.CaptureDiscarded || c.DiscardedAt == nil {
		t.Errorf("discarded capture = %+v", c)
	}
}

func TestACaptureGoesWithItsLineAndItsAccount(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)

	// Nothing deletes a printed line today; if anything ever does, the capture
	// it was linked to must not come back as pending and count again.
	line := addLine(t, db, card, day, -450, store.KindExpense, nil)
	linked := addCapture(t, db, card, day, -450)
	if err := link(t, db, linked, line); err != nil {
		t.Fatal(err)
	}
	if err := storetest.Exec(t, db, `DELETE FROM entries WHERE id = $1`, line); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Q().CaptureByID(ctx, u.ID, linked); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("CaptureByID after its line was deleted = %v, want ErrNotFound", err)
	}

	hash := uuid.NewString()
	if err := insertKeyWithHash(t, db, card, uuid.New(), hash); err != nil {
		t.Fatal(err)
	}
	other := addLine(t, db, card, day, -900, store.KindExpense, nil)
	open := addCapture(t, db, card, day, -900)
	if err := reject(t, db, open, other); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().DeleteAccount(ctx, u.ID, card.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Q().CaptureByID(ctx, u.ID, open); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("CaptureByID after its account was deleted = %v, want ErrNotFound", err)
	}
	// The key's hash is free again: its row went with the account.
	if err := insertKeyWithHash(t, db, newCard(t, db, u.ID), uuid.New(), hash); err != nil {
		t.Errorf("the deleted account's key outlived it: %v", err)
	}
}

// TestBalancesNeverSeeCaptures is ADR-0002's promise: every read a balance,
// an anchor, a drift, a reconciliation or a transfer match is built from gives
// the same answer with captures in every state on the same accounts.
func TestBalancesNeverSeeCaptures(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card, checking := newCard(t, db, u.ID), newChecking(t, db, u.ID)

	addLine(t, db, checking, day.AddDays(-20), 200000, store.KindIncome, nil)
	addLine(t, db, checking, day.AddDays(-1), -5000, store.KindExpense, nil) // pays the card...
	addLine(t, db, card, day, 5000, store.KindIncome, nil)                   // ...which receives it
	addLine(t, db, card, day.AddDays(-3), -950, store.KindExpense, nil)
	lunch := addLine(t, db, card, day, -2500, store.KindExpense, nil)
	last := addLine(t, db, card, day.AddDays(2), 10000, store.KindIncome, nil)
	addStatement(t, db, card, day.AddDays(4))

	type reads struct {
		Sums                         map[uuid.UUID]store.AccountSums
		CardThrough, CheckingThrough money.Cents
		CardBefore                   money.Cents
		CardOrder, CheckingOrder     []store.Entry
		Transfers                    []store.TransferCandidate
		Lunch                        store.Entry
	}
	read := func() reads {
		t.Helper()
		var r reads
		var err error
		if r.Sums, err = db.Q().SumsByAccount(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		if r.CardThrough, err = db.Q().SumThroughDay(ctx, card.ID, day.AddDays(30)); err != nil {
			t.Fatal(err)
		}
		if r.CheckingThrough, err = db.Q().SumThroughDay(ctx, checking.ID, day.AddDays(30)); err != nil {
			t.Fatal(err)
		}
		if r.CardBefore, err = db.Q().SumBefore(ctx, card.ID, last); err != nil {
			t.Fatal(err)
		}
		if r.CardOrder, err = db.Q().AccountEntriesInOrder(ctx, u.ID, card.ID); err != nil {
			t.Fatal(err)
		}
		if r.CheckingOrder, err = db.Q().AccountEntriesInOrder(ctx, u.ID, checking.ID); err != nil {
			t.Fatal(err)
		}
		if r.Transfers, err = db.Q().TransferCandidates(ctx, u.ID, 5, civil.Date{}, civil.Date{}); err != nil {
			t.Fatal(err)
		}
		if r.Lunch, err = db.Q().EntryByID(ctx, u.ID, lunch); err != nil {
			t.Fatal(err)
		}
		return r
	}
	want := read()
	if len(want.Transfers) != 1 || len(want.CardOrder) != 4 || want.CardBefore != 5000-950-2500 {
		t.Fatalf("the fixture is not what this test assumes: %+v", want)
	}

	// One capture in every state, on both accounts.
	pending := addCapture(t, db, card, day.AddDays(3), -1234)
	printed := addCapture(t, db, card, day.AddDays(-3), -950)
	linked := addCapture(t, db, card, day, -2500)
	if err := link(t, db, linked, lunch); err != nil {
		t.Fatal(err)
	}
	missing := addCapture(t, db, card, day.AddDays(-10), -333)
	discarded := addCapture(t, db, checking, day, -777)
	if err := discard(t, db, discarded); err != nil {
		t.Fatal(err)
	}
	// A purchase the size of the card's 100.00 credit: as an entry it would be
	// the other half of a transfer.
	bait := addCapture(t, db, checking, day.AddDays(1), -10000)
	for id, want := range map[uuid.UUID]string{
		pending: store.CapturePending, printed: store.CapturePrinted, linked: store.CaptureLinked,
		missing: store.CaptureMissing, discarded: store.CaptureDiscarded, bait: store.CapturePending,
	} {
		if got := stateOf(t, db, u.ID, id); got != want {
			t.Fatalf("fixture capture: state = %s, want %s", got, want)
		}
	}

	got := read()
	if !reflect.DeepEqual(got.Sums, want.Sums) {
		t.Errorf("SumsByAccount = %+v, want %+v", got.Sums, want.Sums)
	}
	if got.CardThrough != want.CardThrough || got.CheckingThrough != want.CheckingThrough {
		t.Errorf("SumThroughDay = %v, %v; want %v, %v",
			got.CardThrough, got.CheckingThrough, want.CardThrough, want.CheckingThrough)
	}
	if got.CardBefore != want.CardBefore {
		t.Errorf("SumBefore = %v, want %v", got.CardBefore, want.CardBefore)
	}
	if !reflect.DeepEqual(got.CardOrder, want.CardOrder) || !reflect.DeepEqual(got.CheckingOrder, want.CheckingOrder) {
		t.Errorf("AccountEntriesInOrder changed:\ncard %+v\nwant %+v\nchecking %+v\nwant %+v",
			got.CardOrder, want.CardOrder, got.CheckingOrder, want.CheckingOrder)
	}
	if !reflect.DeepEqual(got.Transfers, want.Transfers) {
		t.Errorf("TransferCandidates = %+v, want %+v", got.Transfers, want.Transfers)
	}
	if !reflect.DeepEqual(got.Lunch, want.Lunch) {
		t.Errorf("EntryByID of a linked line = %+v, want %+v", got.Lunch, want.Lunch)
	}
	for _, id := range []uuid.UUID{pending, printed, linked, missing, discarded, bait} {
		if _, err := db.Q().EntryByID(ctx, u.ID, id); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("EntryByID(capture) = %v, want ErrNotFound", err)
		}
	}
}

// An open capture reads as a movement through CaptureAsEntry, and a pending
// one is listed by Entries in exactly the same shape.
func TestAnOpenCaptureReadsAsAMovement(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	addStatement(t, db, card, day)
	key := newKey(t, db, card)

	received := time.Date(2026, time.October, 15, 12, 41, 0, 0, civil.Panama)
	tapped := store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: card.ID, Source: store.CaptureSourceShortcut,
		KeyID: &key, ReceivedAt: received, BookedOn: day, Amount: -450, Merchant: "Café Unido", Note: "con Ana",
		Raw: map[string]string{"amount": "$4.50"}}
	if err := db.Q().InsertCapture(ctx, tapped); err != nil {
		t.Fatal(err)
	}
	typed := addCapture(t, db, card, day, -1200)
	line := addLine(t, db, card, day.AddDays(1), -999, store.KindExpense, nil)
	printed := addCapture(t, db, card, day, -999)
	missing := addCapture(t, db, card, day.AddDays(-3), -333)
	linked := addCapture(t, db, card, day, -777)
	if err := link(t, db, linked, addLine(t, db, card, day, -777, store.KindExpense, nil)); err != nil {
		t.Fatal(err)
	}
	discarded := addCapture(t, db, card, day, -555)
	if err := discard(t, db, discarded); err != nil {
		t.Fatal(err)
	}

	got, err := db.Q().CaptureAsEntry(ctx, u.ID, tapped.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin != store.OriginCapture || got.CaptureSource != store.CaptureSourceShortcut ||
		got.Kind != store.KindExpense || got.UserKind != nil || got.Amount != -450 || got.Description != "Café Unido" ||
		got.Note != "con Ana" || got.BookedOn != day || got.BalanceOn != day || got.AccountID != card.ID {
		t.Errorf("capture as a movement = %+v", got)
	}
	if got.BookedAt == nil || !got.BookedAt.Equal(received) {
		t.Errorf("BookedAt = %v, want the moment the shortcut sent it, %s", got.BookedAt, received)
	}
	if got.StatementID != nil || got.RunID != nil || got.PostedOn != nil || got.BankBalance != nil ||
		got.BankRef != "" || got.BankCategory != "" || got.DedupKey != "" || got.Raw["amount"] != "$4.50" {
		t.Errorf("a capture carries printed fields: %+v", got)
	}
	if m, err := db.Q().CaptureAsEntry(ctx, u.ID, typed); err != nil || m.BookedAt != nil || m.CaptureSource != store.CaptureSourceManual {
		t.Errorf("a capture typed by hand = %+v, %v; want no time of day", m, err)
	}
	// Printed and missing captures are still open: an id handed out while
	// they were pending still finds them.
	for _, id := range []uuid.UUID{printed, missing} {
		if _, err := db.Q().CaptureAsEntry(ctx, u.ID, id); err != nil {
			t.Errorf("CaptureAsEntry(open capture) = %v", err)
		}
	}
	other := newUser(t, db, "other@example.com")
	for name, c := range map[string]struct {
		user, id uuid.UUID
	}{
		"linked": {u.ID, linked}, "discarded": {u.ID, discarded}, "a printed line": {u.ID, line},
		"someone else's": {other.ID, tapped.ID},
	} {
		if _, err := db.Q().CaptureAsEntry(ctx, c.user, c.id); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("CaptureAsEntry(%s) = %v, want ErrNotFound", name, err)
		}
	}

	// The list holds the printed lines and the pending captures, nothing else,
	// and a pending capture reads the same either way.
	list, err := db.Q().Entries(ctx, store.EntryFilter{UserID: u.ID})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uuid.UUID]store.Entry{}
	for _, e := range list {
		byID[e.ID] = e
		if want := store.OriginStatement; e.ID != tapped.ID && e.ID != typed && e.Origin != want {
			t.Errorf("line %s has origin %q, want %q", e.Description, e.Origin, want)
		}
	}
	if len(list) != 4 {
		t.Errorf("Entries = %d movements, want 2 lines and 2 pending captures", len(list))
	}
	if !reflect.DeepEqual(byID[tapped.ID], got) {
		t.Errorf("listed capture = %+v\nwant %+v", byID[tapped.ID], got)
	}
	for _, id := range []uuid.UUID{printed, missing, linked, discarded} {
		if _, listed := byID[id]; listed {
			t.Errorf("Entries lists a capture that is not pending: %s", stateOf(t, db, u.ID, id))
		}
	}
	if line, err := db.Q().EntryByID(ctx, u.ID, line); err != nil || line.Origin != store.OriginStatement {
		t.Errorf("EntryByID = %+v, %v; want a line of origin statement", line, err)
	}
}

func TestOnlyAnOpenCaptureTakesACategoryOrANote(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	cat, err := db.Q().CreateCategory(ctx, store.Category{ID: uuid.New(), UserID: u.ID, Name: "Comida", Kind: "expense"})
	if err != nil {
		t.Fatal(err)
	}

	open := addCapture(t, db, card, day, -450)
	if err := db.Q().SetCaptureCategory(ctx, u.ID, open, &cat.ID, "assistant"); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().SetCaptureNote(ctx, u.ID, open, "con Ana"); err != nil {
		t.Fatal(err)
	}
	c, err := db.Q().CaptureByID(ctx, u.ID, open)
	if err != nil {
		t.Fatal(err)
	}
	if c.CategoryID == nil || *c.CategoryID != cat.ID || c.CategorySource == nil || *c.CategorySource != "assistant" ||
		c.Note != "con Ana" || !c.UpdatedAt.After(c.CreatedAt) {
		t.Errorf("capture = %+v", c)
	}
	if err := db.Q().SetCaptureCategory(ctx, u.ID, open, nil, "user"); err != nil {
		t.Fatal(err)
	}
	if c, _ = db.Q().CaptureByID(ctx, u.ID, open); c.CategoryID != nil || c.CategorySource != nil {
		t.Errorf("a cleared category left %v by %v", c.CategoryID, c.CategorySource)
	}

	linked := addCapture(t, db, card, day, -777)
	if err := link(t, db, linked, addLine(t, db, card, day, -777, store.KindExpense, nil)); err != nil {
		t.Fatal(err)
	}
	discarded := addCapture(t, db, card, day, -555)
	if err := discard(t, db, discarded); err != nil {
		t.Fatal(err)
	}
	other := newUser(t, db, "other@example.com")
	for name, c := range map[string]struct {
		user, id uuid.UUID
	}{"linked": {u.ID, linked}, "discarded": {u.ID, discarded}, "someone else's": {other.ID, open}} {
		if err := db.Q().SetCaptureCategory(ctx, c.user, c.id, &cat.ID, "user"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("SetCaptureCategory(%s) = %v, want ErrNotFound", name, err)
		}
		if err := db.Q().SetCaptureNote(ctx, c.user, c.id, "x"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("SetCaptureNote(%s) = %v, want ErrNotFound", name, err)
		}
	}
}

func TestCaptureSummariesCountWhatIsOpen(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card, checking, quiet := newCard(t, db, u.ID), newChecking(t, db, u.ID), newCard(t, db, u.ID)
	addStatement(t, db, card, day)

	addCapture(t, db, card, day, -100)             // pending: the statement ends that day
	addCapture(t, db, card, day.AddDays(1), -250)  // pending
	addCapture(t, db, card, day.AddDays(-3), -333) // missing: settled, and not on it
	addLine(t, db, card, day, -999, store.KindExpense, nil)
	addCapture(t, db, card, day, -999)                 // printed
	addCapture(t, db, checking, day, -50)              // pending
	addCapture(t, db, checking, day.AddDays(-15), -20) // pending, on the last day of the month before
	addCapture(t, db, checking, day.AddDays(17), -7)   // pending, on the first day of the month after
	linked := addCapture(t, db, quiet, day, -777)      // linked
	if err := link(t, db, linked, addLine(t, db, quiet, day, -777, store.KindExpense, nil)); err != nil {
		t.Fatal(err)
	}
	if err := discard(t, db, addCapture(t, db, quiet, day, -555)); err != nil {
		t.Fatal(err)
	}
	other := newUser(t, db, "other@example.com")
	addCapture(t, db, newCard(t, db, other.ID), day, -4200)

	// The month's total is October's pending purchases only.
	got, err := db.Q().CaptureSummaries(ctx, u.ID, civil.Date{Year: 2026, Month: time.October, Day: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]store.CaptureSummary{
		card.ID:     {PendingTotal: -350, PendingCount: 2, ToReview: 2, MonthTotal: -350},
		checking.ID: {PendingTotal: -77, PendingCount: 3, MonthTotal: -50},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CaptureSummaries = %+v, want %+v", got, want)
	}
}

// --- fixtures ----------------------------------------------------------------

func newCard(t *testing.T, db *store.DB, userID uuid.UUID) store.Account {
	t.Helper()
	return newAccount(t, db, store.Account{ID: uuid.New(), UserID: userID, Class: "liability",
		Type: "credit_card", Institution: "banco_general", ExternalNumber: "**** " + uuid.NewString(), Currency: "USD"})
}

func newChecking(t *testing.T, db *store.DB, userID uuid.UUID) store.Account {
	t.Helper()
	return newAccount(t, db, store.Account{ID: uuid.New(), UserID: userID, Class: "asset",
		Type: "checking", Institution: "bac", ExternalNumber: uuid.NewString(), Currency: "USD"})
}

func newAccount(t *testing.T, db *store.DB, a store.Account) store.Account {
	t.Helper()
	got, created, err := db.Q().EnsureAccount(context.Background(), a)
	if err != nil || !created {
		t.Fatalf("EnsureAccount = %v, %v", created, err)
	}
	return got
}

func addLine(t *testing.T, db *store.DB, a store.Account, on civil.Date, amount money.Cents, kind string, userKind *string) uuid.UUID {
	t.Helper()
	e := store.Entry{ID: uuid.New(), UserID: a.UserID, AccountID: a.ID, BookedOn: on, Amount: amount,
		Kind: kind, UserKind: userKind, Description: "LINEA", DedupKey: uuid.NewString()}
	if ok, err := db.Q().InsertEntryIfNew(context.Background(), e); err != nil || !ok {
		t.Fatalf("InsertEntryIfNew = %v, %v", ok, err)
	}
	return e.ID
}

func addCapture(t *testing.T, db *store.DB, a store.Account, on civil.Date, amount money.Cents) uuid.UUID {
	t.Helper()
	c := store.Capture{ID: uuid.New(), UserID: a.UserID, AccountID: a.ID, Source: store.CaptureSourceManual,
		BookedOn: on, Amount: amount, Merchant: "Café Unido"}
	if err := db.Q().InsertCapture(context.Background(), c); err != nil {
		t.Fatalf("InsertCapture: %v", err)
	}
	return c.ID
}

// addStatement records a statement of the account for the month ending on end.
func addStatement(t *testing.T, db *store.DB, a store.Account, end civil.Date) {
	t.Helper()
	ctx := context.Background()
	run := store.ImportRun{ID: uuid.New(), UserID: a.UserID, AccountID: &a.ID, Source: "bg_card", Status: "ok"}
	if err := db.Q().CreateImportRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().CreateStatement(ctx, store.Statement{ID: uuid.New(), UserID: a.UserID, AccountID: a.ID,
		RunID: run.ID, PeriodStart: end.AddDays(-30), PeriodEnd: end}); err != nil {
		t.Fatal(err)
	}
}

func newKey(t *testing.T, db *store.DB, a store.Account) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := insertKey(t, db, a, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertKey(t *testing.T, db *store.DB, a store.Account, id uuid.UUID) error {
	t.Helper()
	return insertKeyWithHash(t, db, a, id, uuid.NewString())
}

func insertKeyWithHash(t *testing.T, db *store.DB, a store.Account, id uuid.UUID, hash string) error {
	t.Helper()
	return storetest.Exec(t, db, `INSERT INTO capture_keys (id, user_id, account_id, key_hash, hint)
		VALUES ($1, $2, $3, $4, 'x7Qa')`, id, a.UserID, a.ID, hash)
}

func link(t *testing.T, db *store.DB, capture, entry uuid.UUID) error {
	t.Helper()
	return storetest.Exec(t, db, `UPDATE captures SET entry_id = $2, linked_by = 'auto' WHERE id = $1`, capture, entry)
}

func discard(t *testing.T, db *store.DB, capture uuid.UUID) error {
	t.Helper()
	return storetest.Exec(t, db, `UPDATE captures SET discarded_at = now() WHERE id = $1`, capture)
}

func reject(t *testing.T, db *store.DB, capture, entry uuid.UUID) error {
	t.Helper()
	return storetest.Exec(t, db, `INSERT INTO capture_rejections (capture_id, entry_id, user_id)
		SELECT id, $2, user_id FROM captures WHERE id = $1`, capture, entry)
}

func stateOf(t *testing.T, db *store.DB, userID, capture uuid.UUID) string {
	t.Helper()
	c, err := db.Q().CaptureByID(context.Background(), userID, capture)
	if err != nil {
		t.Fatalf("CaptureByID: %v", err)
	}
	return c.State
}

// refusedBy asserts that err is the named constraint refusing a write, so a
// test cannot pass because some other rule happened to fire.
func refusedBy(t *testing.T, err error, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		t.Errorf("accepted; want %s to refuse it", constraint)
	case !errors.As(err, &pgErr) || pgErr.ConstraintName != constraint:
		t.Errorf("err = %v; want %s to refuse it", err, constraint)
	}
}
