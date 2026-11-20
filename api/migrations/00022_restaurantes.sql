-- "Alimentación" held two different kinds of spending: the weekly shop and a
-- meal out. They move differently (one is close to fixed, the other is where a
-- month gets away) and a card statement already tells them apart: its own
-- categories say "Supermercados" or "Comida y Bebida". So the one category
-- becomes two.
--
-- The default "Alimentación" is renamed "Supermercado" rather than replaced: it
-- keeps its id, so the owner's rules, the lines filed under it and any cap the
-- month's plan puts on it stay where they are. "Restaurantes" is new, and the
-- card's "Comida y Bebida" goes there, both the default rule and the lines it
-- had already filed. A line the owner or Claude chose a category for is left
-- as it is: only what a rule placed moves.
--
-- A user who already has a top-level "Supermercado" keeps their Alimentación
-- untouched; they still get Restaurantes, unless they made one themselves.

-- +goose Up
CREATE TEMP TABLE food_split ON COMMIT DROP AS
SELECT c.id, c.user_id
FROM categories c
WHERE c.parent_id IS NULL AND c.kind = 'expense' AND c.name = 'Alimentación'
  AND NOT EXISTS (
      SELECT 1 FROM categories o
      WHERE o.user_id = c.user_id AND o.parent_id IS NULL AND o.name = 'Supermercado'
  );

UPDATE categories c SET name = 'Supermercado', updated_at = now()
FROM food_split f
WHERE c.id = f.id;

INSERT INTO categories (id, user_id, name, kind, is_system)
SELECT gen_random_uuid(), u.id, 'Restaurantes', 'expense', true
FROM users u
WHERE NOT EXISTS (
    SELECT 1 FROM categories c WHERE c.user_id = u.id AND c.parent_id IS NULL AND c.name = 'Restaurantes'
);

-- The card's "Comida y Bebida", where it went to the old Alimentación, now goes
-- to Restaurantes, if that is spending (a Restaurantes of the owner's own could
-- be money they earn).
UPDATE category_rules r SET category_id = rest.id
FROM food_split f
JOIN categories rest ON rest.user_id = f.user_id AND rest.parent_id IS NULL
    AND rest.name = 'Restaurantes' AND rest.kind = 'expense'
WHERE r.category_id = f.id
  AND lower(btrim(r.match_text)) = 'comida y bebida';

-- And the lines that rule filed. The text is folded the way rules read it
-- (store.ruleTextSQL).
UPDATE entries e SET category_id = rest.id
FROM food_split f
JOIN categories rest ON rest.user_id = f.user_id AND rest.parent_id IS NULL
    AND rest.name = 'Restaurantes' AND rest.kind = 'expense'
WHERE e.category_id = f.id
  AND e.category_source = 'rule'
  AND translate(lower(e.bank_category || ' ' || COALESCE(e.description, '')),
                'áéíóúüñÁÉÍÓÚÜÑ', 'aeiouunaeiouun') LIKE '%comida y bebida%';

DROP TABLE food_split;

-- +goose Down
-- Restaurantes and what was filed there stay: they are the owner's now. Only
-- the name goes back.
UPDATE categories SET name = 'Alimentación', updated_at = now()
WHERE parent_id IS NULL AND kind = 'expense' AND name = 'Supermercado' AND is_system
  AND NOT EXISTS (
      SELECT 1 FROM categories o
      WHERE o.user_id = categories.user_id AND o.parent_id IS NULL AND o.name = 'Alimentación'
  );
