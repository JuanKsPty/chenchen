package plan_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

var (
	oct26 = plan.Month{Year: 2026, Month: time.October}
	nov26 = plan.Month{Year: 2026, Month: time.November}
	dec26 = plan.Month{Year: 2026, Month: time.December}
	jan27 = plan.Month{Year: 2027, Month: time.January}
	feb27 = plan.Month{Year: 2027, Month: time.February}
)

type fixture struct {
	db   *store.DB
	svc  *plan.Service
	user uuid.UUID
}

// newFixture stands the plan on the 6th of November 2026, in Panama.
func newFixture(t *testing.T) fixture {
	t.Helper()
	db := storetest.New(t)
	svc := plan.NewService(db)
	svc.SetNow(func() time.Time { return time.Date(2026, 11, 6, 9, 0, 0, 0, civil.Panama) })
	return fixture{db: db, svc: svc, user: newUser(t, db, "o@example.com")}
}

func newUser(t *testing.T, db *store.DB, email string) uuid.UUID {
	t.Helper()
	u, err := db.Q().CreateUser(context.Background(), store.User{ID: uuid.New(), Email: email, PasswordHash: "x", FullName: email})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func (f fixture) category(t *testing.T, user uuid.UUID, name, kind string) uuid.UUID {
	t.Helper()
	c, err := f.db.Q().CreateCategory(context.Background(), store.Category{ID: uuid.New(), UserID: user, Name: name, Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func (f fixture) account(t *testing.T, user uuid.UUID, typ, number string) uuid.UUID {
	t.Helper()
	a := store.Account{ID: uuid.New(), UserID: user, Class: "asset", Type: typ, Institution: "bac",
		ExternalNumber: number, Currency: "USD", DisplayName: number}
	switch typ {
	case "brokerage":
		a.Institution = "ibkr"
	case "credit_card":
		a.Class = "liability"
	}
	got, _, err := f.db.Q().EnsureAccount(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	return got.ID
}

// payment is a fixed payment of amount on day, from m on.
func payment(m plan.Month, amount money.Cents, day int) plan.ItemInput {
	return plan.ItemInput{Kind: plan.KindFixed, Month: m, Scope: plan.ScopeFrom, Name: "Pago a persona uno",
		Amount: amount, Day: day, MatchText: "YAPPY A PERSONA UNO"}
}

func (f fixture) save(t *testing.T, id uuid.UUID, in plan.ItemInput) store.PlanItem {
	t.Helper()
	item, _, err := f.svc.SaveItem(context.Background(), f.user, id, in)
	if err != nil {
		t.Fatalf("SaveItem(%s %s from %s): %v", in.Kind, in.Scope, in.Month, err)
	}
	return item
}

func (f fixture) versions(t *testing.T, id uuid.UUID) []store.PlanVersion {
	t.Helper()
	vs, err := f.db.Q().PlanItemVersions(context.Background(), f.user, id)
	if err != nil {
		t.Fatal(err)
	}
	return vs
}

// span is a stored version as from, to (or "on") and its values, for
// comparing what is stored with what a test expects.
type span struct {
	from, to plan.Month
	amount   money.Cents
	day      int
}

func spansOf(vs []store.PlanVersion) []span {
	out := make([]span, 0, len(vs))
	for _, v := range vs {
		s := span{from: plan.MonthOf(v.From), amount: v.Amount, day: v.Day}
		if !v.To.IsZero() {
			s.to = plan.MonthOf(v.To)
		}
		out = append(out, s)
	}
	return out
}

func wantSpans(t *testing.T, got []store.PlanVersion, want ...span) {
	t.Helper()
	spans := spansOf(got)
	if len(spans) != len(want) {
		t.Fatalf("versions = %+v, want %+v", spans, want)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Fatalf("versions = %+v, want %+v", spans, want)
		}
	}
}

func TestEditingFromAMonthClosesTheOldVersionTheMonthBefore(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	f.save(t, id, payment(nov26, 3267, 5))
	first := f.versions(t, id)[0].ID

	f.save(t, id, payment(jan27, 3500, 5))

	got := f.versions(t, id)
	wantSpans(t, got, span{nov26, dec26, 3267, 5}, span{jan27, plan.Month{}, 3500, 5})
	if got[0].ID != first {
		t.Error("the version that held until December is not the one that was there")
	}
}

func TestEditingOnlyOneMonthLeavesTheMonthsAroundIt(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	f.save(t, id, payment(nov26, 3267, 5))

	in := payment(dec26, 5000, 8)
	in.Scope = plan.ScopeOnly
	f.save(t, id, in)

	wantSpans(t, f.versions(t, id),
		span{nov26, nov26, 3267, 5}, span{dec26, dec26, 5000, 8}, span{jan27, plan.Month{}, 3267, 5})
}

func TestEditingAVersionsFirstMonthChangesItInPlace(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	f.save(t, id, payment(nov26, 3267, 5))
	f.save(t, id, payment(jan27, 3500, 5))
	before := f.versions(t, id)

	f.save(t, id, payment(jan27, 3600, 6))
	f.save(t, id, payment(nov26, 3300, 5))

	got := f.versions(t, id)
	// From November on, January's change gives way to November's.
	wantSpans(t, got, span{nov26, plan.Month{}, 3300, 5})
	if got[0].ID != before[0].ID {
		t.Error("the November version was replaced rather than changed")
	}
}

func TestRepeatingAnEditChangesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := uuid.New()
	in := payment(nov26, 3267, 5)
	first, created, err := f.svc.SaveItem(ctx, f.user, id, in)
	if err != nil || !created {
		t.Fatalf("first SaveItem = %v, created %v", err, created)
	}
	// A form that lost the answer to its first send sends it again.
	if _, created, err := f.svc.SaveItem(ctx, f.user, id, in); err != nil || created {
		t.Fatalf("SaveItem again = %v, created %v; want saved once", err, created)
	}
	wantSpans(t, f.versions(t, id), span{nov26, plan.Month{}, 3267, 5})
	only := payment(jan27, 4000, 5)
	only.Scope = plan.ScopeOnly
	f.save(t, id, only)
	before := f.versions(t, id)

	// The last edit again, and edits that say what a month already holds.
	// (The first save again is not one of them: from November on, it would
	// undo January's.)
	december, march := payment(dec26, 3267, 5), payment(plan.Month{Year: 2027, Month: time.March}, 3267, 5)
	december.Scope, march.Scope = plan.ScopeOnly, plan.ScopeOnly
	for _, again := range []plan.ItemInput{only, december, march, only} {
		item, created, err := f.svc.SaveItem(ctx, f.user, id, again)
		if err != nil {
			t.Fatal(err)
		}
		if created {
			t.Error("saving an item again created it again")
		}
		if !item.UpdatedAt.Equal(first.UpdatedAt) {
			t.Error("saving the same item again rewrote it")
		}
	}
	after := f.versions(t, id)
	if len(after) != len(before) {
		t.Fatalf("versions after repeating = %+v, want %+v", spansOf(after), spansOf(before))
	}
	for i := range after {
		if after[i] != before[i] {
			t.Fatalf("version %d after repeating = %+v, want %+v", i, after[i], before[i])
		}
	}
	if n := storetest.Count(t, f.db, "plan_items"); n != 1 {
		t.Errorf("items = %d, want 1", n)
	}
}

func TestEqualNeighbouringVersionsMerge(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	f.save(t, id, payment(nov26, 1000, 4))
	first := f.versions(t, id)[0].ID

	in := payment(jan27, 1500, 4)
	in.Scope = plan.ScopeOnly
	f.save(t, id, in)
	wantSpans(t, f.versions(t, id),
		span{nov26, dec26, 1000, 4}, span{jan27, jan27, 1500, 4}, span{feb27, plan.Month{}, 1000, 4})

	in.Amount = 1000
	f.save(t, id, in)
	got := f.versions(t, id)
	wantSpans(t, got, span{nov26, plan.Month{}, 1000, 4})
	if got[0].ID != first {
		t.Error("the joined version is not the one that was there")
	}
}

func TestOverlappingVersionsAreRefusedByTheDatabase(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	q := f.db.Q()
	id := uuid.New()
	f.save(t, id, payment(nov26, 3267, 5))
	if err := f.svc.DeleteItem(ctx, f.user, id, jan27, plan.ScopeFrom); err != nil {
		t.Fatal(err)
	}
	held := f.versions(t, id)[0] // November and December

	version := func(from, to plan.Month) store.PlanVersion {
		v := store.PlanVersion{ID: uuid.New(), UserID: f.user, ItemID: id, Kind: plan.KindFixed,
			From: from.First(), Amount: 100, Day: 5}
		if !to.IsZero() {
			v.To = to.First()
		}
		return v
	}
	for _, v := range []store.PlanVersion{version(dec26, dec26), version(oct26, plan.Month{}), version(oct26, nov26)} {
		if err := q.InsertPlanVersion(ctx, v); !store.IsConstraint(err, store.PlanVersionsOverlapConstraint) {
			t.Errorf("a version from %s to %s over November and December = %v, want refused", v.From, v.To, err)
		}
	}
	if err := q.InsertPlanVersion(ctx, version(jan27, plan.Month{})); err != nil {
		t.Fatalf("a version starting the month after = %v, want accepted", err)
	}
	held.To = jan27.First()
	if err := q.UpdatePlanVersion(ctx, held); !store.IsConstraint(err, store.PlanVersionsOverlapConstraint) {
		t.Errorf("stretching a version over the next one = %v, want refused", err)
	}

	// Another item's months are its own.
	other := uuid.New()
	f.save(t, other, payment(nov26, 263, 4))
	if n := len(f.versions(t, other)); n != 1 {
		t.Errorf("the other item has %d versions, want 1", n)
	}

	// And a category's caps are kept apart the same way.
	food := f.category(t, f.user, "Alimentación", "expense")
	if err := f.svc.SaveCap(ctx, f.user, food, plan.CapInput{Month: nov26, Scope: plan.ScopeFrom, Amount: 30000}); err != nil {
		t.Fatal(err)
	}
	err := q.InsertPlanCap(ctx, store.PlanCap{ID: uuid.New(), UserID: f.user, CategoryID: food,
		From: dec26.First(), To: dec26.First(), Amount: 100})
	if !store.IsConstraint(err, store.PlanCapsOverlapConstraint) {
		t.Errorf("a second cap over December = %v, want refused", err)
	}
}

func TestAPlanItemCannotPointAtAnotherPersonsCategoryOrAccount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	other := newUser(t, f.db, "otra@example.com")
	theirCategory := f.category(t, other, "Servicios", "expense")
	theirAccount := f.account(t, other, "savings", "0002")

	// Asked of the plan, someone else's category or account does not exist.
	in := payment(nov26, 3267, 5)
	in.CategoryID = &theirCategory
	if _, _, err := f.svc.SaveItem(ctx, f.user, uuid.New(), in); !errors.Is(err, categories.ErrNotOwned) {
		t.Errorf("a payment filed under someone else's category = %v, want not found", err)
	}
	if _, _, err := f.svc.SaveItem(ctx, f.user, uuid.New(), goal("Viaje", theirAccount, nov26, plan.ScopeFrom)); !errors.Is(err, accounts.ErrNotFound) {
		t.Errorf("a saving in someone else's account = %v, want not found", err)
	}
	capped := plan.CapInput{Month: nov26, Scope: plan.ScopeFrom, Amount: 1000}
	if err := f.svc.SaveCap(ctx, f.user, theirCategory, capped); !errors.Is(err, categories.ErrNotOwned) {
		t.Errorf("a cap on someone else's category = %v, want not found", err)
	}
	if n := storetest.Count(t, f.db, "plan_items"); n != 0 {
		t.Errorf("plan items = %d, want none", n)
	}
	if n := storetest.Count(t, f.db, "plan_caps"); n != 0 {
		t.Errorf("caps = %d, want none", n)
	}

	// And the database refuses it whatever the code above it does.
	q := f.db.Q()
	_, err := q.InsertPlanItem(ctx, store.PlanItem{ID: uuid.New(), UserID: f.user, Kind: plan.KindFixed,
		Name: "Luz", CategoryID: &theirCategory})
	if !store.IsConstraint(err, store.PlanItemCategoryConstraint) {
		t.Errorf("inserting an item under someone else's category = %v, want refused", err)
	}
	_, err = q.InsertPlanItem(ctx, store.PlanItem{ID: uuid.New(), UserID: f.user, Kind: plan.KindSaving,
		Name: "Viaje", AccountID: &theirAccount})
	if !store.IsConstraint(err, store.PlanItemAccountConstraint) {
		t.Errorf("inserting a saving in someone else's account = %v, want refused", err)
	}
	err = q.InsertPlanCap(ctx, store.PlanCap{ID: uuid.New(), UserID: f.user, CategoryID: theirCategory,
		From: nov26.First(), Amount: 1000})
	if !store.IsConstraint(err, store.PlanCapCategoryConstraint) {
		t.Errorf("inserting a cap on someone else's category = %v, want refused", err)
	}
	theirs, err := q.InsertPlanItem(ctx, store.PlanItem{ID: uuid.New(), UserID: other, Kind: plan.KindFixed, Name: "Luz"})
	if err != nil {
		t.Fatal(err)
	}
	err = q.InsertPlanVersion(ctx, store.PlanVersion{ID: uuid.New(), UserID: f.user, ItemID: theirs.ID,
		Kind: plan.KindFixed, From: nov26.First(), Amount: 1000, Day: 1})
	if !store.IsConstraint(err, store.PlanVersionItemKindConstraint) {
		t.Errorf("inserting a version of someone else's item = %v, want refused", err)
	}
}

func TestDeletingACategoryDropsItsCapAndUnfilesItsItems(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	services := f.category(t, f.user, "Servicios", "expense")
	if err := f.svc.SaveCap(ctx, f.user, services, plan.CapInput{Month: nov26, Scope: plan.ScopeFrom, Amount: 8000}); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	in := payment(nov26, 263, 4)
	in.CategoryID = &services
	f.save(t, id, in)

	if err := f.db.Q().DeleteCategory(ctx, services); err != nil {
		t.Fatal(err)
	}

	if n := storetest.Count(t, f.db, "plan_caps"); n != 0 {
		t.Errorf("caps after deleting their category = %d, want none", n)
	}
	item, err := f.db.Q().PlanItemByID(ctx, f.user, id)
	if err != nil {
		t.Fatalf("the payment filed under the category: %v", err)
	}
	if item.CategoryID != nil {
		t.Error("the payment still points at the deleted category")
	}
	wantSpans(t, f.versions(t, id), span{nov26, plan.Month{}, 263, 4})
}

func TestDeletingAnAccountUnlinksItsGoal(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	savings := f.account(t, f.user, "savings", "0001")
	target := money.Cents(1_000_000)
	id := uuid.New()
	f.save(t, id, plan.ItemInput{Kind: plan.KindSaving, Month: nov26, Scope: plan.ScopeFrom, Name: "Fondo",
		Amount: 20000, AccountID: &savings, Target: &target, TargetMonth: plan.Month{Year: 2027, Month: time.December}})

	if err := f.db.Q().DeleteAccount(ctx, f.user, savings); err != nil {
		t.Fatal(err)
	}

	item, err := f.db.Q().PlanItemByID(ctx, f.user, id)
	if err != nil {
		t.Fatalf("the goal of the deleted account: %v", err)
	}
	if item.AccountID != nil {
		t.Error("the goal still points at the deleted account")
	}
	if item.Target == nil || *item.Target != target || item.TargetMonth != (civil.Date{Year: 2027, Month: time.December, Day: 1}) {
		t.Errorf("the goal lost its target: %v by %s", item.Target, item.TargetMonth)
	}
	wantSpans(t, f.versions(t, id), span{nov26, plan.Month{}, 20000, 0})
}

func TestAnItemsKindCannotChange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := uuid.New()
	f.save(t, id, payment(nov26, 3267, 5))

	as := payment(nov26, 3267, 5)
	as.Kind = plan.KindIncome
	if _, _, err := f.svc.SaveItem(ctx, f.user, id, as); !errors.Is(err, plan.ErrKindFixed) {
		t.Errorf("saving a payment as income = %v, want ErrKindFixed", err)
	}

	// Its versions repeat its kind, so the database refuses it too.
	err := storetest.Exec(t, f.db, `UPDATE plan_items SET kind = 'income' WHERE id = $1`, id)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != store.PlanVersionItemKindConstraint {
		t.Errorf("changing an item's kind in the database = %v, want refused by %s", err, store.PlanVersionItemKindConstraint)
	}
	item, err := f.db.Q().PlanItemByID(ctx, f.user, id)
	if err != nil || item.Kind != plan.KindFixed {
		t.Errorf("the item is %q (%v), want still a payment", item.Kind, err)
	}
}

func TestThePlanIsWrittenFromThisMonthInPanamaToTwoYearsAhead(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, _, err := f.svc.SaveItem(ctx, f.user, uuid.New(), payment(oct26, 100, 1)); !errors.Is(err, plan.ErrMonthPast) {
		t.Errorf("planning October in November = %v, want ErrMonthPast", err)
	}
	if _, _, err := f.svc.SaveItem(ctx, f.user, uuid.New(), payment(nov26.AddMonths(25), 100, 1)); !errors.Is(err, plan.ErrMonthTooFar) {
		t.Errorf("planning 25 months ahead = %v, want ErrMonthTooFar", err)
	}
	f.save(t, uuid.New(), payment(nov26.AddMonths(24), 100, 1))
	if _, _, err := f.svc.SaveItem(ctx, f.user, uuid.New(), payment(plan.Month{}, 100, 1)); !errors.Is(err, plan.ErrInvalidMonth) {
		t.Errorf("planning no month = %v, want ErrInvalidMonth", err)
	}

	id := uuid.New()
	f.save(t, id, payment(nov26, 100, 1))
	if err := f.svc.DeleteItem(ctx, f.user, id, oct26, plan.ScopeFrom); !errors.Is(err, plan.ErrMonthPast) {
		t.Errorf("removing October in November = %v, want ErrMonthPast", err)
	}
	food := f.category(t, f.user, "Alimentación", "expense")
	if err := f.svc.SaveCap(ctx, f.user, food, plan.CapInput{Month: oct26, Scope: plan.ScopeFrom, Amount: 100}); !errors.Is(err, plan.ErrMonthPast) {
		t.Errorf("capping October in November = %v, want ErrMonthPast", err)
	}

	// At 10 p.m. on the 30th of November in Panama it is already the 1st of
	// December in UTC; November is still this month.
	f.svc.SetNow(func() time.Time { return time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC) })
	if _, _, err := f.svc.SaveItem(ctx, f.user, id, payment(nov26, 200, 1)); err != nil {
		t.Errorf("planning November on the night of the 30th in Panama = %v", err)
	}
}

func TestAnIDSomeoneElseUsedIsTaken(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	other := newUser(t, f.db, "otra@example.com")
	id := uuid.New()
	if _, _, err := f.svc.SaveItem(ctx, other, id, payment(nov26, 3267, 5)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.SaveItem(ctx, f.user, id, payment(nov26, 100, 1)); !errors.Is(err, plan.ErrItemIDTaken) {
		t.Errorf("saving with someone else's item id = %v, want ErrItemIDTaken", err)
	}
	if err := f.svc.DeleteItem(ctx, f.user, id, nov26, plan.ScopeFrom); !errors.Is(err, plan.ErrItemNotFound) {
		t.Errorf("removing someone else's item = %v, want ErrItemNotFound", err)
	}
	theirs, err := f.db.Q().PlanItemVersions(ctx, other, id)
	if err != nil || len(theirs) != 1 || theirs[0].Amount != 3267 {
		t.Errorf("the other person's item now holds %+v (%v)", theirs, err)
	}
}

func TestRemovingMonthsForgetsWhatWasSaidAboutThem(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := uuid.New()
	f.save(t, id, payment(nov26, 3267, 5))
	for _, m := range []plan.Month{nov26, dec26, jan27, feb27} {
		if err := storetest.Exec(t, f.db, `INSERT INTO plan_links (id, user_id, item_id, month, decision)
			VALUES ($1, $2, $3, $4, 'paid')`, uuid.New(), f.user, id, m.First()); err != nil {
			t.Fatal(err)
		}
	}
	linked := func() []plan.Month {
		var months []plan.Month
		for _, m := range []plan.Month{nov26, dec26, jan27, feb27} {
			if storetest.Count(t, f.db, "plan_links WHERE item_id = $1 AND month = $2", id, m.First()) == 1 {
				months = append(months, m)
			}
		}
		return months
	}

	if err := f.svc.DeleteItem(ctx, f.user, id, dec26, plan.ScopeOnly); err != nil {
		t.Fatal(err)
	}
	wantSpans(t, f.versions(t, id), span{nov26, nov26, 3267, 5}, span{jan27, plan.Month{}, 3267, 5})
	if got := linked(); len(got) != 3 || got[1] != jan27 {
		t.Errorf("links after taking December out are for %v, want November, January and February", got)
	}

	if err := f.svc.DeleteItem(ctx, f.user, id, jan27, plan.ScopeFrom); err != nil {
		t.Fatal(err)
	}
	wantSpans(t, f.versions(t, id), span{nov26, nov26, 3267, 5})
	if got := linked(); len(got) != 1 || got[0] != nov26 {
		t.Errorf("links after taking January on out are for %v, want November", got)
	}

	// The last month out takes the item, and what was said about it, along.
	if err := f.svc.DeleteItem(ctx, f.user, id, nov26, plan.ScopeOnly); err != nil {
		t.Fatal(err)
	}
	if n := storetest.Count(t, f.db, "plan_items"); n != 0 {
		t.Errorf("items after taking every month out = %d, want none", n)
	}
	if n := storetest.Count(t, f.db, "plan_links"); n != 0 {
		t.Errorf("links after taking every month out = %d, want none", n)
	}
}

// goal is a saving of 150.00 a month on account, from m on or for m alone.
func goal(name string, account uuid.UUID, m plan.Month, scope plan.Scope) plan.ItemInput {
	return plan.ItemInput{Kind: plan.KindSaving, Month: m, Scope: scope, Name: name, Amount: 15000, AccountID: &account}
}

func TestAnAccountBacksOneGoalAtATime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	savings := f.account(t, f.user, "savings", "0001")
	first := uuid.New()
	f.save(t, first, goal("Fondo", savings, nov26, plan.ScopeFrom))
	// Saving the same goal again is not a second goal.
	f.save(t, first, goal("Fondo de emergencia", savings, nov26, plan.ScopeFrom))

	second := uuid.New()
	for _, in := range []plan.ItemInput{
		goal("Viaje", savings, nov26, plan.ScopeFrom),
		// The first goal has no end, so even a later month is one of its.
		goal("Viaje", savings, feb27, plan.ScopeOnly),
	} {
		if _, _, err := f.svc.SaveItem(ctx, f.user, second, in); !errors.Is(err, plan.ErrAccountHasGoal) {
			t.Errorf("a second goal on the account %s %s = %v, want ErrAccountHasGoal", in.Scope, in.Month, err)
		}
	}

	checking := f.account(t, f.user, "checking", "0003")
	if _, _, err := f.svc.SaveItem(ctx, f.user, second, goal("Viaje", checking, nov26, plan.ScopeFrom)); !errors.Is(err, plan.ErrGoalAccount) {
		t.Errorf("a goal in a checking account = %v, want ErrGoalAccount", err)
	}
	if n := storetest.Count(t, f.db, "plan_items WHERE id = $1", second); n != 0 {
		t.Errorf("the refused goal was kept %d times", n)
	}
}

func TestAGoalCanStartOnAnAccountTheMonthAfterAnotherEnds(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	savings := f.account(t, f.user, "savings", "0001")
	first, second := uuid.New(), uuid.New()
	f.save(t, first, goal("Fondo", savings, nov26, plan.ScopeFrom))
	if err := f.svc.DeleteItem(ctx, f.user, first, jan27, plan.ScopeFrom); err != nil {
		t.Fatal(err)
	}
	// Still in November, the account is free from January on.
	f.save(t, second, goal("Viaje", savings, jan27, plan.ScopeFrom))

	// Neither can then reach into the other's months.
	if _, _, err := f.svc.SaveItem(ctx, f.user, first, goal("Fondo", savings, dec26, plan.ScopeFrom)); !errors.Is(err, plan.ErrAccountHasGoal) {
		t.Errorf("the first goal from December on = %v, want ErrAccountHasGoal", err)
	}
	if _, _, err := f.svc.SaveItem(ctx, f.user, second, goal("Viaje", savings, dec26, plan.ScopeOnly)); !errors.Is(err, plan.ErrAccountHasGoal) {
		t.Errorf("the second goal in December = %v, want ErrAccountHasGoal", err)
	}

	// Each can still change within its own months.
	more := goal("Fondo", savings, dec26, plan.ScopeOnly)
	more.Amount = 20000
	f.save(t, first, more)
	wantSpans(t, f.versions(t, first), span{nov26, nov26, 15000, 0}, span{dec26, dec26, 20000, 0})
	wantSpans(t, f.versions(t, second), span{jan27, plan.Month{}, 15000, 0})
}

func TestAGoalMovedToAnotherAccountTakesItsPastMonthsWithIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	savings := f.account(t, f.user, "savings", "0001")
	brokerage := f.account(t, f.user, "brokerage", "0002")
	ended, moving := uuid.New(), uuid.New()
	f.save(t, ended, goal("Viaje", savings, nov26, plan.ScopeFrom))
	if err := f.svc.DeleteItem(ctx, f.user, ended, dec26, plan.ScopeFrom); err != nil {
		t.Fatal(err)
	}
	f.save(t, moving, goal("Retiro", brokerage, nov26, plan.ScopeFrom))

	// In January the first goal is over, but the account holds for every
	// month of the goal that moves, November included, when it backed the
	// first one.
	f.svc.SetNow(func() time.Time { return time.Date(2027, 1, 10, 9, 0, 0, 0, civil.Panama) })
	if _, _, err := f.svc.SaveItem(ctx, f.user, moving, goal("Retiro", savings, jan27, plan.ScopeFrom)); !errors.Is(err, plan.ErrAccountHasGoal) {
		t.Errorf("moving a goal onto an account that backed another in its past = %v, want ErrAccountHasGoal", err)
	}
	item, err := f.db.Q().PlanItemByID(ctx, f.user, moving)
	if err != nil || item.AccountID == nil || *item.AccountID != brokerage {
		t.Errorf("the refused move left the goal on %v (%v), want still the brokerage account", item.AccountID, err)
	}

	// A new goal on that account, with no past, is free to start.
	f.save(t, uuid.New(), goal("Retiro", savings, jan27, plan.ScopeFrom))
}

func TestCapsAreKeptMonthByMonthLikeItems(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	food := f.category(t, f.user, "Alimentación", "expense")
	caps := func() []store.PlanVersion {
		stored, err := f.db.Q().PlanCategoryCaps(ctx, f.user, food)
		if err != nil {
			t.Fatal(err)
		}
		var out []store.PlanVersion
		for _, c := range stored {
			out = append(out, store.PlanVersion{ID: c.ID, From: c.From, To: c.To, Amount: c.Amount})
		}
		return out
	}
	save := func(m plan.Month, scope plan.Scope, amount money.Cents) {
		t.Helper()
		if err := f.svc.SaveCap(ctx, f.user, food, plan.CapInput{Month: m, Scope: scope, Amount: amount}); err != nil {
			t.Fatal(err)
		}
	}
	save(nov26, plan.ScopeFrom, 30000)
	save(dec26, plan.ScopeOnly, 40000)
	save(dec26, plan.ScopeOnly, 40000)
	if err := f.svc.DeleteCap(ctx, f.user, food, feb27, plan.ScopeFrom); err != nil {
		t.Fatal(err)
	}
	wantSpans(t, caps(), span{nov26, nov26, 30000, 0}, span{dec26, dec26, 40000, 0}, span{jan27, jan27, 30000, 0})

	salary := f.category(t, f.user, "Salario", "income")
	err := f.svc.SaveCap(ctx, f.user, salary, plan.CapInput{Month: nov26, Scope: plan.ScopeFrom, Amount: 100})
	if !errors.Is(err, plan.ErrExpenseCategory) {
		t.Errorf("a cap on an income category = %v, want ErrExpenseCategory", err)
	}
	if err := f.svc.DeleteCap(ctx, f.user, food, nov26, plan.ScopeFrom); err != nil {
		t.Fatal(err)
	}
	if n := storetest.Count(t, f.db, "plan_caps"); n != 0 {
		t.Errorf("caps after removing every month = %d, want none", n)
	}
}

func TestAPaycheckAndAPaymentNeedACategoryOfTheirOwnKind(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	salary := f.category(t, f.user, "Salario", "income")
	services := f.category(t, f.user, "Servicios", "expense")

	in := payment(nov26, 263, 4)
	in.CategoryID = &salary
	if _, _, err := f.svc.SaveItem(ctx, f.user, uuid.New(), in); !errors.Is(err, plan.ErrExpenseCategory) {
		t.Errorf("a payment under an income category = %v, want ErrExpenseCategory", err)
	}
	paycheck := plan.ItemInput{Kind: plan.KindIncome, Month: nov26, Scope: plan.ScopeFrom, Name: "Sueldo",
		Amount: 65000, Day: 16, MatchText: "ACH EMPRESA X", CategoryID: &services}
	if _, _, err := f.svc.SaveItem(ctx, f.user, uuid.New(), paycheck); !errors.Is(err, plan.ErrIncomeCategory) {
		t.Errorf("a paycheck under an expense category = %v, want ErrIncomeCategory", err)
	}
	paycheck.CategoryID = &salary
	paycheck.Name = "  Sueldo\tde la   quincena "
	paycheck.MatchText = " ACH EMPRESA X "
	item := f.save(t, uuid.New(), paycheck)
	if item.CategoryID == nil || *item.CategoryID != salary {
		t.Errorf("the paycheck is filed under %v, want Salario", item.CategoryID)
	}
	if item.Name != "Sueldo de la quincena" || item.MatchText != "ACH EMPRESA X" {
		t.Errorf("the paycheck was kept as %q recognised by %q, want its spaces tidied", item.Name, item.MatchText)
	}
}
