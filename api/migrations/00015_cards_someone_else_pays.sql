-- A card someone else pays: one the owner uses while somebody else — their
-- father, say — gets its statements and pays its bill. Its purchases are the
-- owner's spending, so they count in Gastos and the month's flow. Its debt is
-- not the owner's, so it is neither part of what they owe nor of their net
-- worth (paid_by_other).
--
-- The owner never sees its statements, so the app keeps it by hand: it lives
-- only on the purchases noted with the phone's shortcut or typed (ADR-0002).
-- Its institution is 'manual', and nothing printed can land in it: no line,
-- statement, stated balance or import run. Its balance is therefore always
-- zero, so it can never lack an opening balance or fail to match a bank, and
-- its noted purchases, with no line to turn out to be and no statement to pass
-- them by, are always pending and always count.
--
-- It is still a card and a liability, so accounts_card_is_liability holds as
-- it is. Its number is '**** ' and its last four digits, so the existing unique
-- key refuses a second one ending in the same digits; its name is the owner's
-- alias for it, and display_name stays empty.

-- +goose Up
ALTER TABLE accounts DROP CONSTRAINT accounts_institution_check; -- 00011's inline CHECK
ALTER TABLE accounts
    ADD CONSTRAINT accounts_institution_check CHECK (institution IN ('banco_general', 'bac', 'ibkr', 'manual')),
    ADD COLUMN paid_by_other BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT accounts_paid_by_other_is_card CHECK (NOT paid_by_other OR type = 'credit_card'),
    ADD CONSTRAINT accounts_manual_is_paid_by_other CHECK (institution <> 'manual' OR paid_by_other);

-- An account kept by hand takes nothing a statement or a sync printed. An
-- account's institution never changes, so checking it as a row arrives, or
-- moves to another account, is enough.
-- +goose StatementBegin
CREATE FUNCTION printed_accounts_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.account_id IS NOT NULL AND EXISTS (
        SELECT 1 FROM accounts WHERE id = NEW.account_id AND institution = 'manual') THEN
        RAISE EXCEPTION '%: account % is kept by hand; nothing printed can land in it', TG_TABLE_NAME, NEW.account_id
            USING ERRCODE = 'check_violation', CONSTRAINT = 'printed_accounts_only';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER entries_printed_only BEFORE INSERT OR UPDATE OF account_id ON entries
    FOR EACH ROW EXECUTE FUNCTION printed_accounts_only();
CREATE TRIGGER statements_printed_only BEFORE INSERT OR UPDATE OF account_id ON statements
    FOR EACH ROW EXECUTE FUNCTION printed_accounts_only();
CREATE TRIGGER checkpoints_printed_only BEFORE INSERT OR UPDATE OF account_id ON balance_checkpoints
    FOR EACH ROW EXECUTE FUNCTION printed_accounts_only();
CREATE TRIGGER import_runs_printed_only BEFORE INSERT OR UPDATE OF account_id ON import_runs
    FOR EACH ROW EXECUTE FUNCTION printed_accounts_only();

-- +goose Down
DROP TRIGGER IF EXISTS import_runs_printed_only ON import_runs;
DROP TRIGGER IF EXISTS checkpoints_printed_only ON balance_checkpoints;
DROP TRIGGER IF EXISTS statements_printed_only ON statements;
DROP TRIGGER IF EXISTS entries_printed_only ON entries;
DROP FUNCTION IF EXISTS printed_accounts_only();
-- A card kept by hand cannot exist without 'manual': it goes, and its noted
-- purchases and shortcut key with it.
DELETE FROM accounts WHERE institution = 'manual';
ALTER TABLE accounts
    DROP CONSTRAINT IF EXISTS accounts_manual_is_paid_by_other,
    DROP CONSTRAINT IF EXISTS accounts_paid_by_other_is_card,
    DROP COLUMN IF EXISTS paid_by_other,
    DROP CONSTRAINT accounts_institution_check,
    ADD CONSTRAINT accounts_institution_check CHECK (institution IN ('banco_general', 'bac', 'ibkr'));
