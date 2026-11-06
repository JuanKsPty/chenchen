package imports_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/auth"
	"github.com/JuanKsPty/chenchen/api/internal/captures"
	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// TestBalancesNeverSeeSignals is ADR-0002's promise kept as a purchase
// arrives by more than one way: noting it, joining another channel's signal
// to it, on a pending purchase or on one linked to its line, never moves a
// balance, a drift, a reconciliation, a checkpoint or a transfer, nor the
// month's spending.
func TestBalancesNeverSeeSignals(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	svc := captures.NewService(e.db, categories.NewService(e.db))

	card := e.importFile(t, "tarjeta.txt", []byte(cardFile)).Accounts[0].AccountID
	if _, err := e.accounts.AddCheckpoint(ctx, e.user, card, accounts.CheckpointInput{
		AsOf: jan(14), Balance: -10000, Pin: true,
	}); err != nil {
		t.Fatal(err)
	}
	bank := e.importFile(t, "estado.csv", bacFileOn("15/01/2026", "600.00", "500.00",
		"10/01/2026, 1, 4A, SUPER, 100.00, 0.00, 500.00")).Accounts[0].AccountID
	e.importFile(t, "estado2.csv", bacFileOn("31/01/2026", "500.00", "400.00",
		"18/01/2026, 2, 4A, PAGO TC, 100.00, 0.00, 400.00"))
	payment := e.lineOf(t, "PAGO TC")
	key, err := svc.CreateKey(ctx, e.user, card)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := auth.NewCaptureKeys(e.db).Authenticate(ctx, "Bearer "+key.Key)
	if err != nil {
		t.Fatal(err)
	}

	before, spent := e.balanceFacts(t, payment), e.spent(t)
	if before.PaymentKind != store.KindTransfer || len(before.Checkpoints) == 0 {
		t.Fatalf("the fixture is not what this test assumes: %+v", before)
	}

	today := civil.Of(time.Now().In(civil.Panama))
	tap := func(merchant string) func() error {
		return func() error {
			_, err := svc.Ingest(ctx, scope, captures.Purchase{Amount: "$9.50", Merchant: merchant})
			return err
		}
	}
	add := func(channel string, account uuid.UUID, paid money.Cents, on civil.Date) func() error {
		return func() error {
			_, err := svc.Add(ctx, e.user, captures.AddInput{AccountID: account, Amount: paid, Merchant: "Café",
				Day: on, Channel: channel})
			return err
		}
	}
	email := func(paid money.Cents, at time.Time) func() error {
		return func() error { return e.email(t, card, paid, at) }
	}
	// A purchase told of by every channel today, while pending; then one on
	// the day of its line, linked at once and told of again; then a repeat,
	// a purchase of its own, and one the assistant notes alone.
	steps := []struct {
		name string
		run  func() error
	}{
		{"the shortcut", tap("Café")},
		{"its email", email(950, time.Now())},
		{"typed", add(store.ChannelManual, card, 950, today)},
		{"by the assistant", add(store.ChannelAgent, card, 950, today)},
		{"typed on its line's day", add(store.ChannelManual, card, 950, jan(18))},
		{"by the assistant on its line's day", add(store.ChannelAgent, card, 950, jan(18))},
		{"its email on its line's day", email(950, jan(18).Time(civil.Panama).Add(12*time.Hour))},
		{"the shortcut's repeat", tap("Café")},
		{"another tap", tap("Tienda")},
		{"the assistant alone", add(store.ChannelAgent, bank, 300, today)},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if after := e.balanceFacts(t, payment); !reflect.DeepEqual(after, before) {
			t.Errorf("%s moved something:\nbefore %+v\nafter  %+v", step.name, before, after)
		}
		if got := e.spent(t); got != spent {
			t.Errorf("%s: January's spending = %s, want %s", step.name, got, spent)
		}
	}

	// It was all told of: four purchases, each counted once.
	var noted []store.Capture
	for _, a := range []uuid.UUID{card, bank} {
		list, err := e.db.Q().CapturesByAccount(ctx, e.user, a)
		if err != nil {
			t.Fatal(err)
		}
		noted = append(noted, list...)
	}
	signals := 0
	for _, c := range noted {
		s, err := e.db.Q().CaptureSignals(ctx, e.user, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		signals += len(s)
	}
	if len(noted) != 4 || signals != 9 {
		t.Errorf("%d purchases with %d signals; want 4 with 9", len(noted), signals)
	}
}

// email tells of a purchase on the account as the bank's email will once it
// is read. Every one here tells of a purchase already noted.
func (e env) email(t *testing.T, account uuid.UUID, paid money.Cents, at time.Time) error {
	t.Helper()
	ctx := context.Background()
	return e.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, e.user); err != nil {
			return err
		}
		_, joined, err := captures.JoinSignal(ctx, q, e.user, account, paid, civil.Of(at.In(civil.Panama)),
			store.CaptureSignal{Channel: store.ChannelEmail, At: at})
		if err == nil && !joined {
			t.Errorf("the email of %s at %s joined nothing", paid, at)
		}
		return err
	})
}
