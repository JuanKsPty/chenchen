package accounts_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

type fixture struct {
	db   *store.DB
	svc  *accounts.Service
	user uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := storetest.New(t)
	return fixture{db: db, svc: accounts.NewService(db), user: newUser(t, db, "o@example.com")}
}

func newUser(t *testing.T, db *store.DB, email string) uuid.UUID {
	t.Helper()
	u, err := db.Q().CreateUser(context.Background(), store.User{ID: uuid.New(), Email: email, PasswordHash: "x", FullName: email})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func (f fixture) account(t *testing.T, user uuid.UUID, typ, number string) store.Account {
	t.Helper()
	a := store.Account{ID: uuid.New(), UserID: user, Class: "asset", Type: typ, Institution: "bac",
		ExternalNumber: number, Currency: "USD", DisplayName: number}
	switch typ {
	case "credit_card":
		a.Class, a.Institution = "liability", "banco_general"
	case "brokerage":
		a.Institution = "ibkr"
	}
	got, _, err := f.db.Q().EnsureAccount(context.Background(), a)
	if err != nil {
		t.Fatalf("EnsureAccount(%s): %v", number, err)
	}
	return got
}

func (f fixture) checkpoint(t *testing.T, accountID uuid.UUID, asOf civil.Date, due *civil.Date, minimum *money.Cents) {
	t.Helper()
	if _, err := f.svc.AddCheckpoint(context.Background(), f.user, accountID, accounts.CheckpointInput{
		AsOf: asOf, Balance: -10000, DueOn: due, MinimumPayment: minimum,
	}); err != nil {
		t.Fatalf("AddCheckpoint(%s): %v", asOf, err)
	}
}

// link connects a brokerage account to IBKR and records one sync outcome.
func (f fixture) link(t *testing.T, user uuid.UUID, a store.Account, status string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := f.db.Q().UpsertIBKRLink(ctx, store.IBKRLink{ID: uuid.New(), UserID: user, AccountID: a.ID,
		IBKRAccountID: a.ExternalNumber, FlexQueryID: "1", FlexTokenCiphertext: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if status == "never" {
		return
	}
	if err := f.db.Q().RecordSyncOutcome(ctx, a.ID, status, "boom", at); err != nil {
		t.Fatal(err)
	}
}

func day(y int, m time.Month, d int) civil.Date { return civil.Date{Year: y, Month: m, Day: d} }

func ptr[T any](v T) *T { return &v }

func byID(list []accounts.Account) map[uuid.UUID]accounts.Account {
	out := map[uuid.UUID]accounts.Account{}
	for _, a := range list {
		out[a.ID] = a
	}
	return out
}

func TestACardCarriesItsLatestStatement(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card", "**** 1111")
	bank := f.account(t, f.user, "checking", "100")

	// A card with no due date yet has no statement.
	f.checkpoint(t, card.ID, day(2026, time.August, 1), nil, nil)
	got, err := f.svc.Get(ctx, f.user, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Statement != nil {
		t.Fatalf("Statement = %+v before any due date, want nil", got.Statement)
	}

	// The latest checkpoint with a due date wins, even when a later balance
	// was stated without one.
	f.checkpoint(t, card.ID, day(2026, time.August, 15), ptr(day(2026, time.September, 5)), ptr(money.Cents(2500)))
	f.checkpoint(t, card.ID, day(2026, time.September, 15), ptr(day(2026, time.October, 5)), nil)
	f.checkpoint(t, card.ID, day(2026, time.October, 1), nil, nil)
	// A bank account's due date is not a statement.
	f.checkpoint(t, bank.ID, day(2026, time.October, 1), ptr(day(2026, time.October, 20)), ptr(money.Cents(100)))

	list, err := f.svc.List(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	listed := byID(list)
	for _, a := range []accounts.Account{listed[card.ID], mustGet(t, f, card.ID)} {
		st := a.Statement
		if st == nil || st.ClosingOn != day(2026, time.September, 15) || st.DueOn == nil ||
			*st.DueOn != day(2026, time.October, 5) || st.MinimumPayment != nil {
			t.Errorf("Statement = %+v, want closing 2026-09-15, due 2026-10-05, no minimum", st)
		}
	}
	if st := listed[bank.ID].Statement; st != nil {
		t.Errorf("a checking account has Statement = %+v", st)
	}

	// The wire shape: due_on and minimum_payment are present even when null.
	card1 := wireAccount(t, list, card.ID)
	statement, ok := card1["statement"].(map[string]any)
	if !ok {
		t.Fatalf("statement = %#v", card1["statement"])
	}
	if statement["closing_on"] != "2026-09-15" || statement["due_on"] != "2026-10-05" {
		t.Errorf("statement = %v", statement)
	}
	if m, present := statement["minimum_payment"]; !present || m != nil {
		t.Errorf("minimum_payment = %#v (present %v), want null", m, present)
	}
	if _, present := wireAccount(t, list, bank.ID)["statement"]; present {
		t.Error("a checking account carries a statement on the wire")
	}

	// A newer statement replaces it, minimum payment and all.
	f.checkpoint(t, card.ID, day(2026, time.October, 15), ptr(day(2026, time.November, 5)), ptr(money.Cents(2500)))
	list, err = f.svc.List(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	statement, _ = wireAccount(t, list, card.ID)["statement"].(map[string]any)
	want := map[string]any{"cents": float64(2500), "formatted": "25.00", "currency": "USD"}
	m, _ := statement["minimum_payment"].(map[string]any)
	if statement["closing_on"] != "2026-10-15" || statement["due_on"] != "2026-11-05" || !sameJSON(m, want) {
		t.Errorf("statement = %v, want closing 2026-10-15, due 2026-11-05, minimum %v", statement, want)
	}
}

func TestABrokerageAccountCarriesItsLastSuccessfulSync(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	synced := time.Date(2026, time.October, 24, 13, 30, 0, 0, time.UTC)

	ok := f.account(t, f.user, "brokerage", "U1111111")
	failed := f.account(t, f.user, "brokerage", "U2222222")
	never := f.account(t, f.user, "brokerage", "U3333333")
	unlinked := f.account(t, f.user, "brokerage", "U4444444")
	bank := f.account(t, f.user, "checking", "100")
	f.link(t, f.user, ok, "ok", synced)
	f.link(t, f.user, failed, "error", synced)
	f.link(t, f.user, never, "never", time.Time{})

	// Somebody else's sync is not this user's.
	other := newUser(t, f.db, "other@example.com")
	theirs := f.account(t, other, "brokerage", "U5555555")
	f.link(t, other, theirs, "ok", synced.Add(time.Hour))
	syncs, err := f.db.Q().LastSyncsByUser(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	if len(syncs) != 1 || !syncs[ok.ID].Equal(synced) {
		t.Fatalf("LastSyncsByUser = %v, want only %s at %s", syncs, ok.ID, synced)
	}

	list, err := f.svc.List(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	listed := byID(list)
	for _, a := range []accounts.Account{listed[ok.ID], mustGet(t, f, ok.ID)} {
		if a.LastSyncedAt == nil || !a.LastSyncedAt.Equal(synced) {
			t.Errorf("LastSyncedAt = %v, want %s", a.LastSyncedAt, synced)
		}
	}
	for name, id := range map[string]uuid.UUID{"failed": failed.ID, "never synced": never.ID,
		"unlinked": unlinked.ID, "checking": bank.ID} {
		if at := listed[id].LastSyncedAt; at != nil {
			t.Errorf("%s account: List LastSyncedAt = %v, want nil", name, at)
		}
		if at := mustGet(t, f, id).LastSyncedAt; at != nil {
			t.Errorf("%s account: Get LastSyncedAt = %v, want nil", name, at)
		}
		if _, present := wireAccount(t, list, id)["last_synced_at"]; present {
			t.Errorf("%s account carries last_synced_at on the wire", name)
		}
	}

	raw, _ := wireAccount(t, list, ok.ID)["last_synced_at"].(string)
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil || !at.Equal(synced) {
		t.Errorf("last_synced_at = %q (%v), want %s", raw, err, synced.Format(time.RFC3339))
	}
}

func mustGet(t *testing.T, f fixture, id uuid.UUID) accounts.Account {
	t.Helper()
	a, err := f.svc.Get(context.Background(), f.user, id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	return a
}

// wireAccount is one account of the list exactly as GET /api/accounts sends it.
func wireAccount(t *testing.T, list []accounts.Account, id uuid.UUID) map[string]any {
	t.Helper()
	body, err := json.Marshal(accounts.NewListView(list))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Accounts []map[string]any `json:"accounts"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, a := range decoded.Accounts {
		if a["id"] == id.String() {
			return a
		}
	}
	t.Fatalf("account %s is not in the list", id)
	return nil
}

func sameJSON(a, b map[string]any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
