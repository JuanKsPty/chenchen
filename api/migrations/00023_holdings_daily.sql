-- What a brokerage account's positions were worth, one figure per day.
--
-- Everything else a past net worth needs is already here: a bank account's or
-- a card's balance on any day is its opening plus the lines through that day.
-- A broker's positions are not: each sync replaces the snapshot in
-- investment_positions, so what they were worth last month is gone. This keeps
-- the total of each snapshot, by the day it was taken, so the history can read
-- it back. It starts empty: a day nobody synced has no figure, and the history
-- says so instead of making one up.

-- +goose Up
CREATE TABLE holdings_daily (
    account_id  UUID   NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    user_id     UUID   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day         DATE   NOT NULL,
    value_cents BIGINT NOT NULL,
    PRIMARY KEY (account_id, day)
);

CREATE INDEX holdings_daily_user_idx ON holdings_daily (user_id, account_id, day);

-- +goose Down
DROP TABLE IF EXISTS holdings_daily;
