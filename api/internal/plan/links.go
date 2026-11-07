package plan

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/movements"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// The owner corrects how the plan recognised one month of an item with a
// link: this movement paid it, this one did not, or it was paid where the app
// cannot see. A link is the only thing about recognition that is stored
// (ADR-0005); everything else is worked out again on every read, with the
// links applied first.

// Decision is what the owner said about one month of an item.
type Decision string

const (
	// DecisionAssign: this movement paid it. The movement pays nothing else,
	// and the month is charged with it, whichever month it was booked in.
	DecisionAssign Decision = store.PlanAssign
	// DecisionReject: this movement did not pay it, so the plan never
	// recognises it as that month of the item again, nor offers it.
	DecisionReject Decision = store.PlanReject
	// DecisionPaid: it was paid where the app cannot see, such as from an
	// account it does not hold. The month keeps what was planned, which is
	// how it counts, once.
	DecisionPaid Decision = store.PlanPaid
)

// linkMonths is how many months from the month of the item a movement may be
// booked and still be said to pay it: a payment made a few days late, in the
// month after, or early, in the month before. It holds when a link is read as
// well as when it is said. A noted purchase can end up further away, when
// its line prints days after it or the owner corrects its day, and the month
// it is booked in reads only the links of the months beside it. So a link
// that reaches further pays nothing, and the movement counts once, in its
// own month.
const linkMonths = 1

// Link is what the owner said about one month of an item, with the movement
// it names as that movement is now.
type Link struct {
	ID       uuid.UUID
	ItemID   uuid.UUID
	Month    Month
	Decision Decision
	// Movement is the movement it names, a printed line or a pending noted
	// purchase, or uuid.Nil when it names none: a month paid outside, a
	// noted purchase that was discarded or that a statement passed without
	// printing, or one a line could be but is not linked to (Lines).
	Movement uuid.UUID
	// Lines are the printed lines a noted purchase could be, oldest first,
	// while it is not linked to one. Every one has its amount, so whichever
	// the link takes, the month adds up the same.
	Lines []uuid.UUID
}

// names are the movements the link can be about.
func (l Link) names() []uuid.UUID {
	if l.Movement != uuid.Nil {
		return []uuid.UUID{l.Movement}
	}
	return l.Lines
}

// LinkOf follows a stored link to the movement it names now. A line is
// itself. A noted purchase is itself while it is pending, its line once
// linked to one, and the lines it could be once one is printed; discarded, or
// passed by a statement that never printed it, it is nothing, and the month
// it paid takes back what it had set aside. The stored link is never
// rewritten: a purchase restored, or unlinked from the wrong line, is
// followed again.
func LinkOf(l store.PlanLink) Link {
	out := Link{ID: l.ID, ItemID: l.ItemID, Month: MonthOf(l.Month), Decision: Decision(l.Decision)}
	switch {
	case l.EntryID != nil:
		out.Movement = *l.EntryID
	case l.CaptureID == nil:
	case l.CaptureState == store.CapturePending:
		out.Movement = *l.CaptureID
	case l.CaptureState == store.CaptureLinked && l.CaptureLine != nil:
		out.Movement = *l.CaptureLine
	case l.CaptureState == store.CapturePrinted:
		out.Lines = l.CaptureLines
	}
	return out
}

// LinkInput is what the owner says about month Month of item ItemID: that
// movement MovementID paid it (DecisionAssign) or did not (DecisionReject),
// or that it was paid where the app cannot see (DecisionPaid), with no
// movement.
type LinkInput struct {
	// ID, chosen by the caller, makes saying it safe to repeat: a second
	// Link with the same ID returns the link the first one made. A form that
	// lost the answer to its first send can send it again. uuid.Nil means a
	// new id.
	ID         uuid.UUID
	ItemID     uuid.UUID
	Month      Month
	MovementID uuid.UUID
	Decision   Decision
}

// check reports every field that is wrong at once.
func (in LinkInput) check() error {
	var errs []error
	switch in.Decision {
	case DecisionAssign, DecisionReject:
		if in.MovementID == uuid.Nil {
			errs = append(errs, ErrLinkMovement)
		}
	case DecisionPaid:
		if in.MovementID != uuid.Nil {
			errs = append(errs, ErrPaidMovement)
		}
	default:
		errs = append(errs, ErrDecision)
	}
	return errors.Join(errs...)
}

// Link records what the owner says about one month of one of their items,
// and created reports whether it is new. Saying it again changes nothing: the
// same id, or the same movement and month of the item with the same word,
// returns the link already there. Saying the opposite about the same
// movement, that it did not pay what they had said it paid or the other way
// round, replaces what they had said. A line is the same movement as the
// noted purchase linked to it, or printed with it among the lines it could
// be, since the plan shows the purchase as that line.
//
// Links may correct a month that has passed, since they change what the plan
// recognised, never what it planned, but none reaches past MonthsAhead. The
// month must be one the item is planned for, and a movement must be booked
// within linkMonths of it. A movement said to pay a paycheck must be income,
// and one said to pay a payment spending; it pays one item at a time, so
// saying it pays a second one is ErrMovementInPlan until the first link goes.
// Something of someone else's is not found.
func (s *Service) Link(ctx context.Context, userID uuid.UUID, in LinkInput) (link store.PlanLink, created bool, err error) {
	if in.Month.IsZero() {
		return store.PlanLink{}, false, ErrInvalidMonth
	}
	if in.Month.Sub(s.thisMonth()) > MonthsAhead {
		return store.PlanLink{}, false, fmt.Errorf("%w: %s", ErrMonthTooFar, in.Month)
	}
	if err := in.check(); err != nil {
		return store.PlanLink{}, false, err
	}
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	err = s.db.InTx(ctx, func(q *store.Queries) error {
		// The user's lock makes the checks below and the insert one step, so
		// two links on one movement take turns and the second hears of the
		// first.
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		said, err := q.PlanLinkByID(ctx, userID, in.ID)
		if err == nil {
			// Said already, by an earlier send whose answer was lost.
			link = said
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		item, err := linkedItem(ctx, q, userID, in.ItemID, in.Month)
		if err != nil {
			return err
		}
		want := store.PlanLink{ID: in.ID, UserID: userID, ItemID: item.ID, Month: in.Month.First(),
			Decision: string(in.Decision)}
		if in.Decision == DecisionPaid {
			said, err := q.PlanLinkPaidOutside(ctx, userID, item.ID, want.Month)
			if err == nil {
				link = said
				return nil
			}
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
		} else {
			mv, err := linkedMovement(ctx, q, userID, in, item)
			if err != nil {
				return err
			}
			if mv.Origin == store.OriginCapture {
				want.CaptureID = &mv.ID
			} else {
				want.EntryID = &mv.ID
			}
			if in.Decision == DecisionAssign {
				// Paying something else is checked first: then saying it pays
				// this too is refused, whatever was said about this month.
				other, err := q.PlanLinkAssigning(ctx, userID, mv.ID)
				switch {
				case err == nil && (other.ItemID != item.ID || other.Month != want.Month):
					return fmt.Errorf("%w: %s pays %s in %s", ErrMovementInPlan, mv.ID, other.ItemID,
						MonthOf(other.Month))
				case err != nil && !errors.Is(err, store.ErrNotFound):
					return err
				}
			}
			said, err := q.PlanLinksOnPair(ctx, userID, item.ID, want.Month, mv.ID)
			if err != nil {
				return err
			}
			// What they said before about this movement, or about the noted
			// purchase the plan shows as this line. The same word is said
			// already. The opposite one means they changed their mind, and
			// the newer word is the one that holds, so the older one goes.
			found := false
			for _, l := range said {
				switch {
				case l.Decision == want.Decision && !found:
					link, found = l, true
				case l.Decision != want.Decision:
					if err := q.DeletePlanLink(ctx, userID, l.ID); err != nil {
						return err
					}
				}
			}
			if found {
				return nil
			}
		}
		link, err = q.InsertPlanLink(ctx, want)
		switch {
		case store.IsConstraint(err, store.PlanLinksPKey):
			return fmt.Errorf("%w: %v", ErrLinkIDTaken, err)
		case store.IsConstraint(err, store.PlanLinkEntryAssigned), store.IsConstraint(err, store.PlanLinkCaptureAssigned):
			return fmt.Errorf("%w: %v", ErrMovementInPlan, err)
		case err != nil:
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return store.PlanLink{}, false, err
	}
	return link, created, nil
}

// linkedItem is the item a link is about, under the user's lock: one of
// theirs, a paycheck or a payment, planned for month m.
func linkedItem(ctx context.Context, q *store.Queries, userID, itemID uuid.UUID, m Month) (store.PlanItem, error) {
	item, err := q.PlanItemByID(ctx, userID, itemID)
	if errors.Is(err, store.ErrNotFound) {
		return store.PlanItem{}, fmt.Errorf("%w: %s", ErrItemNotFound, itemID)
	}
	if err != nil {
		return store.PlanItem{}, err
	}
	if item.Kind != KindIncome && item.Kind != KindFixed {
		return store.PlanItem{}, fmt.Errorf("%w: %s is %s", ErrLinkItemKind, itemID, item.Kind)
	}
	stored, err := q.PlanItemVersions(ctx, userID, itemID)
	if err != nil {
		return store.PlanItem{}, err
	}
	if !slices.ContainsFunc(itemVersions(stored), func(v Version) bool { return v.Holds(m) }) {
		return store.PlanItem{}, fmt.Errorf("%w: %s has no version for %s", ErrItemNotFound, itemID, m)
	}
	return item, nil
}

// linkedMovement is the movement a link names, found as movements.Service.Get
// finds it: a printed line, or a noted purchase that is still open. It must be
// booked within linkMonths of the month of the item, and, to pay it, be
// income for a paycheck and spending for a payment.
func linkedMovement(ctx context.Context, q *store.Queries, userID uuid.UUID, in LinkInput, item store.PlanItem) (store.Entry, error) {
	mv, err := q.EntryByID(ctx, userID, in.MovementID)
	if errors.Is(err, store.ErrNotFound) {
		mv, err = q.CaptureAsEntry(ctx, userID, in.MovementID)
	}
	if errors.Is(err, store.ErrNotFound) {
		return store.Entry{}, fmt.Errorf("%w: %v", movements.ErrNotFound, err)
	}
	if err != nil {
		return store.Entry{}, err
	}
	if apart := abs(MonthOf(mv.BookedOn).Sub(in.Month)); apart > linkMonths {
		return store.Entry{}, fmt.Errorf("%w: %s is booked on %s, %d months from %s",
			ErrMovementTooFar, mv.ID, mv.BookedOn, apart, in.Month)
	}
	if in.Decision != DecisionAssign {
		return mv, nil
	}
	switch kind := mv.EffectiveKind(); {
	case item.Kind == KindIncome && kind != store.KindIncome:
		return store.Entry{}, fmt.Errorf("%w: %s is %s", ErrNotIncome, mv.ID, kind)
	case item.Kind == KindFixed && !slices.Contains(store.SpendKinds, kind):
		return store.Entry{}, fmt.Errorf("%w: %s is %s", ErrNotSpending, mv.ID, kind)
	}
	return mv, nil
}

// Unlink forgets one of the user's links, which is how the owner undoes what
// they said: the month is recognised again as if they had never said it.
func (s *Service) Unlink(ctx context.Context, userID, id uuid.UUID) error {
	err := s.db.Q().DeletePlanLink(ctx, userID, id)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrLinkNotFound, id)
	}
	return err
}

// links reads what the owner said about the months from from to to, each
// followed to the movement it names now.
func links(ctx context.Context, q *store.Queries, userID uuid.UUID, from, to Month) ([]Link, error) {
	stored, err := q.PlanLinksBetween(ctx, userID, from.First(), to.First())
	if err != nil {
		return nil, err
	}
	out := make([]Link, 0, len(stored))
	for _, l := range stored {
		out = append(out, LinkOf(l))
	}
	return out, nil
}

// LinkView is a link on the wire. MovementID is the movement as the owner
// named it, a line or a noted purchase; null for a month paid outside.
type LinkView struct {
	ID         uuid.UUID  `json:"id"`
	ItemID     uuid.UUID  `json:"item_id"`
	Month      Month      `json:"month"`
	Decision   Decision   `json:"decision"`
	MovementID *uuid.UUID `json:"movement_id"`
	CreatedAt  time.Time  `json:"created_at"`
}

// NewLinkView builds a link's wire shape.
func NewLinkView(l store.PlanLink) LinkView {
	v := LinkView{ID: l.ID, ItemID: l.ItemID, Month: MonthOf(l.Month), Decision: Decision(l.Decision),
		CreatedAt: l.CreatedAt}
	if id := l.Movement(); id != uuid.Nil {
		v.MovementID = &id
	}
	return v
}
