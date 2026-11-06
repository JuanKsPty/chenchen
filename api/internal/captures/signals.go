package captures

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// How far apart two channels can tell of one purchase and still be joined
// into it. A test pins them: widening either joins more real purchases into
// one, narrowing either lets one purchase count twice.
const (
	// JoinWindow is how far apart in time two timed channels can hear of one
	// purchase. The shortcut fires as the card is tapped; the bank's email can
	// take minutes, more on a busy day. Two purchases of the same amount on
	// one account within the hour, one of them told of only by the shortcut
	// and the other only by email, are rarer than an email that late.
	JoinWindow = 60 * time.Minute
	// JoinDays is how many days apart the purchase's day and the day another
	// channel tells of it can be: a purchase typed the next morning, or a tap
	// late at night whose email arrives after midnight. Timed channels fall
	// back to it too, because an email can arrive more than JoinWindow late;
	// so two purchases of one amount hours apart on one day, one told of only
	// by the shortcut and the other only by email, join (ADR-0002).
	JoinDays = 1
)

// JoinSignal finds the purchase a new signal tells of among the ones already
// noted in the account, and records the signal on it: the shortcut and the
// bank's email both telling of one tap, or the owner typing a purchase the
// shortcut already sent. paid is what was paid, positive, and day the
// purchase's day; s says which channel heard of it, when, and what it sent.
// It runs inside the caller's transaction, under the user's lock, before
// anything is noted.
//
// The purchase must be of the same account and amount and have no signal of
// this channel yet: a channel that tells of two equal purchases is telling of
// two. A timed channel joins one in any state — pending, printed, missing,
// linked or discarded — looking first for one that another timed channel
// heard of within JoinWindow of s.At, the closest in time. Then, and for a
// purchase typed or noted by the assistant only, whose time is when it was
// written down, it looks for one booked within JoinDays of day: the closest
// day, then the closest in time. A purchase typed or noted by the assistant
// never joins a discarded one: the bank's email of a declined tap is that tap
// again, but the owner typing a purchase they discarded is saying it happened
// after all, and joined it would count for nothing.
//
// The purchase found is returned as it was: joining never changes it, so a
// linked or discarded one stays so and never counts again. joined is false
// when there is none; the caller then notes the purchase, with this signal as
// its first.
//
// When it errs, it leans to counting less: two real purchases of the same
// amount and day join into one only when each was told of by a channel the
// other lacks, and the statement's lines, which count on their own, bring the
// second one back. What it exists to prevent is the opposite: one purchase
// counted twice because two channels told of it at different times.
func JoinSignal(ctx context.Context, q *store.Queries, userID, accountID uuid.UUID, paid money.Cents, day civil.Date,
	s store.CaptureSignal) (c store.Capture, joined bool, err error) {
	if store.IsTimedChannel(s.Channel) {
		c, joined, err = q.CaptureToJoinNear(ctx, userID, accountID, -paid, s.Channel, s.At, JoinWindow)
		if err != nil {
			return store.Capture{}, false, err
		}
	}
	if !joined {
		c, joined, err = q.CaptureToJoinOn(ctx, userID, accountID, -paid, s.Channel, day, JoinDays, s.At,
			store.IsTimedChannel(s.Channel))
		if err != nil || !joined {
			return store.Capture{}, false, err
		}
	}
	s.CaptureID, s.UserID = c.ID, userID
	if err := q.InsertCaptureSignal(ctx, s); err != nil {
		return store.Capture{}, false, err
	}
	return c, true, nil
}
