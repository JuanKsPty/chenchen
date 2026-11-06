-- What a bank or a broker charges for itself (a fraud insurance, an ACH fee,
-- the tax held back from a dividend, a card's interest) is spending, but no
-- shop prints it and no bank labels it, so until now it waited in "Sin
-- categoría" for the owner. The importer already knows such a line is a charge
-- (its kind says fee or interest); now it also knows where it goes.
--
-- fallback_for marks the one category of a user that charges fall to when no
-- rule of theirs placed them. It is a column, not a name to look up, so the
-- owner may rename the category and it keeps its job; deleting it simply
-- leaves charges uncategorised again.
--
-- Every user also gets "Efectivo", for cash taken out at an ATM: spending
-- whose shop nobody will ever know. Unlike charges, nothing files there on its
-- own; the owner's rules do.

-- +goose Up
ALTER TABLE categories
    ADD COLUMN fallback_for TEXT CHECK (fallback_for IN ('fee')),
    ADD CONSTRAINT categories_fallback_is_expense CHECK (fallback_for IS NULL OR kind = 'expense');

CREATE UNIQUE INDEX categories_user_fallback_idx
    ON categories (user_id, fallback_for) WHERE fallback_for IS NOT NULL;

-- The two new defaults, for whoever does not have a top-level category by that
-- name already (whatever its kind: the name is unique at the top level).
INSERT INTO categories (id, user_id, name, kind, is_system)
SELECT gen_random_uuid(), u.id, d.name, 'expense', true
FROM users u
CROSS JOIN (VALUES ('Comisiones'), ('Efectivo')) AS d (name)
WHERE NOT EXISTS (
    SELECT 1 FROM categories c WHERE c.user_id = u.id AND c.parent_id IS NULL AND c.name = d.name
);

-- Charges fall to each user's own expense category called Comisiones, the one
-- just made or one they already had.
UPDATE categories SET fallback_for = 'fee'
WHERE id IN (
    SELECT DISTINCT ON (user_id) id
    FROM categories
    WHERE parent_id IS NULL AND kind = 'expense' AND lower(name) = 'comisiones'
    ORDER BY user_id, (name = 'Comisiones') DESC, created_at
);

-- And the charges already imported go there too, unless someone chose
-- otherwise: a line the owner or Claude left without a category stays so.
UPDATE entries e
SET category_id = c.id, category_source = 'rule'
FROM categories c
WHERE c.user_id = e.user_id
  AND c.fallback_for = 'fee'
  AND e.category_id IS NULL
  AND COALESCE(e.category_source, 'rule') = 'rule'
  AND COALESCE(e.user_kind, e.kind) IN ('fee', 'interest')
  AND e.amount_cents < 0;

-- +goose Down
-- The categories and what was filed under them stay: they are the owner's now.
DROP INDEX IF EXISTS categories_user_fallback_idx;
ALTER TABLE categories
    DROP CONSTRAINT IF EXISTS categories_fallback_is_expense,
    DROP COLUMN IF EXISTS fallback_for;
