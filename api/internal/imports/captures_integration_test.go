package imports_test

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

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/identity"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/movements"
	"github.com/JuanKsPty/chenchen/api/internal/proposals"
	"github.com/JuanKsPty/chenchen/api/internal/reports"
	"github.com/JuanKsPty/chenchen/api/internal/rules"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
	"github.com/JuanKsPty/chenchen/api/internal/transfers"
)

// Purchases the owner noted count as spending from the moment they are noted
// until a printed line could be them, and never once a statement has passed
// their day without one. Every case below checks the same two figures, Gastos
// and the month's flow, which must always agree.

func TestANotedPurchaseCountsUntilItsLineArrivesAndThenOnce(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	card := e.openCard(t, "1111")

	cafe := e.note(t, card, jan(18), -950, "Café de Prueba")
	e.note(t, card, jan(25), -1200, "Super 99")
	if got := e.spent(t); got != 2150 {
		t.Fatalf("spent = %s, want 21.50: both noted purchases count", got)
	}
	// Neither has a category yet: Gastos files them under Sin categoría.
	totals, err := reports.NewService(e.db).Spending(ctx, e.user, jan(1), jan(31), nil)
	if err != nil || len(totals) != 1 || totals[0].CategoryID != nil || totals[0].Total != 2150 || totals[0].Count != 2 {
		t.Errorf("Spending = %+v, %v; want 21.50 in two uncategorized movements", totals, err)
	}

	// The statement prints the café on the 18th: the line counts, the noted
	// purchase no longer, and since it can be no other line the import links
	// them. Nothing prints the 25th's yet, so it still counts.
	e.importFile(t, "tarjeta.txt", []byte(cardFile))
	if got := e.spent(t); got != 1430+1200 {
		t.Errorf("spent = %s, want 26.30: the statement's 14.30 and the unprinted 12.00, the café once", got)
	}
	if c, err := e.db.Q().CaptureByID(ctx, e.user, cafe); err != nil || c.State != store.CaptureLinked {
		t.Errorf("the café's capture = %+v, %v; want linked", c, err)
	}
	// The café appears once in the list too: as its line.
	list, err := e.db.Q().Entries(ctx, store.EntryFilter{UserID: e.user, From: jan(1), Search: "caf"})
	if err != nil || len(list) != 1 || list[0].Origin != store.OriginStatement {
		t.Errorf("café movements = %+v, %v; want only the printed line", list, err)
	}
}

func TestTwinPurchasesNeverCountTwice(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	card := e.openCard(t, "1111")
	twins := []uuid.UUID{e.note(t, card, jan(10), -350, "Café"), e.note(t, card, jan(10), -350, "Café")}
	if got := e.spent(t); got != 700 {
		t.Fatalf("spent = %s, want 7.00", got)
	}

	// The first statement prints one of them. Either noted purchase could be
	// that line, so neither counts and the line does: 3.50 for a while, an
	// undercount, never 10.50.
	line := "**** 1111;**** 1111;10/01/2026;10/01/2026;CAFE DE PRUEBA;R9;Comida y Bebida;-3.5;\r\n"
	e.importFile(t, "uno.txt", []byte(cardHeader+"\r\n"+line))
	if got := e.spent(t); got != 350 {
		t.Errorf("one line printed: spent = %s, want 3.50", got)
	}
	// The next prints both: 7.00, never 14.00.
	e.importFile(t, "dos.txt", []byte(cardHeader+"\r\n"+line+line))
	if got := e.spent(t); got != 700 {
		t.Errorf("both lines printed: spent = %s, want 7.00", got)
	}
	for _, id := range twins {
		if c, err := e.db.Q().CaptureByID(ctx, e.user, id); err != nil || c.State != store.CapturePrinted {
			t.Errorf("twin = %+v, %v; want printed", c, err)
		}
	}
}

func TestCallingALineATransferNeverBringsItsPurchaseBack(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cats := categories.NewService(e.db)
	moves := movements.NewService(e.db, cats)
	card := e.openCard(t, "1111")
	if _, _, err := rules.NewService(e.db, cats).Create(ctx, e.user, rules.Input{MatchText: "farmacia", Transfer: true}); err != nil {
		t.Fatal(err)
	}
	cafe := e.note(t, card, jan(18), -950, "Café de Prueba")
	pharmacy := e.note(t, card, jan(19), -425, "Farmacia")

	// The rule makes the pharmacy's line a transfer as it is imported. That
	// line is still the purchase, so neither counts: 0.55 of charges and the
	// café's line. Both purchases are within the settle days of the
	// statement's end, so only the line can stop them counting. The café is
	// linked to its line; a line that counts as a transfer never is on a
	// guess, so the pharmacy is only printed.
	e.importFile(t, "tarjeta.txt", []byte(cardFile))
	if got := e.spent(t); got != 55+950 {
		t.Errorf("after the import: spent = %s, want 10.05: the pharmacy is a transfer, purchase and line", got)
	}

	// The owner says the café's line is a transfer too: the café stops
	// counting, the noted purchase included.
	lines, err := e.db.Q().Entries(ctx, store.EntryFilter{UserID: e.user, Search: "cafe de prueba"})
	if err != nil || len(lines) != 1 || lines[0].Origin != store.OriginStatement {
		t.Fatalf("the café's line = %+v, %v", lines, err)
	}
	if _, err := moves.SetTransfer(ctx, e.user, lines[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if got := e.spent(t); got != 55 {
		t.Errorf("after marking the café a transfer: spent = %s, want 0.55", got)
	}
	for id, want := range map[uuid.UUID]string{cafe: store.CaptureLinked, pharmacy: store.CapturePrinted} {
		if c, err := e.db.Q().CaptureByID(ctx, e.user, id); err != nil || c.State != want {
			t.Errorf("%s = %+v, %v; want %s", c.Merchant, c, err, want)
		}
	}

	// Taking it back makes the line spending again, once.
	if _, err := moves.SetTransfer(ctx, e.user, lines[0].ID, false); err != nil {
		t.Fatal(err)
	}
	if got := e.spent(t); got != 55+950 {
		t.Errorf("after undoing the transfer: spent = %s, want 10.05", got)
	}
}

func TestAStatementPastTheSettleDaysDropsAPurchaseItDoesNotShow(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	card := e.openCard(t, "1111")
	declined := e.note(t, card, jan(17), -777, "Tienda Rechazada")
	recent := e.note(t, card, jan(18), -555, "Tienda Reciente")
	if got := e.spent(t); got != 1332 {
		t.Fatalf("spent = %s, want 13.32", got)
	}

	// The statement reaches the 20th, three days past the 17th, and has no
	// 7.77: most likely a declined tap, so it stops counting. The 18th is
	// within the days a purchase can stay on hold, so it still counts.
	e.importFile(t, "tarjeta.txt", []byte(cardFile))
	if got := e.spent(t); got != 1430+555 {
		t.Errorf("spent = %s, want 19.85: the statement and the 18th's purchase", got)
	}
	for id, want := range map[uuid.UUID]string{declined: store.CaptureMissing, recent: store.CapturePending} {
		if c, err := e.db.Q().CaptureByID(ctx, e.user, id); err != nil || c.State != want {
			t.Errorf("%s = %+v, %v; want %s", c.Merchant, c, err, want)
		}
	}
	if a := e.account(t, card); a.PendingCount != 1 || a.Pending != -555 || a.ToReview != 1 {
		t.Errorf("card: pending %s in %d, to review %d; want -5.55 in 1, 1 to review", a.Pending, a.PendingCount, a.ToReview)
	}
}

func TestTheListInterleavesNotedPurchasesAndLinesWithoutGapsOrRepeats(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.importFile(t, "tarjeta.txt", []byte(cardFile))
	// Another card, whose only statement is December's, so January's noted
	// purchases on it stay pending between the first card's lines.
	other := e.openCard(t, "3333")
	noted := map[uuid.UUID]bool{}
	for _, d := range []int{14, 15, 15, 16, 19, 21} {
		noted[e.note(t, other, jan(d), -money.Cents(100*d), "Compra")] = true
	}

	want := map[uuid.UUID]bool{}
	for id := range noted {
		want[id] = true
	}
	all, err := e.accounts.List(ctx, e.user)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		lines, err := e.db.Q().AccountEntriesInOrder(ctx, e.user, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lines {
			want[l.ID] = true
		}
	}

	moves := movements.NewService(e.db, categories.NewService(e.db))
	seen := map[uuid.UUID]bool{}
	var prev *store.Entry
	q := movements.Query{Limit: 2}
	for pages := 0; ; pages++ {
		if pages > len(want) {
			t.Fatal("the cursor never ends")
		}
		page, err := moves.List(ctx, e.user, q)
		if err != nil {
			t.Fatal(err)
		}
		for i, m := range page.Entries {
			if seen[m.ID] {
				t.Errorf("%s %s listed twice", m.BookedOn, m.Description)
			}
			seen[m.ID] = true
			if (m.Origin == store.OriginCapture) != noted[m.ID] {
				t.Errorf("%s has origin %q", m.Description, m.Origin)
			}
			if prev != nil && (m.BookedOn.After(prev.BookedOn) ||
				m.BookedOn == prev.BookedOn && strings.Compare(m.ID.String(), prev.ID.String()) >= 0) {
				t.Errorf("%s %s comes after %s %s: not newest first", m.BookedOn, m.ID, prev.BookedOn, prev.ID)
			}
			prev = &page.Entries[i]
		}
		if page.Next == nil {
			break
		}
		q.Before = page.Next
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("listed %d movements, want %d: every line and every pending purchase once", len(seen), len(want))
	}
}

func TestSearchFindsANotedPurchaseByShopAndByAmount(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	card := e.openCard(t, "1111")
	e.importFile(t, "tarjeta.txt", []byte(cardFile))
	id := e.note(t, card, jan(25), -450, "Café Unido")
	moves := movements.NewService(e.db, categories.NewService(e.db))

	for _, search := range []string{"unido", "Café", "$4.50", "4.50"} {
		page, err := moves.List(ctx, e.user, movements.Query{Search: search})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Entries) != 1 || page.Entries[0].ID != id || page.Entries[0].Origin != store.OriginCapture {
			t.Errorf("search %q = %+v, want only the noted purchase", search, page.Entries)
		}
	}
	// And the amount of a printed line finds the line, not the purchase.
	page, err := moves.List(ctx, e.user, movements.Query{Search: "9.50"})
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Origin != store.OriginStatement {
		t.Errorf("search 9.50 = %+v, %v; want the café's line", page.Entries, err)
	}
}

func TestTheExportMarksNotedPurchasesPending(t *testing.T) {
	e := setup(t)
	card := e.openCard(t, "1111")
	e.importFile(t, "tarjeta.txt", []byte(cardFile))
	e.note(t, card, jan(25), -1200, "Super 99")
	h := movements.NewHandler(movements.NewService(e.db, categories.NewService(e.db)))

	rec := e.serve(t, h.Routes(), http.MethodGet, "/export.csv?from=2026-01-01", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("export = %d %s", rec.Code, rec.Body)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(rec.Body.String(), "\xef\xbb\xbf"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	header := rows[0]
	// estado is appended: the columns a spreadsheet already reads stay put.
	if len(header) != 13 || header[11] != "id" || header[12] != "estado" {
		t.Fatalf("header = %v", header)
	}
	// January's five printed lines and the noted purchase.
	if len(rows) != 7 {
		t.Fatalf("%d rows, want a header and 6 movements", len(rows))
	}
	for _, r := range rows[1:] {
		want := ""
		if r[7] == "Super 99" {
			want = "pendiente"
		}
		if r[12] != want {
			t.Errorf("%q has estado %q, want %q", r[7], r[12], want)
		}
	}
	if last := rows[len(rows)-1]; last[7] != "Super 99" || last[3] != "gasto" || last[5] != "-12.00" {
		t.Errorf("the noted purchase's row = %v", last)
	}
}

func TestANotedPurchaseCanBeFiledAndAnnotatedButIsNeverATransfer(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cats := categories.NewService(e.db)
	moves := movements.NewService(e.db, cats)
	svc := proposals.NewService(e.db, e.accounts, cats, moves, rules.NewService(e.db, cats), transfers.NewService(e.db))
	compras, _, _ := cats.FindByName(ctx, e.user, "Compras")
	salud, _, _ := cats.FindByName(ctx, e.user, "Salud")

	e.importFile(t, "tarjeta.txt", []byte(cardFile))
	other := e.openCard(t, "3333")
	super := e.note(t, other, jan(25), -1200, "Super 99")
	pharmacy := e.note(t, other, jan(25), -425, "Farmacia")

	// By hand, through the same service the PATCH uses.
	got, err := moves.SetCategory(ctx, e.user, super, &compras.ID, "user")
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin != store.OriginCapture || got.CategoryID == nil || *got.CategoryID != compras.ID || *got.CategorySource != "user" {
		t.Errorf("after SetCategory: %+v", got)
	}
	if got, err = moves.SetNote(ctx, e.user, super, "  para la casa "); err != nil || got.Note != "para la casa" {
		t.Errorf("after SetNote: %+v, %v", got, err)
	}
	if c, _ := e.db.Q().CaptureByID(ctx, e.user, super); c.CategoryID == nil || *c.CategoryID != compras.ID || c.Note != "para la casa" {
		t.Errorf("stored capture = %+v", c)
	}
	totals, err := reports.NewService(e.db).Spending(ctx, e.user, jan(25), jan(25), nil)
	if err != nil || len(totals) != 2 {
		t.Fatalf("Spending on the 25th = %+v, %v", totals, err)
	}
	for _, total := range totals {
		if total.CategoryID != nil && (*total.CategoryID != compras.ID || total.Total != 1200) {
			t.Errorf("filed total = %+v, want 12.00 in Compras", total)
		}
	}

	// A noted purchase is spending: never a transfer, either way.
	for _, transfer := range []bool{true, false} {
		if _, err := moves.SetTransfer(ctx, e.user, super, transfer); !errors.Is(err, movements.ErrCaptureNotTransfer) {
			t.Errorf("SetTransfer(%v) = %v, want ErrCaptureNotTransfer", transfer, err)
		}
	}
	// Over HTTP the refusal comes before anything is written, so a request
	// that also sets a category does not half apply.
	h := movements.NewHandler(moves)
	rec := e.serve(t, h.Routes(), http.MethodPatch, "/"+super.String(),
		`{"category_id": "`+salud.ID.String()+`", "transfer": true}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Una compra anotada no puede ser una transferencia.") {
		t.Errorf("PATCH transfer = %d %s, want 422", rec.Code, rec.Body)
	}
	if c, _ := e.db.Q().CaptureByID(ctx, e.user, super); *c.CategoryID != compras.ID {
		t.Error("a refused PATCH changed the category")
	}
	var view struct {
		Origin        string `json:"origin"`
		CaptureSource string `json:"capture_source"`
	}
	rec = e.serve(t, h.Routes(), http.MethodPatch, "/"+super.String(), `{"note": "otra"}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil || rec.Code != http.StatusOK ||
		view.Origin != store.OriginCapture || view.CaptureSource != store.CaptureSourceManual {
		t.Errorf("PATCH note = %d %s", rec.Code, rec.Body)
	}

	// The assistant can file and annotate one, through a proposal the owner applies...
	p, err := svc.Propose(ctx, e.user, proposals.Recategorize{EntryID: pharmacy, CategoryID: &salud.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(ctx, e.user, p.ID); err != nil {
		t.Fatal(err)
	}
	n, err := svc.Propose(ctx, e.user, proposals.Note{EntryID: pharmacy, Note: "receta"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(ctx, e.user, n.ID); err != nil {
		t.Fatal(err)
	}
	if c, _ := e.db.Q().CaptureByID(ctx, e.user, pharmacy); c.CategoryID == nil || *c.CategoryID != salud.ID ||
		*c.CategorySource != "assistant" || c.Note != "receta" {
		t.Errorf("after the proposals: %+v", c)
	}

	// ...but cannot make it a transfer, or pair it as one.
	yes := true
	if _, err := svc.Propose(ctx, e.user, proposals.Recategorize{EntryID: pharmacy, Transfer: &yes}); !errors.Is(err, proposals.ErrInvalid) ||
		!strings.Contains(err.Error(), "una compra anotada no es una transferencia") {
		t.Errorf("proposing a transfer = %v, want ErrInvalid", err)
	}
	payment, err := e.db.Q().Entries(ctx, store.EntryFilter{UserID: e.user, Search: "GRACIAS POR SU PAGO"})
	if err != nil || len(payment) != 1 {
		t.Fatalf("the card's payment = %v, %v", payment, err)
	}
	bait := e.note(t, other, jan(20), -10000, "Pago")
	if _, err := svc.Propose(ctx, e.user, proposals.TransferDecision{OutEntryID: bait, InEntryID: payment[0].ID, Confirm: true}); !errors.Is(err, proposals.ErrInvalid) ||
		!strings.Contains(err.Error(), "una compra anotada no es una transferencia") {
		t.Errorf("proposing a transfer pair = %v, want ErrInvalid", err)
	}

	// Once linked to its line, the line is the movement: the capture's id
	// finds nothing, and nothing can be written through it.
	printed := store.Entry{ID: uuid.New(), UserID: e.user, AccountID: other, BookedOn: jan(26), Amount: -1200,
		Kind: store.KindExpense, Description: "SUPER 99", DedupKey: uuid.NewString()}
	if ok, err := e.db.Q().InsertEntryIfNew(ctx, printed); err != nil || !ok {
		t.Fatalf("InsertEntryIfNew = %v, %v", ok, err)
	}
	if err := storetest.Exec(t, e.db, `UPDATE captures SET entry_id = $2, linked_by = 'user' WHERE id = $1`,
		super, printed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := moves.Get(ctx, e.user, super); !errors.Is(err, movements.ErrNotFound) {
		t.Errorf("Get(linked capture) = %v, want ErrNotFound", err)
	}
	if _, err := moves.SetNote(ctx, e.user, super, "x"); !errors.Is(err, movements.ErrNotFound) {
		t.Errorf("SetNote(linked capture) = %v, want ErrNotFound", err)
	}
}

func TestNotedPurchasesNeverMoveABalanceOrItsDrift(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	r := e.importFile(t, "m.xlsx", bgFile(t, []bgRow{
		{day: 1, desc: "YAPPY", debit: -12.5, balance: 87.5},
		{day: 2, desc: "TRANSFERENCIA", credit: 20, balance: 107.5},
	}))
	bank := r.Accounts[0].AccountID
	card := e.importFile(t, "tarjeta.txt", []byte(cardFile)).Accounts[0].AccountID
	quiet := e.openCard(t, "3333")

	type snapshot struct {
		Bank, Card, Quiet accounts.Account
		Assets, Debts     money.Cents
		Lines             []accounts.Line
		Checks            []accounts.Check
		Opening           money.Cents
		FirstBreak        *uuid.UUID
	}
	read := func() snapshot {
		t.Helper()
		all, err := e.accounts.List(ctx, e.user)
		if err != nil {
			t.Fatal(err)
		}
		var s snapshot
		for _, a := range all {
			switch a.ID {
			case bank:
				s.Bank = a
			case card:
				s.Card = a
			case quiet:
				s.Quiet = a
			}
		}
		s.Assets, s.Debts, _ = accounts.NetWorth(all)
		rec, err := e.accounts.Reconcile(ctx, e.user, bank)
		if err != nil {
			t.Fatal(err)
		}
		s.Lines, s.Checks, s.Opening, s.FirstBreak = rec.Lines, rec.Checks, rec.Opening, rec.FirstBreak
		return s
	}
	before := read()
	if !before.Bank.Anchored || before.Bank.Balance != 10750 || before.Bank.Drift == nil || before.Bank.Drift.Difference() != 0 {
		t.Fatalf("the fixture is not what this test assumes: %+v", before.Bank)
	}

	// The bank statement ends on March 2. Two purchases after it are pending,
	// one is its YAPPY line (printed), one is from before it and not on it
	// (missing), and one was discarded.
	march := func(d int) civil.Date { return civil.Date{Year: 2026, Month: time.March, Day: d} }
	e.note(t, bank, march(5), -300, "Café")
	e.note(t, bank, march(6), -200, "Kiosco")
	e.note(t, bank, march(1), -1250, "Yappy")
	e.note(t, bank, civil.Date{Year: 2026, Month: time.February, Day: 20}, -999, "Rechazada")
	if err := storetest.Exec(t, e.db, `UPDATE captures SET discarded_at = now() WHERE id = $1`,
		e.note(t, bank, march(5), -100, "No pasó")); err != nil {
		t.Fatal(err)
	}
	e.note(t, card, jan(25), -1200, "Super 99")

	after := read()
	for name, pair := range map[string][2]accounts.Account{
		"bank": {before.Bank, after.Bank}, "card": {before.Card, after.Card}, "quiet": {before.Quiet, after.Quiet},
	} {
		b, a := pair[0], pair[1]
		if a.Balance != b.Balance || a.Anchored != b.Anchored || !reflect.DeepEqual(a.Drift, b.Drift) ||
			a.Movements != b.Movements || a.LastDay != b.LastDay || a.Holdings != b.Holdings {
			t.Errorf("%s moved: before %+v\nafter %+v", name, b, a)
		}
	}
	if after.Assets != before.Assets || after.Debts != before.Debts {
		t.Errorf("net worth moved: %s/%s, was %s/%s", after.Assets, after.Debts, before.Assets, before.Debts)
	}
	if !reflect.DeepEqual(after.Lines, before.Lines) || !reflect.DeepEqual(after.Checks, before.Checks) ||
		after.Opening != before.Opening || !reflect.DeepEqual(after.FirstBreak, before.FirstBreak) {
		t.Error("the reconciliation changed")
	}
	if b := after.Bank; b.Pending != -500 || b.PendingCount != 2 || b.ToReview != 2 {
		t.Errorf("bank: pending %s in %d, to review %d; want -5.00 in 2, 2 to review", b.Pending, b.PendingCount, b.ToReview)
	}

	// On the wire: an anchored account shows its estimate; an unanchored one
	// only what is pending, since its balance is not the bank's to begin with.
	list := []accounts.Account{after.Bank, after.Card, after.Quiet}
	w := wire(t, list, bank)
	pending, _ := w["pending"].(map[string]any)
	amount, _ := pending["amount"].(map[string]any)
	estimated, _ := w["estimated_balance"].(map[string]any)
	if amount["cents"] != float64(-500) || pending["count"] != float64(2) || estimated["cents"] != float64(10250) ||
		w["to_review"] != float64(2) {
		t.Errorf("bank on the wire: pending %v, estimated %v, to review %v", pending, estimated, w["to_review"])
	}
	w = wire(t, list, card)
	if _, ok := w["pending"]; !ok {
		t.Error("the card's pending purchase is missing on the wire")
	}
	if _, ok := w["estimated_balance"]; ok {
		t.Error("an unanchored card carries an estimated balance")
	}
	w = wire(t, list, quiet)
	for _, field := range []string{"pending", "estimated_balance", "to_review"} {
		if _, ok := w[field]; ok {
			t.Errorf("an account with no noted purchases carries %s", field)
		}
	}
	if got := e.account(t, bank); got.Pending != -500 || got.PendingCount != 2 || got.ToReview != 2 {
		t.Errorf("Get disagrees with List: %+v", got)
	}
}

// --- fixtures ----------------------------------------------------------------

// cardHeader is cardFile's header, for more statements of the same format.
var cardHeader, _, _ = strings.Cut(cardFile, "\r\n")

// openCard imports a December statement of the card ending in last4, so the
// card exists — with nothing in January — before any purchase is noted on it.
func (e env) openCard(t *testing.T, last4 string) uuid.UUID {
	t.Helper()
	n := "**** " + last4
	r := e.importFile(t, "diciembre-"+last4+".txt", []byte(cardHeader+"\r\n"+
		n+";"+n+";01/12/2025;01/12/2025;SUPER DE PRUEBA;R0;Supermercado;-20.00;\r\n"))
	return r.Accounts[0].AccountID
}

// note stores a purchase the owner noted by hand.
func (e env) note(t *testing.T, account uuid.UUID, on civil.Date, amount money.Cents, merchant string) uuid.UUID {
	t.Helper()
	c := store.Capture{ID: uuid.New(), UserID: e.user, AccountID: account, Source: store.CaptureSourceManual,
		BookedOn: on, Amount: amount, Merchant: merchant}
	if err := e.db.Q().InsertCapture(context.Background(), c); err != nil {
		t.Fatalf("InsertCapture: %v", err)
	}
	return c.ID
}

// spent is January 2026's spending as Gastos totals it, checked against the
// month's flow: the two read the same movements and must agree.
func (e env) spent(t *testing.T) money.Cents {
	t.Helper()
	ctx := context.Background()
	rep := reports.NewService(e.db)
	totals, err := rep.Spending(ctx, e.user, jan(1), jan(31), nil)
	if err != nil {
		t.Fatal(err)
	}
	var spent money.Cents
	for _, total := range totals {
		spent += total.Total
	}
	points, err := rep.Flow(ctx, e.user, jan(1), jan(31), true)
	if err != nil {
		t.Fatal(err)
	}
	var out money.Cents
	for _, p := range points {
		out += p.Out
	}
	if out != spent {
		t.Errorf("the flow's spending is %s and Gastos says %s; they must agree", out, spent)
	}
	return spent
}

// serve sends one request to handler as the env's user.
func (e env) serve(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(identity.WithUser(req.Context(), e.user))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// wire is one account of the list exactly as GET /api/accounts sends it.
func wire(t *testing.T, list []accounts.Account, id uuid.UUID) map[string]any {
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

func jan(d int) civil.Date { return civil.Date{Year: 2026, Month: time.January, Day: d} }
