-- A card's closed statement, read from its PDF for the one thing no other
-- export of the card prints: the debt at the cut. Its import runs are
-- 'bg_card_statement'. It stores a statement with no lines and no opening, and
-- its closing balance as the card's checkpoint on the cut day; the lines come
-- from the card's text or Excel export, as before.

--
-- Such a statement prints no lines, so it cannot say a noted purchase is
-- missing from it: capture_states no longer lets a card statement's summary
-- mark a purchase 'missing'. Without that, importing the PDF before the lines
-- it summarises would call every purchase of the period missing.

-- +goose Up
ALTER TABLE import_runs DROP CONSTRAINT import_runs_source_check; -- 00011's inline CHECK
ALTER TABLE import_runs ADD CONSTRAINT import_runs_source_check
    CHECK (source IN ('bg_account', 'bac_account', 'bg_card', 'bg_card_statement', 'ibkr_flex'));

CREATE OR REPLACE VIEW capture_states AS
SELECT c.id, c.user_id, c.account_id, c.source, c.key_id, c.received_at, c.booked_on, c.amount_cents,
       c.merchant, c.category_id, c.category_source, c.note, c.entry_id, c.linked_by, c.discarded_at,
       c.raw, c.created_at, c.updated_at,
       CASE WHEN c.discarded_at IS NOT NULL THEN 'discarded'
            WHEN c.entry_id IS NOT NULL THEN 'linked'
            WHEN EXISTS (SELECT 1 FROM capture_candidates k WHERE k.capture_id = c.id) THEN 'printed'
            WHEN EXISTS (SELECT 1 FROM statements s
                          WHERE s.account_id = c.account_id AND s.period_end >= c.booked_on + 3
                            AND NOT EXISTS (SELECT 1 FROM import_runs r
                                             WHERE r.id = s.run_id AND r.source = 'bg_card_statement')) THEN 'missing'
            ELSE 'pending' END AS state
  FROM captures c;

-- +goose Down
CREATE OR REPLACE VIEW capture_states AS
SELECT c.id, c.user_id, c.account_id, c.source, c.key_id, c.received_at, c.booked_on, c.amount_cents,
       c.merchant, c.category_id, c.category_source, c.note, c.entry_id, c.linked_by, c.discarded_at,
       c.raw, c.created_at, c.updated_at,
       CASE WHEN c.discarded_at IS NOT NULL THEN 'discarded'
            WHEN c.entry_id IS NOT NULL THEN 'linked'
            WHEN EXISTS (SELECT 1 FROM capture_candidates k WHERE k.capture_id = c.id) THEN 'printed'
            WHEN EXISTS (SELECT 1 FROM statements s
                          WHERE s.account_id = c.account_id AND s.period_end >= c.booked_on + 3) THEN 'missing'
            ELSE 'pending' END AS state
  FROM captures c;

-- Its statements and checkpoints go with the runs.
DELETE FROM import_runs WHERE source = 'bg_card_statement';
ALTER TABLE import_runs DROP CONSTRAINT import_runs_source_check;
ALTER TABLE import_runs ADD CONSTRAINT import_runs_source_check
    CHECK (source IN ('bg_account', 'bac_account', 'bg_card', 'ibkr_flex'));
