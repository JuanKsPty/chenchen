package plan_test

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

var fundings = []string{plan.FundingMonthly, plan.FundingThatMonth, plan.FundingSavings}

// shareOf is what versions set aside in month m.
func shareOf(versions []plan.Version, m plan.Month) money.Cents {
	if v := holding(versions, m); v != nil {
		return v.Amount
	}
	return 0
}

// written gives the versions Shares inserted an id, as the database would.
func written(versions []plan.Version) []plan.Version {
	out := slices.Clone(versions)
	for i := range out {
		if out[i].ID == uuid.Nil {
			out[i].ID = uuid.New()
		}
	}
	return out
}

func TestSharesAddUpToExactlyThePrice(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 2026))
	for n := range 3000 {
		// A purchase planned in some month, then edited in a later one with
		// another price, month or way of setting it aside.
		first := mo(1 + r.IntN(3))
		var stored []plan.Version
		if r.IntN(4) > 0 {
			target := first.AddMonths(r.IntN(6))
			after, _ := plan.Shares(nil, first, money.Cents(1+r.IntN(500000)), target, fundings[r.IntN(3)])
			stored = written(after)
		}
		m := first.AddMonths(r.IntN(4))
		target := m.AddMonths(r.IntN(8))
		price := money.Cents(1 + r.IntN(500000))
		if r.IntN(10) == 0 {
			price = money.Cents(1 + r.IntN(12))
		}
		funding := fundings[r.IntN(3)]
		after, ops := plan.Shares(stored, m, price, target, funding)

		var already, total money.Cents
		for x := mo(0); !x.After(mo(30)); x = x.AddMonths(1) {
			share := shareOf(after, x)
			total += share
			if x.Before(m) {
				if share != shareOf(stored, x) {
					t.Fatalf("#%d: %s set aside %s, then %s after an edit from %s", n, x, shareOf(stored, x), share, m)
				}
				already += share
				continue
			}
			if share != 0 && (x.After(target) || funding == plan.FundingSavings ||
				funding == plan.FundingThatMonth && x != target) {
				t.Fatalf("#%d: %s funded %s by %s sets %s aside in %s", n, funding, m, target, share, x)
			}
		}
		want := max(price, already)
		if funding == plan.FundingSavings {
			want = already
		}
		if total != want {
			t.Fatalf("#%d: %s %s from %s to %s over %s already set aside comes to %s, want %s",
				n, funding, price, m, target, already, total, want)
		}
		if funding == plan.FundingMonthly && price > already {
			// Spread evenly: no month a cent more than another but the
			// rounding.
			lo, hi := price, money.Cents(0)
			for x := m; !x.After(target); x = x.AddMonths(1) {
				lo, hi = min(lo, shareOf(after, x)), max(hi, shareOf(after, x))
			}
			if hi-lo > 1 {
				t.Fatalf("#%d: %s spread from %s to %s ranges %s to %s", n, price-already, m, target, lo, hi)
			}
		}
		for _, v := range after {
			if v.Open() || v.Amount <= 0 {
				t.Fatalf("#%d: a share %v has no end or no amount", n, v)
			}
		}
		checkWrites(t, n, stored, after, ops)
		// Saving it again in the same month writes nothing.
		if _, again := plan.Shares(written(after), m, price, target, funding); len(again) > 0 {
			t.Fatalf("#%d: the same edit again writes %v", n, again)
		}
	}
}

// checkWrites makes ops one at a time over stored and checks they never
// overlap two versions on the way, and end where after says.
func checkWrites(t *testing.T, n int, stored, after []plan.Version, ops []plan.Op) {
	t.Helper()
	rows := slices.Clone(stored)
	for i, op := range ops {
		at := slices.IndexFunc(rows, func(v plan.Version) bool { return v.ID == op.Version.ID })
		switch op.Kind {
		case plan.OpDelete:
			rows = slices.Delete(rows, at, at+1)
		case plan.OpUpdate:
			rows[at] = op.Version
		case plan.OpInsert:
			rows = append(rows, op.Version)
		}
		for j := range rows {
			for k := j + 1; k < len(rows); k++ {
				if rows[j].Overlaps(rows[k]) {
					t.Fatalf("#%d: after op %d %v, %v and %v overlap", n, i, op, rows[j], rows[k])
				}
			}
		}
	}
	slices.SortFunc(rows, func(a, b plan.Version) int { return a.From.Compare(b.From) })
	if len(rows) != len(after) {
		t.Fatalf("#%d: the writes leave %v, want %v", n, rows, after)
	}
	for i := range rows {
		if rows[i].From != after[i].From || rows[i].To != after[i].To || rows[i].Values != after[i].Values {
			t.Fatalf("#%d: the writes leave %v, want %v", n, rows, after)
		}
	}
}

// bought is a purchase bought in month by movements.
func bought(month plan.Month, movements ...store.PlanMovement) plan.Bought {
	b := plan.Bought{Month: month}
	for _, mv := range movements {
		b.Movements = append(b.Movements, plan.LineMovement{PlanMovement: mv})
		b.Paid -= mv.Amount
		if b.On.IsZero() && mv.Amount < 0 {
			b.On = mv.BookedOn
		}
	}
	return b
}

func outflow(day civil.Date, amount money.Cents, text string) store.PlanMovement {
	return store.PlanMovement{ID: uuid.New(), Origin: store.OriginStatement, BookedOn: day, Amount: -amount,
		Kind: store.KindExpense, Description: text}
}

func TestAPurchaseCostsThePlanWhatItCostOverItsMonths(t *testing.T) {
	r := rand.New(rand.NewPCG(13, 2026))
	for n := range 2000 {
		start := mo(1 + r.IntN(3))
		target := start.AddMonths(r.IntN(5))
		price := money.Cents(1000 + r.IntN(500000))
		funding := fundings[r.IntN(3)]
		shares, _ := plan.Shares(nil, start, price, target, funding)
		item := store.PlanItem{ID: uuid.New(), Kind: plan.KindPurchase, Name: "Cámara", Target: &price,
			StartMonth: start.First(), TargetMonth: target.First(), Funding: funding}
		p := plan.Purchase{Item: &item, Shares: shares}

		var b plan.Bought
		buy := start.AddMonths(r.IntN(target.Sub(start) + 2))
		paid := price * money.Cents(80+r.IntN(41)) / 100
		if r.IntN(4) > 0 {
			b = bought(buy, outflow(buy.DayIn(1+r.IntN(28)), paid, "TIENDA DE FOTO X"))
		}
		var total money.Cents
		for m := start.AddMonths(-1); !m.After(target.AddMonths(2)); m = m.AddMonths(1) {
			pl := plan.Compute(plan.Input{Month: m, Today: civil.Date{Year: 2028, Month: time.January, Day: 1},
				Purchases: []plan.Purchase{p}, Bought: []plan.Bought{b}, Movements: movementsOf(b)})
			if pl.Summary.Purchases < 0 {
				t.Fatalf("#%d: %s takes %s", n, m, pl.Summary.Purchases)
			}
			total += pl.Summary.Purchases
			for _, l := range pl.Purchases {
				if (l.Status == plan.StatusBought) != (m == b.Month) {
					t.Fatalf("#%d: %s is %s, bought in %s", n, m, l.Status, b.Month)
				}
			}
			// What bought it never counts in what was free.
			if pl.Summary.Spent != 0 || pl.Charged != pl.Spending {
				t.Fatalf("#%d: %s spent %s of what was free, charged %s of %s", n, m, pl.Summary.Spent, pl.Charged, pl.Spending)
			}
		}
		// Not bought, it set its price aside, unless paid with savings. Bought,
		// it cost the plan what it cost, but never less than what the months
		// before had set aside, which stays as it was.
		want := p.SetAsideBefore(target.AddMonths(1))
		if !b.Month.IsZero() {
			want = p.SetAsideBefore(b.Month)
			if funding != plan.FundingSavings {
				want = max(paid, want)
			}
		}
		if total != want {
			t.Fatalf("#%d: %s %s planned %s–%s, bought %s for %s, took %s in all, want %s",
				n, funding, price, start, target, b.Month, paid, total, want)
		}
	}
}

func movementsOf(b plan.Bought) []store.PlanMovement {
	var out []store.PlanMovement
	for _, m := range b.Movements {
		out = append(out, m.PlanMovement)
	}
	return out
}

func TestRecognisingAPurchaseNeverTakesWhatPaysSomethingElse(t *testing.T) {
	price := money.Cents(50000)
	newPurchase := func(matchText string, created int) plan.Purchase {
		item := store.PlanItem{ID: uuid.New(), Kind: plan.KindPurchase, Name: "Cámara", MatchText: matchText,
			Target: &price, StartMonth: nov26.First(), TargetMonth: dec26.First(), Funding: plan.FundingMonthly,
			CreatedAt: time.Unix(int64(created), 0)}
		return plan.Purchase{Item: &item}
	}
	insurance := store.PlanItem{ID: uuid.New(), Kind: plan.KindFixed, Name: "Seguro", MatchText: "SEGURO X"}
	instances := plan.Instances([]store.PlanItem{insurance}, []store.PlanVersion{{ID: uuid.New(), ItemID: insurance.ID,
		Kind: plan.KindFixed, From: nov26.First(), Amount: 48000, Day: 10}}, nov26, dec26)

	// The insurance's own payment is about the price, and so is a payment
	// the owner said is next month's insurance: neither is the camera.
	its := outflow(on(dec26, 10), 48500, "SEGURO X CAMARA")
	said := outflow(on(dec26, 28), 50000, "TIENDA DE FOTO X")
	links := []plan.Link{{ID: uuid.New(), ItemID: insurance.ID, Month: jan27, Decision: plan.DecisionAssign, Movement: said.ID}}
	camera := newPurchase("CAMARA", 1)
	got := plan.RecognisePurchases([]plan.Purchase{camera}, instances, []store.PlanMovement{its, said}, links, nil)
	if !got[0].Month.IsZero() || len(got[0].Candidates) != 0 {
		t.Errorf("the camera was bought by %+v, or could be %+v", got[0].Movements, got[0].Candidates)
	}
	// Unless the owner says the insurance's was the camera: their word takes
	// it from the payment, which then waits for another.
	mine := plan.Link{ID: uuid.New(), ItemID: camera.Item.ID, Month: dec26, Decision: plan.DecisionAssign, Movement: its.ID}
	got = plan.RecognisePurchases([]plan.Purchase{camera}, instances, []store.PlanMovement{its, said},
		append(links, mine), nil)
	if got[0].Month != dec26 || len(got[0].Movements) != 1 || got[0].Movements[0].ID != its.ID {
		t.Errorf("what the owner said bought the camera: %+v", got[0])
	}
	rec := plan.MatchBeside(instances, []store.PlanMovement{its, said}, nil, nil, got)
	if rec.Paid[0] != plan.Unpaid {
		t.Errorf("the insurance still took the camera's outflow: %+v", rec)
	}

	// Two purchases one outflow could be: the one made first takes it, and
	// the other has none left.
	shop := outflow(on(dec26, 5), 50000, "TIENDA DE FOTO X")
	first, second := newPurchase("TIENDA DE FOTO", 1), newPurchase("", 2)
	got = plan.RecognisePurchases([]plan.Purchase{second, first}, nil, []store.PlanMovement{shop}, nil, nil)
	if got[1].Month != dec26 || got[1].Paid != 50000 || !got[0].Month.IsZero() || len(got[0].Candidates) != 0 {
		t.Errorf("the first made got %+v, the second %+v", got[1], got[0])
	}

	// One the owner said bought it does, whatever its text and amount; one
	// said after not to be it is not.
	cheap := outflow(on(dec26, 6), 1000, "OTRA TIENDA")
	links = []plan.Link{
		{ID: uuid.New(), ItemID: first.Item.ID, Month: dec26, Decision: plan.DecisionAssign, Movement: cheap.ID},
		{ID: uuid.New(), ItemID: second.Item.ID, Month: dec26, Decision: plan.DecisionAssign, Movement: shop.ID},
		{ID: uuid.New(), ItemID: second.Item.ID, Month: dec26, Decision: plan.DecisionReject, Movement: shop.ID},
	}
	got = plan.RecognisePurchases([]plan.Purchase{first, second}, nil, []store.PlanMovement{shop, cheap}, links, nil)
	if got[0].Month != dec26 || got[0].Paid != 1000 || len(got[0].Movements) != 1 || got[0].Movements[0].Link != links[0].ID {
		t.Errorf("what the owner said bought the first: %+v", got[0])
	}
	if !got[1].Month.IsZero() || len(got[1].Candidates) != 0 {
		t.Errorf("the second, refused its outflow, got %+v", got[1])
	}
}
