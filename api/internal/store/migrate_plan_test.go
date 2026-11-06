package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migration 00021 adds the month's plan. Its Down takes the plan away and
// leaves everything it pointed at, and Up again brings it back working.
func TestThePlanMigrationComesOffAndGoesBackOnCleanly(t *testing.T) {
	db := scratchDB(t)
	migrateTo(t, db, 21)
	const (
		user     = "00000000-0000-0000-0000-000000000001"
		category = "00000000-0000-0000-0000-0000000000a1"
		account  = "00000000-0000-0000-0000-00000000000a"
		item     = "00000000-0000-0000-0000-0000000000f1"
	)
	exec(t, db, `INSERT INTO users (id, email, password_hash, full_name) VALUES ($1, 'a@example.com', 'x', 'A')`, user)
	exec(t, db, `INSERT INTO categories (id, user_id, name, kind) VALUES ($1, $2, 'Servicios', 'expense')`, category, user)
	exec(t, db, `INSERT INTO accounts (id, user_id, class, type, institution, external_number)
		VALUES ($1, $2, 'asset', 'savings', 'bac', '0001')`, account, user)
	plan := func() {
		t.Helper()
		exec(t, db, `INSERT INTO plan_items (id, user_id, kind, name, category_id, match_text)
			VALUES ($1, $2, 'fixed', 'Seguro', $3, 'SEGURO EMPRESA X')`, item, user, category)
		exec(t, db, `INSERT INTO plan_item_versions (user_id, item_id, kind, from_month, amount_cents, day)
			VALUES ($1, $2, 'fixed', '2026-11-01', 350, 18)`, user, item)
		exec(t, db, `INSERT INTO plan_caps (user_id, category_id, from_month, amount_cents)
			VALUES ($1, $2, '2026-11-01', 8000)`, user, category)
		exec(t, db, `INSERT INTO plan_links (id, user_id, item_id, month, decision)
			VALUES ('00000000-0000-0000-0000-0000000000e1', $1, $2, '2026-11-01', 'paid')`, user, item)
		exec(t, db, `INSERT INTO plan_dismissals (user_id, kind, key) VALUES ($1, 'saving', 'account:' || $2)`, user, account)
	}
	plan()

	// Only a purchase ends before its month, and never before it started.
	for name, sql := range map[string]string{
		"a payment that ends": `INSERT INTO plan_items (id, user_id, kind, name, end_month)
			VALUES ('00000000-0000-0000-0000-0000000000f2', $1, 'fixed', 'Seguro', '2026-12-01')`,
		"a purchase that ends before it starts": `INSERT INTO plan_items (id, user_id, kind, name, target_cents,
				target_month, start_month, funding, end_month)
			VALUES ('00000000-0000-0000-0000-0000000000f3', $1, 'purchase', 'Lente', 30000, '2027-03-01', '2026-11-01',
				'monthly', '2026-10-01')`,
	} {
		if _, err := db.pool.Exec(context.Background(), sql, user); !IsConstraint(wrap("test", err), "plan_items_purchase_end") {
			t.Errorf("%s = %v, want plan_items_purchase_end", name, err)
		}
	}

	sqlDB := stdlib.OpenDBFromPool(db.pool)
	defer sqlDB.Close()
	if err := goose.DownToContext(context.Background(), sqlDB, ".", 20); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM pg_tables WHERE tablename LIKE 'plan\_%'`); n != 0 {
		t.Errorf("%d plan tables survived Down", n)
	}
	if n := count(t, db, `SELECT count(*) FROM pg_proc WHERE proname LIKE 'plan\_%'`); n != 0 {
		t.Errorf("%d plan functions survived Down", n)
	}
	if n := count(t, db, `SELECT count(*) FROM categories c JOIN accounts a USING (user_id)
		WHERE c.id = '`+category+`' AND a.id = '`+account+`'`); n != 1 {
		t.Error("Down took the category or the account the plan pointed at")
	}

	migrateTo(t, db, 21)
	plan()
	if n := count(t, db, `SELECT count(*) FROM plan_item_versions`); n != 1 {
		t.Errorf("versions after Up again = %d, want 1", n)
	}
	// The rule that no two versions share a month is back with it.
	if _, err := db.pool.Exec(context.Background(), `INSERT INTO plan_item_versions
		(user_id, item_id, kind, from_month, to_month, amount_cents, day)
		VALUES ($1, $2, 'fixed', '2027-01-01', '2027-01-01', 400, 18)`, user, item); !IsConstraint(
		wrap("test", err), "plan_item_versions_no_overlap") {
		t.Errorf("an overlapping version after Up again = %v, want refused", err)
	}
}
