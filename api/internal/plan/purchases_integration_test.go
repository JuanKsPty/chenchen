package plan_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/reports"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

var (
	mar27 = plan.Month{Year: 2027, Month: time.March}
	apr27 = plan.Month{Year: 2027, Month: time.April}
)

// morning is nine in the morning of day d of month m, in Panama.
func morning(m plan.Month, d int) time.Time {
	return time.Date(m.Year, m.Month, d, 9, 0, 0, 0, civil.Panama)
}

// camera is a camera of price, planned in month m to be bought in target,
// set aside as funding, that the shop prints as TIENDA DE FOTO X.
func camera(m, target plan.Month, price money.Cents, funding string) plan.ItemInput {
	return plan.ItemInput{Kind: plan.KindPurchase, Month: m, Name: "Cámara", Amount: price,
		MatchText: "TIENDA DE FOTO X", TargetMonth: target, Funding: funding}
}

// purchaseOf is a month's line of a purchase; ok is false when the month
// does not show it.
func purchaseOf(p plan.Plan, id uuid.UUID) (plan.PurchaseLine, bool) {
	for _, l := range p.Purchases {
		if l.Item.ID == id {
			return l, true
		}
	}
	return plan.PurchaseLine{}, false
}

func (f fixture) purchaseIn(t *testing.T, m plan.Month, id uuid.UUID) plan.PurchaseLine {
	t.Helper()
	l, ok := purchaseOf(f.month(t, m), id)
	if !ok {
		t.Fatalf("%s does not show the purchase", m)
	}
	return l
}

func (f fixture) noPurchaseIn(t *testing.T, m plan.Month) {
	t.Helper()
	if p := f.month(t, m); len(p.Purchases) > 0 || p.Summary.Purchases != 0 {
		t.Errorf("%s shows %+v, taking %s", m, p.Purchases, p.Summary.Purchases)
	}
}

// gastos is what Gastos spends in month m.
func (f fixture) gastos(t *testing.T, m plan.Month) money.Cents {
	t.Helper()
	totals, err := reports.NewService(f.db).Spending(context.Background(), f.user, m.First(), m.Last(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var sum money.Cents
	for _, c := range totals {
		sum += c.Total
	}
	return sum
}

func boughtBy(l plan.PurchaseLine, ids ...uuid.UUID) bool {
	return l.Status == plan.StatusBought && len(l.Movements) == len(ids) &&
		!slices.ContainsFunc(l.Movements, func(m plan.LineMovement) bool { return !slices.Contains(ids, m.ID) })
}

func TestAPlannedPurchaseIsSetAsideMonthByMonthAndAddsUpToItsPrice(t *testing.T) {
	f := newFixture(t)
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	id := uuid.New()
	saved := f.save(t, id, camera(nov26, mar27, 100003, plan.FundingMonthly))
	if plan.MonthOf(saved.StartMonth) != nov26 || *saved.Target != 100003 || saved.Funding != plan.FundingMonthly {
		t.Fatalf("saved %+v", saved)
	}
	// $1,000.03 over five months: no month a cent more than another but for
	// the rounding, and together exactly the price.
	wantSpans(t, f.versions(t, id), span{nov26, nov26, 20000, 0}, span{dec26, dec26, 20001, 0},
		span{jan27, jan27, 20000, 0}, span{feb27, mar27, 20001, 0})

	var setAside money.Cents
	for _, m := range []plan.Month{nov26, dec26, jan27, feb27, mar27} {
		p := f.month(t, m)
		l, ok := purchaseOf(p, id)
		if !ok {
			t.Fatalf("%s does not show the purchase", m)
		}
		want := plan.StatusSettingAside
		if m == mar27 {
			want = plan.StatusThisMonth
		}
		if l.Status != want || l.Paid != nil || len(l.Movements) != 0 || l.Planned() != 100003 {
			t.Errorf("%s: %+v, want %s", m, l, want)
		}
		wantCents(t, m.String()+" set aside before", l.SetAsideBefore, setAside)
		wantCents(t, m.String()+" takes", p.Summary.Purchases, l.Share)
		wantCents(t, m.String()+" leaves free", p.Summary.Free, 130000-l.Share)
		setAside += l.Share
	}
	wantCents(t, "what the five months set aside", setAside, 100003)
	// Before it was planned, and after it was due, no month sets anything
	// aside for it.
	f.noPurchaseIn(t, oct26)
	f.noPurchaseIn(t, apr27)

	// All of the price in its month; or none of it, paid with savings. Each
	// still shows from the month it was planned for, setting nothing aside
	// until then.
	whole, savings := uuid.New(), uuid.New()
	f.save(t, whole, camera(nov26, feb27, 50000, plan.FundingThatMonth))
	f.save(t, savings, camera(nov26, jan27, 70000, plan.FundingSavings))
	wantSpans(t, f.versions(t, whole), span{feb27, feb27, 50000, 0})
	wantSpans(t, f.versions(t, savings))
	if l := f.purchaseIn(t, dec26, whole); l.Share != 0 || l.Status != plan.StatusSettingAside {
		t.Errorf("all of it in February, December = %+v", l)
	}
	if l := f.purchaseIn(t, feb27, whole); l.Share != 50000 || l.Status != plan.StatusThisMonth {
		t.Errorf("all of it in February, February = %+v", l)
	}
	if l := f.purchaseIn(t, jan27, savings); l.Share != 0 || l.Status != plan.StatusThisMonth {
		t.Errorf("paid with savings, January = %+v", l)
	}
}

func TestBuyingItIsNotCountedTwice(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	id := uuid.New()
	f.save(t, id, camera(nov26, dec26, 50000, plan.FundingMonthly))

	f.now(morning(dec26, 20))
	buy := f.line(t, card, on(dec26, 15), -50000, store.KindExpense, "COMPRA TIENDA DE FOTO X")
	f.line(t, card, on(dec26, 3), -2000, store.KindExpense, "SUPER X")

	p := f.month(t, dec26)
	l, ok := purchaseOf(p, id)
	if !ok || !boughtBy(l, buy) || l.Movements[0].Link != uuid.Nil || l.BoughtOn != on(dec26, 15) ||
		l.Paid == nil || *l.Paid != 50000 {
		t.Fatalf("December = %+v, want it bought on the 15th by its line", l)
	}
	// Bought for its price in its month, it takes that month's share, and its
	// line is the purchase's, never also what went out of what was free.
	wantCents(t, "what the purchase takes", p.Summary.Purchases, 25000)
	countsOnce(t, p, f.gastos(t, dec26), 50000)
	wantCents(t, "what is left", p.Summary.Left, 130000-25000-2000)

	// November had set its share aside, and saw nothing of the line.
	if l := f.purchaseIn(t, nov26, id); l.Status != plan.StatusSettingAside || l.Committed != 25000 || l.Paid != nil {
		t.Errorf("November = %+v", l)
	}
	// And the months after show it no more.
	f.noPurchaseIn(t, jan27)

	v := plan.NewView(p)
	if len(v.Purchases) != 1 || v.Purchases[0].Status != plan.StatusBought || v.Purchases[0].Paid.Cents != 50000 ||
		v.Purchases[0].BoughtOn != on(dec26, 15) || len(v.Purchases[0].Movements) != 1 ||
		v.Purchases[0].Movements[0].How != "auto" || v.Purchases[0].Share.Cents != 25000 ||
		v.Purchases[0].SetAsideBefore.Cents != 25000 {
		t.Errorf("on the wire = %+v", v.Purchases)
	}
}

func TestBuyingItEarlyChargesTheRestToThatMonth(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	id := uuid.New()
	f.save(t, id, camera(nov26, feb27, 80000, plan.FundingMonthly))

	f.now(morning(dec26, 20))
	buy := f.line(t, card, on(dec26, 12), -80000, store.KindExpense, "TIENDA DE FOTO X")

	if l := f.purchaseIn(t, nov26, id); l.Committed != 20000 {
		t.Errorf("November took %s", l.Committed)
	}
	p := f.month(t, dec26)
	l, _ := purchaseOf(p, id)
	if !boughtBy(l, buy) {
		t.Fatalf("December = %+v, want it bought", l)
	}
	// What November set aside paid for part of it; December pays the rest.
	wantCents(t, "what December takes", p.Summary.Purchases, 60000)
	countsOnce(t, p, 80000, 80000)
	// January and February had nothing left to set aside.
	f.noPurchaseIn(t, jan27)
	f.noPurchaseIn(t, feb27)
}

func TestBuyingItCheaperFreesTheRestOfThatMonth(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	id, lens := uuid.New(), uuid.New()
	f.save(t, id, camera(nov26, jan27, 90000, plan.FundingMonthly))
	f.save(t, lens, plan.ItemInput{Kind: plan.KindPurchase, Month: nov26, Name: "Lente", Amount: 60000,
		MatchText: "TIENDA DE LENTES X", TargetMonth: jan27, Funding: plan.FundingMonthly})

	f.now(morning(jan27, 20))
	buy := f.line(t, card, on(jan27, 10), -84000, store.KindExpense, "TIENDA DE FOTO X")
	// The lens came far cheaper, at a shop the plan would not have looked
	// at; the owner says it was it.
	cheap := f.line(t, card, on(jan27, 11), -10000, store.KindExpense, "OTRA TIENDA")
	link := f.says(t, lens, jan27, cheap, plan.DecisionAssign)

	p := f.month(t, jan27)
	l, _ := purchaseOf(p, id)
	if !boughtBy(l, buy) || l.Share != 30000 {
		t.Fatalf("the camera = %+v", l)
	}
	// $840.00 of $900.00, with $600.00 set aside before: January takes
	// $240.00 of its $300.00 share.
	wantCents(t, "what the camera takes", l.Committed, 24000)
	other, _ := purchaseOf(p, lens)
	if !boughtBy(other, cheap) || other.Movements[0].Link != link.ID {
		t.Fatalf("the lens = %+v", other)
	}
	// $100.00, when $400.00 were set aside before: none of its share.
	wantCents(t, "what the lens takes", other.Committed, 0)
	wantCents(t, "what the purchases take", p.Summary.Purchases, 24000)
	wantCents(t, "what is free", p.Summary.Free, 130000-24000)
	countsOnce(t, p, 94000, 94000)
}

func TestAPurchasePaidWithSavingsDoesNotLowerWhatIsLeft(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	id := uuid.New()
	f.save(t, id, camera(nov26, dec26, 200000, plan.FundingSavings))
	wantSpans(t, f.versions(t, id))

	f.now(morning(dec26, 20))
	f.line(t, card, on(dec26, 3), -2000, store.KindExpense, "SUPER X")
	before := f.month(t, dec26)
	if l, _ := purchaseOf(before, id); l.Status != plan.StatusThisMonth || l.Committed != 0 {
		t.Fatalf("before it is bought = %+v", l)
	}
	wantCents(t, "what is left before", before.Summary.Left, 128000)

	buy := f.line(t, card, on(dec26, 18), -200000, store.KindExpense, "TIENDA DE FOTO X")
	after := f.month(t, dec26)
	if l, _ := purchaseOf(after, id); !boughtBy(l, buy) || l.Committed != 0 || *l.Paid != 200000 {
		t.Fatalf("after it is bought = %+v", l)
	}
	if !sameSummary(before.Summary, after.Summary) {
		t.Errorf("buying it with savings changed the month from %+v to %+v", before.Summary, after.Summary)
	}
	countsOnce(t, after, 202000, 200000)
}

func TestAPurchaseIsRecognisedOnlyWhenOneMovementCouldBeIt(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	tech := f.category(t, f.user, "Tecnología", "expense")
	cams := f.child(t, tech, "Cámaras")
	id := uuid.New()
	in := camera(nov26, dec26, 50000, plan.FundingMonthly)
	in.MatchText, in.CategoryID = "", &tech
	f.save(t, id, in)

	f.now(morning(dec26, 20))
	online := f.filed(t, card, on(dec26, 5), -49000, store.KindExpense, "COMPRA EN LINEA", &cams)
	// About the price, but nothing says it is the camera.
	super := f.line(t, card, on(dec26, 9), -51000, store.KindExpense, "SUPER X")
	// Filed as a camera, but a tenth dearer.
	f.filed(t, card, on(nov26, 20), -60000, store.KindExpense, "COMPRA EN LINEA", &cams)

	// One outflow could be it, filed under a child of its category: it is.
	if l := f.purchaseIn(t, dec26, id); !boughtBy(l, online) {
		t.Fatalf("with one that could be it = %+v", l)
	}

	// Two could: neither is taken, both are offered, nearer the price first,
	// and the month keeps its share aside.
	shop := f.filed(t, card, on(dec26, 12), -50500, store.KindExpense, "COMPRA EN LINEA", &tech)
	p := f.month(t, dec26)
	l, _ := purchaseOf(p, id)
	if l.Status != plan.StatusThisMonth || len(l.Movements) != 0 || len(l.Candidates) != 2 ||
		l.Candidates[0].ID != shop || l.Candidates[1].ID != online || l.Committed != 25000 {
		t.Fatalf("with two that could be it = %+v", l)
	}
	countsOnce(t, p, 150500, 0)

	// The owner says the second is not it: the first is the one left.
	f.says(t, id, dec26, shop, plan.DecisionReject)
	if l := f.purchaseIn(t, dec26, id); !boughtBy(l, online) {
		t.Fatalf("with the other refused = %+v", l)
	}
	// And what the owner says bought it did, whatever it is.
	link := f.says(t, id, dec26, super, plan.DecisionAssign)
	p = f.month(t, dec26)
	if l, _ := purchaseOf(p, id); !boughtBy(l, super) || l.Movements[0].Link != link.ID || l.Committed != 26000 {
		t.Fatalf("with the owner's word = %+v", l)
	}
	countsOnce(t, p, 150500, 51000)
	// Every month reads the same purchase the same way: November set its
	// share aside, January shows it no more.
	if l := f.purchaseIn(t, nov26, id); l.Status != plan.StatusSettingAside || l.Committed != 25000 {
		t.Errorf("November = %+v", l)
	}
	f.noPurchaseIn(t, jan27)
}

func TestChangingThePriceSpreadsWhatIsLeftOverTheMonthsLeft(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := uuid.New()
	f.save(t, id, camera(nov26, mar27, 100000, plan.FundingMonthly))
	wantSpans(t, f.versions(t, id), span{nov26, mar27, 20000, 0})

	f.now(morning(jan27, 6))
	// November and December set aside $400.00 already; the rest of a dearer
	// price is spread over the three months left.
	f.save(t, id, camera(jan27, mar27, 130000, plan.FundingMonthly))
	wantSpans(t, f.versions(t, id), span{nov26, dec26, 20000, 0}, span{jan27, mar27, 30000, 0})
	if l := f.purchaseIn(t, nov26, id); l.Share != 20000 || l.Planned() != 130000 {
		t.Errorf("November = %+v; its share stays as it was", l)
	}
	if item, err := f.db.Q().PlanItemByID(ctx, f.user, id); err != nil || plan.MonthOf(item.StartMonth) != nov26 {
		t.Errorf("the purchase = %+v, %v; still set aside from November", item, err)
	}

	// A later month spreads it over one more.
	f.save(t, id, camera(jan27, apr27, 130000, plan.FundingMonthly))
	wantSpans(t, f.versions(t, id), span{nov26, dec26, 20000, 0}, span{jan27, apr27, 22500, 0})
	// All of the rest in its month.
	f.save(t, id, camera(jan27, apr27, 130000, plan.FundingThatMonth))
	wantSpans(t, f.versions(t, id), span{nov26, dec26, 20000, 0}, span{apr27, apr27, 90000, 0})
	// Saving the same again writes nothing.
	stored := f.versions(t, id)
	f.save(t, id, camera(jan27, apr27, 130000, plan.FundingThatMonth))
	if again := f.versions(t, id); again[0].ID != stored[0].ID || again[1].ID != stored[1].ID {
		t.Errorf("the same save rewrote %+v as %+v", stored, again)
	}
	// Paid with savings, nothing more is set aside, and what was stays.
	f.save(t, id, camera(jan27, apr27, 130000, plan.FundingSavings))
	wantSpans(t, f.versions(t, id), span{nov26, dec26, 20000, 0})
	// Cheaper than what was set aside, nothing more is either.
	f.save(t, id, camera(jan27, mar27, 30000, plan.FundingMonthly))
	wantSpans(t, f.versions(t, id), span{nov26, dec26, 20000, 0})
	if l := f.purchaseIn(t, jan27, id); l.Share != 0 || l.SetAsideBefore != 40000 || l.Status != plan.StatusSettingAside {
		t.Errorf("January, with more set aside than it costs = %+v", l)
	}

	// The months that passed are never written.
	if _, _, err := f.svc.SaveItem(ctx, f.user, id, camera(dec26, mar27, 50000, plan.FundingMonthly)); !errors.Is(err, plan.ErrMonthPast) {
		t.Errorf("saving it from December = %v, want ErrMonthPast", err)
	}
}

func TestAMissedPurchaseStopsBeingSetAside(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	id := uuid.New()
	f.save(t, id, camera(nov26, dec26, 40000, plan.FundingMonthly))

	f.now(morning(jan27, 10))
	// December was its month and passed without it: it set its share aside
	// then, as the past keeps, and is missed.
	if l := f.purchaseIn(t, dec26, id); l.Status != plan.StatusMissed || l.Committed != 20000 {
		t.Errorf("December = %+v", l)
	}
	// From January on it sets nothing aside.
	for _, m := range []plan.Month{jan27, feb27} {
		f.noPurchaseIn(t, m)
		wantCents(t, m.String()+" leaves free", f.month(t, m).Summary.Free, 130000)
	}

	// Bought the month after it was due, what was set aside pays for it, and
	// that month takes only what it cost beyond.
	buy := f.line(t, card, on(jan27, 8), -41000, store.KindExpense, "TIENDA DE FOTO X")
	p := f.month(t, jan27)
	if l, _ := purchaseOf(p, id); !boughtBy(l, buy) || l.Committed != 1000 {
		t.Fatalf("January = %+v", l)
	}
	if l := f.purchaseIn(t, dec26, id); l.Status != plan.StatusThisMonth {
		t.Errorf("December, bought in January = %+v", l)
	}
	f.noPurchaseIn(t, feb27)
}

func TestRemovingAPurchaseKeepsWhatThePastSetAside(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	id, later := uuid.New(), uuid.New()
	// With neither text nor category, any outflow about its price could be
	// it.
	in := camera(nov26, mar27, 100000, plan.FundingMonthly)
	in.MatchText = ""
	f.save(t, id, in)

	f.now(morning(jan27, 6))
	f.save(t, later, camera(jan27, mar27, 30000, plan.FundingMonthly))
	if err := f.svc.DeleteItem(ctx, f.user, id, jan27, plan.ScopeOnly); !errors.Is(err, plan.ErrPurchaseScope) {
		t.Errorf("taking January alone out = %v, want ErrPurchaseScope", err)
	}
	remove := func() {
		t.Helper()
		if err := f.svc.DeleteItem(ctx, f.user, id, jan27, plan.ScopeFrom); err != nil {
			t.Fatal(err)
		}
	}
	remove()
	// November and December keep what they set aside, and still say what
	// they said of it: it was being set aside for March.
	wantSpans(t, f.versions(t, id), span{nov26, dec26, 20000, 0})
	for _, m := range []plan.Month{nov26, dec26} {
		if l := f.purchaseIn(t, m, id); l.Committed != 20000 || l.Status != plan.StatusSettingAside {
			t.Errorf("%s = %+v", m, l)
		}
	}
	// Saving it again, as undoing the removal does, plans it to March again.
	in.Month = jan27
	f.save(t, id, in)
	wantSpans(t, f.versions(t, id), span{nov26, mar27, 20000, 0})
	if l := f.purchaseIn(t, mar27, id); l.Status != plan.StatusThisMonth {
		t.Errorf("March, planned again = %+v", l)
	}

	// Removed, no month from January on shows it, and an outflow about its
	// price in January, the month after the last one it was left in, is
	// something else: it goes out of what was free.
	remove()
	other := f.line(t, card, on(jan27, 4), -95000, store.KindExpense, "SUPER X")
	p := f.month(t, jan27)
	if l, ok := purchaseOf(p, id); ok {
		t.Errorf("January still shows it: %+v", l)
	}
	countsOnce(t, p, 95000, 0)
	wantCents(t, "what is left", p.Summary.Left, 130000-10000-95000)
	for _, m := range []plan.Month{feb27, mar27, apr27} {
		if l, ok := purchaseOf(f.month(t, m), id); ok {
			t.Errorf("%s still shows it: %+v", m, l)
		}
	}
	// Nor can the owner say it was bought then.
	if _, _, err := f.svc.Link(ctx, f.user, plan.LinkInput{ID: uuid.New(), ItemID: id, Month: jan27, MovementID: other,
		Decision: plan.DecisionAssign}); !errors.Is(err, plan.ErrItemNotFound) {
		t.Errorf("saying January's outflow bought it = %v, want ErrItemNotFound", err)
	}

	// One planned from the month it is taken out of has nothing left.
	if err := f.svc.DeleteItem(ctx, f.user, later, jan27, plan.ScopeFrom); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Q().PlanItemByID(ctx, f.user, later); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the purchase planned from January = %v, want it gone", err)
	}
}

func TestARemovedPurchaseIsNotBoughtInTheMonthItWasRemovedFrom(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	// Planned in October, neither sets anything aside before its month: one
	// all of it in January, the other paid with savings in December.
	f.now(morning(oct26, 20))
	gift, phone := uuid.New(), uuid.New()
	f.save(t, gift, plan.ItemInput{Kind: plan.KindPurchase, Month: oct26, Name: "Regalo", Amount: 10000,
		TargetMonth: jan27, Funding: plan.FundingThatMonth})
	f.save(t, phone, plan.ItemInput{Kind: plan.KindPurchase, Month: oct26, Name: "Teléfono", Amount: 50000,
		TargetMonth: dec26, Funding: plan.FundingSavings})

	f.now(novNow)
	for _, id := range []uuid.UUID{gift, phone} {
		if err := f.svc.DeleteItem(ctx, f.user, id, nov26, plan.ScopeFrom); err != nil {
			t.Fatal(err)
		}
	}
	// October keeps them as it had them, being set aside for their months.
	for _, id := range []uuid.UUID{gift, phone} {
		if l := f.purchaseIn(t, oct26, id); l.Status != plan.StatusSettingAside || l.Committed != 0 {
			t.Errorf("October = %+v", l)
		}
	}
	// November has neither, and outflows about their prices are what went
	// out of what was free, all of them in Llevas.
	f.line(t, card, on(nov26, 3), -10000, store.KindExpense, "TIENDA X")
	f.line(t, card, on(nov26, 3), -48000, store.KindExpense, "TIENDA Y")
	p := f.month(t, nov26)
	if len(p.Purchases) != 0 || p.Summary.Purchases != 0 {
		t.Errorf("November shows %+v, taking %s", p.Purchases, p.Summary.Purchases)
	}
	countsOnce(t, p, 58000, 0)
	wantCents(t, "what is left", p.Summary.Left, 130000-58000)
}

func TestWhichPurchaseAMovementBoughtIsTheSameFromEveryMonth(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	f.save(t, uuid.New(), paycheck(nov26, 130000, 15))
	// Two purchases of the same price, with neither text nor category: one
	// outflow could be either. The gift was planned first, so it takes it.
	gift, headphones := uuid.New(), uuid.New()
	f.save(t, gift, plan.ItemInput{Kind: plan.KindPurchase, Month: nov26, Name: "Regalo", Amount: 10000,
		TargetMonth: dec26, Funding: plan.FundingMonthly})
	f.save(t, headphones, plan.ItemInput{Kind: plan.KindPurchase, Month: nov26, Name: "Audífonos", Amount: 10000,
		TargetMonth: apr27, Funding: plan.FundingMonthly})

	f.now(morning(dec26, 20))
	buy := f.line(t, card, on(dec26, 10), -10000, store.KindExpense, "TIENDA X")
	if l := f.purchaseIn(t, dec26, gift); !boughtBy(l, buy) {
		t.Fatalf("December = %+v, want the gift bought", l)
	}
	// The headphones were never bought, whichever month is read, even those
	// that never read the gift: each sets its share aside to April, and they
	// add up to the price.
	var setAside money.Cents
	for _, m := range []plan.Month{nov26, dec26, jan27, feb27, mar27, apr27} {
		p := f.month(t, m)
		l, ok := purchaseOf(p, headphones)
		if !ok {
			t.Fatalf("%s does not show the headphones", m)
		}
		want := plan.StatusSettingAside
		if m == apr27 {
			want = plan.StatusThisMonth
		}
		if l.Status != want || len(l.Movements) != 0 || l.Committed != l.Share || l.SetAsideBefore != setAside {
			t.Errorf("%s = %+v, want %s", m, l, want)
		}
		setAside += l.Share
	}
	wantCents(t, "what the headphones set aside", setAside, 10000)
}

func TestAPlannedPurchaseIsNeverSuggestedAsAFixedPayment(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	card := f.account(t, f.user, "credit_card", "**** 1111")
	tablets := f.category(t, f.user, "Tabletas", "expense")
	cams := f.category(t, f.user, "Cámaras", "expense")
	// Two big purchases at the same shop, a month apart and about the same
	// amount, look like a payment of every month.
	f.filed(t, card, on(sep26, 12), -105000, store.KindExpense, "TIENDA DE FOTO X", &tablets)
	f.filed(t, card, on(oct26, 12), -110000, store.KindExpense, "TIENDA DE FOTO X", &cams)
	shop := func() bool {
		found, err := f.svc.Suggestions(ctx, f.user)
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(found, func(s plan.Suggestion) bool {
			return s.Kind == plan.KindFixed && strings.Contains(s.MatchText, "TIENDA DE FOTO")
		})
	}
	if !shop() {
		t.Fatal("the fixture is not what this test assumes: the shop is not suggested")
	}

	// They were the two purchases the owner planned in August.
	f.now(morning(aug26, 5))
	for _, p := range []struct {
		name     string
		price    money.Cents
		target   plan.Month
		category uuid.UUID
	}{{"Tableta", 105000, sep26, tablets}, {"Cámara", 110000, oct26, cams}} {
		f.save(t, uuid.New(), plan.ItemInput{Kind: plan.KindPurchase, Month: aug26, Name: p.name, Amount: p.price,
			TargetMonth: p.target, Funding: plan.FundingMonthly, CategoryID: &p.category})
	}
	f.now(novNow)
	if shop() {
		t.Error("the purchases' outflows are suggested as a payment of every month")
	}
}

// purchaseBody is the body of a PUT that saves a camera, with fields added
// to it.
func purchaseBody(month, fields string) string {
	body := fmt.Sprintf(`{"kind": "purchase", "month": %q, "name": "Cámara", "amount": "1100.00"`, month)
	if fields != "" {
		body += ", " + fields
	}
	return body + "}"
}

func TestThePurchaseHandlerSaysWhatIsWrong(t *testing.T) {
	f := newFixture(t)
	income := f.category(t, f.user, "Salario", "income")
	id := uuid.NewString()

	rec := f.serve(t, f.user, "PUT", "/items/"+id, purchaseBody("2026-11", `"target_month": "marzo", "funding": "monthly"`))
	if e := failure(t, rec); rec.Code != http.StatusBadRequest || e.Error.Code != "invalid_month" {
		t.Errorf("a month that cannot be read = %d %s", rec.Code, rec.Body)
	}
	for _, tt := range []struct {
		body string
		want map[string]string
	}{
		{purchaseBody("2026-11", `"funding": "monthly"`), map[string]string{
			"target_month": "Elige para cuándo, de aquí a 24 meses como mucho."}},
		{purchaseBody("2026-12", `"target_month": "2026-11", "funding": "monthly"`), map[string]string{
			"target_month": "Elige el mes que estás planeando o uno después."}},
		{purchaseBody("2026-11", `"target_month": "2028-12", "funding": "monthly"`), map[string]string{
			"target_month": "Elige para cuándo, de aquí a 24 meses como mucho."}},
		{purchaseBody("2026-11", `"target_month": "2027-03", "funding": "a plazos"`), map[string]string{
			"funding": "Elige cómo la apartas: un poco cada mes, todo ese mes o con tus ahorros."}},
		{purchaseBody("2026-11", `"target_month": "2027-03"`), map[string]string{
			"funding": "Elige cómo la apartas: un poco cada mes, todo ese mes o con tus ahorros."}},
		{strings.Replace(purchaseBody("2026-11", `"target_month": "2027-03", "funding": "monthly"`), `"1100.00"`, `"0"`, 1),
			map[string]string{"amount": "Indica un monto mayor que cero."}},
		{strings.Replace(purchaseBody("2026-11", `"target_month": "2027-03", "funding": "monthly"`), `"1100.00"`, `"mil"`, 1),
			map[string]string{"amount": "El monto no tiene un formato válido. Usa por ejemplo «12.50»."}},
		{purchaseBody("2026-11", `"target_month": "2027-03", "funding": "monthly", "scope": "only", "target": "1100"`),
			map[string]string{
				"scope":  "Una compra se aparta desde este mes hasta el de la compra.",
				"target": "Solo un ahorro tiene una meta.",
			}},
		{purchaseBody("2026-11", fmt.Sprintf(`"target_month": "2027-03", "funding": "monthly", "category_id": %q`, income)),
			map[string]string{"category_id": "Elige una categoría de gastos."}},
		// A paycheck says nothing of how it is set aside, nor when.
		{item("income", "2026-11", `"funding": "monthly", "target_month": "2027-03"`), map[string]string{
			"funding":      "Solo una compra planeada dice cómo se aparta.",
			"target_month": "Solo un ahorro o una compra tienen fecha.",
		}},
	} {
		rec := f.serve(t, f.user, "PUT", "/items/"+id, tt.body)
		e := failure(t, rec)
		if rec.Code != http.StatusUnprocessableEntity || e.Error.Code != "validation_failed" || !mapsEqual(e.Error.Fields, tt.want) {
			t.Errorf("PUT %s = %d %s; want %v", tt.body, rec.Code, rec.Body, tt.want)
		}
	}
	if n := storetest.Count(t, f.db, "plan_items"); n != 0 {
		t.Fatalf("a refused save wrote %d items", n)
	}

	// Saved: 201, with its price as its amount, the months it is set aside
	// from and due in, and how; the same again, 200.
	body := purchaseBody("2026-11", `"scope": "from", "target_month": "2027-03", "funding": "monthly", "match_text": "TIENDA DE FOTO X"`)
	var created, again map[string]any
	rec = f.serve(t, f.user, "PUT", "/items/"+id, body)
	if decode(t, rec, &created); rec.Code != http.StatusCreated {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	want := map[string]any{"id": id, "kind": "purchase", "name": "Cámara", "match_text": "TIENDA DE FOTO X",
		"from": "2026-11", "to": "2027-03", "start_month": "2026-11", "target_month": "2027-03", "funding": "monthly",
		"amount": map[string]any{"cents": float64(110000), "formatted": "1100.00", "currency": "USD"}}
	for field, value := range want {
		if fmt.Sprint(created[field]) != fmt.Sprint(value) {
			t.Errorf("created %s = %v, want %v", field, created[field], value)
		}
	}
	for _, absent := range []string{"day", "target", "account_id", "category_id"} {
		if _, ok := created[absent]; ok {
			t.Errorf("created has %s: %v", absent, created[absent])
		}
	}
	// Without a scope, it is from this month on, as a purchase always is.
	rec = f.serve(t, f.user, "PUT", "/items/"+id, strings.Replace(body, `"scope": "from", `, "", 1))
	if decode(t, rec, &again); rec.Code != http.StatusOK || fmt.Sprint(again) != fmt.Sprint(created) {
		t.Errorf("the same PUT again = %d %s", rec.Code, rec.Body)
	}
	rec = f.serve(t, f.user, "DELETE", "/items/"+id+"?month=2026-12&scope=only", "")
	if e := failure(t, rec); rec.Code != http.StatusUnprocessableEntity ||
		e.Error.Fields["scope"] != "Una compra se aparta desde este mes hasta el de la compra." {
		t.Errorf("DELETE December alone = %d %s", rec.Code, rec.Body)
	}

	// A purchase is bought by a movement, never marked bought; a saving is
	// recognised by its account, never by a link.
	card := f.account(t, f.user, "credit_card", "**** 1111")
	savings := f.account(t, f.user, "savings", "0002")
	fund := uuid.New()
	f.save(t, fund, goal("Fondo", savings, nov26, plan.ScopeFrom))
	mv := f.line(t, card, on(nov26, 2), -110000, store.KindExpense, "TIENDA DE FOTO X")
	link := func(item uuid.UUID, month, movement, decision string) string {
		s := fmt.Sprintf(`{"id": %q, "item_id": %q, "month": %q, "decision": %q`, uuid.New(), item, month, decision)
		if movement != "" {
			s += fmt.Sprintf(`, "movement_id": %q`, movement)
		}
		return s + "}"
	}
	for _, tt := range []struct {
		body          string
		status        int
		code, message string
		fields        map[string]string
	}{
		{link(uuid.MustParse(id), "2026-11", "", "paid"), 422, "validation_failed", "", map[string]string{
			"decision": "Elige el movimiento con que la compraste."}},
		{link(fund, "2026-11", mv.String(), "assign"), 422, "validation_failed", "", map[string]string{
			"item_id": "Solo un ingreso, un pago fijo o una compra se reconocen por un movimiento."}},
		// It can be bought from November to the month after March.
		{link(uuid.MustParse(id), "2027-05", mv.String(), "assign"), 404, "plan_item_not_found", "Eso ya no está en tu plan.", nil},
	} {
		rec := f.serve(t, f.user, "POST", "/links", tt.body)
		e := failure(t, rec)
		if rec.Code != tt.status || e.Error.Code != tt.code || tt.message != "" && e.Error.Message != tt.message ||
			tt.fields != nil && !mapsEqual(e.Error.Fields, tt.fields) {
			t.Errorf("POST %s = %d %s; want %d %s %v", tt.body, rec.Code, rec.Body, tt.status, tt.code, tt.fields)
		}
	}
	rec = f.serve(t, f.user, "POST", "/links", link(uuid.MustParse(id), "2026-11", mv.String(), "assign"))
	if rec.Code != http.StatusCreated {
		t.Errorf("POST the movement that bought it = %d %s", rec.Code, rec.Body)
	}

	// Taken out from the month it was planned in, nothing is left of it.
	if rec := f.serve(t, f.user, "DELETE", "/items/"+id+"?month=2026-11&scope=from", ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE = %d %s", rec.Code, rec.Body)
	}
	if n := storetest.Count(t, f.db, "plan_items WHERE kind = 'purchase'"); n != 0 {
		t.Errorf("%d purchases left", n)
	}
}

func TestThePlanShowsPurchasesAndGoalsOnTheWire(t *testing.T) {
	f := newFixture(t)
	card := f.account(t, f.user, "credit_card", "**** 1111")
	savings := f.account(t, f.user, "savings", "0001")
	camID, lensID, fund := uuid.New(), uuid.New(), uuid.New()
	f.save(t, camID, camera(nov26, mar27, 110000, plan.FundingMonthly))
	f.save(t, lensID, plan.ItemInput{Kind: plan.KindPurchase, Month: nov26, Name: "Lente", Amount: 30000,
		MatchText: "TIENDA DE LENTES X", TargetMonth: nov26, Funding: plan.FundingThatMonth})
	target := money.Cents(1000000)
	in := goal("Fondo de emergencia", savings, nov26, plan.ScopeFrom)
	in.Target, in.TargetMonth = &target, oct26.AddMonths(12)
	f.save(t, fund, in)
	f.line(t, savings, on(nov26, 3), 345000, store.KindTransfer, "TRASPASO DE CORRIENTE")
	lens := f.line(t, card, on(nov26, 4), -31000, store.KindExpense, "TIENDA DE LENTES X")

	rec := f.serve(t, f.user, "GET", "/?month=2026-11", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	var v struct {
		Summary   map[string]any   `json:"summary"`
		Purchases []map[string]any `json:"purchases"`
		Savings   []map[string]any `json:"savings"`
	}
	decode(t, rec, &v)
	cents := func(a any) any {
		if m, ok := a.(map[string]any); ok {
			return m["cents"]
		}
		return a
	}
	if len(v.Purchases) != 2 {
		t.Fatalf("purchases = %v", v.Purchases)
	}
	cam, bought := v.Purchases[0], v.Purchases[1]
	for field, want := range map[string]any{"id": camID.String(), "status": "setting_aside", "share": float64(22000),
		"set_aside_before": float64(0), "paid": nil, "bought_on": nil, "funding": "monthly", "amount": float64(110000),
		"target_month": "2027-03", "start_month": "2026-11"} {
		if fmt.Sprint(cents(cam[field])) != fmt.Sprint(want) {
			t.Errorf("the camera's %s = %v, want %v", field, cam[field], want)
		}
	}
	for _, list := range []string{"movements", "candidates"} {
		if l, ok := cam[list].([]any); !ok || len(l) != 0 {
			t.Errorf("the camera's %s = %v, want an empty list", list, cam[list])
		}
	}
	for field, want := range map[string]any{"id": lensID.String(), "status": "bought", "share": float64(30000),
		"paid": float64(31000), "bought_on": "2026-11-04"} {
		if fmt.Sprint(cents(bought[field])) != fmt.Sprint(want) {
			t.Errorf("the lens's %s = %v, want %v", field, bought[field], want)
		}
	}
	if mvs, _ := bought["movements"].([]any); len(mvs) != 1 || mvs[0].(map[string]any)["id"] != lens.String() ||
		mvs[0].(map[string]any)["how"] != "auto" {
		t.Errorf("the lens's movements = %v", bought["movements"])
	}
	// $220.00 of the camera, and the lens's $310.00, all in its month.
	if fmt.Sprint(cents(v.Summary["purchases"])) != fmt.Sprint(float64(22000+31000)) {
		t.Errorf("summary purchases = %v", v.Summary["purchases"])
	}

	if len(v.Savings) != 1 {
		t.Fatalf("savings = %v", v.Savings)
	}
	// $3,450.00 of $10,000.00 by October 2027, twelve months from now:
	// $545.84 a month, rounded up.
	for field, want := range map[string]any{"status": "done", "saved_month": float64(345000), "progress": float64(345000),
		"needed_per_month": float64(54584), "target": float64(1000000), "target_month": "2027-10"} {
		if fmt.Sprint(cents(v.Savings[0][field])) != fmt.Sprint(want) {
			t.Errorf("the goal's %s = %v, want %v", field, v.Savings[0][field], want)
		}
	}
}
