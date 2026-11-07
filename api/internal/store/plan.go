package store

import (
	"context"
	"fmt"
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

// FoldRuleText folds text the way a rule and the text it is matched against
// are compared: without case or Spanish accents. The plan recognises an
// item's movements by its text exactly as a rule would, so the text the owner
// already uses for a rule works for a payment too.
func FoldRuleText(s string) string { return foldRuleText(s) }

// planKinds are the effective kinds the plan reads: what counts as spending,
// which it splits into what paid a planned item and what was free, and what
// counts as income, which says whether a paycheck arrived. A transfer or a
// trade is never either, so the plan never counts one; PlanTransfersOut reads
// transfers apart, only to point at a payment the bank called one.
var planKinds = append(append([]string{}, SpendKinds...), IncomeKinds...)

// PlanMovement is a movement as the plan reads it: what Gastos and the
// month's flow add up, printed line or pending capture alike, with only what
// recognising it and filing it into a bucket need. Kind is the effective kind.
type PlanMovement struct {
	ID uuid.UUID
	// Origin is OriginStatement or OriginCapture.
	Origin    string
	AccountID uuid.UUID
	BookedOn  civil.Date
	// Amount is signed from the holder's view: spending is negative.
	Amount       money.Cents
	Kind         string
	CategoryID   *uuid.UUID
	BankCategory string
	Description  string
}

// PlanMovements lists the user's spending and income booked from from to to,
// both inclusive, oldest first: the same rows movementSource gives Gastos and
// the month's flow, so the plan can never count a movement they do not. It
// reads no raw columns, since three months of movements are read on every
// look at the plan.
func (q *Queries) PlanMovements(ctx context.Context, userID uuid.UUID, from, to civil.Date) ([]PlanMovement, error) {
	where, args, err := EntryFilter{UserID: userID, From: from, To: to, Kinds: planKinds}.where()
	if err != nil {
		return nil, err
	}
	return q.planMovements(ctx, "store.PlanMovements", where, args)
}

// PlanMovementsByID lists those of the given movements that the plan reads,
// oldest first: what an owner's link names beyond the months PlanMovements
// read, such as a payment for next month made in the month after it. A
// movement that is not spending or income, or not the user's, is left out,
// as PlanMovements would leave it.
func (q *Queries) PlanMovementsByID(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]PlanMovement, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	where, args, err := EntryFilter{UserID: userID, Kinds: planKinds}.where()
	if err != nil {
		return nil, err
	}
	args = append(args, ids)
	return q.planMovements(ctx, "store.PlanMovementsByID", fmt.Sprintf("%s AND e.id = ANY($%d)", where, len(args)), args)
}

// PlanTransfersOut lists the transfers out of the user's accounts booked from
// from to to, both inclusive, oldest first. They are never spending, and the
// plan never counts them: it only reads them to tell the owner when a payment
// it is waiting for went out as one, which the bank sometimes calls a
// transfer to another person.
func (q *Queries) PlanTransfersOut(ctx context.Context, userID uuid.UUID, from, to civil.Date) ([]PlanMovement, error) {
	where, args, err := EntryFilter{UserID: userID, From: from, To: to, Kinds: []string{KindTransfer}}.where()
	if err != nil {
		return nil, err
	}
	return q.planMovements(ctx, "store.PlanTransfersOut", where+" AND e.amount_cents < 0", args)
}

func (q *Queries) planMovements(ctx context.Context, op, where string, args []any) ([]PlanMovement, error) {
	rows, err := q.q.Query(ctx, `
		SELECT e.id, e.origin, e.account_id, e.booked_on, e.amount_cents, COALESCE(e.user_kind, e.kind)::text,
		       e.category_id, e.bank_category, e.description
		FROM `+movementSource+` WHERE `+where+`
		ORDER BY e.booked_on, e.id`, args...)
	if err != nil {
		return nil, wrap(op, err)
	}
	defer rows.Close()
	var out []PlanMovement
	for rows.Next() {
		var m PlanMovement
		var amount int64
		if err := rows.Scan(&m.ID, &m.Origin, &m.AccountID, &m.BookedOn, &amount, &m.Kind,
			&m.CategoryID, &m.BankCategory, &m.Description); err != nil {
			return nil, wrap(op, err)
		}
		m.Amount = money.Cents(amount)
		out = append(out, m)
	}
	return out, wrap(op, rows.Err())
}

// What the owner can say about one month of an item (plan_links.decision):
// this movement paid it, this one did not, or it was paid where the app
// cannot see.
const (
	PlanAssign = "assign"
	PlanReject = "reject"
	PlanPaid   = "paid"
)

// Constraint names of plan_links a caller may need to distinguish.
const (
	PlanLinksPKey = "plan_links_pkey"
	// PlanLinkEntryAssigned and PlanLinkCaptureAssigned say a movement
	// already pays another item.
	PlanLinkEntryAssigned   = "plan_links_entry_assigned_idx"
	PlanLinkCaptureAssigned = "plan_links_capture_assigned_idx"
)

// PlanLink is where the owner corrected how one month of an item was
// recognised. EntryID or CaptureID is the movement it names, as the owner
// named it; neither for PlanPaid.
type PlanLink struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	ItemID   uuid.UUID
	Month    civil.Date
	Decision string
	// EntryID is a printed line, CaptureID a purchase the owner noted.
	EntryID   *uuid.UUID
	CaptureID *uuid.UUID
	CreatedAt time.Time

	// What a noted purchase the link names is now, filled by PlanLinksBetween
	// only: its state (Capture*), its line once linked to one, and the lines
	// it could be once printed, oldest first.
	CaptureState string
	CaptureLine  *uuid.UUID
	CaptureLines []uuid.UUID
}

// Movement is the id of the movement the link names, or uuid.Nil.
func (l PlanLink) Movement() uuid.UUID {
	switch {
	case l.EntryID != nil:
		return *l.EntryID
	case l.CaptureID != nil:
		return *l.CaptureID
	default:
		return uuid.Nil
	}
}

const planLinkColumns = `id, user_id, item_id, month, decision, entry_id, capture_id, created_at`

func scanPlanLink(s scanner, extra ...any) (PlanLink, error) {
	var l PlanLink
	err := s.Scan(append([]any{&l.ID, &l.UserID, &l.ItemID, &l.Month, &l.Decision, &l.EntryID, &l.CaptureID,
		&l.CreatedAt}, extra...)...)
	return l, err
}

// InsertPlanLink records what the owner said about one month of an item. An
// id already taken, by this owner or another, is a ConstraintError on
// PlanLinksPKey; a movement that already pays another item, one on
// PlanLinkEntryAssigned or PlanLinkCaptureAssigned. A movement or an item of
// someone else's is refused by the foreign keys.
func (q *Queries) InsertPlanLink(ctx context.Context, l PlanLink) (PlanLink, error) {
	created, err := scanPlanLink(q.q.QueryRow(ctx, `
		INSERT INTO plan_links (id, user_id, item_id, month, decision, entry_id, capture_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+planLinkColumns,
		l.ID, l.UserID, l.ItemID, l.Month, l.Decision, l.EntryID, l.CaptureID))
	return created, wrap("store.InsertPlanLink", err)
}

// PlanLinkByID returns one of the user's links. One that belongs to someone
// else is ErrNotFound, like one that does not exist.
func (q *Queries) PlanLinkByID(ctx context.Context, userID, id uuid.UUID) (PlanLink, error) {
	l, err := scanPlanLink(q.q.QueryRow(ctx,
		`SELECT `+planLinkColumns+` FROM plan_links WHERE id = $1 AND user_id = $2`, id, userID))
	return l, wrap("store.PlanLinkByID", err)
}

// PlanLinksOnPair lists, earliest said first, what the user said about
// movement for month of an item: that it paid it or that it did not. A link
// counts when it names movement itself or, for a line, a noted purchase that
// is that line, either linked to it or printed with it among the lines it
// could be. The plan shows such a purchase as its line, so what the owner
// says about the line is said about the purchase.
func (q *Queries) PlanLinksOnPair(ctx context.Context, userID, itemID uuid.UUID, month civil.Date, movement uuid.UUID) ([]PlanLink, error) {
	rows, err := q.q.Query(ctx, `
		SELECT `+planLinkColumns+` FROM plan_links
		WHERE user_id = $1 AND item_id = $2 AND month = $3 AND decision IN ('assign', 'reject')
		  AND (entry_id = $4 OR capture_id = $4
		       OR capture_id IN (SELECT id FROM captures WHERE user_id = $1 AND entry_id = $4)
		       OR capture_id IN (SELECT capture_id FROM capture_candidates WHERE user_id = $1 AND entry_id = $4))
		ORDER BY created_at, id`, userID, itemID, month, movement)
	if err != nil {
		return nil, wrap("store.PlanLinksOnPair", err)
	}
	defer rows.Close()
	var out []PlanLink
	for rows.Next() {
		l, err := scanPlanLink(rows)
		if err != nil {
			return nil, wrap("store.PlanLinksOnPair", err)
		}
		out = append(out, l)
	}
	return out, wrap("store.PlanLinksOnPair", rows.Err())
}

// PlanLinkPaidOutside returns where the user said month of an item was paid
// where the app cannot see; ErrNotFound when they did not.
func (q *Queries) PlanLinkPaidOutside(ctx context.Context, userID, itemID uuid.UUID, month civil.Date) (PlanLink, error) {
	l, err := scanPlanLink(q.q.QueryRow(ctx, `
		SELECT `+planLinkColumns+` FROM plan_links
		WHERE user_id = $1 AND item_id = $2 AND month = $3 AND decision = 'paid'`, userID, itemID, month))
	return l, wrap("store.PlanLinkPaidOutside", err)
}

// PlanLinkAssigning returns the user's earliest link saying movement paid an
// item: the line or noted purchase itself, or, for a line, the noted purchase
// linked to it, which is the same purchase. A printed purchase that could be
// the line is not counted: it could as well be another. ErrNotFound when none
// does.
func (q *Queries) PlanLinkAssigning(ctx context.Context, userID, movement uuid.UUID) (PlanLink, error) {
	l, err := scanPlanLink(q.q.QueryRow(ctx, `
		SELECT `+planLinkColumns+` FROM plan_links
		WHERE user_id = $1 AND decision = 'assign'
		  AND (entry_id = $2 OR capture_id = $2
		       OR capture_id IN (SELECT id FROM captures WHERE user_id = $1 AND entry_id = $2))
		ORDER BY created_at, id LIMIT 1`, userID, movement))
	return l, wrap("store.PlanLinkAssigning", err)
}

// DeletePlanLink forgets one of the user's links.
func (q *Queries) DeletePlanLink(ctx context.Context, userID, id uuid.UUID) error {
	return q.execOne(ctx, "store.DeletePlanLink",
		`DELETE FROM plan_links WHERE id = $1 AND user_id = $2`, id, userID)
}

// PlanLinksBetween lists the user's links about the months from from to to,
// both first days of months, earliest said first, each with what the noted
// purchase it names is now (capture_states), so the link can follow it to its
// line. The candidate lines of a printed purchase come oldest first, the
// order CaptureCandidates gives.
func (q *Queries) PlanLinksBetween(ctx context.Context, userID uuid.UUID, from, to civil.Date) ([]PlanLink, error) {
	rows, err := q.q.Query(ctx, `
		SELECT `+prefixed("l.", planLinkColumns)+`, COALESCE(c.state, ''), c.entry_id,
		       COALESCE((SELECT array_agg(e.id ORDER BY e.booked_on, e.id)
		                   FROM capture_candidates k JOIN entries e ON e.id = k.entry_id
		                  WHERE k.capture_id = l.capture_id AND c.state = 'printed'), '{}')
		FROM plan_links l LEFT JOIN capture_states c ON c.id = l.capture_id AND c.user_id = l.user_id
		WHERE l.user_id = $1 AND l.month BETWEEN $2 AND $3
		ORDER BY l.created_at, l.id`, userID, from, to)
	if err != nil {
		return nil, wrap("store.PlanLinksBetween", err)
	}
	defer rows.Close()
	var out []PlanLink
	for rows.Next() {
		var state string
		var line *uuid.UUID
		var lines []uuid.UUID
		l, err := scanPlanLink(rows, &state, &line, &lines)
		if err != nil {
			return nil, wrap("store.PlanLinksBetween", err)
		}
		l.CaptureState, l.CaptureLine, l.CaptureLines = state, line, lines
		out = append(out, l)
	}
	return out, wrap("store.PlanLinksBetween", rows.Err())
}

// PlanCard is a credit card's latest statement, as the plan shows it beside
// the month: when it closed, when it is due and its minimum payment, and what
// reached the card as payments since it closed.
type PlanCard struct {
	AccountID      uuid.UUID
	ClosingOn      civil.Date
	DueOn          civil.Date
	MinimumPayment *money.Cents
	// PaidSince is the sum of the money that came into the card as transfers
	// booked after ClosingOn: what paid it.
	PaidSince money.Cents
}

// PlanCards lists the user's credit cards that have a statement with a due
// date, by that date: for each, the latest checkpoint that carries one, the
// statement Cuentas shows (its as_of is the day the statement closed). A card
// someone else pays is left out: its statement is not the owner's to pay.
func (q *Queries) PlanCards(ctx context.Context, userID uuid.UUID) ([]PlanCard, error) {
	rows, err := q.q.Query(ctx, `
		SELECT a.id, s.as_of, s.due_on, s.minimum_payment_cents,
		       COALESCE((SELECT sum(e.amount_cents) FROM entries e
		                  WHERE e.user_id = a.user_id AND e.account_id = a.id AND e.amount_cents > 0
		                    AND COALESCE(e.user_kind, e.kind) = 'transfer' AND e.booked_on > s.as_of), 0)
		FROM accounts a
		CROSS JOIN LATERAL (
		    SELECT c.as_of, c.due_on, c.minimum_payment_cents FROM balance_checkpoints c
		     WHERE c.account_id = a.id AND c.user_id = a.user_id AND c.due_on IS NOT NULL
		     ORDER BY c.as_of DESC, c.created_at DESC LIMIT 1) s
		WHERE a.user_id = $1 AND a.type = 'credit_card' AND NOT a.paid_by_other
		ORDER BY s.due_on, a.id`, userID)
	if err != nil {
		return nil, wrap("store.PlanCards", err)
	}
	defer rows.Close()
	var out []PlanCard
	for rows.Next() {
		var c PlanCard
		var minimum *int64
		var paid int64
		if err := rows.Scan(&c.AccountID, &c.ClosingOn, &c.DueOn, &minimum, &paid); err != nil {
			return nil, wrap("store.PlanCards", err)
		}
		c.MinimumPayment, c.PaidSince = centsPtr(minimum), money.Cents(paid)
		out = append(out, c)
	}
	return out, wrap("store.PlanCards", rows.Err())
}
