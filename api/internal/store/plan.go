package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// The kinds of a plan item (00021_plan.sql).
const (
	PlanIncome   = "income"
	PlanFixed    = "fixed"
	PlanSaving   = "saving"
	PlanPurchase = "purchase"
)

// Constraint names a caller may need to distinguish.
//
// The two overlap rules are triggers, not constraints, but they raise under
// these names (see 00021_plan.sql), so IsConstraint names them all the same.
//
// The foreign keys keep their default names. Each points at its target
// together with the owner, so a plan can never reach another person's
// category, account or item, and a version can never have a kind its item
// does not.
const (
	PlanItemsPKey                 = "plan_items_pkey"
	PlanItemCategoryConstraint    = "plan_items_category_id_user_id_fkey"
	PlanItemAccountConstraint     = "plan_items_account_id_user_id_fkey"
	PlanVersionItemKindConstraint = "plan_item_versions_item_id_kind_user_id_fkey"
	PlanVersionsOverlapConstraint = "plan_item_versions_no_overlap"
	PlanCapCategoryConstraint     = "plan_caps_category_id_user_id_fkey"
	PlanCapsOverlapConstraint     = "plan_caps_no_overlap"
)

// PlanItem is one thing the owner plans: a paycheck, a payment that goes out
// every month, a saving or a purchase. What it is called and how its movements
// are recognised holds for every month; what it amounts to is in its
// versions.
type PlanItem struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Kind       string
	Name       string
	CategoryID *uuid.UUID
	// MatchText is the text its movements contain, compared like a rule's;
	// empty when the owner gave none.
	MatchText string
	// AccountID is a saving's real account.
	AccountID *uuid.UUID
	// Target is a saving's goal or a purchase's price; nil when there is none.
	Target *money.Cents
	// TargetMonth, StartMonth: the first day of a month, or the zero Date.
	TargetMonth civil.Date
	StartMonth  civil.Date
	// Funding is how a purchase is paid; empty for anything else.
	Funding   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

const planItemColumns = `id, user_id, kind, name, category_id, match_text, account_id, target_cents,
	target_month, start_month, COALESCE(funding, ''), created_at, updated_at`

func scanPlanItem(s scanner) (PlanItem, error) {
	var it PlanItem
	var target *int64
	err := s.Scan(&it.ID, &it.UserID, &it.Kind, &it.Name, &it.CategoryID, &it.MatchText, &it.AccountID,
		&target, &it.TargetMonth, &it.StartMonth, &it.Funding, &it.CreatedAt, &it.UpdatedAt)
	it.Target = centsPtr(target)
	return it, err
}

// InsertPlanItem adds an item. An id already taken, by this owner or another,
// is a ConstraintError on PlanItemsPKey.
func (q *Queries) InsertPlanItem(ctx context.Context, it PlanItem) (PlanItem, error) {
	created, err := scanPlanItem(q.q.QueryRow(ctx, `
		INSERT INTO plan_items (id, user_id, kind, name, category_id, match_text, account_id, target_cents,
			target_month, start_month, funding)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+planItemColumns,
		it.ID, it.UserID, it.Kind, it.Name, it.CategoryID, it.MatchText, it.AccountID, centsArg(it.Target),
		it.TargetMonth, it.StartMonth, nullIfEmpty(it.Funding)))
	return created, wrap("store.InsertPlanItem", err)
}

// PlanItemByID returns one of the user's plan items. One that belongs to
// someone else is ErrNotFound, like one that does not exist.
func (q *Queries) PlanItemByID(ctx context.Context, userID, id uuid.UUID) (PlanItem, error) {
	it, err := scanPlanItem(q.q.QueryRow(ctx,
		`SELECT `+planItemColumns+` FROM plan_items WHERE id = $1 AND user_id = $2`, id, userID))
	return it, wrap("store.PlanItemByID", err)
}

// PlanItemsByUser lists every item the user ever planned, oldest first.
func (q *Queries) PlanItemsByUser(ctx context.Context, userID uuid.UUID) ([]PlanItem, error) {
	rows, err := q.q.Query(ctx,
		`SELECT `+planItemColumns+` FROM plan_items WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, wrap("store.PlanItemsByUser", err)
	}
	defer rows.Close()
	var items []PlanItem
	for rows.Next() {
		it, err := scanPlanItem(rows)
		if err != nil {
			return nil, wrap("store.PlanItemsByUser", err)
		}
		items = append(items, it)
	}
	return items, wrap("store.PlanItemsByUser", rows.Err())
}

// UpdatePlanItem changes what holds for every month of one of the user's
// items. Its kind is not among it: a kind never changes.
func (q *Queries) UpdatePlanItem(ctx context.Context, it PlanItem) error {
	return q.execOne(ctx, "store.UpdatePlanItem", `
		UPDATE plan_items
		SET name = $3, category_id = $4, match_text = $5, account_id = $6, target_cents = $7,
		    target_month = $8, start_month = $9, funding = $10, updated_at = now()
		WHERE id = $1 AND user_id = $2`,
		it.ID, it.UserID, it.Name, it.CategoryID, it.MatchText, it.AccountID, centsArg(it.Target),
		it.TargetMonth, it.StartMonth, nullIfEmpty(it.Funding))
}

// DeletePlanItem removes one of the user's items with its versions and links.
func (q *Queries) DeletePlanItem(ctx context.Context, userID, id uuid.UUID) error {
	return q.execOne(ctx, "store.DeletePlanItem",
		`DELETE FROM plan_items WHERE id = $1 AND user_id = $2`, id, userID)
}

// PlanVersion is what an item amounts to over a run of months.
type PlanVersion struct {
	ID     uuid.UUID
	UserID uuid.UUID
	ItemID uuid.UUID
	Kind   string
	// From and To are first days of months; a zero To has no end.
	From civil.Date
	To   civil.Date
	// Amount is per month; for a purchase, that month's share of the price.
	Amount money.Cents
	// Day is the day of the month it is expected, 1 to 31, or 0 for none.
	Day int
}

const planVersionColumns = `id, user_id, item_id, kind, from_month, to_month, amount_cents, COALESCE(day, 0)`

func scanPlanVersion(s scanner) (PlanVersion, error) {
	var v PlanVersion
	var amount int64
	err := s.Scan(&v.ID, &v.UserID, &v.ItemID, &v.Kind, &v.From, &v.To, &amount, &v.Day)
	v.Amount = money.Cents(amount)
	return v, err
}

func (q *Queries) planVersions(ctx context.Context, op, sql string, args ...any) ([]PlanVersion, error) {
	rows, err := q.q.Query(ctx, sql, args...)
	if err != nil {
		return nil, wrap(op, err)
	}
	defer rows.Close()
	var versions []PlanVersion
	for rows.Next() {
		v, err := scanPlanVersion(rows)
		if err != nil {
			return nil, wrap(op, err)
		}
		versions = append(versions, v)
	}
	return versions, wrap(op, rows.Err())
}

// PlanItemVersions lists the versions of one of the user's items in month
// order.
func (q *Queries) PlanItemVersions(ctx context.Context, userID, itemID uuid.UUID) ([]PlanVersion, error) {
	return q.planVersions(ctx, "store.PlanItemVersions",
		`SELECT `+planVersionColumns+` FROM plan_item_versions
		 WHERE user_id = $1 AND item_id = $2 ORDER BY from_month`, userID, itemID)
}

// PlanVersionsBetween lists the versions of all the user's items that hold
// for any month from from to to, both first days of months, by item and in
// month order.
func (q *Queries) PlanVersionsBetween(ctx context.Context, userID uuid.UUID, from, to civil.Date) ([]PlanVersion, error) {
	return q.planVersions(ctx, "store.PlanVersionsBetween",
		`SELECT `+planVersionColumns+` FROM plan_item_versions
		 WHERE user_id = $1 AND from_month <= $3 AND (to_month IS NULL OR to_month >= $2)
		 ORDER BY item_id, from_month`, userID, from, to)
}

// PlanGoalVersionsOnAccount lists the versions of the user's savings tied to
// an account, by item and in month order, for the rule that an account backs
// one goal at a time. The caller compares their months with the ones an edit
// would leave, which only it knows before the edit is written.
func (q *Queries) PlanGoalVersionsOnAccount(ctx context.Context, userID, accountID uuid.UUID) ([]PlanVersion, error) {
	return q.planVersions(ctx, "store.PlanGoalVersionsOnAccount",
		`SELECT `+planVersionColumns+` FROM plan_item_versions
		 WHERE user_id = $1 AND kind = 'saving'
		   AND item_id IN (SELECT id FROM plan_items WHERE user_id = $1 AND account_id = $2)
		 ORDER BY item_id, from_month`, userID, accountID)
}

// InsertPlanVersion adds a version. One that shares a month with another of
// the same item is a ConstraintError on PlanVersionsOverlapConstraint.
func (q *Queries) InsertPlanVersion(ctx context.Context, v PlanVersion) error {
	_, err := q.q.Exec(ctx, `
		INSERT INTO plan_item_versions (id, user_id, item_id, kind, from_month, to_month, amount_cents, day)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, 0))`,
		v.ID, v.UserID, v.ItemID, v.Kind, v.From, v.To, int64(v.Amount), v.Day)
	return wrap("store.InsertPlanVersion", err)
}

// UpdatePlanVersion changes the months and values of one of the user's
// versions.
func (q *Queries) UpdatePlanVersion(ctx context.Context, v PlanVersion) error {
	return q.execOne(ctx, "store.UpdatePlanVersion", `
		UPDATE plan_item_versions SET from_month = $3, to_month = $4, amount_cents = $5, day = NULLIF($6, 0)
		WHERE id = $1 AND user_id = $2`, v.ID, v.UserID, v.From, v.To, int64(v.Amount), v.Day)
}

// DeletePlanVersion removes one of the user's versions.
func (q *Queries) DeletePlanVersion(ctx context.Context, userID, id uuid.UUID) error {
	return q.execOne(ctx, "store.DeletePlanVersion",
		`DELETE FROM plan_item_versions WHERE id = $1 AND user_id = $2`, id, userID)
}

// DeletePlanLinks forgets what the owner said about the months of an item
// from from to to (first days of months; a zero to has no end), for months
// taken out of the plan. It returns how many links went.
func (q *Queries) DeletePlanLinks(ctx context.Context, userID, itemID uuid.UUID, from, to civil.Date) (int64, error) {
	tag, err := q.q.Exec(ctx, `
		DELETE FROM plan_links
		WHERE user_id = $1 AND item_id = $2 AND month >= $3 AND ($4::date IS NULL OR month <= $4)`,
		userID, itemID, from, to)
	if err != nil {
		return 0, wrap("store.DeletePlanLinks", err)
	}
	return tag.RowsAffected(), nil
}

// PlanCap is a cap on what a category takes of the free money over a run of
// months.
type PlanCap struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	CategoryID uuid.UUID
	// From and To are first days of months; a zero To has no end.
	From   civil.Date
	To     civil.Date
	Amount money.Cents
}

const planCapColumns = `id, user_id, category_id, from_month, to_month, amount_cents`

func (q *Queries) planCaps(ctx context.Context, op, sql string, args ...any) ([]PlanCap, error) {
	rows, err := q.q.Query(ctx, sql, args...)
	if err != nil {
		return nil, wrap(op, err)
	}
	defer rows.Close()
	var caps []PlanCap
	for rows.Next() {
		var c PlanCap
		var amount int64
		if err := rows.Scan(&c.ID, &c.UserID, &c.CategoryID, &c.From, &c.To, &amount); err != nil {
			return nil, wrap(op, err)
		}
		c.Amount = money.Cents(amount)
		caps = append(caps, c)
	}
	return caps, wrap(op, rows.Err())
}

// PlanCategoryCaps lists the caps of one of the user's categories in month
// order.
func (q *Queries) PlanCategoryCaps(ctx context.Context, userID, categoryID uuid.UUID) ([]PlanCap, error) {
	return q.planCaps(ctx, "store.PlanCategoryCaps",
		`SELECT `+planCapColumns+` FROM plan_caps
		 WHERE user_id = $1 AND category_id = $2 ORDER BY from_month`, userID, categoryID)
}

// PlanCapsBetween lists the user's caps that hold for any month from from to
// to, both first days of months, by category and in month order.
func (q *Queries) PlanCapsBetween(ctx context.Context, userID uuid.UUID, from, to civil.Date) ([]PlanCap, error) {
	return q.planCaps(ctx, "store.PlanCapsBetween",
		`SELECT `+planCapColumns+` FROM plan_caps
		 WHERE user_id = $1 AND from_month <= $3 AND (to_month IS NULL OR to_month >= $2)
		 ORDER BY category_id, from_month`, userID, from, to)
}

// InsertPlanCap adds a cap. One that shares a month with another of the same
// category is a ConstraintError on PlanCapsOverlapConstraint.
func (q *Queries) InsertPlanCap(ctx context.Context, c PlanCap) error {
	_, err := q.q.Exec(ctx, `
		INSERT INTO plan_caps (id, user_id, category_id, from_month, to_month, amount_cents)
		VALUES ($1, $2, $3, $4, $5, $6)`, c.ID, c.UserID, c.CategoryID, c.From, c.To, int64(c.Amount))
	return wrap("store.InsertPlanCap", err)
}

// UpdatePlanCap changes the months and amount of one of the user's caps.
func (q *Queries) UpdatePlanCap(ctx context.Context, c PlanCap) error {
	return q.execOne(ctx, "store.UpdatePlanCap", `
		UPDATE plan_caps SET from_month = $3, to_month = $4, amount_cents = $5
		WHERE id = $1 AND user_id = $2`, c.ID, c.UserID, c.From, c.To, int64(c.Amount))
}

// DeletePlanCap removes one of the user's caps.
func (q *Queries) DeletePlanCap(ctx context.Context, userID, id uuid.UUID) error {
	return q.execOne(ctx, "store.DeletePlanCap",
		`DELETE FROM plan_caps WHERE id = $1 AND user_id = $2`, id, userID)
}
