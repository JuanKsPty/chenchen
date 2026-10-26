package captures_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/captures"
	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/reports"
	"github.com/JuanKsPty/chenchen/api/internal/rules"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

// day is when most purchases below were made: in the past, whatever the clock
// says, since a purchase cannot be dated after today.
var day = civil.Date{Year: 2026, Month: time.March, Day: 10}

// A form that lost the answer to its first try sends the same id again; the
// purchase is noted once, and another user can never claim that id.
func TestNotingTheSamePurchaseTwiceNotesItOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card")
	in := captures.AddInput{ID: uuid.New(), AccountID: card.ID, Amount: 1250, Merchant: "Super 99", Day: day}

	first, err := f.svc.Add(ctx, f.user, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.svc.Add(ctx, f.user, in)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != in.ID || again.ID != in.ID || again.State != store.CapturePending {
		t.Errorf("ids = %s, %s, want both %s", first.ID, again.ID, in.ID)
	}
	if got := f.spent(t); got != 1250 {
		t.Errorf("spent = %s, want 12.50 once", got)
	}

	other := f.newUser(t, "b@example.com")
	theirs := f.account(t, other, "checking")
	in.AccountID = theirs.ID
	if _, err := f.svc.Add(ctx, other, in); !errors.Is(err, captures.ErrIDTaken) {
		t.Errorf("other user's add with a taken id = %v, want ErrIDTaken", err)
	}
}

func TestNotingAPurchaseByHand(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card")
	compras, salud := f.category(t, f.user, "Compras"), f.category(t, f.user, "Salud")
	f.rule(t, "farmacia", salud)

	// What the owner types is what was paid. It is stored with the holder's
	// sign, filed by the rules and dated today unless another day is given.
	before := today()
	c, err := f.svc.Add(ctx, f.user, captures.AddInput{AccountID: card.ID, Amount: 1250, Merchant: "  Farmacia del Sol "})
	after := today()
	if err != nil {
		t.Fatal(err)
	}
	if c.Amount != -1250 || c.Merchant != "Farmacia del Sol" || c.Source != store.CaptureSourceManual ||
		c.State != store.CapturePending || (c.BookedOn != before && c.BookedOn != after) || c.Line != nil {
		t.Errorf("capture = %+v", c.Capture)
	}
	if c.CategoryID == nil || *c.CategoryID != salud || *c.CategorySource != "rule" {
		t.Errorf("category = %v by %v, want Salud by the rule", c.CategoryID, c.CategorySource)
	}
	// A category the owner picks is theirs, whatever the rules say.
	c, err = f.svc.Add(ctx, f.user, captures.AddInput{AccountID: card.ID, Amount: 300, Merchant: "Farmacia", Day: day,
		CategoryID: &compras, Note: " para mamá "})
	if err != nil {
		t.Fatal(err)
	}
	if c.BookedOn != day || c.CategoryID == nil || *c.CategoryID != compras || *c.CategorySource != "user" || c.Note != "para mamá" {
		t.Errorf("capture = %+v", c.Capture)
	}
	if got := f.spent(t); got != 1550 {
		t.Errorf("spent = %s, want 15.50: both count from the moment they are noted", got)
	}

	// What cannot be a purchase is refused, and nothing is stored.
	brokerage := f.account(t, f.user, "brokerage")
	other := f.newUser(t, "b@example.com")
	theirs, theirCategory := f.account(t, other, "checking"), f.category(t, other, "Compras")
	ok := captures.AddInput{AccountID: card.ID, Amount: 100, Merchant: "Kiosco", Day: day}
	for name, tt := range map[string]struct {
		change func(*captures.AddInput)
		want   error
	}{
		"a brokerage account":     {func(in *captures.AddInput) { in.AccountID = brokerage.ID }, captures.ErrBrokerage},
		"someone else's account":  {func(in *captures.AddInput) { in.AccountID = theirs.ID }, captures.ErrAccountNotFound},
		"no such account":         {func(in *captures.AddInput) { in.AccountID = uuid.New() }, captures.ErrAccountNotFound},
		"no merchant":             {func(in *captures.AddInput) { in.Merchant = "   " }, captures.ErrMerchantRequired},
		"a merchant too long":     {func(in *captures.AddInput) { in.Merchant = strings.Repeat("ñ", 141) }, captures.ErrMerchantTooLong},
		"a note too long":         {func(in *captures.AddInput) { in.Note = strings.Repeat("n", 501) }, captures.ErrNoteTooLong},
		"tomorrow":                {func(in *captures.AddInput) { in.Day = today().AddDays(1) }, captures.ErrFutureDay},
		"nothing paid":            {func(in *captures.AddInput) { in.Amount = 0 }, money.ErrNotPositive},
		"a card number as amount": {func(in *captures.AddInput) { in.Amount = 4111111111111111_00 }, captures.ErrAmountTooLarge},
		"someone else's category": {func(in *captures.AddInput) { in.CategoryID = &theirCategory }, categories.ErrNotOwned},
	} {
		in := ok
		tt.change(&in)
		if _, err := f.svc.Add(ctx, f.user, in); !errors.Is(err, tt.want) {
			t.Errorf("%s: Add = %v, want %v", name, err, tt.want)
		}
	}
	if s, _ := f.db.Q().CaptureSummaries(ctx, f.user); len(s) != 1 || s[card.ID].PendingCount != 2 {
		t.Errorf("CaptureSummaries = %+v; a refused purchase was stored", s)
	}
	// The longest merchant there may be is fine, and so is the largest amount.
	in := ok
	in.Merchant = strings.Repeat("ñ", 140)
	if _, err := f.svc.Add(ctx, f.user, in); err != nil {
		t.Errorf("a 140-letter merchant: %v", err)
	}
	in = ok
	in.Amount = 10_000_000_00
	if _, err := f.svc.Add(ctx, f.user, in); err != nil {
		t.Errorf("ten million: %v", err)
	}
}

func TestAManualLinkTakesATip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card")
	food := f.category(t, f.user, "Alimentación")
	tipped := f.line(t, card, day.AddDays(1), -4600, store.KindExpense, "CAFE UNIDO PANAMA")
	f.statement(t, card, day.AddDays(3))
	c, err := f.svc.Add(ctx, f.user, captures.AddInput{AccountID: card.ID, Amount: 4000, Merchant: "Café Unido", Day: day,
		CategoryID: &food, Note: "con propina"})
	if err != nil {
		t.Fatal(err)
	}

	// No line has its amount and the statement passed its day: it no longer
	// counts, and review offers the line with the tip.
	if c.State != store.CaptureMissing {
		t.Fatalf("state = %s, want missing", c.State)
	}
	if got := f.spent(t); got != 4600 {
		t.Errorf("spent = %s, want 46.00: the line only", got)
	}
	review, err := f.svc.Review(ctx, f.user)
	if err != nil || len(review) != 1 || review[0].Capture.ID != c.ID || len(review[0].Candidates) != 0 ||
		len(review[0].Near) != 1 || review[0].Near[0].ID != tipped.ID {
		t.Fatalf("Review = %+v, %v; want the purchase with the tipped line near it", review, err)
	}

	// The owner says it is that line: any amount links.
	got, err := f.svc.Link(ctx, f.user, c.ID, tipped.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.CaptureLinked || *got.LinkedBy != store.LinkedByUser || got.Line == nil || got.Line.ID != tipped.ID {
		t.Fatalf("after Link: %+v", got)
	}
	if l := got.Line; l.Amount != -4600 || l.CategoryID == nil || *l.CategoryID != food || *l.CategorySource != "user" || l.Note != "con propina" {
		t.Errorf("the line = %+v; want 46.00 still, filed and annotated as the purchase was", l)
	}
	if got := f.spent(t); got != 4600 {
		t.Errorf("spent = %s, want 46.00 once", got)
	}
	if review, err := f.svc.Review(ctx, f.user); err != nil || len(review) != 0 {
		t.Errorf("Review = %+v, %v; want nothing left", review, err)
	}
}

func TestUnlinkingRemembersTheLineIsAnotherPurchase(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card")
	line := f.line(t, card, day, -950, store.KindExpense, "CAFE DE PRUEBA")
	c := f.add(t, card, day, 950, "Café")
	if c.State != store.CaptureLinked || *c.LinkedBy != store.LinkedByAuto {
		t.Fatalf("a purchase whose line is already imported = %+v; want linked at once", c.Capture)
	}

	// "No es esta compra": the line is another purchase, so both count.
	got, err := f.svc.Unlink(ctx, f.user, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.CapturePending || got.EntryID != nil || got.LinkedBy != nil || got.Line != nil {
		t.Errorf("after Unlink: %+v", got)
	}
	if spent := f.spent(t); spent != 1900 {
		t.Errorf("spent = %s, want 19.00", spent)
	}
	if _, err := f.svc.Unlink(ctx, f.user, c.ID); !errors.Is(err, captures.ErrNotLinked) {
		t.Errorf("unlinking again = %v, want ErrNotLinked", err)
	}

	// Nothing links them again on its own: not another write to the
	// account, not a plain AutoLink.
	f.add(t, card, day, 100, "Otra")
	if err := f.db.InTx(ctx, func(q *store.Queries) error {
		n, err := captures.AutoLink(ctx, q, f.user, nil)
		if n != 0 {
			t.Errorf("AutoLink linked %d", n)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if s := f.state(t, c.ID); s != store.CapturePending {
		t.Errorf("state = %s, want pending", s)
	}
	// Once a statement passes its day it is missing, and review does not
	// offer the line it was told is not it.
	f.statement(t, card, day.AddDays(3))
	review, err := f.svc.Review(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range review {
		if r.Capture.ID == c.ID {
			found = true
			if r.Capture.State != store.CaptureMissing || len(r.Candidates) != 0 || len(r.Near) != 0 {
				t.Errorf("in review: %+v; want missing, with nothing near it", r)
			}
		}
	}
	if !found {
		t.Errorf("Review = %+v; want the purchase in it", review)
	}

	// The owner may still link them by hand.
	if got, err := f.svc.Link(ctx, f.user, c.ID, line.ID); err != nil || got.State != store.CaptureLinked {
		t.Errorf("Link by hand = %+v, %v", got, err)
	}
}

// The wrong purchase's category and note leave the line with the link, so the
// rules and then the purchase the line really is can file it.
func TestUndoingAWrongLinkLetsTheRightPurchaseFileTheLine(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card")
	fun, salud := f.category(t, f.user, "Entretenimiento"), f.category(t, f.user, "Salud")
	line := f.line(t, card, day, -700, store.KindExpense, "FARMACIA ARROCHA")
	movie, err := f.svc.Add(ctx, f.user, captures.AddInput{AccountID: card.ID, Amount: 700, Merchant: "Cine", Day: day,
		CategoryID: &fun, Note: "con Ana"})
	if err != nil {
		t.Fatal(err)
	}
	if movie.State != store.CaptureLinked || movie.Line == nil || *movie.Line.CategoryID != fun || movie.Line.Note != "con Ana" {
		t.Fatalf("fixture: the movie = %+v, line %+v; want linked, the line filed as the movie", movie.Capture, movie.Line)
	}

	// "No es esta compra": the line is as the statement left it.
	if _, err := f.svc.Unlink(ctx, f.user, movie.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.lineByID(t, line.ID); got.CategoryID != nil || got.CategorySource != nil || got.Note != "" {
		t.Errorf("after unlinking, the line = %+v; want nothing of the movie's", got)
	}
	// The movie keeps what the owner said about it.
	if c := f.get(t, f.user, movie.ID); c.State != store.CapturePending || *c.CategoryID != fun || c.Note != "con Ana" {
		t.Errorf("the movie = %+v", c)
	}

	// A rule files the line again.
	if _, applied, err := rules.NewService(f.db, f.cats).Create(ctx, f.user, rules.Input{MatchText: "farmacia",
		CategoryID: &salud, ApplyToExisting: true}); err != nil || applied != 1 {
		t.Fatalf("the rule applied to %d, %v; want the line", applied, err)
	}
	if got := f.lineByID(t, line.ID); got.CategoryID == nil || *got.CategoryID != salud || *got.CategorySource != "rule" {
		t.Errorf("after the rule, the line = %+v; want Salud by the rule", got)
	}

	// And the purchase it really is carries the owner's choice and note.
	pharmacy, err := f.svc.Add(ctx, f.user, captures.AddInput{AccountID: card.ID, Amount: 700, Merchant: "Farmacia",
		Day: day, CategoryID: &salud, Note: "receta"})
	if err != nil {
		t.Fatal(err)
	}
	if pharmacy.State != store.CaptureLinked || pharmacy.Line == nil || pharmacy.Line.ID != line.ID {
		t.Fatalf("the pharmacy = %+v; want linked to the line", pharmacy.Capture)
	}
	if l := pharmacy.Line; *l.CategoryID != salud || *l.CategorySource != "user" || l.Note != "receta" {
		t.Errorf("the line = %+v; want Salud by the owner, with «receta»", l)
	}
}

func TestRejectingOneOfTwoLinesLinksTheOther(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card, checking := f.account(t, f.user, "credit_card"), f.account(t, f.user, "checking")
	first := f.line(t, card, day, -350, store.KindExpense, "CAFE")
	second := f.line(t, card, day.AddDays(1), -350, store.KindExpense, "CAFE")
	c := f.add(t, card, day, 350, "Café")
	if c.State != store.CapturePrinted {
		t.Fatalf("with two lines it could be: %s, want printed", c.State)
	}

	got, err := f.svc.Reject(ctx, f.user, c.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.CaptureLinked || got.Line == nil || got.Line.ID != second.ID || *got.LinkedBy != store.LinkedByAuto {
		t.Errorf("after rejecting the first line: %+v; want linked to the second", got)
	}

	other := f.newUser(t, "b@example.com")
	theirLine := f.line(t, f.account(t, other, "credit_card"), day, -350, store.KindExpense, "CAFE")
	open := f.add(t, card, day.AddDays(-20), 777, "Kiosco")
	for name, tt := range map[string]struct {
		capture, line uuid.UUID
		want          error
	}{
		"a line of another account": {open.ID, f.line(t, checking, day, -777, store.KindExpense, "X").ID, captures.ErrOtherAccount},
		"someone else's line":       {open.ID, theirLine.ID, captures.ErrLineNotFound},
		"a linked purchase":         {c.ID, first.ID, captures.ErrLinked},
		"no such purchase":          {uuid.New(), first.ID, captures.ErrNotFound},
	} {
		if _, err := f.svc.Reject(ctx, f.user, tt.capture, tt.line); !errors.Is(err, tt.want) {
			t.Errorf("Reject(%s) = %v, want %v", name, err, tt.want)
		}
	}
}

func TestADiscardedPurchaseCountsNoMoreUntilRestored(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card")
	c := f.add(t, card, day, 777, "Tienda")

	got, err := f.svc.Discard(ctx, f.user, c.ID)
	if err != nil || got.State != store.CaptureDiscarded || got.DiscardedAt == nil {
		t.Fatalf("Discard = %+v, %v", got, err)
	}
	if spent := f.spent(t); spent != 0 {
		t.Errorf("spent = %s, want nothing", spent)
	}
	if again, err := f.svc.Discard(ctx, f.user, c.ID); err != nil || !again.DiscardedAt.Equal(*got.DiscardedAt) {
		t.Errorf("discarding again = %+v, %v; want nothing to change", again, err)
	}
	// It cannot be corrected, linked or rejected until restored.
	amount := money.Cents(800)
	if _, err := f.svc.Edit(ctx, f.user, c.ID, captures.EditInput{Amount: &amount}); !errors.Is(err, captures.ErrDiscarded) {
		t.Errorf("Edit = %v, want ErrDiscarded", err)
	}

	// Its line arrives while it is discarded; restoring it links them.
	line := f.line(t, card, day.AddDays(2), -777, store.KindExpense, "TIENDA")
	if _, err := f.svc.Link(ctx, f.user, c.ID, line.ID); !errors.Is(err, captures.ErrDiscarded) {
		t.Errorf("Link = %v, want ErrDiscarded", err)
	}
	got, err = f.svc.Restore(ctx, f.user, c.ID)
	if err != nil || got.State != store.CaptureLinked || got.DiscardedAt != nil || got.Line == nil || got.Line.ID != line.ID {
		t.Fatalf("Restore = %+v, %v; want linked to its line", got, err)
	}
	if again, err := f.svc.Restore(ctx, f.user, c.ID); err != nil || again.State != store.CaptureLinked {
		t.Errorf("restoring again = %+v, %v", again, err)
	}
	if _, err := f.svc.Discard(ctx, f.user, c.ID); !errors.Is(err, captures.ErrLinked) {
		t.Errorf("discarding a linked purchase = %v, want ErrLinked", err)
	}
	if spent := f.spent(t); spent != 777 {
		t.Errorf("spent = %s, want 7.77: the line", spent)
	}

	// A restored purchase with no line counts again.
	pending := f.add(t, card, day, 120, "Kiosco")
	if _, err := f.svc.Discard(ctx, f.user, pending.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := f.svc.Restore(ctx, f.user, pending.ID); err != nil || got.State != store.CapturePending {
		t.Errorf("Restore = %+v, %v; want pending", got, err)
	}
	if spent := f.spent(t); spent != 777+120 {
		t.Errorf("spent = %s, want 8.97", spent)
	}
}

func TestCorrectingAPurchaseDerivesItsStateAgain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card")
	food, compras := f.category(t, f.user, "Alimentación"), f.category(t, f.user, "Compras")
	f.rule(t, "super", food)
	line := f.line(t, card, day, -1200, store.KindExpense, "SUPER 99 PANAMA")

	// Typed wrong, it matches neither the line nor the rule.
	c := f.add(t, card, day, 1000, "Supr 99")
	if c.State != store.CapturePending || c.CategoryID != nil {
		t.Fatalf("capture = %+v", c.Capture)
	}
	amount, merchant := money.Cents(1200), " Super 99 "
	got, err := f.svc.Edit(ctx, f.user, c.ID, captures.EditInput{Amount: &amount, Merchant: &merchant})
	if err != nil {
		t.Fatal(err)
	}
	// Corrected, the rule files it and it is its line's.
	if got.State != store.CaptureLinked || got.Amount != -1200 || got.Merchant != "Super 99" || got.Line == nil ||
		got.Line.ID != line.ID || got.CategoryID == nil || *got.CategoryID != food {
		t.Errorf("after Edit: %+v", got)
	}
	if got.Line.CategoryID == nil || *got.Line.CategoryID != food || *got.Line.CategorySource != "rule" {
		t.Errorf("the line = %+v; want filed by the rule the purchase was", got.Line)
	}
	if _, err := f.svc.Edit(ctx, f.user, c.ID, captures.EditInput{Amount: &amount}); !errors.Is(err, captures.ErrLinked) {
		t.Errorf("editing a linked purchase = %v, want ErrLinked", err)
	}

	// A day too far from its line keeps it apart; the right day joins them.
	late := f.line(t, card, day, -500, store.KindExpense, "KIOSCO")
	k := f.add(t, card, day.AddDays(-20), 500, "Kiosco")
	if k.State != store.CapturePending {
		t.Fatalf("state = %s, want pending", k.State)
	}
	when := day.AddDays(-1)
	if got, err := f.svc.Edit(ctx, f.user, k.ID, captures.EditInput{Day: &when}); err != nil || got.State != store.CaptureLinked ||
		got.Line.ID != late.ID || got.BookedOn != when {
		t.Errorf("after moving the day: %+v, %v", got, err)
	}

	// A category the owner chose stays when the merchant changes.
	mine, err := f.svc.Add(ctx, f.user, captures.AddInput{AccountID: card.ID, Amount: 50, Merchant: "Tienda", Day: day, CategoryID: &compras})
	if err != nil {
		t.Fatal(err)
	}
	merchant = "Super Rey"
	if got, err := f.svc.Edit(ctx, f.user, mine.ID, captures.EditInput{Merchant: &merchant}); err != nil ||
		*got.CategoryID != compras || *got.CategorySource != "user" {
		t.Errorf("after renaming: %+v, %v; want the owner's category", got, err)
	}

	tomorrow, zero, huge, blank := today().AddDays(1), money.Cents(0), money.Cents(10_000_000_01), " "
	for name, tt := range map[string]struct {
		in   captures.EditInput
		want error
	}{
		"tomorrow":     {captures.EditInput{Day: &tomorrow}, captures.ErrFutureDay},
		"nothing paid": {captures.EditInput{Amount: &zero}, money.ErrNotPositive},
		"too much":     {captures.EditInput{Amount: &huge}, captures.ErrAmountTooLarge},
		"no merchant":  {captures.EditInput{Merchant: &blank}, captures.ErrMerchantRequired},
	} {
		if _, err := f.svc.Edit(ctx, f.user, mine.ID, tt.in); !errors.Is(err, tt.want) {
			t.Errorf("Edit(%s) = %v, want %v", name, err, tt.want)
		}
	}
	if _, err := f.svc.Edit(ctx, f.newUser(t, "b@example.com"), mine.ID, captures.EditInput{Merchant: &merchant}); !errors.Is(err, captures.ErrNotFound) {
		t.Errorf("someone else's Edit = %v, want ErrNotFound", err)
	}
}

func TestReviewListsWhatNeedsTheOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card, checking := f.account(t, f.user, "credit_card"), f.account(t, f.user, "checking")

	// Two equal coffees and one line: either could be it, so neither is
	// linked, and both wait with that line.
	twins := []uuid.UUID{f.add(t, card, day, 350, "Café").ID, f.add(t, card, day, 350, "Café").ID}
	coffee := f.line(t, card, day, -350, store.KindExpense, "CAFE")

	// A tap the statement passed without printing, with lines that come
	// close: the exact amount first, though booked late; then the line that
	// names the shop; then the closest amounts. Five at most.
	f.statement(t, checking, day.AddDays(12))
	declined := f.add(t, checking, day, 2000, "Café Unido")
	late := f.line(t, checking, day.AddDays(8), -2000, store.KindExpense, "POS 1234")
	named := f.line(t, checking, day.AddDays(1), -2500, store.KindExpense, "CAFE UNIDO PTY")
	o1 := f.line(t, checking, day, -2050, store.KindExpense, "OTRO")
	o2 := f.line(t, checking, day.AddDays(2), -2100, store.KindExpense, "OTRO")
	o3 := f.line(t, checking, day.AddDays(1), -2200, store.KindExpense, "OTRO")
	f.line(t, checking, day.AddDays(1), -1500, store.KindExpense, "OTRO") // close, but sixth
	// Never close: too far in amount or day, not an expense, rejected, or
	// another purchase's.
	f.line(t, checking, day, -2700, store.KindExpense, "LEJOS")
	f.line(t, checking, day.AddDays(11), -2000, store.KindExpense, "TARDE")
	f.line(t, checking, day.AddDays(-2), -2000, store.KindExpense, "ANTES")
	f.line(t, checking, day, 2000, store.KindIncome, "DEPOSITO")
	rejected := f.line(t, checking, day, -1990, store.KindExpense, "RECHAZADA")
	if _, err := f.svc.Reject(ctx, f.user, declined.ID, rejected.ID); err != nil {
		t.Fatal(err)
	}
	f.line(t, checking, day.AddDays(3), -1950, store.KindExpense, "TOMADA")
	if c := f.add(t, checking, day.AddDays(3), 1950, "Tomada"); c.State != store.CaptureLinked {
		t.Fatalf("fixture: %s", c.State)
	}

	// Waiting for its statement, not for the owner: a pending purchase, even
	// one a line comes close to. Settled: a discarded one.
	f.add(t, card, day.AddDays(1), 999, "Pendiente")
	f.line(t, card, day.AddDays(2), -1000, store.KindExpense, "PEND")
	gone := f.add(t, checking, day, 333, "No pasó")
	if _, err := f.svc.Discard(ctx, f.user, gone.ID); err != nil {
		t.Fatal(err)
	}
	// Another owner's never shows.
	other := f.newUser(t, "b@example.com")
	theirCard := f.account(t, other, "credit_card")
	f.line(t, theirCard, day, -350, store.KindExpense, "CAFE")
	f.line(t, theirCard, day, -350, store.KindExpense, "CAFE")
	theirs := f.add(t, theirCard, day, 350, "Café")

	review, err := f.svc.Review(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uuid.UUID]store.CaptureReview{}
	for _, r := range review {
		byID[r.Capture.ID] = r
	}
	if len(review) != 3 || len(byID) != 3 {
		t.Fatalf("Review lists %d purchases, want the two twins and the declined tap: %+v", len(review), review)
	}
	for _, id := range twins {
		r := byID[id]
		if r.Capture.State != store.CapturePrinted || len(r.Candidates) != 1 || r.Candidates[0].ID != coffee.ID || len(r.Near) != 0 {
			t.Errorf("twin = %+v", r)
		}
	}
	r := byID[declined.ID]
	want := []uuid.UUID{late.ID, named.ID, o1.ID, o2.ID, o3.ID}
	var near []uuid.UUID
	for _, l := range r.Near {
		near = append(near, l.ID)
	}
	if r.Capture.State != store.CaptureMissing || len(r.Candidates) != 0 || !equalIDs(near, want) {
		t.Errorf("declined tap: %s with candidates %v and near %v; want missing with near %v", r.Capture.State,
			r.Candidates, near, want)
	}
	if got, err := f.svc.Review(ctx, other); err != nil || len(got) != 1 || got[0].Capture.ID != theirs.ID ||
		len(got[0].Candidates) != 2 {
		t.Errorf("the other owner's review = %+v, %v; want only their own purchase", got, err)
	}
}

func TestARuleFilesOpenPurchasesButNeverTheOwnersChoice(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card")
	food, compras := f.category(t, f.user, "Alimentación"), f.category(t, f.user, "Compras")

	pending := f.add(t, card, day, 450, "Super Rey")
	chosen, err := f.svc.Add(ctx, f.user, captures.AddInput{AccountID: card.ID, Amount: 460, Merchant: "Super Rey",
		Day: day, CategoryID: &compras})
	if err != nil {
		t.Fatal(err)
	}
	printed := []uuid.UUID{f.add(t, card, day, 600, "Super Rey").ID, f.add(t, card, day, 600, "Super Rey").ID}
	f.line(t, card, day.AddDays(1), -600, store.KindExpense, "X")
	f.line(t, card, day, -800, store.KindExpense, "Y")
	linked := f.add(t, card, day, 800, "Super Rey")
	discarded := f.add(t, card, day, 900, "SUPER REY")
	if _, err := f.svc.Discard(ctx, f.user, discarded.ID); err != nil {
		t.Fatal(err)
	}
	other := f.newUser(t, "b@example.com")
	theirs, err := f.svc.Add(ctx, other, captures.AddInput{AccountID: f.account(t, other, "credit_card").ID, Amount: 450,
		Merchant: "Super Rey", Day: day})
	if err != nil {
		t.Fatal(err)
	}

	_, applied, err := rules.NewService(f.db, f.cats).Create(ctx, f.user, rules.Input{MatchText: "super rey",
		CategoryID: &food, ApplyToExisting: true})
	if err != nil {
		t.Fatal(err)
	}
	// One movement changed that the owner can see: the pending purchase. The
	// printed ones are filed too, for their line, but are not listed.
	if applied != 1 {
		t.Errorf("applied = %d, want 1", applied)
	}
	for id, want := range map[uuid.UUID]*uuid.UUID{pending.ID: &food, chosen.ID: &compras, printed[0]: &food, printed[1]: &food,
		linked.ID: nil, discarded.ID: nil} {
		c := f.get(t, f.user, id)
		if (want == nil) != (c.CategoryID == nil) || (want != nil && *c.CategoryID != *want) {
			t.Errorf("%s (%s) is filed under %v, want %v", c.Merchant, c.State, c.CategoryID, want)
		}
	}
	if c := f.get(t, other, theirs.ID); c.CategoryID != nil {
		t.Errorf("another owner's purchase was filed: %v", c.CategoryID)
	}
}

// A rule made from a purchase the phone noted, accents and all, files the
// bank's unaccented lines too, and the other way round.
func TestARuleAppliedToWhatIsStoredIgnoresAccents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card")
	food := f.category(t, f.user, "Alimentación")
	noted := f.add(t, card, day.AddDays(20), 450, "Café Unido")
	line := f.line(t, card, day, -375, store.KindExpense, "CAFE UNIDO MARBELLA PANAMA PA")

	_, applied, err := rules.NewService(f.db, f.cats).Create(ctx, f.user, rules.Input{MatchText: "café unido",
		CategoryID: &food, ApplyToExisting: true})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 2 {
		t.Errorf("applied = %d, want the line and the purchase", applied)
	}
	if c := f.get(t, f.user, noted.ID); c.CategoryID == nil || *c.CategoryID != food {
		t.Errorf("purchase filed under %v, want Alimentación", c.CategoryID)
	}
	got, err := f.db.Q().EntryByID(ctx, f.user, line.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CategoryID == nil || *got.CategoryID != food {
		t.Errorf("line filed under %v, want Alimentación", got.CategoryID)
	}
}

// Applying a rule files lines, then purchases; linking writes a purchase, then
// its line. Both wait for the user's lock, so one never deadlocks the other.
func TestApplyingARuleWaitsForTheUsersLock(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	salud := f.category(t, f.user, "Salud")
	svc := rules.NewService(f.db, f.cats)
	rule, _, err := svc.Create(ctx, f.user, rules.Input{MatchText: "farmacia", CategoryID: &salud})
	if err != nil {
		t.Fatal(err)
	}
	for name, apply := range map[string]func() error{
		"Create": func() error {
			_, _, err := svc.Create(ctx, f.user, rules.Input{MatchText: "arrocha", CategoryID: &salud, ApplyToExisting: true})
			return err
		},
		"Update": func() error {
			_, _, err := svc.Update(ctx, f.user, rule.ID, rules.Input{MatchText: "farmacia", CategoryID: &salud, ApplyToExisting: true})
			return err
		},
	} {
		locked, release, held := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			held <- f.db.InTx(ctx, func(q *store.Queries) error {
				if err := q.LockUser(ctx, f.user); err != nil {
					return err
				}
				close(locked)
				<-release
				return nil
			})
		}()
		<-locked
		applied := make(chan error, 1)
		go func() { applied <- apply() }()
		waited := true
		select {
		case err := <-applied:
			waited = false
			t.Errorf("%s applied the rule while a link held the user's lock: %v", name, err)
		case <-time.After(200 * time.Millisecond):
		}
		close(release)
		if err := <-held; err != nil {
			t.Fatal(err)
		}
		if waited {
			if err := <-applied; err != nil {
				t.Errorf("%s after the lock: %v", name, err)
			}
		}
	}
}

func equalIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- fixtures ----------------------------------------------------------------

type fixture struct {
	db   *store.DB
	svc  *captures.Service
	cats *categories.Service
	user uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := storetest.New(t)
	cats := categories.NewService(db)
	f := fixture{db: db, svc: captures.NewService(db, cats), cats: cats}
	f.user = f.newUser(t, "o@example.com")
	return f
}

// newUser registers someone with the default categories.
func (f fixture) newUser(t *testing.T, email string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	u, err := f.db.Q().CreateUser(ctx, store.User{ID: uuid.New(), Email: email, PasswordHash: "x", FullName: email})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.InTx(ctx, func(q *store.Queries) error { return f.cats.SeedDefaults(ctx, q, u.ID) }); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func (f fixture) account(t *testing.T, user uuid.UUID, typ string) store.Account {
	t.Helper()
	a := store.Account{ID: uuid.New(), UserID: user, Class: "asset", Type: typ, Institution: "bac",
		ExternalNumber: uuid.NewString(), Currency: "USD"}
	switch typ {
	case "credit_card":
		a.Class, a.Institution = "liability", "banco_general"
	case "brokerage":
		a.Institution = "ibkr"
	}
	got, _, err := f.db.Q().EnsureAccount(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (f fixture) category(t *testing.T, user uuid.UUID, name string) uuid.UUID {
	t.Helper()
	c, found, err := f.cats.FindByName(context.Background(), user, name)
	if err != nil || !found {
		t.Fatalf("FindByName(%s) = %v, %v", name, found, err)
	}
	return c.ID
}

func (f fixture) rule(t *testing.T, text string, category uuid.UUID) {
	t.Helper()
	if _, err := f.db.Q().CreateCategoryRuleIfNew(context.Background(), store.CategoryRule{ID: uuid.New(),
		UserID: f.user, MatchText: text, CategoryID: &category}); err != nil {
		t.Fatal(err)
	}
}

// line stores a line a statement printed.
func (f fixture) line(t *testing.T, a store.Account, on civil.Date, amount money.Cents, kind, description string) store.Entry {
	t.Helper()
	e := store.Entry{ID: uuid.New(), UserID: a.UserID, AccountID: a.ID, BookedOn: on, Amount: amount, Kind: kind,
		Description: description, DedupKey: uuid.NewString()}
	if ok, err := f.db.Q().InsertEntryIfNew(context.Background(), e); err != nil || !ok {
		t.Fatalf("InsertEntryIfNew = %v, %v", ok, err)
	}
	return e
}

// statement records a statement of the account that ends on end.
func (f fixture) statement(t *testing.T, a store.Account, end civil.Date) {
	t.Helper()
	ctx := context.Background()
	run := store.ImportRun{ID: uuid.New(), UserID: a.UserID, AccountID: &a.ID, Source: "bg_card", Status: "ok"}
	if err := f.db.Q().CreateImportRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Q().CreateStatement(ctx, store.Statement{ID: uuid.New(), UserID: a.UserID, AccountID: a.ID,
		RunID: run.ID, PeriodStart: end.AddDays(-30), PeriodEnd: end}); err != nil {
		t.Fatal(err)
	}
}

// add notes a purchase by hand, as the account's owner.
func (f fixture) add(t *testing.T, a store.Account, on civil.Date, amount money.Cents, merchant string) captures.Capture {
	t.Helper()
	c, err := f.svc.Add(context.Background(), a.UserID, captures.AddInput{AccountID: a.ID, Amount: amount,
		Merchant: merchant, Day: on})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return c
}

func (f fixture) get(t *testing.T, user, id uuid.UUID) store.Capture {
	t.Helper()
	c, err := f.db.Q().CaptureByID(context.Background(), user, id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f fixture) lineByID(t *testing.T, id uuid.UUID) store.Entry {
	t.Helper()
	e, err := f.db.Q().EntryByID(context.Background(), f.user, id)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (f fixture) state(t *testing.T, id uuid.UUID) string {
	t.Helper()
	return f.get(t, f.user, id).State
}

// spent is everything the owner has spent, as Gastos totals it.
func (f fixture) spent(t *testing.T) money.Cents {
	t.Helper()
	totals, err := reports.NewService(f.db).Spending(context.Background(), f.user, civil.Date{}, civil.Date{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var spent money.Cents
	for _, total := range totals {
		spent += total.Total
	}
	return spent
}

func today() civil.Date { return civil.Of(time.Now().In(civil.Panama)) }
