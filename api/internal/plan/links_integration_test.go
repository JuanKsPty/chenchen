package plan_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/movements"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/reports"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

var (
	aug26 = plan.Month{Year: 2026, Month: time.August}
	sep26 = plan.Month{Year: 2026, Month: time.September}
)

// says records what the owner says about month m of an item.
func (f fixture) says(t *testing.T, item uuid.UUID, m plan.Month, movement uuid.UUID, decision plan.Decision) store.PlanLink {
	t.Helper()
	link, created, err := f.svc.Link(context.Background(), f.user,
		plan.LinkInput{ID: uuid.New(), ItemID: item, Month: m, MovementID: movement, Decision: decision})
	if err != nil || !created {
		t.Fatalf("Link(%s %s %s) = %v, created %v", decision, m, movement, err, created)
	}
	return link
}

// fixedLine is month m's line of a payment.
func fixedLine(t *testing.T, p plan.Plan, item uuid.UUID) plan.Line {
	t.Helper()
	for _, l := range p.Fixed {
		if l.Item.ID == item {
			return l
		}
	}
	t.Fatalf("%s has no line for %s: %+v", p.Month, item, p.Fixed)
	return plan.Line{}
}

// paidBy reports whether line was paid by exactly these movements.
func paidBy(l plan.Line, ids ...uuid.UUID) bool {
	if len(l.Movements) != len(ids) {
		return false
	}
	for i, mv := range l.Movements {
		if mv.ID != ids[i] {
			return false
		}
	}
	return true
}

// sameSummary reports whether two months' accounts say the same.
func sameSummary(a, b plan.Summary) bool {
	perDay := (a.PerDay == nil) == (b.PerDay == nil) && (a.PerDay == nil || *a.PerDay == *b.PerDay)
	a.PerDay, b.PerDay = nil, nil
	return perDay && a == b
}

// countsOnce checks the month spends exactly what its movements add up to,
// each of them once: what was free plus what items took.
func countsOnce(t *testing.T, p plan.Plan, spending, charged money.Cents) {
	t.Helper()
	wantCents(t, "the spending", p.Spending, spending)
	wantCents(t, "what items took", p.Charged, charged)
	wantCents(t, "what went out of what is free", p.Summary.Spent, spending-charged)
}

func TestAMovementBelongsToOnePlannedItemAtATime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	person, gift := uuid.New(), uuid.New()
	f.save(t, person, payment(nov26, 3267, 5))
	f.save(t, gift, named(payment(nov26, 3000, 5), "Regalo", "REGALO"))
	paid := f.line(t, checking, on(nov26, 5), -3267, store.KindExpense, "YAPPY A PERSONA UNO")

	p := f.month(t, nov26)
	if l := fixedLine(t, p, person); !paidBy(l, paid) || l.Movements[0].Link != uuid.Nil {
		t.Fatalf("the payment to the person = %+v, want it recognised by its text", l)
	}

	// The owner says it was the gift: it pays the gift alone, and the payment
	// to the person keeps its money aside until it goes out.
	link := f.says(t, gift, nov26, paid, plan.DecisionAssign)
	p = f.month(t, nov26)
	if l := fixedLine(t, p, gift); !paidBy(l, paid) || l.Movements[0].Link != link.ID ||
		l.Status != plan.StatusOut || *l.Paid != 3267 {
		t.Errorf("the gift = %+v, want it paid by the movement the owner named", l)
	}
	if l := fixedLine(t, p, person); len(l.Movements) != 0 || l.Status != plan.StatusLate || l.Committed != 3267 {
		t.Errorf("the payment to the person = %+v, want it still to go out", l)
	}
	countsOnce(t, p, 3267, 3267)
	wantCents(t, "the payments", p.Summary.Fixed, 3267+3267)

	// It cannot pay the person as well, in this month or another, until the
	// owner takes back what they said.
	for _, m := range []plan.Month{nov26, dec26} {
		_, _, err := f.svc.Link(ctx, f.user,
			plan.LinkInput{ID: uuid.New(), ItemID: person, Month: m, MovementID: paid, Decision: plan.DecisionAssign})
		if !errors.Is(err, plan.ErrMovementInPlan) {
			t.Errorf("saying it also paid the person in %s = %v, want ErrMovementInPlan", m, err)
		}
	}
	if err := f.svc.Unlink(ctx, f.user, link.ID); err != nil {
		t.Fatal(err)
	}
	p = f.month(t, nov26)
	if l := fixedLine(t, p, person); !paidBy(l, paid) {
		t.Errorf("after undoing, the payment to the person = %+v, want it recognised again", l)
	}
	if l := fixedLine(t, p, gift); len(l.Movements) != 0 {
		t.Errorf("after undoing, the gift = %+v", l)
	}
	if err := f.svc.Unlink(ctx, f.user, link.ID); !errors.Is(err, plan.ErrLinkNotFound) {
		t.Errorf("undoing twice = %v, want ErrLinkNotFound", err)
	}
}

func TestAPendingPurchaseCountsUntilItsLineArrivesThenTheLineDoes(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	gym := uuid.New()
	f.save(t, gym, named(payment(nov26, 2500, 3), "Gimnasio", ""))
	noted := f.noted(t, card, on(nov26, 3), 2500, "Gimnasio")

	// With no text to look for, the plan only offers it.
	l := fixedLine(t, f.month(t, nov26), gym)
	if len(l.Movements) != 0 || len(l.Candidates) != 1 || l.Candidates[0].ID != noted {
		t.Fatalf("the gym = %+v, want the noted purchase offered", l)
	}

	link := f.says(t, gym, nov26, noted, plan.DecisionAssign)
	p := f.month(t, nov26)
	if l := fixedLine(t, p, gym); !paidBy(l, noted) || l.Movements[0].Origin != store.OriginCapture ||
		l.Movements[0].Link != link.ID || l.Status != plan.StatusOut || len(l.Candidates) != 0 {
		t.Fatalf("the gym = %+v, want it paid by the noted purchase", l)
	}
	countsOnce(t, p, 2500, 2500)

	// The statement prints the purchase: its line is the movement now, and it
	// pays the gym in its place, once.
	line := f.line(t, card, on(nov26, 4), -2500, store.KindExpense, "POS 0001 COMERCIO")
	p = f.month(t, nov26)
	if l := fixedLine(t, p, gym); !paidBy(l, line) || l.Movements[0].Origin != store.OriginStatement ||
		l.Movements[0].Link != link.ID || *l.Paid != 2500 {
		t.Errorf("the gym = %+v, want it paid by the line", l)
	}
	countsOnce(t, p, 2500, 2500)
}

func TestAnAssignedCaptureFollowsItsLineWhenPrinted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	gym := uuid.New()
	f.save(t, gym, named(payment(nov26, 2500, 3), "Gimnasio", "GIMNASIO"))
	noted := f.noted(t, card, on(nov26, 3), 2500, "Gimnasio")
	link := f.says(t, gym, nov26, noted, plan.DecisionAssign)

	// Its line has none of the item's text, and is linked to the purchase.
	line := f.line(t, card, on(nov26, 5), -2500, store.KindExpense, "POS 0001 COMERCIO")
	if err := f.db.Q().LinkCapture(ctx, f.user, noted, line, store.LinkedByUser); err != nil {
		t.Fatal(err)
	}
	p := f.month(t, nov26)
	if l := fixedLine(t, p, gym); !paidBy(l, line) || l.Movements[0].Link != link.ID {
		t.Errorf("the gym = %+v, want it paid by the purchase's line", l)
	}
	countsOnce(t, p, 2500, 2500)

	// The owner says the line is not that purchase: the purchase counts again
	// on its own, and still pays the gym; the line is spending of its own.
	if err := f.db.Q().UnlinkCapture(ctx, f.user, noted); err != nil {
		t.Fatal(err)
	}
	p = f.month(t, nov26)
	if l := fixedLine(t, p, gym); !paidBy(l, noted) {
		t.Errorf("the gym = %+v, want it paid by the purchase again", l)
	}
	countsOnce(t, p, 5000, 2500)
	// Following the purchase never rewrote what the owner said.
	if n := storetest.Count(t, f.db, "plan_links WHERE capture_id = $1", noted); n != 1 {
		t.Errorf("%d links name the purchase, want the one said", n)
	}
}

func TestAnAmbiguousPrintedCaptureStillPaysItsItemOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	gym, pool := uuid.New(), uuid.New()
	f.save(t, gym, named(payment(nov26, 2500, 3), "Gimnasio", ""))
	f.save(t, pool, named(payment(nov26, 2500, 3), "Piscina", ""))
	f.says(t, gym, nov26, f.noted(t, card, on(nov26, 3), 2500, "Gimnasio"), plan.DecisionAssign)
	poolLink := f.says(t, pool, nov26, f.noted(t, card, on(nov26, 3), 2500, "Piscina"), plan.DecisionAssign)
	// Two lines of the same amount: either purchase could be either line, so
	// neither is linked, and both purchases are printed.
	first := f.line(t, card, on(nov26, 4), -2500, store.KindExpense, "POS 0001 COMERCIO")
	second := f.line(t, card, on(nov26, 4), -2500, store.KindExpense, "POS 0001 COMERCIO")
	if n := storetest.Count(t, f.db, "capture_states WHERE state = 'printed'"); n != 2 {
		t.Fatalf("%d purchases are printed, want both", n)
	}

	p := f.month(t, nov26)
	g, w := fixedLine(t, p, gym), fixedLine(t, p, pool)
	if len(g.Movements) != 1 || len(w.Movements) != 1 || g.Movements[0].ID == w.Movements[0].ID ||
		*g.Paid != 2500 || *w.Paid != 2500 {
		t.Fatalf("the gym = %+v and the pool = %+v, want each paid by a line of its own", g, w)
	}
	countsOnce(t, p, 5000, 5000)

	// With only the gym said, it takes one line, once; the other is spending.
	if err := f.svc.Unlink(ctx, f.user, poolLink.ID); err != nil {
		t.Fatal(err)
	}
	p = f.month(t, nov26)
	if g := fixedLine(t, p, gym); len(g.Movements) != 1 || (g.Movements[0].ID != first && g.Movements[0].ID != second) ||
		*g.Paid != 2500 {
		t.Errorf("the gym = %+v, want it paid by one of the lines", g)
	}
	countsOnce(t, p, 5000, 2500)
}

// countedOnce checks that, over months, what went out of what was free plus
// what each of their payments was charged is what Gastos spent in them:
// every outflow counted once, in one month. It holds when each movement that
// paid one of the months is booked in one of them.
func (f fixture) countedOnce(t *testing.T, months ...plan.Month) {
	t.Helper()
	var counted, gastos money.Cents
	for _, m := range months {
		p := f.month(t, m)
		counted += p.Summary.Spent
		for _, l := range p.Fixed {
			if l.Paid != nil {
				counted += *l.Paid
			}
		}
		totals, err := reports.NewService(f.db).Spending(context.Background(), f.user, m.First(), m.Last(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range totals {
			gastos += c.Total
		}
	}
	wantCents(t, fmt.Sprintf("what the plan counted from %s to %s", months[0], months[len(months)-1]), counted, gastos)
}

func TestAPurchaseThatEndsUpFarFromTheMonthItPaidCountsOnceInItsOwnMonth(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.now(time.Date(2026, time.September, 10, 9, 0, 0, 0, civil.Panama))
	gym := uuid.New()
	f.save(t, gym, named(payment(sep26, 2500, 3), "Gimnasio", ""))
	f.save(t, uuid.New(), paycheck(sep26, 100000, 1))
	f.now(novNow)

	// Noted on the last day of October and said to be September's gym: a
	// month after it, which is near enough.
	noted := f.noted(t, card, on(oct26, 31), 2500, "Gimnasio")
	f.says(t, gym, sep26, noted, plan.DecisionAssign)
	if l := fixedLine(t, f.month(t, sep26), gym); !paidBy(l, noted) {
		t.Fatalf("September's gym = %+v, want it paid by the purchase", l)
	}
	f.countedOnce(t, sep26, oct26, nov26)

	// Its line prints on the 3rd of November, two months from September and
	// too far to pay it. November never reads what was said about
	// September, so the line is November's spending, and September keeps the
	// gym's money aside again, as for a purchase that did not happen.
	line := f.line(t, card, on(nov26, 3), -2500, store.KindExpense, "POS 0001 COMERCIO")
	if err := f.db.Q().LinkCapture(ctx, f.user, noted, line, store.LinkedByAuto); err != nil {
		t.Fatal(err)
	}
	if l := fixedLine(t, f.month(t, sep26), gym); len(l.Movements) != 0 || l.Paid != nil || l.Committed != 2500 {
		t.Errorf("September's gym = %+v, want its money aside", l)
	}
	countsOnce(t, f.month(t, nov26), 2500, 0)
	f.countedOnce(t, sep26, oct26, nov26)

	// Noted for November's gym, then its day corrected to September: the
	// same, the other way round.
	moved := f.noted(t, card, on(nov26, 20), 2500, "Gimnasio")
	f.says(t, gym, nov26, moved, plan.DecisionAssign)
	if l := fixedLine(t, f.month(t, nov26), gym); !paidBy(l, moved) {
		t.Fatalf("November's gym = %+v, want it paid by the purchase", l)
	}
	if err := f.db.Q().UpdateCapture(ctx, f.user, moved, -2500, "Gimnasio", on(sep26, 20)); err != nil {
		t.Fatal(err)
	}
	if l := fixedLine(t, f.month(t, nov26), gym); len(l.Movements) != 0 || l.Paid != nil || l.Committed != 2500 {
		t.Errorf("November's gym = %+v, want its money aside", l)
	}
	countsOnce(t, f.month(t, sep26), 2500, 0)
	f.countedOnce(t, sep26, oct26, nov26)
}

func TestTheNewerWordHoldsWhenALineIsThePurchaseTheOwnerNamed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	gym, pool := uuid.New(), uuid.New()
	f.save(t, gym, named(payment(nov26, 2500, 3), "Gimnasio", ""))
	f.save(t, pool, named(payment(nov26, 4000, 10), "Piscina", ""))
	reject := func(item, movement uuid.UUID) *httptest.ResponseRecorder {
		return f.serve(t, f.user, "POST", "/links", fmt.Sprintf(
			`{"id": %q, "item_id": %q, "month": "2026-11", "movement_id": %q, "decision": "reject"}`, uuid.New(), item, movement))
	}

	// The gym was the purchase the owner noted, which turned out to be this
	// line: the plan shows the line.
	noted := f.noted(t, card, on(nov26, 3), 2500, "Gimnasio")
	f.says(t, gym, nov26, noted, plan.DecisionAssign)
	line := f.line(t, card, on(nov26, 5), -2500, store.KindExpense, "POS 0001 COMERCIO")
	if err := f.db.Q().LinkCapture(ctx, f.user, noted, line, store.LinkedByUser); err != nil {
		t.Fatal(err)
	}
	if l := fixedLine(t, f.month(t, nov26), gym); !paidBy(l, line) {
		t.Fatalf("the gym = %+v, want it paid by the purchase's line", l)
	}
	// "Este no" on the line it shows replaces what they said of the purchase.
	if rec := reject(gym, line); rec.Code != http.StatusCreated {
		t.Fatalf("POST reject = %d %s", rec.Code, rec.Body)
	}
	p := f.month(t, nov26)
	if l := fixedLine(t, p, gym); len(l.Movements) != 0 || l.Committed != 2500 || len(l.Candidates) != 0 {
		t.Errorf("the gym = %+v, want it still to go out, with the line not offered", l)
	}
	countsOnce(t, p, 2500, 0)
	if n := storetest.Count(t, f.db, "plan_links WHERE item_id = $1", gym); n != 1 {
		t.Errorf("%d links on the gym, want only the newer word", n)
	}

	// A purchase noted without the tip is linked to its line only after the
	// owner refused the line for the pool. Both words are about one movement
	// now, and the newer one holds.
	swim := f.noted(t, card, on(nov26, 10), 4000, "Piscina")
	f.says(t, pool, nov26, swim, plan.DecisionAssign)
	tipped := f.line(t, card, on(nov26, 11), -4400, store.KindExpense, "POS 0002 COMERCIO")
	if rec := reject(pool, tipped); rec.Code != http.StatusCreated {
		t.Fatalf("POST reject = %d %s", rec.Code, rec.Body)
	}
	if err := f.db.Q().LinkCapture(ctx, f.user, swim, tipped, store.LinkedByUser); err != nil {
		t.Fatal(err)
	}
	p = f.month(t, nov26)
	if l := fixedLine(t, p, pool); len(l.Movements) != 0 || l.Committed != 4000 {
		t.Errorf("the pool = %+v, want the line refused, as the owner said last", l)
	}
	countsOnce(t, p, 2500+4400, 0)

	// And saying it was the line after all brings back what they said of
	// the purchase, which is that line.
	again, created, err := f.svc.Link(ctx, f.user,
		plan.LinkInput{ID: uuid.New(), ItemID: pool, Month: nov26, MovementID: tipped, Decision: plan.DecisionAssign})
	if err != nil || created || again.CaptureID == nil || *again.CaptureID != swim {
		t.Fatalf("saying the line paid the pool = %+v, created %v, %v; want what was said of the purchase", again, created, err)
	}
	p = f.month(t, nov26)
	if l := fixedLine(t, p, pool); !paidBy(l, tipped) || *l.Paid != 4400 {
		t.Errorf("the pool = %+v, want it paid by the line", l)
	}
	countsOnce(t, p, 2500+4400, 4400)
	if n := storetest.Count(t, f.db, "plan_links WHERE item_id = $1", pool); n != 1 {
		t.Errorf("%d links on the pool, want only the newer word", n)
	}
}

func TestSayingWhatThePlanShowsAgainSaysItOnce(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	gym, pool := uuid.New(), uuid.New()
	f.save(t, gym, named(payment(nov26, 2500, 3), "Gimnasio", ""))
	f.save(t, pool, named(payment(nov26, 3000, 10), "Piscina", ""))
	assign := func(item, movement uuid.UUID) (*httptest.ResponseRecorder, map[string]any) {
		rec := f.serve(t, f.user, "POST", "/links", fmt.Sprintf(
			`{"id": %q, "item_id": %q, "month": "2026-11", "movement_id": %q, "decision": "assign"}`, uuid.New(), item, movement))
		var got map[string]any
		decode(t, rec, &got)
		return rec, got
	}

	// The gym was a noted purchase that either of two lines could be, and
	// the plan shows one of them.
	link := f.says(t, gym, nov26, f.noted(t, card, on(nov26, 3), 2500, "Gimnasio"), plan.DecisionAssign)
	lines := []uuid.UUID{
		f.line(t, card, on(nov26, 4), -2500, store.KindExpense, "POS 0001 COMERCIO"),
		f.line(t, card, on(nov26, 4), -2500, store.KindExpense, "POS 0001 COMERCIO"),
	}
	l := fixedLine(t, f.month(t, nov26), gym)
	if len(l.Movements) != 1 || l.Movements[0].Link != link.ID {
		t.Fatalf("the gym = %+v, want it paid by one of the lines", l)
	}
	// Saying that line paid it, or the other one the purchase could be, is
	// what the owner said already.
	for _, mv := range []uuid.UUID{l.Movements[0].ID, lines[0], lines[1]} {
		if rec, got := assign(gym, mv); rec.Code != http.StatusOK || got["id"] != link.ID.String() {
			t.Errorf("POST assign %s = %d %s, want the link said", mv, rec.Code, rec.Body)
		}
	}
	p := f.month(t, nov26)
	if l := fixedLine(t, p, gym); len(l.Movements) != 1 || *l.Paid != 2500 {
		t.Errorf("the gym = %+v, want it paid once", l)
	}
	countsOnce(t, p, 5000, 2500)
	if n := storetest.Count(t, f.db, "plan_links"); n != 1 {
		t.Errorf("%d links, want the one said", n)
	}

	// The owner said which line paid the pool, and then that their noted
	// purchase, which either line could be, did: it is that line, and the
	// pool is paid once.
	ctx := context.Background()
	first := f.line(t, card, on(nov26, 11), -3000, store.KindExpense, "POS 0002 COMERCIO")
	second := f.line(t, card, on(nov26, 11), -3000, store.KindExpense, "POS 0002 COMERCIO")
	for _, which := range []uuid.UUID{first, second} {
		said := f.says(t, pool, nov26, which, plan.DecisionAssign)
		swim := f.noted(t, card, on(nov26, 10), 3000, "Piscina")
		rec, got := assign(pool, swim)
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST assign the purchase = %d %s", rec.Code, rec.Body)
		}
		p = f.month(t, nov26)
		if l := fixedLine(t, p, pool); !paidBy(l, which) || l.Movements[0].Link != said.ID || *l.Paid != 3000 {
			t.Errorf("the pool = %+v, want it paid by the line named, once", l)
		}
		countsOnce(t, p, 5000+6000, 2500+3000)

		// Start again with the other line.
		for _, id := range []uuid.UUID{said.ID, uuid.MustParse(got["id"].(string))} {
			if err := f.svc.Unlink(ctx, f.user, id); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.db.Q().DiscardCapture(ctx, f.user, swim); err != nil {
			t.Fatal(err)
		}
	}
}

func TestADiscardedCaptureGivesTheReserveBackAndARestoredOneTakesItAgain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	gym := uuid.New()
	f.save(t, gym, named(payment(nov26, 2500, 3), "Gimnasio", ""))
	noted := f.noted(t, card, on(nov26, 3), 2600, "Gimnasio")
	f.says(t, gym, nov26, noted, plan.DecisionAssign)
	p := f.month(t, nov26)
	if l := fixedLine(t, p, gym); !paidBy(l, noted) || l.Committed != 2600 {
		t.Fatalf("the gym = %+v", l)
	}
	countsOnce(t, p, 2600, 2600)
	wantCents(t, "what is left", p.Summary.Left, 100000-2600)

	// It did not happen: the gym has not gone out, and keeps what was planned
	// aside again.
	if err := f.db.Q().DiscardCapture(ctx, f.user, noted); err != nil {
		t.Fatal(err)
	}
	p = f.month(t, nov26)
	if l := fixedLine(t, p, gym); len(l.Movements) != 0 || l.Paid != nil || l.Committed != 2500 || l.Status != plan.StatusLate {
		t.Errorf("after discarding, the gym = %+v, want it still to go out", l)
	}
	countsOnce(t, p, 0, 0)
	wantCents(t, "what is left", p.Summary.Left, 100000-2500)

	// It did: the link was never forgotten.
	if err := f.db.Q().RestoreCapture(ctx, f.user, noted); err != nil {
		t.Fatal(err)
	}
	p = f.month(t, nov26)
	if l := fixedLine(t, p, gym); !paidBy(l, noted) || l.Committed != 2600 || l.Status != plan.StatusOut {
		t.Errorf("after restoring, the gym = %+v, want it paid by the purchase", l)
	}
	countsOnce(t, p, 2600, 2600)
}

func TestAPaymentMarkedTransferIsOfferedAsAHintAndCountsOnceItIsNot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	rent := uuid.New()
	f.save(t, rent, named(payment(nov26, 45500, 2), "Alquiler", "TEF A PERSONA DOS"))
	// The bank printed the rent as a transfer to another person's account. A
	// transfer between the owner's own accounts with other text is no hint.
	tef := f.line(t, checking, on(nov26, 2), -45500, store.KindTransfer, "TEF A PERSONA DOS")
	f.line(t, checking, on(nov26, 3), -20000, store.KindTransfer, "TRASPASO A AHORRO")

	p := f.month(t, nov26)
	l := fixedLine(t, p, rent)
	if l.Status != plan.StatusLate || len(l.Movements) != 0 || l.Hint == nil || l.Hint.Movement.ID != tef ||
		l.Hint.Reason != plan.HintTransfer || len(l.Candidates) != 0 {
		t.Fatalf("the rent = %+v, hint %+v; want the transfer pointed at", l, l.Hint)
	}
	countsOnce(t, p, 0, 0)
	left := p.Summary.Left
	var got struct {
		Fixed []struct {
			Hint *struct {
				Movement struct {
					ID  string `json:"id"`
					How string `json:"how"`
				} `json:"movement"`
				Reason string `json:"reason"`
			} `json:"hint"`
			Candidates []any `json:"candidates"`
		} `json:"fixed"`
	}
	decode(t, f.serve(t, f.user, "GET", "/", ""), &got)
	if len(got.Fixed) != 1 || got.Fixed[0].Hint == nil || got.Fixed[0].Hint.Movement.ID != tef.String() ||
		got.Fixed[0].Hint.Reason != "transfer" || got.Fixed[0].Hint.Movement.How != "" || got.Fixed[0].Candidates == nil {
		t.Errorf("GET /api/plan fixed = %+v", got.Fixed)
	}

	// The owner says it is not a transfer, as anywhere else in the app: it is
	// the rent, and counts once, in the rent.
	if _, err := movements.NewService(f.db, categories.NewService(f.db)).SetTransfer(ctx, f.user, tef, false); err != nil {
		t.Fatal(err)
	}
	p = f.month(t, nov26)
	if l := fixedLine(t, p, rent); !paidBy(l, tef) || l.Hint != nil || *l.Paid != 45500 || l.Status != plan.StatusOut {
		t.Errorf("the rent = %+v, want it paid by the line", l)
	}
	countsOnce(t, p, 45500, 45500)
	wantCents(t, "what is left", p.Summary.Left, left)
}

func TestALatePaymentAssignedToLastMonthIsChargedToLastMonth(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.now(time.Date(2026, time.October, 10, 9, 0, 0, 0, civil.Panama))
	rent := uuid.New()
	f.save(t, rent, named(payment(oct26, 40000, 30), "Alquiler", "ALQUILER"))
	f.save(t, uuid.New(), paycheck(oct26, 130000, 15))
	f.now(novNow)
	late := f.line(t, checking, on(nov26, 2), -40000, store.KindExpense, "ALQUILER")
	f.line(t, checking, on(nov26, 3), -1500, store.KindExpense, "SUPER X")

	// Booked in November, the plan sees November's rent in it.
	if l := fixedLine(t, f.month(t, nov26), rent); !paidBy(l, late) {
		t.Fatalf("November's rent = %+v", l)
	}
	// It was October's, paid two days late: October has passed, and the
	// owner can still say so.
	link := f.says(t, rent, oct26, late, plan.DecisionAssign)

	oct := f.month(t, oct26)
	if l := fixedLine(t, oct, rent); !paidBy(l, late) || l.Movements[0].Link != link.ID || l.Status != plan.StatusOut ||
		*l.Paid != 40000 {
		t.Errorf("October's rent = %+v, want it paid by the 2nd of November", l)
	}
	wantCents(t, "October's payments", oct.Summary.Fixed, 40000)
	countsOnce(t, oct, 0, 0)

	// November spent it, as Gastos says, but charged it to October: what was
	// free in November is untouched, and November's rent is still to go.
	nov := f.month(t, nov26)
	if l := fixedLine(t, nov, rent); len(l.Movements) != 0 || l.Status != plan.StatusUpcoming || l.Committed != 40000 {
		t.Errorf("November's rent = %+v, want it still to go out", l)
	}
	countsOnce(t, nov, 41500, 40000)
	wantCents(t, "what went out of what is free", nov.Summary.Spent, 1500)
}

func TestALinkBeyondTheNeighbouringMonthsIsRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	checking := f.account(t, f.user, "checking", "0001")
	f.now(time.Date(2026, time.August, 10, 9, 0, 0, 0, civil.Panama))
	rent := uuid.New()
	f.save(t, rent, named(payment(aug26, 40000, 30), "Alquiler", "ALQUILER"))
	f.now(novNow)
	mv := f.line(t, checking, on(nov26, 2), -40000, store.KindExpense, "ALQUILER")

	for _, m := range []plan.Month{aug26, sep26, jan27} {
		for _, decision := range []plan.Decision{plan.DecisionAssign, plan.DecisionReject} {
			_, _, err := f.svc.Link(ctx, f.user, plan.LinkInput{ItemID: rent, Month: m, MovementID: mv, Decision: decision})
			if !errors.Is(err, plan.ErrMovementTooFar) {
				t.Errorf("%s for %s = %v, want ErrMovementTooFar", decision, m, err)
			}
		}
	}
	far := plan.Month{Year: 2028, Month: time.December}
	if _, _, err := f.svc.Link(ctx, f.user, plan.LinkInput{ItemID: rent, Month: far, Decision: plan.DecisionPaid}); !errors.Is(err, plan.ErrMonthTooFar) {
		t.Errorf("paid in %s = %v, want ErrMonthTooFar", far, err)
	}
	rec := f.serve(t, f.user, "POST", "/links", fmt.Sprintf(
		`{"id": %q, "item_id": %q, "month": "2026-09", "movement_id": %q, "decision": "assign"}`, uuid.New(), rent, mv))
	if e := failure(t, rec); rec.Code != http.StatusUnprocessableEntity || e.Error.Code != "movement_too_far" ||
		e.Error.Message != "Elige un movimiento de ese mes o de uno de al lado." {
		t.Errorf("POST for September = %d %s", rec.Code, rec.Body)
	}
	if n := storetest.Count(t, f.db, "plan_links"); n != 0 {
		t.Fatalf("refused links wrote %d", n)
	}

	// The month before and the month after are near enough.
	for _, m := range []plan.Month{oct26, nov26, dec26} {
		link := f.says(t, rent, m, mv, plan.DecisionReject)
		if err := f.svc.Unlink(ctx, f.user, link.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPaidOutsideChenchenCountsOnceThroughThePlan(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	school := uuid.New()
	f.save(t, school, named(payment(nov26, 30000, 3), "Colegio", "COLEGIO X"))
	f.line(t, checking, on(nov26, 4), -29000, store.KindExpense, "LIBRERIA")

	// It went out from an account the app does not hold.
	link := f.says(t, school, nov26, uuid.Nil, plan.DecisionPaid)
	p := f.month(t, nov26)
	l := fixedLine(t, p, school)
	if l.Status != plan.StatusOut || l.MarkedPaid != link.ID || l.Paid != nil || l.Committed != 30000 ||
		len(l.Movements) != 0 || len(l.Candidates) != 0 || l.Hint != nil {
		t.Fatalf("the school = %+v, want it paid where the app cannot see", l)
	}
	wantCents(t, "the payments", p.Summary.Fixed, 30000)
	countsOnce(t, p, 29000, 0)
	wantCents(t, "what is left", p.Summary.Left, 100000-30000-29000)
	var got struct {
		Fixed []struct {
			Status     string  `json:"status"`
			MarkedPaid bool    `json:"marked_paid"`
			PaidLinkID string  `json:"paid_link_id"`
			Paid       *string `json:"paid"`
		} `json:"fixed"`
	}
	decode(t, f.serve(t, f.user, "GET", "/", ""), &got)
	if len(got.Fixed) != 1 || got.Fixed[0].Status != "out" || !got.Fixed[0].MarkedPaid ||
		got.Fixed[0].PaidLinkID != link.ID.String() || got.Fixed[0].Paid != nil {
		t.Errorf("GET /api/plan fixed = %+v", got.Fixed)
	}

	// Saying it twice is saying it once.
	again, created, err := f.svc.Link(context.Background(), f.user,
		plan.LinkInput{ID: uuid.New(), ItemID: school, Month: nov26, Decision: plan.DecisionPaid})
	if err != nil || created || again.ID != link.ID {
		t.Errorf("saying it again = %+v, created %v, %v", again, created, err)
	}

	// Once a movement says what it cost, it counts through that, still once.
	paid := f.line(t, checking, on(nov26, 20), -31000, store.KindExpense, "COLEGIO X")
	p = f.month(t, nov26)
	if l := fixedLine(t, p, school); !paidBy(l, paid) || *l.Paid != 31000 || l.Committed != 31000 {
		t.Errorf("the school = %+v, want it paid by the line", l)
	}
	wantCents(t, "the payments", p.Summary.Fixed, 31000)
	countsOnce(t, p, 29000+31000, 31000)
}

func TestACardDueDateIsShownButNeverSubtracted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	checking := f.account(t, f.user, "checking", "0001")
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	f.line(t, card, on(nov26, 2), -4000, store.KindExpense, "SUPER X")
	before := f.month(t, nov26)

	statement := func(closing, due civil.Date, minimum money.Cents) {
		t.Helper()
		if err := f.db.Q().CreateCheckpoint(ctx, store.Checkpoint{ID: uuid.New(), UserID: f.user, AccountID: card,
			AsOf: closing, Balance: -48620, Source: store.SourceManual, DueOn: &due, MinimumPayment: &minimum}); err != nil {
			t.Fatal(err)
		}
	}
	statement(on(sep26, 28), on(oct26, 8), 2000)
	statement(on(oct26, 28), on(nov26, 8), 2500)
	// Paying the card is a transfer: part of the minimum, then the rest.
	pay := func(day int, amount money.Cents) {
		f.line(t, checking, on(nov26, day), -amount, store.KindTransfer, "PAGO A TARJETA")
		f.line(t, card, on(nov26, day), amount, store.KindTransfer, "SU PAGO")
	}
	pay(3, 1000)

	p := f.month(t, nov26)
	if len(p.Cards) != 1 {
		t.Fatalf("cards = %+v, want the latest statement", p.Cards)
	}
	c := p.Cards[0]
	if c.AccountID != card || c.ClosingOn != on(oct26, 28) || c.DueOn != on(nov26, 8) || *c.MinimumPayment != 2500 ||
		c.Paid {
		t.Errorf("the card = %+v, want it due on the 8th and not paid", c)
	}
	if !sameSummary(p.Summary, before.Summary) || p.Spending != 4000 {
		t.Errorf("with the card due, the summary = %+v, want %+v", p.Summary, before.Summary)
	}
	if len(p.Alerts) != 1 || p.Alerts[0].Kind != plan.AlertPayment || p.Alerts[0].Level != plan.LevelSoon ||
		p.Alerts[0].AccountID == nil || *p.Alerts[0].AccountID != card || p.Alerts[0].DueOn != on(nov26, 8) {
		t.Errorf("alerts = %+v, want the card due soon", p.Alerts)
	}

	pay(5, 1500)
	p = f.month(t, nov26)
	if len(p.Cards) != 1 || !p.Cards[0].Paid || len(p.Alerts) != 0 {
		t.Errorf("once the minimum is paid, cards = %+v and alerts %+v", p.Cards, p.Alerts)
	}
	if !sameSummary(p.Summary, before.Summary) {
		t.Errorf("with the card paid, the summary = %+v, want %+v", p.Summary, before.Summary)
	}
	var got struct {
		Cards []struct {
			AccountID      string `json:"account_id"`
			ClosingOn      string `json:"closing_on"`
			DueOn          string `json:"due_on"`
			MinimumPayment struct {
				Cents int64 `json:"cents"`
			} `json:"minimum_payment"`
			Paid bool `json:"paid"`
		} `json:"cards"`
	}
	decode(t, f.serve(t, f.user, "GET", "/", ""), &got)
	if len(got.Cards) != 1 || got.Cards[0].AccountID != card.String() || got.Cards[0].ClosingOn != "2026-10-28" ||
		got.Cards[0].DueOn != "2026-11-08" || got.Cards[0].MinimumPayment.Cents != 2500 || !got.Cards[0].Paid {
		t.Errorf("GET /api/plan cards = %+v", got.Cards)
	}

	// A statement due in November is November's: October has passed and
	// December is ahead.
	for _, m := range []plan.Month{oct26, dec26} {
		if cards := f.month(t, m).Cards; len(cards) != 0 {
			t.Errorf("%s shows cards %+v", m, cards)
		}
	}
}

func TestARejectedMovementIsNeverMatchedAgain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 100000, 1))
	person := uuid.New()
	f.save(t, person, payment(nov26, 3267, 5))
	first := f.line(t, checking, on(nov26, 5), -3267, store.KindExpense, "YAPPY A PERSONA UNO")
	if l := fixedLine(t, f.month(t, nov26), person); !paidBy(l, first) {
		t.Fatalf("the payment = %+v", l)
	}

	// It was a loan to the same person, not the monthly payment.
	f.says(t, person, nov26, first, plan.DecisionReject)
	p := f.month(t, nov26)
	if l := fixedLine(t, p, person); len(l.Movements) != 0 || l.Status != plan.StatusLate || l.Committed != 3267 ||
		len(l.Candidates) != 0 {
		t.Errorf("the payment = %+v, want it still to go out with nothing offered", l)
	}
	countsOnce(t, p, 3267, 0)

	// The monthly payment goes out later: that one is it.
	second := f.line(t, checking, on(nov26, 20), -3267, store.KindExpense, "YAPPY A PERSONA UNO")
	p = f.month(t, nov26)
	if l := fixedLine(t, p, person); !paidBy(l, second) {
		t.Errorf("the payment = %+v, want the second one", l)
	}
	countsOnce(t, p, 6534, 3267)

	// The owner changes their mind: the first one was it after all. What
	// they say now replaces what they said.
	link := f.says(t, person, nov26, first, plan.DecisionAssign)
	if n := storetest.Count(t, f.db, "plan_links"); n != 1 {
		t.Errorf("%d links, want only the newer word", n)
	}
	p = f.month(t, nov26)
	if l := fixedLine(t, p, person); !paidBy(l, first) || l.Movements[0].Link != link.ID {
		t.Errorf("the payment = %+v, want the first one, as the owner said", l)
	}
	countsOnce(t, p, 6534, 3267)
	again, created, err := f.svc.Link(ctx, f.user,
		plan.LinkInput{ID: uuid.New(), ItemID: person, Month: nov26, MovementID: first, Decision: plan.DecisionAssign})
	if err != nil || created || again.ID != link.ID {
		t.Errorf("saying it again = %+v, created %v, %v", again, created, err)
	}
}

func TestAddingExtraIncomeToTheMonthTakesItsMovements(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	f.save(t, uuid.New(), paycheck(nov26, 65000, 15))
	f.line(t, checking, on(nov26, 14), 65000, store.KindIncome, "ACH EMPRESA X")
	bonus := f.line(t, checking, on(nov26, 3), 30000, store.KindIncome, "TRANSFERENCIA DE PERSONA DOS")
	sale := f.line(t, checking, on(nov26, 5), 10000, store.KindIncome, "YAPPY DE PERSONA TRES")
	before := f.month(t, nov26)
	wantCents(t, "the income no paycheck took", before.Income.Unplanned, 40000)
	if len(before.Income.Items[0].Candidates) != 0 {
		t.Errorf("the paycheck that arrived is offered %+v", before.Income.Items[0].Candidates)
	}

	// "Sumar a lo libre": an income for November alone, as much as came in
	// unplanned, and each of those movements said to be it.
	extra := uuid.New()
	for _, req := range []struct {
		method, target, body string
		status               int
	}{
		{"PUT", "/items/" + extra.String(), `{"kind": "income", "month": "2026-11", "scope": "only", "name": "Ingreso extra", "amount": "400.00", "day": 6}`, http.StatusCreated},
		{"POST", "/links", fmt.Sprintf(`{"id": %q, "item_id": %q, "month": "2026-11", "movement_id": %q, "decision": "assign"}`, uuid.New(), extra, bonus), http.StatusCreated},
		{"POST", "/links", fmt.Sprintf(`{"id": %q, "item_id": %q, "month": "2026-11", "movement_id": %q, "decision": "assign"}`, uuid.New(), extra, sale), http.StatusCreated},
	} {
		if rec := f.serve(t, f.user, req.method, req.target, req.body); rec.Code != req.status {
			t.Fatalf("%s %s = %d %s", req.method, req.target, rec.Code, rec.Body)
		}
	}
	after := f.month(t, nov26)
	wantCents(t, "the income planned", after.Income.Expected, 105000)
	wantCents(t, "the income that came in", after.Income.Actual, 105000)
	wantCents(t, "the income no paycheck took", after.Income.Unplanned, 0)
	wantCents(t, "what is free", after.Summary.Free, before.Summary.Free+40000)
	var line plan.Line
	for _, l := range after.Income.Items {
		if l.Item.ID == extra {
			line = l
		}
	}
	if line.Status != plan.StatusDone || !paidBy(line, bonus, sale) || line.Movements[0].Link == uuid.Nil {
		t.Errorf("the extra income = %+v, want it to have come in as both", line)
	}
	if dec := f.month(t, dec26); len(dec.Income.Items) != 1 || dec.Summary.Income != 65000 {
		t.Errorf("December expects %s from %d incomes, want November's extra left out", dec.Summary.Income, len(dec.Income.Items))
	}

	// Undone, it goes with what was said about it.
	if rec := f.serve(t, f.user, "DELETE", "/items/"+extra.String()+"?month=2026-11&scope=only", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s", rec.Code, rec.Body)
	}
	if n := storetest.Count(t, f.db, "plan_links"); n != 0 {
		t.Errorf("%d links outlived their item", n)
	}
	undone := f.month(t, nov26)
	wantCents(t, "the income no paycheck took", undone.Income.Unplanned, 40000)
	wantCents(t, "what is free", undone.Summary.Free, before.Summary.Free)
}

func TestThePlanOffersWhatAPaymentOrAPaycheckCouldBe(t *testing.T) {
	f := newFixture(t)
	checking := f.account(t, f.user, "checking", "0001")
	salary, gym, insurance := uuid.New(), uuid.New(), uuid.New()
	f.save(t, salary, named(paycheck(nov26, 65000, 1), "Sueldo", "PLANILLA EMPRESA Y"))
	f.save(t, gym, named(payment(nov26, 2500, 3), "Gimnasio", ""))
	f.save(t, insurance, named(payment(nov26, 3000, 2), "Seguro", "SEGURO X"))
	// The paycheck came in under another name; the gym is one of three
	// charges near its amount and day; the insurance went up a lot.
	pay := f.line(t, checking, on(nov26, 2), 66000, store.KindIncome, "ACH EMPRESA X")
	f.line(t, checking, on(nov26, 25), 65000, store.KindIncome, "ACH EMPRESA X")
	near := f.line(t, checking, on(nov26, 4), -2600, store.KindExpense, "POS 0002")
	nearer := f.line(t, checking, on(nov26, 3), -2400, store.KindExpense, "POS 0001")
	f.line(t, checking, on(nov26, 3), -2900, store.KindExpense, "POS 0003")
	f.line(t, checking, on(nov26, 15), -2500, store.KindExpense, "POS 0004")
	up := f.line(t, checking, on(nov26, 2), -9000, store.KindExpense, "SEGURO X")

	p := f.month(t, nov26)
	if l := p.Income.Items[0]; len(l.Movements) != 0 || len(l.Candidates) != 1 || l.Candidates[0].ID != pay {
		t.Errorf("the paycheck = %+v, want the one within a week offered", l)
	}
	if l := fixedLine(t, p, gym); len(l.Candidates) != 2 || l.Candidates[0].ID != nearer || l.Candidates[1].ID != near {
		t.Errorf("the gym is offered %+v, want the two within a tenth, nearest first", l.Candidates)
	}
	if l := fixedLine(t, p, insurance); len(l.Movements) != 0 || len(l.Candidates) != 1 || l.Candidates[0].ID != up {
		t.Errorf("the insurance = %+v, want the one with its text offered", l)
	}

	// Offered, it is one tap: the owner says which it was.
	f.says(t, gym, nov26, near, plan.DecisionAssign)
	if l := fixedLine(t, f.month(t, nov26), gym); !paidBy(l, near) || len(l.Candidates) != 0 {
		t.Errorf("the gym = %+v", l)
	}
}

func TestTheLinksHandlerSaysWhatIsWrong(t *testing.T) {
	f := newFixture(t)
	b := newUser(t, f.db, "b@example.com")
	theirs := f
	theirs.user = b
	checking := f.account(t, f.user, "checking", "0001")
	rent, person := uuid.New(), uuid.New()
	f.save(t, rent, named(payment(nov26, 40000, 2), "Alquiler", "ALQUILER"))
	f.save(t, person, payment(nov26, 3267, 5))
	mv := f.line(t, checking, on(nov26, 2), -40000, store.KindExpense, "ALQUILER")
	far := f.line(t, checking, on(sep26, 2), -40000, store.KindExpense, "ALQUILER")
	transfer := f.line(t, checking, on(nov26, 3), -1000, store.KindTransfer, "TRASPASO")
	salary := uuid.New()
	f.save(t, salary, paycheck(nov26, 100000, 1))
	came := f.line(t, checking, on(nov26, 4), 30000, store.KindTransfer, "TRANSFERENCIA DE PERSONA DOS")
	theirItem := uuid.New()
	theirs.save(t, theirItem, payment(nov26, 3267, 5))
	theirLine := theirs.line(t, theirs.account(t, b, "checking", "0009"), on(nov26, 5), -3267, store.KindExpense, "YAPPY")

	body := func(id, item uuid.UUID, month string, movement *uuid.UUID, decision string) string {
		s := fmt.Sprintf(`{"id": %q, "item_id": %q, "month": %q, "decision": %q`, id, item, month, decision)
		if movement != nil {
			s += fmt.Sprintf(`, "movement_id": %q`, *movement)
		}
		return s + "}"
	}

	// Said: 201 with the link; the same id again, or the same movement and
	// month again, 200 with the link already there.
	id := uuid.New()
	var created, again map[string]any
	rec := f.serve(t, f.user, "POST", "/links", body(id, rent, "2026-11", &mv, "assign"))
	if decode(t, rec, &created); rec.Code != http.StatusCreated || created["id"] != id.String() ||
		created["item_id"] != rent.String() || created["month"] != "2026-11" || created["decision"] != "assign" ||
		created["movement_id"] != mv.String() {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	for _, again1 := range []string{body(id, rent, "2026-11", &mv, "assign"), body(uuid.New(), rent, "2026-11", &mv, "assign")} {
		rec := f.serve(t, f.user, "POST", "/links", again1)
		if decode(t, rec, &again); rec.Code != http.StatusOK || fmt.Sprint(again) != fmt.Sprint(created) {
			t.Errorf("POST %s again = %d %s", again1, rec.Code, rec.Body)
		}
	}
	paidID := uuid.New()
	rec = f.serve(t, f.user, "POST", "/links", body(paidID, person, "2026-11", nil, "paid"))
	var paid map[string]any
	if decode(t, rec, &paid); rec.Code != http.StatusCreated || paid["movement_id"] != nil || paid["decision"] != "paid" {
		t.Errorf("POST paid = %d %s", rec.Code, rec.Body)
	}

	for _, tt := range []struct {
		user          uuid.UUID
		body          string
		status        int
		code, message string
		fields        map[string]string
	}{
		{f.user, body(uuid.New(), person, "2026-11", &mv, "assign"), 409, "movement_in_plan", "Ese movimiento ya está en tu plan.", nil},
		{f.user, body(uuid.New(), uuid.New(), "2026-11", &mv, "assign"), 404, "plan_item_not_found", "Eso ya no está en tu plan.", nil},
		{f.user, body(uuid.New(), rent, "2026-10", &mv, "assign"), 404, "plan_item_not_found", "Eso ya no está en tu plan.", nil},
		{b, body(uuid.New(), rent, "2026-11", &theirLine, "assign"), 404, "plan_item_not_found", "", nil},
		{f.user, body(uuid.New(), person, "2026-11", &theirLine, "assign"), 404, "entry_not_found", "El movimiento no existe.", nil},
		{f.user, body(uuid.New(), rent, "2026-11", &far, "reject"), 422, "movement_too_far", "", nil},
		{f.user, body(uuid.New(), rent, "2026-13", &mv, "assign"), 400, "invalid_month", "Revisa el mes.", nil},
		{f.user, body(uuid.New(), rent, "2029-01", nil, "paid"), 422, "plan_month_too_far", "", nil},
		{b, body(paidID, theirItem, "2026-11", nil, "paid"), 409, "plan_link_id_taken", "", nil},
		{f.user, body(uuid.New(), rent, "2026-11", &mv, "maybe"), 422, "validation_failed", "", map[string]string{
			"decision": "Elige si fue este movimiento, si no lo fue o si se pagó por otro lado."}},
		{f.user, body(uuid.New(), rent, "2026-11", nil, "assign"), 422, "validation_failed", "", map[string]string{
			"movement_id": "Elige el movimiento."}},
		{f.user, body(uuid.New(), rent, "2026-11", &mv, "paid"), 422, "validation_failed", "", map[string]string{
			"movement_id": "Si se pagó por otro lado, no elijas un movimiento."}},
		{f.user, body(uuid.New(), rent, "2026-11", &transfer, "assign"), 422, "validation_failed", "", map[string]string{
			"movement_id": "Elige un gasto: una transferencia o un ingreso no es este pago."}},
		{f.user, body(uuid.New(), salary, "2026-11", &came, "assign"), 422, "validation_failed", "", map[string]string{
			"movement_id": "Elige un ingreso: ese movimiento no es un ingreso."}},
	} {
		rec := f.serve(t, tt.user, "POST", "/links", tt.body)
		e := failure(t, rec)
		if rec.Code != tt.status || e.Error.Code != tt.code || tt.message != "" && e.Error.Message != tt.message {
			t.Errorf("POST %s = %d %s; want %d %s", tt.body, rec.Code, rec.Body, tt.status, tt.code)
		}
		for field, message := range tt.fields {
			if e.Error.Fields[field] != message {
				t.Errorf("POST %s: %s = %q, want %q", tt.body, field, e.Error.Fields[field], message)
			}
		}
	}
	if n := storetest.Count(t, f.db, "plan_links"); n != 2 {
		t.Errorf("%d links, want the two said", n)
	}

	// Undone: 204, and then it is not there, for anyone.
	for _, tt := range []struct {
		user   uuid.UUID
		target string
		status int
	}{
		{b, "/links/" + id.String(), http.StatusNotFound},
		{f.user, "/links/" + id.String(), http.StatusNoContent},
		{f.user, "/links/" + id.String(), http.StatusNotFound},
		{f.user, "/links/no-es-un-id", http.StatusNotFound},
	} {
		rec := f.serve(t, tt.user, "DELETE", tt.target, "")
		if rec.Code != tt.status {
			t.Errorf("DELETE %s = %d %s, want %d", tt.target, rec.Code, rec.Body, tt.status)
			continue
		}
		if tt.status == http.StatusNotFound {
			if e := failure(t, rec); e.Error.Code != "plan_link_not_found" || e.Error.Message != "Eso ya se había deshecho." {
				t.Errorf("DELETE %s = %s", tt.target, rec.Body)
			}
		}
	}
}
