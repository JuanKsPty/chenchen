package plan

import (
	"context"
	"errors"
	"fmt"
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
	// KindPurchase is a planned purchase. It is not kept yet.
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
	// AccountID, Target and TargetMonth are a saving's: the savings or
	// brokerage account that holds it, and its goal and by when.
	AccountID   *uuid.UUID
	Target      *money.Cents
	TargetMonth Month
}

// normalise cleans up what the owner typed and reports every field that is
// wrong at once, so a form hears of all its mistakes together.
func (in *ItemInput) normalise() error {
	var errs []error
	switch in.Kind {
	case KindIncome, KindFixed, KindSaving:
	case KindPurchase:
		errs = append(errs, ErrPurchasesNotYet)
	default:
		errs = append(errs, ErrKind)
	}
	if _, err := ParseScope(string(in.Scope)); err != nil {
		errs = append(errs, err)
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
	if in.Day < 0 || in.Day > maxDay || (needsDay && in.Day == 0) {
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
		if in.Target != nil || !in.TargetMonth.IsZero() {
			errs = append(errs, ErrTargetNotGoal)
		}
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
func (s *Service) SaveItem(ctx context.Context, userID, id uuid.UUID, in ItemInput) (item store.PlanItem, created bool, err error) {
	if err := s.writable(in.Month); err != nil {
		return store.PlanItem{}, false, err
	}
	if err := in.normalise(); err != nil {
		return store.PlanItem{}, false, err
	}
	want := store.PlanItem{
		ID: id, UserID: userID, Kind: in.Kind, Name: in.Name, CategoryID: in.CategoryID,
		MatchText: in.MatchText, AccountID: in.AccountID, Target: in.Target, TargetMonth: dateOf(in.TargetMonth),
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
		after, ops := Split(itemVersions(stored), in.Month, in.Scope, Values{Amount: in.Amount, Day: in.Day})
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
		a.StartMonth == b.StartMonth && a.Funding == b.Funding
}

func sameID(a, b *uuid.UUID) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func sameCents(a, b *money.Cents) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// DeleteItem takes month m on (ScopeFrom), or m alone (ScopeOnly), out of
// the item. The months before keep their plan. An item left with no month is
// deleted. What the owner said about which movement paid the months taken out
// goes with them.
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
