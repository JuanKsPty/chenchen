package accounts_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/identity"
	"github.com/JuanKsPty/chenchen/api/internal/imports"
	"github.com/JuanKsPty/chenchen/api/internal/mcpserver"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/movements"
	"github.com/JuanKsPty/chenchen/api/internal/proposals"
	"github.com/JuanKsPty/chenchen/api/internal/reports"
	"github.com/JuanKsPty/chenchen/api/internal/rules"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/transfers"
)

// A card someone else pays — the owner's father's, say — is the owner's
// spending and not the owner's debt. Its purchases count in Gastos and the
// month's flow like any other; nothing about it reaches what the owner owes,
// their net worth, or the accounts that lack an opening balance. It lives only
// on noted purchases: nothing a statement printed can ever land in it.

// octoberNow is a moment late in October 2026 in Panama: this month, for the
// card's figures, is October.
var octoberNow = time.Date(2026, time.October, 26, 21, 0, 0, 0, civil.Panama)

func oct(d int) civil.Date { return civil.Date{Year: 2026, Month: time.October, Day: d} }

func TestACardSomeoneElsePaysIsTheOwnersSpendingButNotTheirDebt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.svc.SetNow(func() time.Time { return octoberNow })

	// The owner's own money: a bank account holding 500.00 and a card owing
	// 100.00, both anchored, and an unanchored card with movements.
	bank := f.account(t, f.user, "checking", "100")
	own := f.account(t, f.user, "credit_card", "**** 1111")
	f.state(t, bank.ID, 50000)
	f.state(t, own.ID, -10000)
	loose := f.account(t, f.user, "credit_card", "**** 2222")
	f.line(t, loose, oct(2), -700)
	before, err := f.svc.List(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	worthBefore := accounts.NewListView(before).NetWorth
	if worthBefore.Owed.Cents != 10700 || worthBefore.Total.Cents != 39300 || worthBefore.Unanchored != 1 {
		t.Fatalf("the fixture is not what this test assumes: %+v", worthBefore)
	}

	// The card arrives from the app, as the owner adds it.
	rec := f.serve(t, http.MethodPost, "/", `{"alias": "  Tarjeta de papá ", "last4": "4321"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/accounts = %d %s", rec.Code, rec.Body)
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"class": "liability", "type": "credit_card", "institution": "manual",
		"external_number": "**** 4321", "display_name": "", "alias": "Tarjeta de papá", "paid_by_other": true,
		"anchored": false, "movements": float64(0)}
	for field, value := range want {
		if created[field] != value {
			t.Errorf("created %s = %#v, want %#v", field, created[field], value)
		}
	}
	card, err := uuid.Parse(created["id"].(string))
	if err != nil {
		t.Fatal(err)
	}

	// Its purchases: two this month, one last month, one the owner discarded.
	f.purchase(t, card, oct(3), -1250, store.CaptureSourceManual)
	f.purchase(t, card, oct(20), -3000, store.CaptureSourceShortcut)
	f.purchase(t, card, civil.Date{Year: 2026, Month: time.September, Day: 28}, -999, store.CaptureSourceManual)
	if err := f.db.Q().DiscardCapture(ctx, f.user, f.purchase(t, card, oct(5), -500, store.CaptureSourceShortcut)); err != nil {
		t.Fatal(err)
	}

	after, err := f.svc.List(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	// What the owner owes and is worth has not moved, and the card does not
	// count among the accounts that lack an opening balance.
	worthAfter := accounts.NewListView(after).NetWorth
	if worthAfter != worthBefore {
		t.Errorf("net worth = %+v, was %+v", worthAfter, worthBefore)
	}
	assets, liabilities, holdings := accounts.NetWorth(after)
	if assets != 50000 || liabilities != -10700 || holdings != 0 {
		t.Errorf("NetWorth = %s/%s/%s, want 500.00/-107.00/0.00", assets, liabilities, holdings)
	}
	// Its balance is always zero, so the figures above would hold even if
	// they counted it. They do not: a card someone else pays with a debt and
	// movements but no opening would still be neither in what the owner owes
	// nor a reason their net worth is incomplete.
	hand := byID(after)[card]
	hand.Balance, hand.Movements = -5000, 3
	if v := accounts.NewListView([]accounts.Account{hand}); v.NetWorth.Owed.Cents != 0 || v.NetWorth.Total.Cents != 0 ||
		v.NetWorth.Unanchored != 0 || v.NetWorth.Incomplete || v.Accounts[0].Owed != nil {
		t.Errorf("a card someone else pays counted: %+v, owed %v", v.NetWorth, v.Accounts[0].Owed)
	}

	// It has nothing to anchor, nothing to match and no debt of the owner's.
	for _, a := range []accounts.Account{byID(after)[card], mustGet(t, f, card)} {
		if !a.PaidByOther || a.Balance != 0 || a.Anchored || a.Drift != nil || a.Movements != 0 ||
			a.Statement != nil || a.ThisMonth != 4250 || a.Pending != -5249 || a.PendingCount != 3 || a.ToReview != 0 {
			t.Errorf("card = %+v", a)
		}
	}
	w := wireAccount(t, after, card)
	for _, field := range []string{"owed", "statement", "estimated_balance", "drift", "to_review", "holdings"} {
		if _, present := w[field]; present {
			t.Errorf("the card carries %s on the wire: %v", field, w[field])
		}
	}
	month, _ := w["this_month"].(map[string]any)
	pending, _ := w["pending"].(map[string]any)
	pendingAmount, _ := pending["amount"].(map[string]any)
	if w["paid_by_other"] != true || month["cents"] != float64(4250) || month["formatted"] != "42.50" ||
		pendingAmount["cents"] != float64(-5249) || pending["count"] != float64(3) {
		t.Errorf("the card on the wire: paid_by_other %v, this_month %v, pending %v", w["paid_by_other"], month, pending)
	}
	if shortcut, ok := w["shortcut"].(map[string]any); !ok || shortcut["configured"] != false {
		t.Errorf("the card takes a shortcut key: shortcut = %v", w["shortcut"])
	}
	// Every other account says nothing about it.
	for _, id := range []uuid.UUID{bank.ID, own.ID, loose.ID} {
		w := wireAccount(t, after, id)
		if _, present := w["paid_by_other"]; present {
			t.Errorf("%s carries paid_by_other", id)
		}
		if _, present := w["this_month"]; present {
			t.Errorf("%s carries this_month", id)
		}
	}
	if _, present := wireAccount(t, after, own.ID)["owed"]; !present {
		t.Error("the owner's own card lost its owed")
	}

	// With nothing noted this month, the month is zero, and says so.
	f.svc.SetNow(func() time.Time { return octoberNow.AddDate(0, 2, 0) })
	if w := wireAccount(t, mustList(t, f), card); !sameJSON(w["this_month"].(map[string]any),
		map[string]any{"cents": float64(0), "formatted": "0.00", "currency": "USD"}) {
		t.Errorf("this_month in December = %v, want 0.00", w["this_month"])
	}

	// Its purchases are the owner's spending: October's in Gastos and in the
	// month's flow, September's in September's.
	rep := reports.NewService(f.db)
	for _, tt := range []struct {
		from, to civil.Date
		want     money.Cents
	}{
		{oct(1), oct(31), 4250 + 700},
		{civil.Date{Year: 2026, Month: time.September, Day: 1}, civil.Date{Year: 2026, Month: time.September, Day: 30}, 999},
	} {
		totals, err := rep.Spending(ctx, f.user, tt.from, tt.to, nil)
		if err != nil {
			t.Fatal(err)
		}
		var spent money.Cents
		for _, total := range totals {
			spent += total.Total
		}
		points, err := rep.Flow(ctx, f.user, tt.from, tt.to, true)
		if err != nil {
			t.Fatal(err)
		}
		var out money.Cents
		for _, p := range points {
			out += p.Out
		}
		if spent != tt.want || out != tt.want {
			t.Errorf("%s to %s: Gastos %s, flow %s; want %s in both", tt.from, tt.to, spent, out, tt.want)
		}
		only, err := rep.Spending(ctx, f.user, tt.from, tt.to, []uuid.UUID{card})
		if err != nil {
			t.Fatal(err)
		}
		var onCard money.Cents
		for _, total := range only {
			onCard += total.Total
		}
		if tt.from == oct(1) && onCard != 4250 {
			t.Errorf("October's spending on the card = %s, want 42.50", onCard)
		}
	}
}

func TestACardSomeoneElsePaysTakesNothingPrinted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cats := categories.NewService(f.db)
	if err := f.db.InTx(ctx, func(q *store.Queries) error { return cats.SeedDefaults(ctx, q, f.user) }); err != nil {
		t.Fatal(err)
	}
	card := f.handCard(t, f.user, "Tarjeta de papá", "1111")

	// No balance can be stated for it: not by the owner...
	_, err := f.svc.AddCheckpoint(ctx, f.user, card.ID, accounts.CheckpointInput{AsOf: oct(1), Balance: -10000, Pin: true})
	if !errors.Is(err, accounts.ErrKeptByHand) {
		t.Errorf("AddCheckpoint = %v, want ErrKeptByHand", err)
	}
	rec := f.serve(t, http.MethodPost, "/"+card.ID.String()+"/checkpoints", `{"as_of": "2026-10-01", "balance": "-100.00"}`)
	if code, msg := errorOf(t, rec); rec.Code != http.StatusUnprocessableEntity || code != "kept_by_hand" ||
		msg != "Esta tarjeta la paga otra persona: no lleva saldo." {
		t.Errorf("POST checkpoint = %d %s", rec.Code, rec.Body)
	}
	// ...nor by the assistant, whose proposal is refused before it is shown...
	moves := movements.NewService(f.db, cats)
	props := proposals.NewService(f.db, f.svc, cats, moves, rules.NewService(f.db, cats), transfers.NewService(f.db))
	_, err = props.Propose(ctx, f.user, proposals.Checkpoint{AccountID: card.ID, AsOf: oct(1), BalanceCents: -10000})
	if !errors.Is(err, proposals.ErrInvalid) {
		t.Errorf("Propose(checkpoint) = %v, want ErrInvalid", err)
	}
	// ...and a statement imported from its page is refused, with nothing of it
	// stored anywhere.
	statement := "Cuenta;Tarjeta;Fecha tran;Fecha proceso;Descripcion;Referencia;Categoria;Cargos (Db);Pagos (Cr)\r\n" +
		"**** 1111;**** 1111;15/10/2026;15/10/2026;CAFE DE PRUEBA;R1;Comida y Bebida;-9.5;\r\n"
	_, err = imports.NewService(f.db, cats).Import(ctx, f.user, "tarjeta.txt", []byte(statement), &card.ID)
	if !errors.Is(err, imports.ErrAccountMismatch) {
		t.Errorf("Import(expect the card) = %v, want ErrAccountMismatch", err)
	}
	if list := mustList(t, f); len(list) != 1 || list[0].ID != card.ID || list[0].Movements != 0 {
		t.Errorf("after the refused import: %+v", list)
	}

	// Below every service, the database itself refuses anything printed.
	run := store.ImportRun{ID: uuid.New(), UserID: f.user, AccountID: &card.ID, Source: "bg_card", Status: "ok",
		PeriodStart: oct(1), PeriodEnd: oct(31)}
	refusals := map[string]error{
		"an import run": f.db.Q().CreateImportRun(ctx, run),
		"a checkpoint": f.db.Q().CreateCheckpoint(ctx, store.Checkpoint{ID: uuid.New(), UserID: f.user, AccountID: card.ID,
			AsOf: oct(1), Balance: -100, Source: store.SourceManual}),
	}
	// A statement and a line need a run of an account that takes them; then
	// they are pointed at the card.
	bank := f.account(t, f.user, "checking", "100")
	run.ID, run.AccountID = uuid.New(), &bank.ID
	if err := f.db.Q().CreateImportRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	refusals["a statement"] = f.db.Q().CreateStatement(ctx, store.Statement{ID: uuid.New(), UserID: f.user,
		AccountID: card.ID, RunID: run.ID, PeriodStart: oct(1), PeriodEnd: oct(31)})
	_, refusals["a line"] = f.db.Q().InsertEntryIfNew(ctx, store.Entry{ID: uuid.New(), UserID: f.user,
		AccountID: card.ID, RunID: &run.ID, BookedOn: oct(2), Amount: -950, Kind: store.KindExpense, DedupKey: "x"})
	refusals["moving a run to it"] = f.db.Q().SetImportRunAccount(ctx, run.ID, card.ID)
	for what, err := range refusals {
		if !store.IsConstraint(err, "printed_accounts_only") {
			t.Errorf("%s in the card = %v, want the printed_accounts_only check", what, err)
		}
	}
	// A raw insert is a check_violation, whoever writes it.
	err = f.db.Exec(ctx, `INSERT INTO entries (id, user_id, account_id, booked_on, amount_cents, kind, dedup_key)
		VALUES ($1, $2, $3, '2026-10-02', -950, 'expense', 'raw')`, uuid.New(), f.user, card.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("a raw line in the card = %v, want check_violation", err)
	}
	// The bank account it was not meant for still takes all of it.
	if _, err := f.db.Q().InsertEntryIfNew(ctx, store.Entry{ID: uuid.New(), UserID: f.user, AccountID: bank.ID,
		RunID: &run.ID, BookedOn: oct(2), Amount: -950, Kind: store.KindExpense, DedupKey: "x"}); err != nil {
		t.Errorf("a line in a bank account: %v", err)
	}

	a := mustGet(t, f, card.ID)
	if a.Balance != 0 || a.Anchored || a.Movements != 0 || a.Drift != nil {
		t.Errorf("the card after all that = %+v", a)
	}

	// Only a card someone else pays is kept by hand, and only a card is paid
	// by someone else.
	for what, constraint := range map[string]string{
		`INSERT INTO accounts (id, user_id, class, type, institution, external_number)
		 VALUES ($1, $2, 'liability', 'credit_card', 'manual', '**** 9999')`: "accounts_manual_is_paid_by_other",
		`INSERT INTO accounts (id, user_id, class, type, institution, external_number, paid_by_other)
		 VALUES ($1, $2, 'asset', 'checking', 'bac', '999', true)`: "accounts_paid_by_other_is_card",
	} {
		err := f.db.Exec(ctx, what, uuid.New(), f.user)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != constraint {
			t.Errorf("%s = %v, want %s", what, err, constraint)
		}
	}
}

func TestAddingACardSomeoneElsePaysSaysWhatIsWrong(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.handCard(t, f.user, "Tarjeta de papá", "4321")

	for _, tt := range []struct {
		name, body string
		status     int
		field      string
	}{
		{"three digits", `{"alias": "Papá", "last4": "432"}`, http.StatusUnprocessableEntity, "last4"},
		{"five digits", `{"alias": "Papá", "last4": "43210"}`, http.StatusUnprocessableEntity, "last4"},
		{"a letter", `{"alias": "Papá", "last4": "43a1"}`, http.StatusUnprocessableEntity, "last4"},
		{"other digits", `{"alias": "Papá", "last4": "４３２１"}`, http.StatusUnprocessableEntity, "last4"},
		{"no name", `{"alias": "  ", "last4": "9999"}`, http.StatusUnprocessableEntity, "alias"},
		{"a name too long", `{"alias": "` + string(bytes.Repeat([]byte("ñ"), 41)) + `", "last4": "9999"}`,
			http.StatusUnprocessableEntity, "alias"},
	} {
		rec := f.serve(t, http.MethodPost, "/", tt.body)
		var body struct {
			Error struct {
				Fields map[string]string `json:"fields"`
			} `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != tt.status || body.Error.Fields[tt.field] == "" {
			t.Errorf("%s: %d %s, want %d on %s", tt.name, rec.Code, rec.Body, tt.status, tt.field)
		}
	}

	// A form with both fields wrong hears of both at once.
	rec := f.serve(t, http.MethodPost, "/", `{"alias": "  ", "last4": "12"}`)
	var both struct {
		Error struct {
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &both)
	if rec.Code != http.StatusUnprocessableEntity || both.Error.Fields["alias"] != "Ponle un nombre a la tarjeta." ||
		both.Error.Fields["last4"] != "Escribe los últimos 4 dígitos de la tarjeta." {
		t.Errorf("both fields wrong = %d %s", rec.Code, rec.Body)
	}
	if _, err := f.svc.AddHandCard(ctx, f.user, uuid.Nil, "\u202e", "1"); !errors.Is(err, accounts.ErrAliasInvalid) ||
		!errors.Is(err, accounts.ErrLast4Invalid) {
		t.Errorf("AddHandCard(both wrong) = %v, want both errors", err)
	}

	// The same last four digits twice is the same card.
	rec = f.serve(t, http.MethodPost, "/", `{"alias": "Otra", "last4": "4321"}`)
	if code, msg := errorOf(t, rec); rec.Code != http.StatusConflict || code != "hand_card_taken" ||
		msg != "Ya tienes una tarjeta de otra persona que termina en 4321." {
		t.Errorf("a duplicate = %d %s", rec.Code, rec.Body)
	}
	// So is a card whose statements the owner imports, or one its lines were
	// made with: kept by hand as well, each of its purchases would count
	// twice, and its debt would be the owner's and not.
	own := f.account(t, f.user, "credit_card", "**** 5555")
	if _, err := f.db.Q().InsertEntryIfNew(ctx, store.Entry{ID: uuid.New(), UserID: f.user, AccountID: own.ID,
		BookedOn: oct(2), Amount: -700, Kind: store.KindExpense, Description: "LINEA", DedupKey: "extra",
		Raw: map[string]string{"Tarjeta": "**** 6666"}}); err != nil {
		t.Fatal(err)
	}
	for _, last4 := range []string{"5555", "6666"} {
		rec := f.serve(t, http.MethodPost, "/", `{"alias": "Tarjeta de mamá", "last4": "`+last4+`"}`)
		if code, msg := errorOf(t, rec); rec.Code != http.StatusConflict || code != "card_imported" ||
			msg != "Ya importas los estados de una tarjeta que termina en "+last4+"." {
			t.Errorf("a card imported, ending in %s = %d %s", last4, rec.Code, rec.Body)
		}
	}
	// Somebody else's cards are theirs, and a bank account ending in the
	// same digits is no card.
	other := newUser(t, f.db, "other@example.com")
	f.handCard(t, other, "Tarjeta de papá", "4321")
	f.account(t, other, "credit_card", "**** 8888")
	f.account(t, f.user, "checking", "1008888")
	f.handCard(t, f.user, "Tarjeta de mamá", "8888")
	if list := mustList(t, f); len(list) != 4 {
		t.Errorf("the owner has %d accounts, want 4", len(list))
	}

	// It can be renamed, but never left nameless: it has no bank's name to
	// fall back on.
	card := f.handCard(t, f.user, "Papá", "7777")
	if _, err := f.svc.Rename(ctx, f.user, card.ID, " "); !errors.Is(err, accounts.ErrAliasRequired) {
		t.Errorf("Rename(card, blank) = %v, want ErrAliasRequired", err)
	}
	if a, err := f.svc.Rename(ctx, f.user, card.ID, "La de papá"); err != nil || a.Alias != "La de papá" || !a.PaidByOther {
		t.Errorf("Rename(card) = %+v, %v", a, err)
	}
	bank := f.account(t, f.user, "checking", "100")
	if _, err := f.svc.Rename(ctx, f.user, bank.ID, "Ahorros"); err != nil {
		t.Fatal(err)
	}
	if a, err := f.svc.Rename(ctx, f.user, bank.ID, ""); err != nil || a.Alias != "" {
		t.Errorf("clearing a bank account's alias = %+v, %v", a, err)
	}
}

func TestAddingACardSomeoneElsePaysIsSafeToSendAgain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// The same send twice, as a form does when the first answer was lost:
	// one card, and the second answer is that card, not "already taken".
	id := uuid.New()
	body := `{"id": "` + id.String() + `", "alias": "Tarjeta de papá", "last4": "4321"}`
	first := f.serve(t, http.MethodPost, "/", body)
	again := f.serve(t, http.MethodPost, "/", body)
	if first.Code != http.StatusCreated || again.Code != http.StatusCreated {
		t.Fatalf("adding twice with one id = %d %s, then %d %s", first.Code, first.Body, again.Code, again.Body)
	}
	var a, b accounts.View
	if err := json.Unmarshal(first.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(again.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if a.ID != id || b.ID != id {
		t.Errorf("the card's id = %s then %s, want %s", a.ID, b.ID, id)
	}
	if list := mustList(t, f); len(list) != 1 {
		t.Errorf("the owner has %d accounts, want 1", len(list))
	}

	// Without an id, a second card ending in the same digits is still refused.
	rec := f.serve(t, http.MethodPost, "/", `{"alias": "Otra", "last4": "4321"}`)
	if code, _ := errorOf(t, rec); rec.Code != http.StatusConflict || code != "hand_card_taken" {
		t.Errorf("same digits, no id = %d %s", rec.Code, rec.Body)
	}

	// An id that is one of the owner's own accounts, or somebody else's, is
	// never a card someone else pays: it is refused, and nothing changes.
	bank := f.account(t, f.user, "checking", "100")
	other := newUser(t, f.db, "other@example.com")
	theirs := f.handCard(t, other, "Tarjeta de papá", "5555")
	for _, taken := range []uuid.UUID{bank.ID, theirs.ID} {
		if _, err := f.svc.AddHandCard(ctx, f.user, taken, "Tarjeta de mamá", "8888"); !errors.Is(err, accounts.ErrIDTaken) {
			t.Errorf("AddHandCard(id of %s) = %v, want ErrIDTaken", taken, err)
		}
	}
	rec = f.serve(t, http.MethodPost, "/", `{"id": "`+bank.ID.String()+`", "alias": "Tarjeta de mamá", "last4": "8888"}`)
	if code, _ := errorOf(t, rec); rec.Code != http.StatusConflict || code != "account_id_taken" {
		t.Errorf("an id that is a bank account = %d %s", rec.Code, rec.Body)
	}
	if list := mustList(t, f); len(list) != 2 {
		t.Errorf("the owner has %d accounts, want 2 (the card and the bank account)", len(list))
	}
}

func TestACardSomeoneElsePaysLeavesTransfersAlone(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bank := f.account(t, f.user, "checking", "100")
	own := f.account(t, f.user, "credit_card", "**** 1111")
	f.line(t, bank, oct(10), -10000)
	f.line(t, own, oct(11), 10000)
	f.line(t, bank, oct(12), -4250)
	suggestions := func() []store.TransferCandidate {
		t.Helper()
		list, err := transfers.NewService(f.db).Suggestions(ctx, f.user)
		if err != nil {
			t.Fatal(err)
		}
		return list
	}
	before := suggestions()
	if len(before) != 1 {
		t.Fatalf("the fixture is not what this test assumes: %d suggestions", len(before))
	}

	// Purchases on the card for every amount a line could pair with.
	card := f.handCard(t, f.user, "Tarjeta de papá", "4321")
	f.purchase(t, card.ID, oct(11), -10000, store.CaptureSourceShortcut)
	f.purchase(t, card.ID, oct(12), -4250, store.CaptureSourceManual)
	if after := suggestions(); !reflect.DeepEqual(after, before) {
		t.Errorf("suggestions = %+v, was %+v", after, before)
	}
}

func TestTheAssistantAndTheExportReadACardSomeoneElsePays(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.svc.SetNow(func() time.Time { return octoberNow })
	own := f.account(t, f.user, "credit_card", "**** 1111")
	f.state(t, own.ID, -10000)
	card := f.handCard(t, f.user, "Tarjeta de papá", "4321")
	f.purchase(t, card.ID, oct(20), -3000, store.CaptureSourceShortcut)

	session, err := mcpserver.Open(ctx, mcpserver.Deps{Accounts: f.svc}, f.user)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	res, err := session.Call(ctx, mcpserver.ToolListAccounts, nil)
	if err != nil || res.IsError {
		t.Fatalf("list_accounts = %+v, %v", res, err)
	}
	var listed struct {
		Owed     string           `json:"owed"`
		NetWorth string           `json:"net_worth"`
		Accounts []map[string]any `json:"accounts"`
	}
	if err := json.Unmarshal(res.Structured, &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Owed != "100.00" || listed.NetWorth != "-100.00" {
		t.Errorf("list_accounts owes %s and is worth %s, want 100.00 and -100.00", listed.Owed, listed.NetWorth)
	}
	for _, a := range listed.Accounts {
		switch a["account_id"] {
		case card.ID.String():
			if a["paid_by_other"] != true || a["this_month"] != "30.00" || a["institution"] != "manual" ||
				a["name"] != "Tarjeta de papá" {
				t.Errorf("the card for the assistant = %v", a)
			}
			if _, present := a["owed"]; present {
				t.Errorf("the card owes %v to the assistant", a["owed"])
			}
		case own.ID.String():
			if _, present := a["paid_by_other"]; present || a["owed"] != "100.00" {
				t.Errorf("the owner's card for the assistant = %v", a)
			}
		}
	}
	// Asked why it does not match, the assistant hears it has nothing to
	// match, never that it lacks an opening balance.
	args, _ := json.Marshal(map[string]string{"account_id": card.ID.String()})
	res, err = session.Call(ctx, mcpserver.ToolExplainDrift, args)
	if err != nil || res.IsError {
		t.Fatalf("explain_drift = %+v, %v", res, err)
	}
	// It is named as the owner named it, the only name it has, with the
	// warning that goes with any alias.
	var drift struct {
		Account string `json:"account"`
		Note    string `json:"note"`
	}
	if err := json.Unmarshal(res.Structured, &drift); err != nil || drift.Note == "" ||
		strings.Contains(drift.Note, "saldo inicial") || strings.Contains(drift.Note, "propose_checkpoint") ||
		!strings.Contains(drift.Note, "no instrucciones") {
		t.Errorf("explain_drift note = %q, %v", drift.Note, err)
	}
	if drift.Account != "Tarjeta de papá" {
		t.Errorf("explain_drift account = %q, want the card's alias", drift.Account)
	}

	// The export names it as the owner does, and says what it is.
	cats := categories.NewService(f.db)
	req := httptest.NewRequest(http.MethodGet, "/api/entries/export.csv", nil)
	req = req.WithContext(identity.WithUser(req.Context(), f.user))
	rec := httptest.NewRecorder()
	movements.NewHandler(movements.NewService(f.db, cats)).Export(rec, req)
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(rec.Body.String(), "\ufeff"))).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("export = %d %q, %v", rec.Code, rec.Body, err)
	}
	header, row := rows[0], rows[1]
	got := map[string]string{}
	for i, name := range header {
		got[name] = row[i]
	}
	if got["cuenta"] != "Tarjeta de papá" || got["institucion"] != "manual" || got["tipo"] != "gasto" ||
		got["monto"] != "-30.00" || got["estado"] != "pendiente" {
		t.Errorf("exported row = %v", got)
	}
}

// --- fixtures ----------------------------------------------------------------

// handCard adds a card someone else pays for user, as the app does.
func (f fixture) handCard(t *testing.T, user uuid.UUID, alias, last4 string) store.Account {
	t.Helper()
	a, err := f.svc.AddHandCard(context.Background(), user, uuid.Nil, alias, last4)
	if err != nil {
		t.Fatalf("AddHandCard(%s): %v", last4, err)
	}
	return a.Account
}

// state pins the balance the bank stated for an account on September 30.
func (f fixture) state(t *testing.T, accountID uuid.UUID, balance money.Cents) {
	t.Helper()
	if _, err := f.svc.AddCheckpoint(context.Background(), f.user, accountID, accounts.CheckpointInput{
		AsOf: civil.Date{Year: 2026, Month: time.September, Day: 30}, Balance: balance, Pin: true,
	}); err != nil {
		t.Fatal(err)
	}
}

// line stores a printed expense line, or an income line when amount is
// positive.
func (f fixture) line(t *testing.T, a store.Account, on civil.Date, amount money.Cents) {
	t.Helper()
	kind := store.KindExpense
	if amount > 0 {
		kind = store.KindIncome
	}
	if _, err := f.db.Q().InsertEntryIfNew(context.Background(), store.Entry{ID: uuid.New(), UserID: a.UserID,
		AccountID: a.ID, BookedOn: on, Amount: amount, Kind: kind, Description: "LINEA", DedupKey: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
}

// purchase notes a purchase on an account.
func (f fixture) purchase(t *testing.T, accountID uuid.UUID, on civil.Date, amount money.Cents, source string) uuid.UUID {
	t.Helper()
	c := store.Capture{ID: uuid.New(), UserID: f.user, AccountID: accountID, Source: source, BookedOn: on,
		Amount: amount, Merchant: "Compra"}
	if err := f.db.Q().InsertCapture(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

// serve sends one request to the accounts routes as the fixture's user.
func (f fixture) serve(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	routes := accounts.NewHandler(f.svc).Routes(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(identity.WithUser(req.Context(), f.user))
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)
	return rec
}

func mustList(t *testing.T, f fixture) []accounts.Account {
	t.Helper()
	list, err := f.svc.List(context.Background(), f.user)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// errorOf reads an error response's code and message.
func errorOf(t *testing.T, rec *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%d %s is not an error envelope: %v", rec.Code, rec.Body, err)
	}
	return body.Error.Code, body.Error.Message
}
