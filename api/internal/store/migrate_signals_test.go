package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migration 00017 gives every capture noted before it its first signal, the
// way it arrived, and its Down takes away only what cannot exist without it:
// a purchase another channel told of too is that channel's again.
func TestEveryCaptureNotedBeforeSignalsGetsItsFirstOne(t *testing.T) {
	db := scratchDB(t)
	migrateTo(t, db, 15)
	exec(t, db, `INSERT INTO users (id, email, password_hash, full_name) VALUES
		('00000000-0000-0000-0000-000000000001', 'a@example.com', 'x', 'A')`)
	exec(t, db, `INSERT INTO accounts (id, user_id, class, type, institution, external_number) VALUES
		('00000000-0000-0000-0000-00000000000a', '00000000-0000-0000-0000-000000000001', 'liability', 'credit_card', 'bac', '**** 0001')`)
	exec(t, db, `INSERT INTO captures (id, user_id, account_id, source, received_at, booked_on, amount_cents, merchant, raw) VALUES
		('00000000-0000-0000-0000-0000000000c1', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-00000000000a',
		 'shortcut', '2026-10-15 12:41:00-05', '2026-10-15', -450, 'Café', '{"amount": "$4.50", "merchant": "Café"}'),
		('00000000-0000-0000-0000-0000000000c2', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-00000000000a',
		 'manual', '2026-10-16 08:00:00-05', '2026-10-14', -1200, 'Super', '{}')`)

	migrateTo(t, db, 17)
	if n := count(t, db, `SELECT count(*) FROM capture_signals s JOIN captures c ON c.id = s.capture_id
		WHERE s.user_id = c.user_id AND s.channel = c.source AND s.at = c.received_at AND s.raw = c.raw
		  AND s.alert_id IS NULL`); n != 2 {
		t.Errorf("captures with their first signal = %d, want both", n)
	}
	if n := count(t, db, `SELECT count(*) FROM capture_signals`); n != 2 {
		t.Errorf("signals = %d, want one each", n)
	}
	// The assistant's purchases: one only it told of, one the shortcut and the
	// owner's hand told of too, the shortcut first, and one only typed too.
	exec(t, db, `INSERT INTO captures (id, user_id, account_id, source, received_at, booked_on, amount_cents, merchant) VALUES
		('00000000-0000-0000-0000-0000000000c3', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-00000000000a',
		 'agent', '2026-10-16 09:00:00-05', '2026-10-16', -700, 'Kiosco'),
		('00000000-0000-0000-0000-0000000000c4', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-00000000000a',
		 'agent', '2026-10-17 09:00:00-05', '2026-10-16', -2500, 'Gasolinera'),
		('00000000-0000-0000-0000-0000000000c5', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-00000000000a',
		 'agent', '2026-10-17 09:00:00-05', '2026-10-15', -999, 'Libros')`)
	exec(t, db, `INSERT INTO capture_signals (capture_id, user_id, channel, at, raw, caller_id) VALUES
		('00000000-0000-0000-0000-0000000000c3', '00000000-0000-0000-0000-000000000001', 'agent', '2026-10-16 09:00:00-05', '{}', NULL),
		('00000000-0000-0000-0000-0000000000c4', '00000000-0000-0000-0000-000000000001', 'agent', '2026-10-17 09:00:00-05', '{}', NULL),
		('00000000-0000-0000-0000-0000000000c4', '00000000-0000-0000-0000-000000000001', 'manual', '2026-10-17 10:00:00-05',
		 '{"merchant": "Gasolina"}', '00000000-0000-0000-0000-0000000000f1'),
		('00000000-0000-0000-0000-0000000000c4', '00000000-0000-0000-0000-000000000001', 'shortcut', '2026-10-16 19:05:00-05',
		 '{"amount": "$25.00", "merchant": "Gasolinera"}', NULL),
		('00000000-0000-0000-0000-0000000000c5', '00000000-0000-0000-0000-000000000001', 'agent', '2026-10-17 09:00:00-05', '{}', NULL),
		('00000000-0000-0000-0000-0000000000c5', '00000000-0000-0000-0000-000000000001', 'manual', '2026-10-17 11:00:00-05',
		 '{"merchant": "Libros"}', NULL)`)

	// Down: what the shortcut or the owner's hand told of too stays theirs,
	// as the first of them noted it; only what the assistant alone told of
	// goes, and every other capture stays as it was.
	sqlDB := stdlib.OpenDBFromPool(db.pool)
	defer sqlDB.Close()
	if err := goose.DownToContext(context.Background(), sqlDB, ".", 15); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT count(*) FROM captures`); n != 4 {
		t.Errorf("captures after Down = %d, want the two from before and two the assistant did not alone tell of", n)
	}
	if n := count(t, db, `SELECT count(*) FROM captures WHERE id = '00000000-0000-0000-0000-0000000000c3'`); n != 0 {
		t.Error("the purchase only the assistant told of survived Down")
	}
	if n := count(t, db, `SELECT count(*) FROM captures WHERE id = '00000000-0000-0000-0000-0000000000c4'
		AND source = 'shortcut' AND received_at = '2026-10-16 19:05:00-05' AND raw->>'amount' = '$25.00'
		AND booked_on = '2026-10-16' AND amount_cents = -2500 AND merchant = 'Gasolinera'`); n != 1 {
		t.Error("the purchase the shortcut told of too is not the shortcut's, as it sent it, after Down")
	}
	if n := count(t, db, `SELECT count(*) FROM captures WHERE id = '00000000-0000-0000-0000-0000000000c5'
		AND source = 'manual' AND received_at = '2026-10-17 11:00:00-05' AND amount_cents = -999`); n != 1 {
		t.Error("the purchase typed too is not the owner's after Down")
	}
	if n := count(t, db, `SELECT count(*) FROM captures WHERE id IN ('00000000-0000-0000-0000-0000000000c1',
		'00000000-0000-0000-0000-0000000000c2') AND source IN ('shortcut', 'manual')`); n != 2 {
		t.Error("the captures from before changed in Down")
	}
	if n := count(t, db, `SELECT count(*) FROM pg_tables WHERE tablename = 'capture_signals'`); n != 0 {
		t.Error("capture_signals survived Down")
	}
	if _, err := db.pool.Exec(context.Background(), `INSERT INTO captures (id, user_id, account_id, source, booked_on, amount_cents)
		VALUES (gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-00000000000a',
		        'agent', '2026-10-16', -700)`); err == nil {
		t.Error("an agent capture after Down was accepted")
	}

	// And Up again gives each its first signal.
	migrateTo(t, db, 17)
	if n := count(t, db, `SELECT count(*) FROM capture_signals`); n != 4 {
		t.Errorf("signals after Up again = %d, want 4", n)
	}
}
