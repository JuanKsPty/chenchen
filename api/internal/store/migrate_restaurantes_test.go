package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migration 00022 renames the default Alimentación to Supermercado, gives
// every user Restaurantes, and sends there the card's "Comida y Bebida": its
// default rule and the lines that rule filed.
func TestAlimentacionSplitsIntoSupermercadoAndRestaurantes(t *testing.T) {
	db := scratchDB(t)
	migrateTo(t, db, 21)
	const (
		plain  = "00000000-0000-0000-0000-000000000001" // the old defaults
		hasSup = "00000000-0000-0000-0000-000000000002" // already made a Supermercado
		food   = "00000000-0000-0000-0000-0000000000a1"
		salud  = "00000000-0000-0000-0000-0000000000a2"
		food2  = "00000000-0000-0000-0000-0000000000b1"
		super2 = "00000000-0000-0000-0000-0000000000b2"
		a      = "00000000-0000-0000-0000-00000000000a"
		b      = "00000000-0000-0000-0000-00000000000b"
	)
	exec(t, db, `INSERT INTO users (id, email, password_hash, full_name) VALUES
		($1, 'a@example.com', 'x', 'A'), ($2, 'b@example.com', 'x', 'B')`, plain, hasSup)
	exec(t, db, `INSERT INTO categories (id, user_id, name, kind, is_system) VALUES
		($3, $1, 'Alimentación', 'expense', true),
		($4, $1, 'Salud', 'expense', true),
		($5, $2, 'Alimentación', 'expense', true),
		($6, $2, 'Supermercado', 'expense', false)`, plain, hasSup, food, salud, food2, super2)
	exec(t, db, `INSERT INTO category_rules (id, user_id, match_text, category_id) VALUES
		('00000000-0000-0000-0000-000000000f01', $1, 'Comida y Bebida', $3),
		('00000000-0000-0000-0000-000000000f02', $1, 'Supermercados', $3),
		('00000000-0000-0000-0000-000000000f03', $1, 'Popeyes', $3),
		('00000000-0000-0000-0000-000000000f04', $2, 'Comida y Bebida', $4)`, plain, hasSup, food, food2)
	exec(t, db, `INSERT INTO accounts (id, user_id, class, type, institution, external_number) VALUES
		($3, $1, 'asset', 'checking', 'bac', '0001'),
		($4, $2, 'asset', 'checking', 'bac', '0002')`, plain, hasSup, a, b)
	line := func(id, user, account, bankCategory, description, category, source string) {
		t.Helper()
		exec(t, db, `INSERT INTO entries (id, user_id, account_id, booked_on, amount_cents, kind,
				bank_category, description, category_id, category_source, dedup_key)
			VALUES ($1::uuid, $2, $3, '2026-10-01', -1000, 'expense', $4, $5,
				NULLIF($6, '')::uuid, NULLIF($7, ''), $1::text)`,
			id, user, account, bankCategory, description, category, source)
	}
	line("00000000-0000-0000-0000-000000000e01", plain, a, "Comida y Bebida", "SUSHI", food, "rule")     // → Restaurantes
	line("00000000-0000-0000-0000-000000000e02", plain, a, "COMIDA Y BEBIDA", "PIZZA", food, "rule")     // → Restaurantes
	line("00000000-0000-0000-0000-000000000e03", plain, a, "Supermercados", "SUPER 99", food, "rule")    // stays
	line("00000000-0000-0000-0000-000000000e04", plain, a, "Comida y Bebida", "CAFE", food, "user")      // the owner chose: stays
	line("00000000-0000-0000-0000-000000000e05", plain, a, "Comida y Bebida", "FARMACIA", salud, "rule") // another category: stays
	line("00000000-0000-0000-0000-000000000e06", hasSup, b, "Comida y Bebida", "SUSHI", food2, "rule")   // not renamed: stays

	migrateTo(t, db, 22)

	if n := count(t, db, `SELECT count(*) FROM categories WHERE id = '`+food+`' AND name = 'Supermercado'`); n != 1 {
		t.Error("the default Alimentación was not renamed Supermercado")
	}
	if n := count(t, db, `SELECT count(*) FROM categories WHERE id = '`+food2+`' AND name = 'Alimentación'`); n != 1 {
		t.Error("the Alimentación of a user who already had a Supermercado was renamed")
	}
	for _, user := range []string{plain, hasSup} {
		if n := count(t, db, `SELECT count(*) FROM categories WHERE user_id = '`+user+`' AND parent_id IS NULL
			AND name = 'Restaurantes' AND kind = 'expense' AND is_system`); n != 1 {
			t.Errorf("user %s has %d Restaurantes, want one", user, n)
		}
	}

	restaurantes := `(SELECT id FROM categories WHERE user_id = '` + plain + `' AND name = 'Restaurantes')`
	if n := count(t, db, `SELECT count(*) FROM category_rules WHERE id = '00000000-0000-0000-0000-000000000f01'
		AND category_id = `+restaurantes); n != 1 {
		t.Error("the rule for Comida y Bebida does not go to Restaurantes")
	}
	if n := count(t, db, `SELECT count(*) FROM category_rules WHERE category_id = '`+food+`'
		AND id IN ('00000000-0000-0000-0000-000000000f02', '00000000-0000-0000-0000-000000000f03')`); n != 2 {
		t.Error("a rule other than Comida y Bebida moved")
	}
	if n := count(t, db, `SELECT count(*) FROM category_rules WHERE id = '00000000-0000-0000-0000-000000000f04'
		AND category_id = '`+food2+`'`); n != 1 {
		t.Error("the rule of a user whose Alimentación was not renamed moved")
	}

	if n := count(t, db, `SELECT count(*) FROM entries WHERE category_id = `+restaurantes+`
		AND category_source = 'rule' AND id IN ('00000000-0000-0000-0000-000000000e01',
		'00000000-0000-0000-0000-000000000e02')`); n != 2 {
		t.Errorf("lines of Comida y Bebida under Restaurantes = %d, want 2", n)
	}
	if n := count(t, db, `SELECT count(*) FROM entries WHERE
		(id = '00000000-0000-0000-0000-000000000e03' AND category_id = '`+food+`') OR
		(id = '00000000-0000-0000-0000-000000000e04' AND category_id = '`+food+`') OR
		(id = '00000000-0000-0000-0000-000000000e05' AND category_id = '`+salud+`') OR
		(id = '00000000-0000-0000-0000-000000000e06' AND category_id = '`+food2+`')`); n != 4 {
		t.Errorf("lines that should stay where they were = %d, want 4", n)
	}

	// Down gives Alimentación its name back and leaves Restaurantes, which is
	// the owner's now. Up again makes no second Restaurantes.
	sqlDB := stdlib.OpenDBFromPool(db.pool)
	defer sqlDB.Close()
	if err := goose.DownToContext(context.Background(), sqlDB, ".", 21); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM categories WHERE id = '`+food+`' AND name = 'Alimentación'`); n != 1 {
		t.Error("Down did not give Alimentación its name back")
	}
	if n := count(t, db, `SELECT count(*) FROM categories WHERE id = '`+super2+`' AND name = 'Supermercado'`); n != 1 {
		t.Error("Down renamed a Supermercado the owner had made")
	}
	migrateTo(t, db, 22)
	if n := count(t, db, `SELECT count(*) FROM categories WHERE user_id = '`+plain+`' AND name = 'Restaurantes'`); n != 1 {
		t.Errorf("after Up again a user has %d Restaurantes, want one", n)
	}
}
