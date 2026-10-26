package captures

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

var (
	// ErrNotFound means no such noted purchase belongs to the caller.
	ErrNotFound = errors.New("captures: purchase not found")
	// ErrAccountNotFound means no such account belongs to the caller.
	ErrAccountNotFound = errors.New("captures: account not found")
	// ErrBrokerage means the account is a brokerage account, which no card
	// pays from.
	ErrBrokerage = errors.New("captures: a brokerage account takes no purchases")
	// ErrMerchantRequired means the purchase says nothing about where it was.
	ErrMerchantRequired = errors.New("captures: the merchant is required")
	// ErrMerchantTooLong means the merchant is longer than maxMerchantRunes.
	ErrMerchantTooLong = errors.New("captures: the merchant is too long")
	// ErrNoteTooLong means the note is longer than maxNoteRunes.
	ErrNoteTooLong = errors.New("captures: the note is too long")
	// ErrAmountTooLarge means the amount is more than maxAmount.
	ErrAmountTooLarge = errors.New("captures: the amount is too large")
	// ErrFutureDay means the purchase is dated after today in Panama.
	ErrFutureDay = errors.New("captures: the day is after today")
	// ErrLinked means the purchase is linked to its line, which is the
	// movement now: unlink it first.
	ErrLinked = errors.New("captures: the purchase is linked to its line")
	// ErrDiscarded means the owner discarded the purchase: restore it first.
	ErrDiscarded = errors.New("captures: the purchase is discarded")
	// ErrNotLinked means the purchase has no line to unlink.
	ErrNotLinked = errors.New("captures: the purchase is not linked to a line")
	// ErrLineNotFound means no such printed line belongs to the caller.
	ErrLineNotFound = errors.New("captures: line not found")
	// ErrOtherAccount means the line is in a different account from the
	// purchase, so it cannot be that purchase.
	ErrOtherAccount = errors.New("captures: the line is in another account")
	// ErrNotAPurchase means neither the importer nor the owner called the
	// line an expense: a payment or a deposit is no purchase.
	ErrNotAPurchase = errors.New("captures: the line is not an expense")
	// ErrLineTaken means another purchase is already linked to the line.
	ErrLineTaken = errors.New("captures: the line is linked to another purchase")
	// ErrIDTaken means the id a caller chose for a new purchase is another
	// user's purchase.
	ErrIDTaken = errors.New("captures: the id belongs to another purchase")
)

const (
	// maxMerchantRunes and maxNoteRunes are the schema's limits, checked
	// here first so the owner hears which field is too long.
	maxMerchantRunes = 140
	maxNoteRunes     = 500
)

// maxAmount is the most one noted purchase can be. No card purchase comes
// close; a card number pasted into the amount does, and would count as
// spending, swamp every total and overflow the arithmetic that sums them.
const maxAmount money.Cents = 10_000_000 * 100

// Service notes purchases by hand and pairs them with their printed lines.
// Every write takes the user's lock, like an import, so a link and an import
// can never interleave over the same lines.
type Service struct {
	db         *store.DB
	categories *categories.Service
	now        func() time.Time
}

func NewService(db *store.DB, cats *categories.Service) *Service {
	return &Service{db: db, categories: cats, now: time.Now}
}

// Capture is a noted purchase as the owner sees it: what it is now and, once
// linked, the printed line it turned out to be.
type Capture struct {
	store.Capture
	Line *store.Entry
}

// AddInput is a purchase the owner types.
type AddInput struct {
	// ID, when the caller chooses one, makes noting safe to repeat: a second
	// Add with the same ID returns the purchase the first one noted instead
	// of noting it twice. A form that lost the answer to its first try can
	// send it again. Zero means a new id.
	ID        uuid.UUID
	AccountID uuid.UUID
	// Amount is what was paid, positive; it is stored with the holder's sign.
	Amount   money.Cents
	Merchant string
	// Day is the purchase's day; zero means today in Panama.
	Day        civil.Date
	CategoryID *uuid.UUID
	Note       string
}

// Add notes a purchase by hand in one of the user's accounts. Without a
// category, the user's rules file it by merchant, as they would its line. If
// its line is already imported and could be no other purchase, it is linked
// at once and never counts twice.
func (s *Service) Add(ctx context.Context, userID uuid.UUID, in AddInput) (Capture, error) {
	merchant, err := cleanMerchant(in.Merchant)
	if err != nil {
		return Capture{}, err
	}
	note, err := cleanNote(in.Note)
	if err != nil {
		return Capture{}, err
	}
	if err := checkAmount(in.Amount); err != nil {
		return Capture{}, err
	}
	day := in.Day
	if day.IsZero() {
		day = s.today()
	} else if err := s.checkDay(day); err != nil {
		return Capture{}, err
	}
	if in.CategoryID != nil {
		if _, err := s.categories.Get(ctx, userID, *in.CategoryID); err != nil {
			return Capture{}, err
		}
	}

	var out Capture
	err = s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		account, err := q.AccountByID(ctx, userID, in.AccountID)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%w: %v", ErrAccountNotFound, err)
		}
		if err != nil {
			return err
		}
		if account.Type == "brokerage" {
			return ErrBrokerage
		}
		id := in.ID
		if id == uuid.Nil {
			id = uuid.New()
		} else if noted, err := load(ctx, q, userID, id); err == nil {
			// Noted already, by an earlier try whose answer was lost. The
			// user's lock makes this check and the insert below one step.
			out = noted
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		c := store.Capture{ID: id, UserID: userID, AccountID: account.ID, Source: store.CaptureSourceManual,
			BookedOn: day, Amount: -in.Amount, Merchant: merchant, Note: note}
		if in.CategoryID != nil {
			src := "user"
			c.CategoryID, c.CategorySource = in.CategoryID, &src
		} else if c.CategoryID, err = ruleCategory(ctx, q, userID, merchant); err != nil {
			return err
		} else if c.CategoryID != nil {
			src := "rule"
			c.CategorySource = &src
		}
		if err := q.InsertCapture(ctx, c); store.IsConstraint(err, "captures_pkey") {
			return fmt.Errorf("%w: %v", ErrIDTaken, err)
		} else if err != nil {
			return err
		}
		if _, err := AutoLink(ctx, q, userID, []uuid.UUID{account.ID}); err != nil {
			return err
		}
		out, err = load(ctx, q, userID, c.ID)
		return err
	})
	return out, err
}

// EditInput corrects what was noted; a nil field stays as it is.
type EditInput struct {
	// Amount is positive, as in AddInput.
	Amount   *money.Cents
	Merchant *string
	Day      *civil.Date
}

// Edit corrects an open purchase's amount, merchant or day. Whether it counts
// derives again, and it is linked if its line is now obvious. A new merchant
// is filed again by the rules, unless the owner or the assistant chose its
// category.
func (s *Service) Edit(ctx context.Context, userID, id uuid.UUID, in EditInput) (Capture, error) {
	if in.Merchant != nil {
		merchant, err := cleanMerchant(*in.Merchant)
		if err != nil {
			return Capture{}, err
		}
		in.Merchant = &merchant
	}
	if in.Amount != nil {
		if err := checkAmount(*in.Amount); err != nil {
			return Capture{}, err
		}
	}
	if in.Day != nil && in.Day.IsZero() {
		in.Day = nil
	}
	if in.Day != nil {
		if err := s.checkDay(*in.Day); err != nil {
			return Capture{}, err
		}
	}

	var out Capture
	err := s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		c, err := openCapture(ctx, q, userID, id)
		if err != nil {
			return err
		}
		amount, merchant, day := c.Amount, c.Merchant, c.BookedOn
		if in.Amount != nil {
			amount = -*in.Amount
		}
		if in.Merchant != nil {
			merchant = *in.Merchant
		}
		if in.Day != nil {
			day = *in.Day
		}
		if err := q.UpdateCapture(ctx, userID, id, amount, merchant, day); err != nil {
			return notFound(err)
		}
		// The owner may file it from the movements screen meanwhile, without
		// the user's lock: FileCaptureByRule checks who chose as it writes.
		if merchant != c.Merchant {
			category, err := ruleCategory(ctx, q, userID, merchant)
			if err != nil {
				return err
			}
			if err := q.FileCaptureByRule(ctx, userID, id, category); err != nil {
				return err
			}
		}
		if _, err := AutoLink(ctx, q, userID, []uuid.UUID{c.AccountID}); err != nil {
			return err
		}
		out, err = load(ctx, q, userID, id)
		return err
	})
	return out, err
}

// Link pairs an open purchase with the printed line the owner says it is. Any
// amount is accepted: a tip or a pre-authorization is why the two differ, and
// the line is what counts either way. The line must be in the purchase's
// account, be an expense to the importer or the owner, and have no other
// purchase.
func (s *Service) Link(ctx context.Context, userID, id, entryID uuid.UUID) (Capture, error) {
	var out Capture
	err := s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		c, err := openCapture(ctx, q, userID, id)
		if err != nil {
			return err
		}
		line, err := lineFor(ctx, q, userID, c, entryID)
		if err != nil {
			return err
		}
		if !couldBePurchase(line) {
			return ErrNotAPurchase
		}
		err = q.LinkCapture(ctx, userID, id, entryID, store.LinkedByUser)
		if store.IsConstraint(err, "captures_one_per_line_idx") {
			return fmt.Errorf("%w: %v", ErrLineTaken, err)
		}
		if err != nil {
			return notFound(err)
		}
		// The line is taken now, which may leave another purchase only one.
		if _, err := AutoLink(ctx, q, userID, []uuid.UUID{c.AccountID}); err != nil {
			return err
		}
		out, err = load(ctx, q, userID, id)
		return err
	})
	return out, err
}

// Unlink undoes a link the owner says is wrong ("No es esta compra"). The line
// is never offered for this purchase again, which may make it count again
// until its real line arrives: the owner just said it is a purchase of its
// own. The category and note the link carried to the line go back, so the
// rules, or the purchase the line really is, can file it again.
func (s *Service) Unlink(ctx context.Context, userID, id uuid.UUID) (Capture, error) {
	var out Capture
	err := s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		c, err := capture(ctx, q, userID, id)
		if err != nil {
			return err
		}
		if c.EntryID == nil {
			return ErrNotLinked
		}
		if err := q.UnlinkCapture(ctx, userID, id); err != nil {
			return notFound(err)
		}
		if _, err := AutoLink(ctx, q, userID, []uuid.UUID{c.AccountID}); err != nil {
			return err
		}
		out, err = load(ctx, q, userID, id)
		return err
	})
	return out, err
}

// Reject records that a line is not an open purchase ("No"), whether it was
// one it could be or one review suggested as close. It is never offered for
// this purchase again.
func (s *Service) Reject(ctx context.Context, userID, id, entryID uuid.UUID) (Capture, error) {
	var out Capture
	err := s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		c, err := openCapture(ctx, q, userID, id)
		if err != nil {
			return err
		}
		if _, err := lineFor(ctx, q, userID, c, entryID); err != nil {
			return err
		}
		if err := q.RejectPair(ctx, userID, id, entryID); err != nil {
			return err
		}
		if _, err := AutoLink(ctx, q, userID, []uuid.UUID{c.AccountID}); err != nil {
			return err
		}
		out, err = load(ctx, q, userID, id)
		return err
	})
	return out, err
}

// Discard records that an open purchase did not happen — a declined tap, one
// noted twice — so it never counts. Discarding it again changes nothing.
func (s *Service) Discard(ctx context.Context, userID, id uuid.UUID) (Capture, error) {
	return s.setDiscarded(ctx, userID, id, true)
}

// Restore undoes Discard: the purchase counts again, or not, as a new one
// would. Restoring one that is not discarded changes nothing.
func (s *Service) Restore(ctx context.Context, userID, id uuid.UUID) (Capture, error) {
	return s.setDiscarded(ctx, userID, id, false)
}

func (s *Service) setDiscarded(ctx context.Context, userID, id uuid.UUID, discard bool) (Capture, error) {
	var out Capture
	err := s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		c, err := capture(ctx, q, userID, id)
		if err != nil {
			return err
		}
		if discard && c.EntryID != nil {
			return ErrLinked
		}
		switch {
		case discard && c.DiscardedAt == nil:
			err = q.DiscardCapture(ctx, userID, id)
		case !discard && c.DiscardedAt != nil:
			err = q.RestoreCapture(ctx, userID, id)
		}
		if err != nil {
			return notFound(err)
		}
		// Either way a line may now have one purchase fewer or more to choose
		// from.
		if _, err := AutoLink(ctx, q, userID, []uuid.UUID{c.AccountID}); err != nil {
			return err
		}
		out, err = load(ctx, q, userID, id)
		return err
	})
	return out, err
}

// Review lists the purchases that need the owner: those a line could be but
// none was linked, and those a statement passed without printing, with what
// could be their line.
func (s *Service) Review(ctx context.Context, userID uuid.UUID) ([]store.CaptureReview, error) {
	return s.db.Q().CapturesForReview(ctx, userID)
}

func (s *Service) today() civil.Date { return civil.Of(s.now().In(civil.Panama)) }

// checkDay refuses a day after today: a typo in the year would otherwise file
// a purchase in a month that has not happened.
func (s *Service) checkDay(day civil.Date) error {
	if day.After(s.today()) {
		return ErrFutureDay
	}
	return nil
}

// checkAmount refuses an amount that is not a purchase: nothing paid, or more
// than maxAmount.
func checkAmount(amount money.Cents) error {
	switch {
	case amount <= 0:
		return money.ErrNotPositive
	case amount > maxAmount:
		return ErrAmountTooLarge
	}
	return nil
}

func cleanMerchant(merchant string) (string, error) {
	merchant = strings.TrimSpace(merchant)
	switch {
	case merchant == "":
		return "", ErrMerchantRequired
	case utf8.RuneCountInString(merchant) > maxMerchantRunes:
		return "", ErrMerchantTooLong
	}
	return merchant, nil
}

func cleanNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxNoteRunes {
		return "", ErrNoteTooLong
	}
	return note, nil
}

// ruleCategory is the category the user's rules give a merchant, if any.
func ruleCategory(ctx context.Context, q *store.Queries, userID uuid.UUID, merchant string) (*uuid.UUID, error) {
	rules, err := q.CategoryRulesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return store.MatchRule(merchant, rules).CategoryID, nil
}

// capture reads one of the user's purchases, whatever its state.
func capture(ctx context.Context, q *store.Queries, userID, id uuid.UUID) (store.Capture, error) {
	c, err := q.CaptureByID(ctx, userID, id)
	return c, notFound(err)
}

// openCapture reads one of the user's purchases that is neither linked nor
// discarded: the only ones whose line is still a question.
func openCapture(ctx context.Context, q *store.Queries, userID, id uuid.UUID) (store.Capture, error) {
	c, err := capture(ctx, q, userID, id)
	switch {
	case err != nil:
		return store.Capture{}, err
	case c.EntryID != nil:
		return store.Capture{}, ErrLinked
	case c.DiscardedAt != nil:
		return store.Capture{}, ErrDiscarded
	}
	return c, nil
}

// lineFor reads one of the user's printed lines that could be paired with c:
// one of its own account.
func lineFor(ctx context.Context, q *store.Queries, userID uuid.UUID, c store.Capture, entryID uuid.UUID) (store.Entry, error) {
	line, err := q.EntryByID(ctx, userID, entryID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Entry{}, fmt.Errorf("%w: %v", ErrLineNotFound, err)
	}
	if err != nil {
		return store.Entry{}, err
	}
	if line.AccountID != c.AccountID {
		return store.Entry{}, ErrOtherAccount
	}
	return line, nil
}

// load reads a purchase as the owner sees it, with its line when linked.
func load(ctx context.Context, q *store.Queries, userID, id uuid.UUID) (Capture, error) {
	c, err := capture(ctx, q, userID, id)
	if err != nil {
		return Capture{}, err
	}
	out := Capture{Capture: c}
	if c.EntryID != nil {
		line, err := q.EntryByID(ctx, userID, *c.EntryID)
		if err != nil {
			return Capture{}, err
		}
		out.Line = &line
	}
	return out, nil
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return err
}
