package store

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CategoryRule files a matching movement under a category, marks it as a
// transfer between the owner's own accounts, or both.
type CategoryRule struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	MatchText  string
	CategoryID *uuid.UUID
	// SetKind, when "transfer", marks matching movements as transfers.
	SetKind   *string
	Priority  int
	CreatedAt time.Time
}

// CreateCategoryRuleIfNew adds a rule unless the user already has one for the
// same text, in which case nothing changes.
func (q *Queries) CreateCategoryRuleIfNew(ctx context.Context, r CategoryRule) (bool, error) {
	tag, err := q.q.Exec(ctx, `
		INSERT INTO category_rules (id, user_id, match_text, category_id, set_kind, priority)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, lower(btrim(match_text))) DO NOTHING`,
		r.ID, r.UserID, r.MatchText, r.CategoryID, r.SetKind, r.Priority)
	if err != nil {
		return false, wrap("store.CreateCategoryRuleIfNew", err)
	}
	return tag.RowsAffected() == 1, nil
}

// CategoryRulesByUser lists a user's rules in the order they are tried:
// highest priority first, then the longest (most specific) text, then oldest.
func (q *Queries) CategoryRulesByUser(ctx context.Context, userID uuid.UUID) ([]CategoryRule, error) {
	rows, err := q.q.Query(ctx, `
		SELECT id, user_id, match_text, category_id, set_kind, priority, created_at
		FROM category_rules WHERE user_id = $1
		ORDER BY priority DESC, char_length(btrim(match_text)) DESC, created_at, id`, userID)
	if err != nil {
		return nil, wrap("store.CategoryRulesByUser", err)
	}
	defer rows.Close()
	var out []CategoryRule
	for rows.Next() {
		var r CategoryRule
		if err := rows.Scan(&r.ID, &r.UserID, &r.MatchText, &r.CategoryID, &r.SetKind, &r.Priority, &r.CreatedAt); err != nil {
			return nil, wrap("store.CategoryRulesByUser", err)
		}
		out = append(out, r)
	}
	return out, wrap("store.CategoryRulesByUser", rows.Err())
}

// UpdateCategoryRule changes one of the user's rules.
func (q *Queries) UpdateCategoryRule(ctx context.Context, r CategoryRule) error {
	return q.execOne(ctx, "store.UpdateCategoryRule", `
		UPDATE category_rules SET match_text = $3, category_id = $4, set_kind = $5, priority = $6
		WHERE id = $1 AND user_id = $2`, r.ID, r.UserID, r.MatchText, r.CategoryID, r.SetKind, r.Priority)
}

// DeleteCategoryRule removes one of the user's rules.
func (q *Queries) DeleteCategoryRule(ctx context.Context, userID, id uuid.UUID) error {
	return q.execOne(ctx, "store.DeleteCategoryRule",
		`DELETE FROM category_rules WHERE id = $1 AND user_id = $2`, id, userID)
}

// RuleMatch is what the user's rules decide for a movement: the category to
// file it under and the kind it counts as; either may be nil.
type RuleMatch struct {
	CategoryID *uuid.UUID
	SetKind    *string
}

// foldRuleText is how a rule and the text it is matched against are compared:
// without case or Spanish accents, so «Café Unido» noted on the phone and
// «CAFE UNIDO» printed by the bank answer to the same rule. ruleTextSQL does
// the same in SQL, for rules applied to what is already stored.
func foldRuleText(s string) string {
	return accentFolder.Replace(strings.ToLower(s))
}

var accentFolder = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")

// ruleTextSQL folds a SQL text expression the way foldRuleText folds Go text.
// The capitals are listed too: lower() only knows ASCII under the C locale.
func ruleTextSQL(expr string) string {
	return "translate(lower(" + expr + "), 'áéíóúüñÁÉÍÓÚÜÑ', 'aeiouunaeiouun')"
}

// MatchRule tries the user's rules, in the order CategoryRulesByUser returns
// them (priority, then the most specific text), against a movement's text,
// ignoring case: a line's bank category and description, or the merchant of a
// purchase the owner noted. The first rule that names a category decides the
// category; the first that sets a kind decides the kind. ApplyCategoryRule is
// the same test in SQL, for what is already stored.
func MatchRule(haystack string, rules []CategoryRule) RuleMatch {
	haystack = foldRuleText(haystack)
	var m RuleMatch
	for _, r := range rules {
		text := foldRuleText(strings.TrimSpace(r.MatchText))
		if text == "" || !strings.Contains(haystack, text) {
			continue
		}
		if m.CategoryID == nil && r.CategoryID != nil {
			m.CategoryID = r.CategoryID
		}
		if m.SetKind == nil && r.SetKind != nil {
			m.SetKind = r.SetKind
		}
		if m.CategoryID != nil && m.SetKind != nil {
			break
		}
	}
	return m
}

// ApplyCategoryRule files the user's already-imported movements that match a
// rule, and the purchases they noted that are still open, by merchant. A
// category the owner or the assistant chose is never overwritten, and a kind
// the owner set is never changed; a noted purchase is always spending, so a
// rule's kind never touches one.
//
// The count is what the owner sees change: lines, and noted purchases that
// still count. One a printed line could be, or a statement passed, is filed
// too, so its category reaches its line if they are linked, but it is not
// listed as a movement and is not counted.
//
// The caller holds the user's lock (LockUser): this writes lines, then
// captures, the other order from LinkCapture, and the lock is what keeps the
// two from deadlocking.
func (q *Queries) ApplyCategoryRule(ctx context.Context, r CategoryRule) (int64, error) {
	pattern := "%" + escapeLike(foldRuleText(strings.TrimSpace(r.MatchText))) + "%"
	var total int64
	if r.CategoryID != nil {
		tag, err := q.q.Exec(ctx, `
			UPDATE entries SET category_id = $3, category_source = 'rule'
			WHERE user_id = $1 AND `+ruleTextSQL("bank_category || ' ' || description")+` LIKE $2
			  AND (category_source IS NULL OR category_source = 'rule')`, r.UserID, pattern, *r.CategoryID)
		if err != nil {
			return 0, wrap("store.ApplyCategoryRule", err)
		}
		total += tag.RowsAffected()

		// capture_states reads the rows as they were before the update, which
		// is fine: a capture's state does not depend on its category.
		var pending int64
		err = q.q.QueryRow(ctx, `
			WITH filed AS (
				UPDATE captures SET category_id = $3, category_source = 'rule', updated_at = now()
				WHERE user_id = $1 AND `+ruleTextSQL("merchant")+` LIKE $2
				  AND (category_source IS NULL OR category_source = 'rule')
				  AND entry_id IS NULL AND discarded_at IS NULL
				RETURNING id)
			SELECT count(*) FROM filed JOIN capture_states s ON s.id = filed.id
			WHERE s.state = 'pending'`, r.UserID, pattern, *r.CategoryID).Scan(&pending)
		if err != nil {
			return 0, wrap("store.ApplyCategoryRule", err)
		}
		total += pending
	}
	if r.SetKind != nil {
		tag, err := q.q.Exec(ctx, `
			UPDATE entries SET user_kind = $3::entry_kind
			WHERE user_id = $1 AND `+ruleTextSQL("bank_category || ' ' || description")+` LIKE $2
			  AND user_kind IS NULL AND kind <> $3::entry_kind AND kind <> 'trade'`, r.UserID, pattern, *r.SetKind)
		if err != nil {
			return 0, wrap("store.ApplyCategoryRule", err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}
