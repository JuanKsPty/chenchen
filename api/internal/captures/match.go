// Package captures pairs each purchase the owner noted — by hand, or with the
// phone's shortcut — with the line its statement printed for it, and lets the
// owner note, correct, link, discard and review them.
//
// A noted purchase is never a row of entries and never reaches a balance
// (ADR-0002). Whether it counts as spending is derived when read: pending
// until a printed line could be it. Linking it to that line therefore changes
// no total — the line already counts in its place — and only carries over
// what the owner said about the purchase: its category and its note.
package captures

import (
	"context"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// AutoLink links the user's open captures in these accounts (all of them when
// none are given) to their printed lines, inside the caller's transaction, and
// reports how many it linked. It links only what needs no judgement: a capture
// with exactly one line it could be, when that line could be no other capture
// — the uniqueness rule transfers.AutoMatch uses for transfers. Anything
// ambiguous, such as two equal coffees on one day, stays printed, which
// already keeps it from counting, for the owner to settle in review.
//
// A line that counts as something other than an expense, say one the owner
// or transfers.AutoMatch called a transfer, still keeps its purchase from
// counting, but is never linked on a guess: the owner can link it by hand.
// That is why AutoLink runs after AutoMatch in an import.
func AutoLink(ctx context.Context, q *store.Queries, userID uuid.UUID, accountIDs []uuid.UUID) (int, error) {
	candidates, err := q.CaptureCandidates(ctx, userID, accountIDs)
	if err != nil {
		return 0, err
	}
	perCapture := map[uuid.UUID]int{}
	perLine := map[uuid.UUID]int{}
	for _, c := range candidates {
		perCapture[c.CaptureID]++
		perLine[c.Line.ID]++
	}
	linked := 0
	for _, c := range candidates {
		if perCapture[c.CaptureID] != 1 || perLine[c.Line.ID] != 1 || c.Line.EffectiveKind() != store.KindExpense {
			continue
		}
		if err := q.LinkCapture(ctx, userID, c.CaptureID, c.Line.ID, store.LinkedByAuto); err != nil {
			return linked, err
		}
		linked++
	}
	return linked, nil
}

// couldBePurchase reports whether a printed line can be a noted purchase: one
// the importer or the owner called an expense, as capture_candidates reads
// it. Calling a line a transfer later must not make its purchase count again,
// and must not stop the owner linking them either.
func couldBePurchase(e store.Entry) bool {
	return e.Kind == store.KindExpense || (e.UserKind != nil && *e.UserKind == store.KindExpense)
}
