package plan

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// The kinds of a plan item.
const (
	// KindIncome is one expected paycheck or other income, one per deposit:
	// two paychecks a month are two items.
	KindIncome = store.PlanIncome
	// KindFixed is a payment that goes out every month.
	KindFixed = store.PlanFixed
	// KindSaving is what the owner sets aside each month, optionally tied to
	// the real account that holds it and to a goal.
	KindSaving = store.PlanSaving
	// KindPurchase is something the owner means to buy by a month, set aside
	// in the plan beforehand.
	KindPurchase = store.PlanPurchase
)

// MonthsAhead is how far after the current month the plan can be written:
// as far as the purchase picker reaches.
const MonthsAhead = 24

const (
	// maxNameRunes and maxMatchRunes match the CHECKs in 00021_plan.sql.
	maxNameRunes  = 60
	maxMatchRunes = 60
	maxDay        = 31
)

// Service keeps the owner's plan.
type Service struct {
	db  *store.DB
	now func() time.Time
}

func NewService(db *store.DB) *Service { return &Service{db: db, now: time.Now} }

// thisMonth is the current month in Panama. The server decides it, not the
// browser, so the web and Claude agree on which month has passed.
func (s *Service) thisMonth() Month { return MonthOf(civil.Of(s.now().In(civil.Panama))) }

// writable refuses an edit for a month the plan cannot be written in: one
// before the current month, whose plan stays as it was, or one more than
// MonthsAhead after it.
func (s *Service) writable(m Month) error {
	if m.IsZero() {
		return ErrInvalidMonth
	}
	now := s.thisMonth()
	switch {
	case m.Before(now):
		return fmt.Errorf("%w: %s", ErrMonthPast, m)
	case m.Sub(now) > MonthsAhead:
		return fmt.Errorf("%w: %s", ErrMonthTooFar, m)
	}
	return nil
}

// ItemInput is an item as the owner saves it: from Month on (ScopeFrom) or
// for Month alone (ScopeOnly), Amount on Day of each month. The rest holds for
// every month of the item.
//
// A planned purchase is saved from Month on, always: Amount is its price,
// TargetMonth the month it is to be bought in and Funding how it is set
// aside. The plan works out its shares (Shares).
type ItemInput struct {
	Kind   string
	Month  Month
	Scope  Scope
	Name   string
	Amount money.Cents
	// Day is 1 to 31; a paycheck and a payment need one, a saving may not.
	Day int
	// MatchText is the text its movements contain, compared like a rule's.
	// A saving has none: it is recognised by its account.
	MatchText string
	// CategoryID is an expense category for a payment, an income one for a
	// paycheck, and none for a saving.
	CategoryID *uuid.UUID
	// AccountID and Target are a saving's: the savings or brokerage account
	// that holds it, and its goal. TargetMonth is when a saving's goal is to
	// be reached, or the month a purchase is to be bought in.
	AccountID   *uuid.UUID
	Target      *money.Cents
	TargetMonth Month
	// StartMonth is from when a purchase is set aside: the month it is first
	// saved for, which the plan keeps, so a save never sends one. Funding is
	// how a purchase is set aside: FundingMonthly, FundingThatMonth or
	// FundingSavings.
	StartMonth Month
	Funding    string
}

// normalise cleans up what the owner typed and reports every field that is
// wrong at once, so a form hears of all its mistakes together. now is the
// current month, which bounds when a purchase can be planned for.
func (in *ItemInput) normalise(now Month) error {
	var errs []error
	switch in.Kind {
	case KindIncome, KindFixed, KindSaving, KindPurchase:
	default:
		errs = append(errs, ErrItemKind)
	}
	purchase := in.Kind == KindPurchase
	if purchase && in.Scope == "" {
		in.Scope = ScopeFrom
	}
	switch _, err := ParseScope(string(in.Scope)); {
	case err != nil:
		errs = append(errs, err)
	case purchase && in.Scope != ScopeFrom:
		// Its shares are worked out from the month on, so that they still
		// add up to the price; one month alone cannot change.
		errs = append(errs, ErrPurchaseScope)
	}
	name, err := normaliseName(in.Name)
	if err != nil {
		errs = append(errs, err)
	}
	in.Name = name
	if in.Amount <= 0 {
		errs = append(errs, ErrAmount)
	}
	needsDay := in.Kind == KindIncome || in.Kind == KindFixed
	switch {
	case purchase && in.Day != 0:
		errs = append(errs, ErrPurchaseDay)
	case in.Day < 0 || in.Day > maxDay || (needsDay && in.Day == 0):
		errs = append(errs, ErrDay)
	}
	in.MatchText = strings.TrimSpace(in.MatchText)
	if utf8.RuneCountInString(in.MatchText) > maxMatchRunes {
		errs = append(errs, ErrMatchText)
	}
	if in.Kind == KindSaving {
		if in.CategoryID != nil {
			errs = append(errs, ErrSavingCategory)
		}
		if in.MatchText != "" {
			errs = append(errs, ErrSavingMatchText)
		}
		if in.Target != nil && *in.Target <= 0 {
			errs = append(errs, ErrTarget)
		}
	} else {
		if in.AccountID != nil {
			errs = append(errs, ErrAccountNotGoal)
		}
		// A purchase's price is its amount, never a goal.
		if in.Target != nil {
			errs = append(errs, ErrTargetNotGoal)
		}
	}
	switch {
	case purchase:
		// It is bought in the month saved for or a later one, and no later
		// than the plan can be written. The month saved for can be ahead of
		// the current one, so each bound has its own message: "from this
		// month" would not say which.
		switch t := in.TargetMonth; {
		case t.IsZero() || t.Sub(now) > MonthsAhead:
			errs = append(errs, ErrTargetMonth)
		case t.Before(in.Month):
			errs = append(errs, ErrTargetMonthEarly)
		}
	case in.Kind != KindSaving && !in.TargetMonth.IsZero():
		errs = append(errs, ErrTargetMonthNotGoal)
	}
	if !in.StartMonth.IsZero() {
		errs = append(errs, ErrStartMonth)
	}
	switch {
	case purchase && in.Funding != FundingMonthly && in.Funding != FundingThatMonth && in.Funding != FundingSavings:
		errs = append(errs, ErrFunding)
	case !purchase && in.Funding != "":
		errs = append(errs, ErrFundingNotPurchase)
	}
	return errors.Join(errs...)
}

// normaliseName cleans up a name the way a category's is: every run of
// spaces becomes one, and characters that cannot be shown, or could pass for
// something else on screen, are refused.
func normaliseName(raw string) (string, error) {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return "", fmt.Errorf("%w: %q", ErrNameInvalid, r)
		default:
			b.WriteRune(r)
		}
	}
	name := strings.Join(strings.Fields(b.String()), " ")
	if name == "" || utf8.RuneCountInString(name) > maxNameRunes {
		return "", ErrName
	}
	return name, nil
}

// SaveItem creates the item with the caller's id, or edits it when it is
// already the owner's: what holds for every month changes in place, and the
// amount and day are split into versions from in.Month on or for it alone.
// created reports which happened. Saving the same thing again writes
// nothing, so a form that lost the answer to its first send can send it
// again, and an undo is the previous save sent again.
//
// A planned purchase's versions are its shares, which the plan writes: what
// is left of its price after what the months before in.Month set aside is
// spread from in.Month on (Shares). So a new price, month or way of setting it
// aside changes the months from in.Month on, never the ones before. It is set
// aside from the month it is first saved for, or an earlier one it is saved
// for later. Saving one the owner took out of the plan plans it again to its
// month, as undoing that does.
func (s *Service) SaveItem(ctx context.Context, userID, id uuid.UUID, in ItemInput) (item store.PlanItem, created bool, err error) {
	if err := s.writable(in.Month); err != nil {
		return store.PlanItem{}, false, err
	}
	if err := in.normalise(s.thisMonth()); err != nil {
		return store.PlanItem{}, false, err
	}
	want := store.PlanItem{
		ID: id, UserID: userID, Kind: in.Kind, Name: in.Name, CategoryID: in.CategoryID,
		MatchText: in.MatchText, AccountID: in.AccountID, Target: in.Target, TargetMonth: dateOf(in.TargetMonth),
	}
	purchase := in.Kind == KindPurchase
	if purchase {
		price := in.Amount
		want.Target, want.StartMonth, want.Funding = &price, in.Month.First(), in.Funding
	}
	err = s.db.InTx(ctx, func(q *store.Queries) error {
		// The user's lock makes the checks below and the writes one step, so
		// two saves of the same item, or two goals on one account, take turns.
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		existing, err := q.PlanItemByID(ctx, userID, id)
		found := err == nil
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if found && existing.Kind != in.Kind {
			return fmt.Errorf("%w: %s is %s", ErrKindFixed, id, existing.Kind)
		}
		// The months the item will hold for are worked out before anything
		// is written, so its account can be checked against them.
		stored, err := q.PlanItemVersions(ctx, userID, id)
		if err != nil {
			return err
		}
		var after []Version
		var ops []Op
		if purchase {
			if found && existing.StartMonth.Before(want.StartMonth) {
				want.StartMonth = existing.StartMonth
			}
			after, ops = Shares(itemVersions(stored), in.Month, in.Amount, in.TargetMonth, in.Funding)
		} else {
			after, ops = Split(itemVersions(stored), in.Month, in.Scope, Values{Amount: in.Amount, Day: in.Day})
		}
		if err := checkItemLabels(ctx, q, userID, id, in, after); err != nil {
			return err
		}
		switch {
		case !found:
			item, err = q.InsertPlanItem(ctx, want)
			if store.IsConstraint(err, store.PlanItemsPKey) {
				return fmt.Errorf("%w: %v", ErrItemIDTaken, err)
			}
			if err != nil {
				return err
			}
			created = true
		case sameLabels(existing, want):
			item = existing
		default:
			if err := q.UpdatePlanItem(ctx, want); err != nil {
				return err
			}
			if item, err = q.PlanItemByID(ctx, userID, id); err != nil {
				return err
			}
		}
		return applyItemOps(ctx, q, item, ops)
	})
	if err != nil {
		return store.PlanItem{}, false, err
	}
	return item, created, nil
}

// checkItemLabels checks what an item points at, under the user's lock: its
// category is the owner's and of the kind the item is, and its account is a
// savings or brokerage account of theirs that backs no other goal in the
// months the item will hold for, after. Someone else's category or account is
// reported as not found.
func checkItemLabels(ctx context.Context, q *store.Queries, userID, id uuid.UUID, in ItemInput, after []Version) error {
	if in.CategoryID != nil {
		want, wrong := "expense", ErrExpenseCategory
		if in.Kind == KindIncome {
			want, wrong = "income", ErrIncomeCategory
		}
		if err := checkCategory(ctx, q, userID, *in.CategoryID, want, wrong); err != nil {
			return err
		}
	}
	if in.AccountID == nil {
		return nil
	}
	account, err := q.AccountByID(ctx, userID, *in.AccountID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %v", accounts.ErrNotFound, err)
	}
	if err != nil {
		return err
	}
	if account.Type != "savings" && account.Type != "brokerage" {
		return fmt.Errorf("%w: %s is %s", ErrGoalAccount, account.ID, account.Type)
	}
	return checkOneGoal(ctx, q, userID, id, account.ID, after)
}

// checkOneGoal refuses a goal on an account that backs another goal in one
// of the months the goal will hold for, after. Two goals that never share a
// month can use the same account one after the other. Every month of after
// counts, the past ones too: the account holds for every month of an item,
// so pointing a goal with a history at another account moves its past there
// as well.
func checkOneGoal(ctx context.Context, q *store.Queries, userID, id, accountID uuid.UUID, after []Version) error {
	others, err := q.PlanGoalVersionsOnAccount(ctx, userID, accountID)
	if err != nil {
		return err
	}
	for _, o := range others {
		if o.ItemID == id {
			continue
		}
		theirs := Version{From: MonthOf(o.From), To: monthOf(o.To)}
		for _, v := range after {
			if v.Overlaps(theirs) {
				shared := v.From
				if theirs.From.After(shared) {
					shared = theirs.From
				}
				return fmt.Errorf("%w: %s backs %s in %s", ErrAccountHasGoal, accountID, o.ItemID, shared)
			}
		}
	}
	return nil
}

// checkCategory checks that one of the user's categories is of kind, and
// returns wrong when it is not. Someone else's is reported as not found,
// the way categories.Service.Get reports it.
func checkCategory(ctx context.Context, q *store.Queries, userID, id uuid.UUID, kind string, wrong error) error {
	c, err := q.CategoryByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %s", categories.ErrNotFound, id)
	}
	if err != nil {
		return err
	}
	if c.UserID != userID {
		return fmt.Errorf("%w: %s", categories.ErrNotOwned, id)
	}
	if c.Kind != kind {
		return fmt.Errorf("%w: %s is %s", wrong, id, c.Kind)
	}
	return nil
}

// sameLabels reports whether what holds for every month of an item is
// already what the owner sent, so saving it again writes nothing.
func sameLabels(a, b store.PlanItem) bool {
	return a.Name == b.Name && sameID(a.CategoryID, b.CategoryID) && a.MatchText == b.MatchText &&
		sameID(a.AccountID, b.AccountID) && sameCents(a.Target, b.Target) && a.TargetMonth == b.TargetMonth &&
		a.StartMonth == b.StartMonth && a.EndMonth == b.EndMonth && a.Funding == b.Funding
}

func sameID(a, b *uuid.UUID) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func sameCents(a, b *money.Cents) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// DeleteItem takes month m on (ScopeFrom), or m alone (ScopeOnly), out of
// the item. The months before keep their plan. An item left with no month is
// deleted. What the owner said about which movement paid the months taken out
// goes with them.
//
// A planned purchase is taken out from m on only (deletePurchase).
func (s *Service) DeleteItem(ctx context.Context, userID, id uuid.UUID, m Month, scope Scope) error {
	if err := s.writable(m); err != nil {
		return err
	}
	if _, err := ParseScope(string(scope)); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		item, err := q.PlanItemByID(ctx, userID, id)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrItemNotFound, id)
		}
		if err != nil {
			return err
		}
		stored, err := q.PlanItemVersions(ctx, userID, id)
		if err != nil {
			return err
		}
		if item.Kind == KindPurchase {
			return deletePurchase(ctx, q, item, stored, m, scope)
		}
		after, ops := Remove(itemVersions(stored), m, scope)
		if len(after) == 0 {
			// Its versions and links go with it.
			return q.DeletePlanItem(ctx, userID, id)
		}
		if err := applyItemOps(ctx, q, item, ops); err != nil {
			return err
		}
		var through civil.Date // from m on
		if scope == ScopeOnly {
			through = m.First()
		}
		_, err = q.DeletePlanLinks(ctx, userID, id, m.First(), through)
		return err
	})
}

// deletePurchase takes month m on out of a planned purchase. A purchase
// planned from m on has no month left and is deleted. One planned before m
// keeps those months as they were: what they set aside for it, whether it was
// bought in one of them, and where it stood in each, which is said against
// the month it was due, so that stays too. It ends the month before m
// (EndMonth): no month from m on shows it, sets anything aside for it or
// takes an outflow for it, not even the month after it was due, so an
// outflow of those months goes to what was free.
func deletePurchase(ctx context.Context, q *store.Queries, item store.PlanItem, stored []store.PlanVersion, m Month,
	scope Scope) error {
	if scope != ScopeFrom {
		return ErrPurchaseScope
	}
	if !MonthOf(item.StartMonth).Before(m) {
		return q.DeletePlanItem(ctx, item.UserID, item.ID)
	}
	_, ops := Remove(itemVersions(stored), m, ScopeFrom)
	if err := applyItemOps(ctx, q, item, ops); err != nil {
		return err
	}
	if end := monthOf(item.EndMonth); end.IsZero() || !end.Before(m) {
		item.EndMonth = m.AddMonths(-1).First()
		if err := q.UpdatePlanItem(ctx, item); err != nil {
			return err
		}
	}
	_, err := q.DeletePlanLinks(ctx, item.UserID, item.ID, m.First(), civil.Date{})
	return err
}

// CapInput is a cap on a category from Month on (ScopeFrom) or for Month
// alone (ScopeOnly).
type CapInput struct {
	Month  Month
	Scope  Scope
	Amount money.Cents
}

// SaveCap sets a cap on what one of the user's expense categories, with its
// children, takes of the free money each month. Like an item's amount, it is
// split into versions; saving the same cap again writes nothing.
func (s *Service) SaveCap(ctx context.Context, userID, categoryID uuid.UUID, in CapInput) error {
	if err := s.writable(in.Month); err != nil {
		return err
	}
	var errs []error
	if _, err := ParseScope(string(in.Scope)); err != nil {
		errs = append(errs, err)
	}
	if in.Amount <= 0 {
		errs = append(errs, ErrAmount)
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		if err := checkCategory(ctx, q, userID, categoryID, "expense", ErrExpenseCategory); err != nil {
			return err
		}
		stored, err := q.PlanCategoryCaps(ctx, userID, categoryID)
		if err != nil {
			return err
		}
		_, ops := Split(capVersions(stored), in.Month, in.Scope, Values{Amount: in.Amount})
		return applyCapOps(ctx, q, userID, categoryID, ops)
	})
}

// DeleteCap takes month m on (ScopeFrom), or m alone (ScopeOnly), out of a
// category's cap. A category with no cap for those months is left as it is.
func (s *Service) DeleteCap(ctx context.Context, userID, categoryID uuid.UUID, m Month, scope Scope) error {
	if err := s.writable(m); err != nil {
		return err
	}
	if _, err := ParseScope(string(scope)); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		if err := checkCategory(ctx, q, userID, categoryID, "expense", ErrExpenseCategory); err != nil {
			return err
		}
		stored, err := q.PlanCategoryCaps(ctx, userID, categoryID)
		if err != nil {
			return err
		}
		_, ops := Remove(capVersions(stored), m, scope)
		return applyCapOps(ctx, q, userID, categoryID, ops)
	})
}

// applyItemOps writes Split's operations on an item's versions, in its order.
func applyItemOps(ctx context.Context, q *store.Queries, item store.PlanItem, ops []Op) error {
	stored := func(v Version) store.PlanVersion {
		return store.PlanVersion{ID: v.ID, UserID: item.UserID, ItemID: item.ID, Kind: item.Kind,
			From: v.From.First(), To: dateOf(v.To), Amount: v.Amount, Day: v.Day}
	}
	for _, op := range ops {
		var err error
		switch op.Kind {
		case OpDelete:
			err = q.DeletePlanVersion(ctx, item.UserID, op.Version.ID)
		case OpUpdate:
			err = q.UpdatePlanVersion(ctx, stored(op.Version))
		case OpInsert:
			op.Version.ID = uuid.New()
			err = q.InsertPlanVersion(ctx, stored(op.Version))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// applyCapOps writes Split's operations on a category's caps, in its order.
func applyCapOps(ctx context.Context, q *store.Queries, userID, categoryID uuid.UUID, ops []Op) error {
	stored := func(v Version) store.PlanCap {
		return store.PlanCap{ID: v.ID, UserID: userID, CategoryID: categoryID,
			From: v.From.First(), To: dateOf(v.To), Amount: v.Amount}
	}
	for _, op := range ops {
		var err error
		switch op.Kind {
		case OpDelete:
			err = q.DeletePlanCap(ctx, userID, op.Version.ID)
		case OpUpdate:
			err = q.UpdatePlanCap(ctx, stored(op.Version))
		case OpInsert:
			op.Version.ID = uuid.New()
			err = q.InsertPlanCap(ctx, stored(op.Version))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func itemVersions(stored []store.PlanVersion) []Version {
	out := make([]Version, 0, len(stored))
	for _, v := range stored {
		out = append(out, Version{ID: v.ID, From: MonthOf(v.From), To: monthOf(v.To),
			Values: Values{Amount: v.Amount, Day: v.Day}})
	}
	return out
}

func capVersions(stored []store.PlanCap) []Version {
	out := make([]Version, 0, len(stored))
	for _, c := range stored {
		out = append(out, Version{ID: c.ID, From: MonthOf(c.From), To: monthOf(c.To),
			Values: Values{Amount: c.Amount}})
	}
	return out
}

// dateOf is how a month is stored: its first day, or NULL for no month.
func dateOf(m Month) civil.Date {
	if m.IsZero() {
		return civil.Date{}
	}
	return m.First()
}

// monthOf reads a stored month back; NULL is no month.
func monthOf(d civil.Date) Month {
	if d.IsZero() {
		return Month{}
	}
	return MonthOf(d)
}

// Month is month m's plan beside what happened in it, as the owner reads it
// today in Panama; the zero Month is the current one. It reads only: the
// items and caps that hold around m, the movements of m and the months either
// side, what the owner said about which paid what, the transfers out of m
// for hints, the cards with a statement due and, when there are caps or
// purchases filed under a category, the categories they count. With planned
// purchases, it also reads, for every one of them, the outflows near its price
// over the months it can be bought in (purchasesOf), and with savings tied to
// an account, what those accounts hold and what reached them in m. Nothing of
// it is written.
func (s *Service) Month(ctx context.Context, userID uuid.UUID, m Month) (Plan, error) {
	today := civil.Of(s.now().In(civil.Panama))
	if m.IsZero() {
		m = MonthOf(today)
	}
	before, after := m.AddMonths(-1), m.AddMonths(1)
	q := s.db.Q()
	// Every item, even with no version around m: a purchase paid with
	// savings has none, and may still be planned or bought in m.
	items, err := q.PlanItemsByUser(ctx, userID)
	if err != nil {
		return Plan{}, err
	}
	var versions []store.PlanVersion
	if len(items) > 0 {
		if versions, err = q.PlanVersionsBetween(ctx, userID, before.First(), after.First()); err != nil {
			return Plan{}, err
		}
	}
	stored, err := q.PlanCapsBetween(ctx, userID, m.First(), m.First())
	if err != nil {
		return Plan{}, err
	}
	caps := make([]Cap, 0, len(stored))
	for i, v := range capVersions(stored) {
		caps = append(caps, Cap{CategoryID: stored[i].CategoryID, Version: v})
	}
	var parents map[uuid.UUID]uuid.UUID
	if len(caps) > 0 || purchaseFiled(items) {
		if parents, err = categoryParents(ctx, q, userID); err != nil {
			return Plan{}, err
		}
	}
	purchases, bought, err := purchasesOf(ctx, q, userID, items, parents)
	if err != nil {
		return Plan{}, err
	}
	movements, err := q.PlanMovements(ctx, userID, before.First(), after.Last())
	if err != nil {
		return Plan{}, err
	}
	var said []Link
	if len(versions) > 0 {
		if said, err = links(ctx, q, userID, before, after); err != nil {
			return Plan{}, err
		}
		if movements, err = withLinked(ctx, q, userID, movements, said); err != nil {
			return Plan{}, err
		}
	}
	transfers, err := q.PlanTransfersOut(ctx, userID, m.First(), m.Last())
	if err != nil {
		return Plan{}, err
	}
	cards, err := q.PlanCards(ctx, userID)
	if err != nil {
		return Plan{}, err
	}
	instances := Instances(items, versions, before, after)
	balances, saved, err := goals(ctx, q, userID, instances, m)
	if err != nil {
		return Plan{}, err
	}
	return Compute(Input{
		Month: m, Today: today, Instances: instances, Caps: caps, Parents: parents, Movements: movements,
		Recognition: MatchBeside(instances, movements, said, transfers, bought), Cards: cards,
		Purchases: purchases, Bought: bought, Balances: balances, Saved: saved,
	}), nil
}

// purchaseFiled reports whether a planned purchase among items is filed
// under a category, so recognising it needs the categories' children.
func purchaseFiled(items []store.PlanItem) bool {
	return slices.ContainsFunc(items, func(it store.PlanItem) bool {
		return it.Kind == KindPurchase && it.CategoryID != nil
	})
}

// purchasesOf reads every planned purchase among items and recognises each
// over all the months it can be bought in (RecognisePurchases). Every one,
// whichever month is read: two purchases can be the same movement, the
// earliest made taking it, so recognising only those around the month read
// would let one month give the movement to one and another month to the
// other, and a purchase would be bought in some months' eyes and not in
// others'. It reads, over the months from the first purchase's to the last
// one's and only when there is one: the versions of every item, for the
// shares and for the payments a movement may be instead; what the owner said;
// the outflows near each price; and the movements a link to one names beyond
// them. That is the same few queries however many purchases there are.
// parents are the categories' parents, for a purchase filed under one.
func purchasesOf(ctx context.Context, q *store.Queries, userID uuid.UUID, items []store.PlanItem,
	parents map[uuid.UUID]uuid.UUID) ([]Purchase, []Bought, error) {
	var first, last Month
	for i := range items {
		if items[i].Kind != KindPurchase {
			continue
		}
		p := Purchase{Item: &items[i]}
		if first.IsZero() || p.Start().Before(first) {
			first = p.Start()
		}
		if last.IsZero() || p.lastBuyable().After(last) {
			last = p.lastBuyable()
		}
	}
	if first.IsZero() {
		return nil, nil, nil
	}
	versions, err := q.PlanVersionsBetween(ctx, userID, first.First(), last.First())
	if err != nil {
		return nil, nil, err
	}
	// A link names a movement of its month or one beside it.
	said, err := links(ctx, q, userID, first.AddMonths(-1), last.AddMonths(1))
	if err != nil {
		return nil, nil, err
	}
	purchases := Purchases(items, versions)
	ranges := make([]store.PlanPriceRange, 0, len(purchases))
	ours := make(map[uuid.UUID]bool, len(purchases))
	for _, p := range purchases {
		margin := p.Price() * purchasePercent / 100
		ranges = append(ranges, store.PlanPriceRange{From: p.Start().First(), To: p.lastBuyable().Last(),
			Min: p.Price() - margin, Max: p.Price() + margin})
		ours[p.Item.ID] = true
	}
	movements, err := q.PlanOutflowsNear(ctx, userID, ranges)
	if err != nil {
		return nil, nil, err
	}
	read := make(map[uuid.UUID]bool, len(movements))
	for _, mv := range movements {
		read[mv.ID] = true
	}
	var named []uuid.UUID
	for _, l := range said {
		if !ours[l.ItemID] || l.Decision != DecisionAssign {
			continue
		}
		for _, id := range l.names() {
			if !read[id] {
				read[id] = true
				named = append(named, id)
			}
		}
	}
	more, err := q.PlanMovementsByID(ctx, userID, named)
	if err != nil {
		return nil, nil, err
	}
	instances := Instances(items, versions, first, last)
	return purchases, RecognisePurchases(purchases, instances, append(movements, more...), said, parents), nil
}

// goals reads, for the savings of month m tied to an account, what each
// account holds, as Cuentas shows it, and what reached it as transfers in m,
// net of what left it the same way: interest and dividends are income, and a
// trade moves money within the account, so neither is something the owner
// set aside. Every account is read at once, whatever the number of goals.
func goals(ctx context.Context, q *store.Queries, userID uuid.UUID, instances []Instance, m Month) (balances, saved map[uuid.UUID]money.Cents, err error) {
	var ids []uuid.UUID
	for _, inst := range instances {
		if inst.Month == m && inst.Item.Kind == KindSaving && inst.Item.AccountID != nil {
			ids = append(ids, *inst.Item.AccountID)
		}
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	if balances, err = accounts.Balances(ctx, q, userID, ids); err != nil {
		return nil, nil, err
	}
	inflows, err := q.PlanInflows(ctx, userID, m.First(), m.Last())
	if err != nil {
		return nil, nil, err
	}
	saved = make(map[uuid.UUID]money.Cents, len(inflows))
	for _, in := range inflows {
		saved[in.AccountID] += in.Net
	}
	return balances, saved, nil
}

// withLinked adds to movements those the owner's links name that were booked
// beyond the months read, such as next month's payment made in the month
// after it, so the month they paid is seen paid by them. They are outside the
// month read, so they never count in it.
func withLinked(ctx context.Context, q *store.Queries, userID uuid.UUID, movements []store.PlanMovement, said []Link) ([]store.PlanMovement, error) {
	read := make(map[uuid.UUID]bool, len(movements))
	for _, mv := range movements {
		read[mv.ID] = true
	}
	var missing []uuid.UUID
	for _, l := range said {
		for _, id := range l.names() {
			if !read[id] && l.Decision == DecisionAssign {
				read[id] = true
				missing = append(missing, id)
			}
		}
	}
	more, err := q.PlanMovementsByID(ctx, userID, missing)
	if err != nil {
		return nil, err
	}
	return append(movements, more...), nil
}

// categoryParents maps each of the user's categories that has a parent to
// it, the tree categories.Service.Tree draws: two levels, so a cap on a
// parent counts its children and nothing deeper.
func categoryParents(ctx context.Context, q *store.Queries, userID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	all, err := q.CategoriesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	parents := make(map[uuid.UUID]uuid.UUID, len(all))
	for _, c := range all {
		if c.ParentID != nil {
			parents[c.ID] = *c.ParentID
		}
	}
	return parents, nil
}

// Item is month m of one of the user's items, with the values that hold for
// it: what a save answers with.
func (s *Service) Item(ctx context.Context, userID, id uuid.UUID, m Month) (Instance, error) {
	q := s.db.Q()
	item, err := q.PlanItemByID(ctx, userID, id)
	if errors.Is(err, store.ErrNotFound) {
		return Instance{}, fmt.Errorf("%w: %s", ErrItemNotFound, id)
	}
	if err != nil {
		return Instance{}, err
	}
	if item.Kind == KindPurchase {
		// Its price, and the months from when it is set aside to when it is
		// due, hold for the whole of it.
		return Purchase{Item: &item}.instance(m), nil
	}
	stored, err := q.PlanItemVersions(ctx, userID, id)
	if err != nil {
		return Instance{}, err
	}
	for _, v := range itemVersions(stored) {
		if v.Holds(m) {
			return Instance{Item: &item, Month: m, Version: v}, nil
		}
	}
	return Instance{}, fmt.Errorf("%w: %s has no version for %s", ErrItemNotFound, id, m)
}

// Cap is the cap on one of the user's categories that holds for month m:
// what a save answers with.
func (s *Service) Cap(ctx context.Context, userID, categoryID uuid.UUID, m Month) (Cap, error) {
	stored, err := s.db.Q().PlanCategoryCaps(ctx, userID, categoryID)
	if err != nil {
		return Cap{}, err
	}
	for _, v := range capVersions(stored) {
		if v.Holds(m) {
			return Cap{CategoryID: categoryID, Version: v}, nil
		}
	}
	return Cap{}, fmt.Errorf("%w: category %s has no cap for %s", store.ErrNotFound, categoryID, m)
}
