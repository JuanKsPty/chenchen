-- Purchases the owner noted before a statement printed them: sent by the
-- phone's shortcut the moment a card is tapped, or typed by hand (ADR-0002).
--
-- They live apart from entries, which stays one row per line a statement
-- printed (ADR-0001). Every balance, anchor, drift, reconciliation and transfer
-- match sums entries with no filter at all, so a query that does not know about
-- captures simply never sees one, and a noted purchase can never move a
-- balance or raise a mismatch.
--
-- Whether a capture counts as spending is derived when read, in
-- capture_states, never stored. Once a printed line could be the same purchase,
-- or a statement has passed its day without one, it stops counting: every
-- mistake leans toward counting a purchase late, never twice. The one
-- exception is a bank charging a different amount (a tip, a pre-authorization):
-- no line matches exactly, so both count until the owner links them or a
-- statement reaches the settle day.

-- +goose Up

-- A capture can be linked only to a line of its own account, and finding the
-- line it could be is a lookup by account, amount and day.
ALTER TABLE entries ADD CONSTRAINT entries_id_account_key UNIQUE (id, account_id);
CREATE INDEX entries_capture_match_idx ON entries (account_id, amount_cents, booked_on);

-- The key a shortcut writes with. It can note purchases in one account and do
-- nothing else; only its hash is stored, and the owner sees its last four
-- characters. Replacing it revokes the old one, so an account has at most one
-- that works.
CREATE TABLE capture_keys (
    id           UUID        PRIMARY KEY,
    user_id      UUID        NOT NULL,
    account_id   UUID        NOT NULL,
    key_hash     TEXT        NOT NULL UNIQUE,
    hint         TEXT        NOT NULL CHECK (char_length(hint) = 4),
    -- The first card name the shortcut sent, to notice when it changes.
    card_label   TEXT        NOT NULL DEFAULT '' CHECK (char_length(card_label) <= 80),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    tested_at    TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE CASCADE,
    UNIQUE (id, account_id)
);
CREATE UNIQUE INDEX capture_keys_one_live_idx ON capture_keys (account_id) WHERE revoked_at IS NULL;

CREATE TABLE captures (
    id              UUID        PRIMARY KEY,
    user_id         UUID        NOT NULL,
    account_id      UUID        NOT NULL,
    source          TEXT        NOT NULL CHECK (source IN ('shortcut', 'manual')),
    key_id          UUID,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The day in Panama the purchase reached the server. No statement format
    -- carries a purchase time, so the day is all matching ever compares.
    booked_on       DATE        NOT NULL,
    -- Signed from the holder's view, like entries: a purchase is negative.
    amount_cents    BIGINT      NOT NULL CHECK (amount_cents < 0),
    merchant        TEXT        NOT NULL DEFAULT '' CHECK (char_length(merchant) <= 140),
    category_id     UUID,
    category_source TEXT        CHECK (category_source IN ('rule', 'user', 'assistant')),
    note            TEXT        NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    -- The printed line this purchase turned out to be. Linking only carries the
    -- category and note over; the line is what counts.
    entry_id        UUID,
    linked_by       TEXT        CHECK (linked_by IN ('auto', 'user')),
    discarded_at    TIMESTAMPTZ,
    -- What the shortcut sent, as it sent it.
    raw             JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE SET NULL (category_id),
    -- Only a key of the same account wrote it.
    FOREIGN KEY (key_id, account_id) REFERENCES capture_keys (id, account_id) ON DELETE SET NULL (key_id),
    -- Only a line of its own account. A capture whose line is gone goes with
    -- it rather than start counting again.
    FOREIGN KEY (entry_id, account_id) REFERENCES entries (id, account_id) ON DELETE CASCADE,
    CONSTRAINT captures_linked_has_who CHECK ((entry_id IS NULL) = (linked_by IS NULL)),
    CONSTRAINT captures_linked_xor_discarded CHECK (entry_id IS NULL OR discarded_at IS NULL),
    UNIQUE (id, user_id)
);
CREATE UNIQUE INDEX captures_one_per_line_idx ON captures (entry_id) WHERE entry_id IS NOT NULL;
CREATE INDEX captures_account_day_idx ON captures (account_id, booked_on);
CREATE INDEX captures_recent_idx ON captures (account_id, received_at DESC);

-- "No es esta compra": a line the owner said is not this capture, so it is
-- never offered or linked again.
CREATE TABLE capture_rejections (
    capture_id UUID        NOT NULL,
    entry_id   UUID        NOT NULL,
    user_id    UUID        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (capture_id, entry_id),
    FOREIGN KEY (capture_id, user_id) REFERENCES captures (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (entry_id, user_id) REFERENCES entries (id, user_id) ON DELETE CASCADE
);

-- The printed lines an open capture could be: same account, exact amount, an
-- expense, booked from the day before to five days after, not already another
-- capture's line and never rejected for this one. The merchant is not
-- compared: the phone and the statement name the same shop differently.
-- The window is store.CaptureDaysBefore and store.CaptureDaysAfter; a test
-- pins them together.
--
-- An expense is what the importer or the owner called one. A line the importer
-- printed as an expense stays a candidate when the owner, a rule or a transfer
-- match later calls it something else: saying the line is a transfer or a fee
-- must make its purchase count less, never bring it back to pending.
CREATE VIEW capture_candidates AS
SELECT c.id AS capture_id, e.id AS entry_id, c.user_id, c.account_id
  FROM captures c
  JOIN entries e ON e.account_id = c.account_id AND e.amount_cents = c.amount_cents
                AND e.booked_on BETWEEN c.booked_on - 1 AND c.booked_on + 5
 WHERE c.entry_id IS NULL AND c.discarded_at IS NULL
   AND 'expense' IN (e.kind, e.user_kind)
   AND NOT EXISTS (SELECT 1 FROM captures m WHERE m.entry_id = e.id)
   AND NOT EXISTS (SELECT 1 FROM capture_rejections r WHERE r.capture_id = c.id AND r.entry_id = e.id);

-- Every capture with what it is now. Only 'pending' counts as spending:
--   discarded  the owner said it did not happen
--   linked     joined to its line, which counts instead
--   printed    a line it could be exists, and counts instead
--   missing    a statement of the account reaches three days past it and
--              shows nothing it could be; typically a declined tap
--   pending    nothing printed could be it yet
-- The three days are store.CaptureSettleDays: a statement can end on the day
-- it was downloaded, while the last few days' purchases are still on hold.
CREATE VIEW capture_states AS
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

-- +goose Down
DROP VIEW IF EXISTS capture_states;
DROP VIEW IF EXISTS capture_candidates;
DROP TABLE IF EXISTS capture_rejections, captures, capture_keys;
DROP INDEX IF EXISTS entries_capture_match_idx;
ALTER TABLE entries DROP CONSTRAINT IF EXISTS entries_id_account_key;
