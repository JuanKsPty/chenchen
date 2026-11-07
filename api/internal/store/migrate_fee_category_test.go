package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migration 00020 gives every user Comisiones and Efectivo, marks the
// Comisiones their charges fall to, and files there the charges already
// imported that nobody chose a category for.
func TestTheChargesAlreadyImportedFallToComisiones(t *testing.T) {
	db := scratchDB(t)
	migrateTo(t, db, 19)
	const (
		plain    = "00000000-0000-0000-0000-000000000001" // only the old defaults
		hasOwn   = "00000000-0000-0000-0000-000000000002" // already made a Comisiones
		earnsFee = "00000000-0000-0000-0000-000000000003" // Comisiones is money they earn
	)
	exec(t, db, `INSERT INTO users (id, email, password_hash, full_name) VALUES
		($1, 'a@example.com', 'x', 'A'), ($2, 'b@example.com', 'x', 'B'), ($3, 'c@example.com', 'x', 'C')`,
		plain, hasOwn, earnsFee)
	exec(t, db, `INSERT INTO categories (id, user_id, name, kind, is_system) VALUES
		('00000000-0000-0000-0000-0000000000a1', $1, 'Salud', 'expense', true),
		('00000000-0000-0000-0000-0000000000b1', $2, 'Comisiones', 'expense', false),
		('00000000-0000-0000-0000-0000000000c1', $3, 'Comisiones', 'income', false)`,
		plain, hasOwn, earnsFee)
	exec(t, db, `INSERT INTO accounts (id, user_id, class, type, institution, external_number) VALUES
		('00000000-0000-0000-0000-00000000000a', $1, 'asset', 'checking', 'bac', '0001'),
		('00000000-0000-0000-0000-00000000000b', $2, 'asset', 'checking', 'bac', '0002'),
		('00000000-0000-0000-0000-00000000000c', $3, 'asset', 'checking', 'bac', '0003')`,
		plain, hasOwn, earnsFee)
	line := func(id, user, account string, cents int, kind, userKind, category, source string) {
		t.Helper()
		exec(t, db, `INSERT INTO entries (id, user_id, account_id, booked_on, amount_cents, kind, user_kind,
				category_id, category_source, dedup_key)
			VALUES ($1::uuid, $2, $3, '2026-10-01', $4, $5, NULLIF($6, '')::entry_kind,
				NULLIF($7, '')::uuid, NULLIF($8, ''), $1::text)`,
			id, user, account, cents, kind, userKind, category, source)
	}
	const a, b, c = "00000000-0000-0000-0000-00000000000a", "00000000-0000-0000-0000-00000000000b", "00000000-0000-0000-0000-00000000000c"
	line("00000000-0000-0000-0000-000000000e01", plain, a, -350, "fee", "", "", "")        // → Comisiones
	line("00000000-0000-0000-0000-000000000e02", plain, a, -100, "interest", "", "", "")   // → Comisiones
	line("00000000-0000-0000-0000-000000000e03", plain, a, -500, "expense", "fee", "", "") // the owner says it is a charge → Comisiones
	line("00000000-0000-0000-0000-000000000e04", plain, a, 200, "fee", "", "", "")         // a charge given back: not spending
	line("00000000-0000-0000-0000-000000000e05", plain, a, -54, "fee", "", "", "user")     // the owner took its category away
	line("00000000-0000-0000-0000-000000000e06", plain, a, -75, "fee", "", "00000000-0000-0000-0000-0000000000a1", "rule")
	line("00000000-0000-0000-0000-000000000e07", plain, a, -900, "expense", "", "", "")
	line("00000000-0000-0000-0000-000000000e08", plain, a, -300, "fee", "transfer", "", "")
	line("00000000-0000-0000-0000-000000000e09", hasOwn, b, -350, "fee", "", "", "")
	line("00000000-0000-0000-0000-000000000e10", earnsFee, c, -350, "fee", "", "", "")

	migrateTo(t, db, 20)

	if n := count(t, db, `SELECT count(*) FROM categories WHERE user_id = '`+plain+`' AND parent_id IS NULL
		AND kind = 'expense' AND is_system AND ((name = 'Comisiones' AND fallback_for = 'fee')
		OR (name = 'Efectivo' AND fallback_for IS NULL))`); n != 2 {
		t.Errorf("a user with the old defaults has %d of Comisiones (for charges) and Efectivo, want both", n)
	}
	if n := count(t, db, `SELECT count(*) FROM categories WHERE user_id = '`+hasOwn+`' AND name = 'Comisiones'`); n != 1 {
		t.Errorf("a user who had made Comisiones has %d of them, want theirs alone", n)
	}
	if n := count(t, db, `SELECT count(*) FROM categories WHERE id = '00000000-0000-0000-0000-0000000000b1'
		AND fallback_for = 'fee'`); n != 1 {
		t.Error("the Comisiones a user had made is not where their charges fall")
	}
	if n := count(t, db, `SELECT count(*) FROM categories WHERE user_id = '`+earnsFee+`' AND fallback_for IS NOT NULL`); n != 0 {
		t.Error("a user whose Comisiones is income got a category for charges")
	}
	if n := count(t, db, `SELECT count(*) FROM categories WHERE user_id = '`+earnsFee+`' AND name = 'Efectivo'`); n != 1 {
		t.Error("a user whose Comisiones is income did not get Efectivo")
	}

	filed := `SELECT count(*) FROM entries e JOIN categories c ON c.id = e.category_id
		WHERE c.fallback_for = 'fee' AND e.category_source = 'rule' AND e.id IN `
	if n := count(t, db, filed+`('00000000-0000-0000-0000-000000000e01', '00000000-0000-0000-0000-000000000e02',
		'00000000-0000-0000-0000-000000000e03', '00000000-0000-0000-0000-000000000e09')`); n != 4 {
		t.Errorf("charges filed under Comisiones = %d, want the fee, the interest, the line the owner made a charge and the other user's fee", n)
	}
	if n := count(t, db, `SELECT count(*) FROM entries WHERE category_id IS NULL AND id IN (
		'00000000-0000-0000-0000-000000000e04', '00000000-0000-0000-0000-000000000e05', '00000000-0000-0000-0000-000000000e07',
		'00000000-0000-0000-0000-000000000e08', '00000000-0000-0000-0000-000000000e10')`); n != 5 {
		t.Errorf("lines left without a category = %d, want a fee given back, one the owner cleared, a purchase, "+
			"a fee the owner made a transfer and the fee of a user with no category for charges", n)
	}
	if n := count(t, db, `SELECT count(*) FROM entries WHERE id = '00000000-0000-0000-0000-000000000e06'
		AND category_id = '00000000-0000-0000-0000-0000000000a1'`); n != 1 {
		t.Error("a charge a rule had already filed moved")
	}

	// Down leaves the categories and what is filed under them: they are the
	// owner's now. Up again makes no second Comisiones.
	sqlDB := stdlib.OpenDBFromPool(db.pool)
	defer sqlDB.Close()
	if err := goose.DownToContext(context.Background(), sqlDB, ".", 19); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'categories' AND column_name = 'fallback_for'`); n != 0 {
		t.Error("fallback_for survived Down")
	}
	if n := count(t, db, `SELECT count(*) FROM entries WHERE category_id IS NOT NULL
		AND id = '00000000-0000-0000-0000-000000000e01'`); n != 1 {
		t.Error("Down took a charge out of its category")
	}
	migrateTo(t, db, 20)
	if n := count(t, db, `SELECT count(*) FROM categories WHERE user_id = '`+plain+`' AND name IN ('Comisiones', 'Efectivo')`); n != 2 {
		t.Errorf("after Up again a user has %d Comisiones and Efectivo, want one each", n)
	}
}
