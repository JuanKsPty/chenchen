package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

// A capture is noted with its first signal, in one statement: the channel it
// came from, when, and what that channel sent.
func TestACaptureIsNotedWithItsFirstSignal(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	at := time.Date(2026, time.October, 15, 12, 41, 0, 0, civil.Panama)
	tapped := store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: card.ID, Source: store.CaptureSourceShortcut,
		ReceivedAt: at, BookedOn: day, Amount: -450, Merchant: "Café Unido",
		Raw: map[string]string{"amount": "$4.50", "merchant": "Café Unido"}}
	if err := db.Q().InsertCapture(ctx, tapped); err != nil {
		t.Fatal(err)
	}
	signals, err := db.Q().CaptureSignals(ctx, u.ID, tapped.ID)
	if err != nil || len(signals) != 1 {
		t.Fatalf("CaptureSignals = %+v, %v; want one", signals, err)
	}
	if s := signals[0]; s.CaptureID != tapped.ID || s.UserID != u.ID || s.Channel != store.ChannelShortcut ||
		!s.At.Equal(at) || s.AlertID != nil || s.Raw["amount"] != "$4.50" || s.Raw["merchant"] != "Café Unido" {
		t.Errorf("first signal = %+v", s)
	}

	// Typed by hand, received now, sent nothing.
	typed := addCapture(t, db, card, day, -1200)
	c, err := db.Q().CaptureByID(ctx, u.ID, typed)
	if err != nil {
		t.Fatal(err)
	}
	signals, err = db.Q().CaptureSignals(ctx, u.ID, typed)
	if err != nil || len(signals) != 1 || signals[0].Channel != store.ChannelManual || !signals[0].At.Equal(c.ReceivedAt) ||
		len(signals[0].Raw) != 0 {
		t.Errorf("a typed purchase's signals = %+v, %v", signals, err)
	}

	// Another channel tells of it later; the capture is untouched.
	alert := uuid.New()
	later := store.CaptureSignal{CaptureID: tapped.ID, UserID: u.ID, Channel: store.ChannelEmail, At: at.Add(time.Minute),
		AlertID: &alert, Raw: map[string]string{"alert_id": alert.String()}}
	if err := db.Q().InsertCaptureSignal(ctx, later); err != nil {
		t.Fatal(err)
	}
	signals, err = db.Q().CaptureSignals(ctx, u.ID, tapped.ID)
	if err != nil || len(signals) != 2 || signals[1].Channel != store.ChannelEmail || *signals[1].AlertID != alert {
		t.Errorf("signals = %+v, %v; want the shortcut's, then the email's", signals, err)
	}
	if got, err := db.Q().CaptureByID(ctx, u.ID, tapped.ID); err != nil || got.Source != store.CaptureSourceShortcut ||
		!got.ReceivedAt.Equal(at) || got.State != store.CapturePending {
		t.Errorf("after the email: %+v, %v", got, err)
	}

	// Nobody else reads them.
	other := newUser(t, db, "other@example.com")
	if signals, err := db.Q().CaptureSignals(ctx, other.ID, tapped.ID); err != nil || len(signals) != 0 {
		t.Errorf("another user's CaptureSignals = %+v, %v", signals, err)
	}

	// They go with their capture.
	if err := storetest.Exec(t, db, `DELETE FROM accounts WHERE id = $1`, card.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{tapped.ID, typed} {
		if signals, err := db.Q().CaptureSignals(ctx, u.ID, id); err != nil || len(signals) != 0 {
			t.Errorf("signals left after the account = %+v, %v", signals, err)
		}
	}
}

func TestTheSchemaRefusesImpossibleSignals(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	c := addCapture(t, db, card, day, -450)

	t.Run("a channel twice", func(t *testing.T) {
		err := db.Q().InsertCaptureSignal(ctx, store.CaptureSignal{CaptureID: c, UserID: u.ID,
			Channel: store.ChannelManual, At: time.Now()})
		refusedBy(t, err, "capture_signals_pkey")
	})

	t.Run("an unknown channel", func(t *testing.T) {
		err := db.Q().InsertCaptureSignal(ctx, store.CaptureSignal{CaptureID: c, UserID: u.ID,
			Channel: "sms", At: time.Now()})
		refusedBy(t, err, "capture_signals_channel_check")
	})

	t.Run("another user's capture", func(t *testing.T) {
		other := newUser(t, db, "other@example.com")
		err := db.Q().InsertCaptureSignal(ctx, store.CaptureSignal{CaptureID: c, UserID: other.ID,
			Channel: store.ChannelEmail, At: time.Now()})
		refusedBy(t, err, "capture_signals_capture_id_user_id_fkey")
	})

	t.Run("a caller's id twice", func(t *testing.T) {
		first, second := addCapture(t, db, card, day, -900), addCapture(t, db, card, day, -901)
		id := uuid.New()
		if err := db.Q().InsertCaptureSignal(ctx, store.CaptureSignal{CaptureID: first, UserID: u.ID,
			Channel: store.ChannelAgent, At: time.Now(), CallerID: &id}); err != nil {
			t.Fatal(err)
		}
		err := db.Q().InsertCaptureSignal(ctx, store.CaptureSignal{CaptureID: second, UserID: u.ID,
			Channel: store.ChannelAgent, At: time.Now(), CallerID: &id})
		refusedBy(t, err, "capture_signals_caller_idx")

		// Another user's purchase may carry the same id: it names one per user.
		them := newUser(t, db, "them@example.com")
		theirs := addCapture(t, db, newCard(t, db, them.ID), day, -450)
		if err := db.Q().InsertCaptureSignal(ctx, store.CaptureSignal{CaptureID: theirs, UserID: them.ID,
			Channel: store.ChannelAgent, At: time.Now(), CallerID: &id}); err != nil {
			t.Errorf("their signal with the id: %v", err)
		}
	})

	t.Run("every channel there is", func(t *testing.T) {
		for _, channel := range []string{store.ChannelShortcut, store.ChannelAgent, store.ChannelEmail, store.ChannelNotification} {
			if err := db.Q().InsertCaptureSignal(ctx, store.CaptureSignal{CaptureID: c, UserID: u.ID,
				Channel: channel, At: time.Now()}); err != nil {
				t.Errorf("a %s signal: %v", channel, err)
			}
		}
	})

	t.Run("a purchase the assistant noted", func(t *testing.T) {
		if err := db.Q().InsertCapture(ctx, store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: card.ID,
			Source: store.CaptureSourceAgent, BookedOn: day, Amount: -1}); err != nil {
			t.Errorf("an agent capture: %v", err)
		}
	})
}

// Only a channel that hears of a purchase as it happens gives the movement
// its time: one typed by hand or noted by the assistant has none.
func TestOnlyATimedChannelGivesAPurchaseItsTime(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	at := time.Date(2026, time.October, 15, 12, 41, 0, 0, civil.Panama)

	for source, timed := range map[string]bool{store.CaptureSourceShortcut: true, store.CaptureSourceManual: false,
		store.CaptureSourceAgent: false} {
		c := store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: card.ID, Source: source, ReceivedAt: at,
			BookedOn: day, Amount: -450, Merchant: "Café"}
		if err := db.Q().InsertCapture(ctx, c); err != nil {
			t.Fatal(err)
		}
		listed, err := db.Q().CaptureAsEntry(ctx, u.ID, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		read, err := db.Q().CaptureByID(ctx, u.ID, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		for name, e := range map[string]store.Entry{"in SQL": listed, "in Go": read.AsEntry()} {
			if (e.BookedAt != nil) != timed || (timed && !e.BookedAt.Equal(at)) || e.CaptureSource != source {
				t.Errorf("%s %s: BookedAt = %v, source %q; want a time %v", source, name, e.BookedAt, e.CaptureSource, timed)
			}
		}
	}
	for _, channel := range []string{store.ChannelShortcut, store.ChannelEmail, store.ChannelNotification} {
		if !store.IsTimedChannel(channel) {
			t.Errorf("%s is not timed", channel)
		}
	}
	for _, channel := range []string{store.ChannelManual, store.ChannelAgent, ""} {
		if store.IsTimedChannel(channel) {
			t.Errorf("%q is timed", channel)
		}
	}
}

// A repeat of a form whose purchase joined another is found by the id it
// carried, for its user only: by the signals' caller ids, which are indexed,
// never by reading what each signal sent.
func TestACaptureIsFoundByTheIdOfTheSignalThatJoinedIt(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	c := addCapture(t, db, card, day, -450)
	id := uuid.New()
	if err := db.Q().InsertCaptureSignal(ctx, store.CaptureSignal{CaptureID: c, UserID: u.ID, Channel: store.ChannelAgent,
		At: time.Now(), Raw: map[string]string{"id": id.String()}, CallerID: &id}); err != nil {
		t.Fatal(err)
	}
	if got, found, err := db.Q().CaptureJoinedAs(ctx, u.ID, id); err != nil || !found || got.ID != c {
		t.Errorf("CaptureJoinedAs = %v, %v, %v; want %s", got.ID, found, err, c)
	}
	if signals, err := db.Q().CaptureSignals(ctx, u.ID, c); err != nil || len(signals) != 2 ||
		signals[1].CallerID == nil || *signals[1].CallerID != id || signals[0].CallerID != nil {
		t.Errorf("signals = %+v, %v; want the caller's id on the agent's only", signals, err)
	}
	// Only the column finds it: an id in what a signal sent is not one.
	sent := uuid.New()
	if err := db.Q().InsertCaptureSignal(ctx, store.CaptureSignal{CaptureID: c, UserID: u.ID, Channel: store.ChannelEmail,
		At: time.Now(), Raw: map[string]string{"id": sent.String()}}); err != nil {
		t.Fatal(err)
	}
	other := newUser(t, db, "other@example.com")
	for name, tt := range map[string]struct{ user, id uuid.UUID }{
		"another id":       {u.ID, uuid.New()},
		"an id in its raw": {u.ID, sent},
		"another user":     {other.ID, id},
	} {
		if _, found, err := db.Q().CaptureJoinedAs(ctx, tt.user, tt.id); err != nil || found {
			t.Errorf("%s: found %v, %v", name, found, err)
		}
	}
	if _, err := db.Q().CaptureByID(ctx, u.ID, id); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the id became a capture: %v", err)
	}
}
