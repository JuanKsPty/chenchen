-- The month's plan: what the owner expects to come in (one row per paycheck),
-- the payments that go out every month, what they set aside, the purchases
-- they plan, and an optional cap per category. From it the app says what is
-- free to spend this month and how much of it is left (ADR-0005).
--
-- It is metadata, like a category or a note: it never moves a balance
-- (ADR-0001). Nothing here writes a line, a noted purchase or a stated
-- balance, and nothing that builds a balance reads these tables.
--
-- Only what the owner decided is stored: the items, their values month by
-- month, the caps, the links where they told the app which movement paid
-- which item, and the suggestions they turned down. Everything actual (what
-- was spent, which movement paid what, how close a cap is) is worked out each
-- time the plan is read, from the same movements Gastos adds up, so the plan
-- can never disagree with them.
--
-- A value holds for a run of months, a version from from_month to to_month
-- (open-ended when to_month is NULL), rather than one row per month or one
-- standing plan:
--   * a past month keeps the plan it had: the app never writes a month before
--     the current one, so editing today cannot rewrite last month;
--   * next month is already planned, because an open version carries forward;
--   * an edit holds "from this month on" or "only this month", and either is
--     a split of the version that covered the month.
-- No two versions of an item, and no two caps of a category, cover the same
-- month. Triggers refuse it, because an exclusion constraint would need the
-- btree_gist extension and the API would then fail to boot wherever it is
-- missing. They raise check_violation with a constraint name, so the API can
-- tell which rule a write broke.
--
-- What lives on the item itself (its name, category, account, goal and the
-- text its movements are recognised by) holds for every month. Changing
-- match_text therefore changes which movements are recognised in past months
-- too, as re-filing a movement changes past Gastos; a planned amount of a past
-- month never changes. An item's kind never changes either: its versions
-- repeat it and point at the item through (id, kind, user_id), so the
-- database refuses to change it under them.
--
-- The names say plan, never budget: that word already names the assistant's
-- spending ceiling (00002_ai_budget).
--
-- Down drops the owner's plan with its tables; nothing else points at them.

-- +goose Up

-- What the owner plans. A month is always stored as its first day.
CREATE TABLE plan_items (
    -- Chosen by the caller, so saving the same item twice saves it once.
    id           UUID        PRIMARY KEY,
    user_id      UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind         TEXT        NOT NULL CHECK (kind IN ('income', 'fixed', 'saving', 'purchase')),
    name         TEXT        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    -- A label: an expense category for a payment or a purchase, an income
    -- category for a paycheck.
    category_id  UUID,
    -- The text a movement of this item contains, compared like a rule's.
    match_text   TEXT        NOT NULL DEFAULT '' CHECK (char_length(match_text) <= 60),
    -- A saving's real account, where its progress is read from.
    account_id   UUID,
    -- A saving's goal, when it has one; a purchase's price.
    target_cents BIGINT      CHECK (target_cents > 0),
    target_month DATE        CHECK (EXTRACT(DAY FROM target_month) = 1),
    -- A purchase: from when it is set aside, and how it is paid.
    start_month  DATE        CHECK (EXTRACT(DAY FROM start_month) = 1),
    funding      TEXT        CHECK (funding IN ('monthly', 'that_month', 'savings')),
    -- A purchase taken out of the plan from a month on: the last month that
    -- still plans it. The months before keep what they set aside, as a past
    -- month keeps the plan it had, and no month after it shows the purchase or
    -- takes a movement for it. It is not a new target_month, because every
    -- month before read the purchase as due then, and a purchase can be bought
    -- the month after its target. NULL while it is planned to its month.
    end_month    DATE        CHECK (EXTRACT(DAY FROM end_month) = 1),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, user_id),
    UNIQUE (id, kind, user_id),
    -- Deleting a category or an account leaves the item, without it.
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE SET NULL (category_id),
    FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE SET NULL (account_id),
    CONSTRAINT plan_items_account_is_saving CHECK (account_id IS NULL OR kind = 'saving'),
    -- A saving is recognised by its account, never by text or category.
    CONSTRAINT plan_items_saving_unmatched CHECK (kind <> 'saving' OR (match_text = '' AND category_id IS NULL)),
    CONSTRAINT plan_items_targets CHECK (kind IN ('saving', 'purchase') OR (target_cents IS NULL AND target_month IS NULL)),
    CONSTRAINT plan_items_purchase_shape CHECK (
        (kind = 'purchase') = (funding IS NOT NULL)
        AND (kind = 'purchase') = (start_month IS NOT NULL)
        AND (kind <> 'purchase' OR (target_cents IS NOT NULL AND target_month IS NOT NULL AND start_month <= target_month))),
    CONSTRAINT plan_items_purchase_end CHECK (end_month IS NULL OR (kind = 'purchase' AND end_month >= start_month))
);
CREATE INDEX plan_items_user_idx ON plan_items (user_id);

-- An item's values for a run of months.
CREATE TABLE plan_item_versions (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL,
    item_id      UUID        NOT NULL,
    kind         TEXT        NOT NULL,
    from_month   DATE        NOT NULL CHECK (EXTRACT(DAY FROM from_month) = 1),
    -- NULL: from from_month on, with no end.
    to_month     DATE        CHECK (EXTRACT(DAY FROM to_month) = 1),
    -- Per month; for a purchase, that month's share of the price.
    amount_cents BIGINT      NOT NULL CHECK (amount_cents > 0),
    -- The day it is expected. 31 means the last day of a shorter month: it
    -- is fitted to the month's length when read.
    day          SMALLINT    CHECK (day BETWEEN 1 AND 31),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (item_id, kind, user_id) REFERENCES plan_items (id, kind, user_id) ON DELETE CASCADE,
    CONSTRAINT plan_item_versions_order CHECK (to_month IS NULL OR to_month >= from_month),
    CONSTRAINT plan_item_versions_day_required CHECK (kind NOT IN ('income', 'fixed') OR day IS NOT NULL),
    CONSTRAINT plan_item_versions_purchase_ends CHECK (kind <> 'purchase' OR to_month IS NOT NULL)
);
CREATE UNIQUE INDEX plan_item_versions_item_from_idx ON plan_item_versions (item_id, from_month);
CREATE INDEX plan_item_versions_user_month_idx ON plan_item_versions (user_id, from_month);

-- A cap on what a category (and its children) takes of the free money,
-- versioned like an item's values. The category is its identity: deleting it
-- deletes its caps.
CREATE TABLE plan_caps (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL,
    category_id  UUID        NOT NULL,
    from_month   DATE        NOT NULL CHECK (EXTRACT(DAY FROM from_month) = 1),
    to_month     DATE        CHECK (EXTRACT(DAY FROM to_month) = 1),
    amount_cents BIGINT      NOT NULL CHECK (amount_cents > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (category_id, user_id) REFERENCES categories (id, user_id) ON DELETE CASCADE,
    CONSTRAINT plan_caps_order CHECK (to_month IS NULL OR to_month >= from_month)
);
CREATE UNIQUE INDEX plan_caps_category_from_idx ON plan_caps (category_id, from_month);
CREATE INDEX plan_caps_user_month_idx ON plan_caps (user_id, from_month);

-- Where the owner corrected the recognition of one month of an item: this
-- movement paid it (assign), this one did not (reject), or it was paid where
-- the app cannot see (paid). A movement is a printed line or a noted purchase,
-- never both, and a deleted one takes its links with it.
CREATE TABLE plan_links (
    id         UUID        PRIMARY KEY,
    user_id    UUID        NOT NULL,
    item_id    UUID        NOT NULL,
    -- The month of the item it is about (for a purchase, the month bought).
    month      DATE        NOT NULL CHECK (EXTRACT(DAY FROM month) = 1),
    decision   TEXT        NOT NULL CHECK (decision IN ('assign', 'reject', 'paid')),
    entry_id   UUID,
    capture_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (item_id, user_id) REFERENCES plan_items (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (entry_id, user_id) REFERENCES entries (id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (capture_id, user_id) REFERENCES captures (id, user_id) ON DELETE CASCADE,
    CONSTRAINT plan_links_one_movement CHECK (num_nonnulls(entry_id, capture_id) <= 1),
    CONSTRAINT plan_links_paid_has_no_movement CHECK ((decision = 'paid') = (entry_id IS NULL AND capture_id IS NULL))
);
-- A movement pays one item at a time.
CREATE UNIQUE INDEX plan_links_entry_assigned_idx ON plan_links (entry_id)
    WHERE decision = 'assign' AND entry_id IS NOT NULL;
CREATE UNIQUE INDEX plan_links_capture_assigned_idx ON plan_links (capture_id)
    WHERE decision = 'assign' AND capture_id IS NOT NULL;
-- And is said about one month of an item once.
CREATE UNIQUE INDEX plan_links_pair_idx ON plan_links (item_id, month, COALESCE(entry_id, capture_id))
    WHERE decision IN ('assign', 'reject');
CREATE UNIQUE INDEX plan_links_paid_idx ON plan_links (item_id, month) WHERE decision = 'paid';
CREATE INDEX plan_links_user_month_idx ON plan_links (user_id, month);

-- Suggestions the owner turned down, so they are not offered again: a payment
-- or an income by its folded text, a saving by 'account:<uuid>'.
CREATE TABLE plan_dismissals (
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT        NOT NULL CHECK (kind IN ('income', 'fixed', 'saving')),
    key        TEXT        NOT NULL CHECK (char_length(key) BETWEEN 1 AND 120),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, kind, key)
);

-- No two versions of an item cover the same month. The item's row is locked
-- first, so two writers of the same item take turns and the second one sees
-- what the first wrote; the API also writes under the user's lock.
-- +goose StatementBegin
CREATE FUNCTION plan_item_versions_no_overlap() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM plan_items WHERE id = NEW.item_id FOR NO KEY UPDATE;
    IF EXISTS (
        SELECT 1 FROM plan_item_versions v
        WHERE v.item_id = NEW.item_id AND v.id <> NEW.id
          AND v.from_month <= COALESCE(NEW.to_month, 'infinity'::date)
          AND NEW.from_month <= COALESCE(v.to_month, 'infinity'::date)) THEN
        RAISE EXCEPTION 'plan_item_versions: item % already has a version for a month from % to %',
            NEW.item_id, NEW.from_month, COALESCE(NEW.to_month::text, 'on')
            USING ERRCODE = 'check_violation', CONSTRAINT = 'plan_item_versions_no_overlap';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER plan_item_versions_no_overlap BEFORE INSERT OR UPDATE ON plan_item_versions
    FOR EACH ROW EXECUTE FUNCTION plan_item_versions_no_overlap();

-- The same for the caps of a category, taking turns on the category's row.
-- +goose StatementBegin
CREATE FUNCTION plan_caps_no_overlap() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM categories WHERE id = NEW.category_id FOR NO KEY UPDATE;
    IF EXISTS (
        SELECT 1 FROM plan_caps c
        WHERE c.category_id = NEW.category_id AND c.id <> NEW.id
          AND c.from_month <= COALESCE(NEW.to_month, 'infinity'::date)
          AND NEW.from_month <= COALESCE(c.to_month, 'infinity'::date)) THEN
        RAISE EXCEPTION 'plan_caps: category % already has a cap for a month from % to %',
            NEW.category_id, NEW.from_month, COALESCE(NEW.to_month::text, 'on')
            USING ERRCODE = 'check_violation', CONSTRAINT = 'plan_caps_no_overlap';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER plan_caps_no_overlap BEFORE INSERT OR UPDATE ON plan_caps
    FOR EACH ROW EXECUTE FUNCTION plan_caps_no_overlap();

-- +goose Down
-- The owner's plan goes with its tables.
DROP TRIGGER IF EXISTS plan_caps_no_overlap ON plan_caps;
DROP TRIGGER IF EXISTS plan_item_versions_no_overlap ON plan_item_versions;
DROP FUNCTION IF EXISTS plan_caps_no_overlap();
DROP FUNCTION IF EXISTS plan_item_versions_no_overlap();
DROP TABLE IF EXISTS plan_dismissals;
DROP TABLE IF EXISTS plan_links;
DROP TABLE IF EXISTS plan_caps;
DROP TABLE IF EXISTS plan_item_versions;
DROP TABLE IF EXISTS plan_items;
