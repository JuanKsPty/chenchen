// Package accounts serves the owner's real-world accounts — bank accounts,
// cards and brokerage accounts — with balances computed from what their
// statements printed.
//
// An account's balance is its opening plus the sum of its movements. The
// opening is not stored: it is derived from one anchor, a balance somebody
// stated (a statement's opening, IBKR's cash report, or the owner reading their
// bank or card statement). Every other stated balance is an assertion, and its
// drift — stated minus computed — is how a mismatch is found rather than hidden
// under a correcting entry.
//
// Purchases the owner noted that no statement has printed yet are shown beside
// the balance, never in it (ADR-0002): nothing here that derives a balance, an
// opening or a drift reads them.
//
// A card someone else pays is kept by hand: the owner adds it with its last
// four digits, and it lives only on the purchases they note. Nothing printed
// can land in it, so it has no balance to anchor or check; its purchases are
// the owner's spending, but its debt is not theirs and stays out of net worth.
package accounts

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

var (
	// ErrNotFound means no such account belongs to the caller. An account
	// that exists but is someone else's is reported the same way, so an id
	// cannot be probed.
	ErrNotFound = errors.New("accounts: account not found")
	// ErrNotRelabelable means the account is a card or a brokerage account,
	// whose type follows from what it is.
	ErrNotRelabelable = errors.New("accounts: only a bank account can be relabelled checking or savings")
	// ErrCheckpointNotFound means no such checkpoint belongs to the account.
	ErrCheckpointNotFound = errors.New("accounts: checkpoint not found")
	// ErrKeptByHand means the account is a card someone else pays, kept by
	// hand: no statement reaches it, so it has no balance to state.
	ErrKeptByHand = errors.New("accounts: a card kept by hand has no balance")
	// ErrIDTaken means the id a caller chose for a new card someone else pays
	// is another account's.
	ErrIDTaken = errors.New("accounts: the id belongs to another account")
	// ErrAliasRequired means a card someone else pays was left without a
	// name. It has no bank's name to fall back on.
	ErrAliasRequired = errors.New("accounts: a card someone else pays needs a name")
	// ErrLast4Invalid means the last four digits of a card are not four
	// digits.
	ErrLast4Invalid = errors.New("accounts: the last four digits must be four digits")
)

// HandCardTakenError means the owner already has a card ending in the same
// four digits: one someone else pays or, when Printed, one whose statements
// they import.
type HandCardTakenError struct {
	Last4   string
	Printed bool
}

func (e HandCardTakenError) Error() string {
	if e.Printed {
		return "accounts: a card whose statements are imported already ends in " + e.Last4
	}
	return "accounts: a card someone else pays already ends in " + e.Last4
}

// Service reads and manages accounts.
type Service struct {
	db  *store.DB
	now func() time.Time
}

func NewService(db *store.DB) *Service { return &Service{db: db, now: time.Now} }

// Account is an account with its computed position.
type Account struct {
	store.Account
	// Balance is signed from the holder's view: a card that owes 14.30 has
	// -14.30. It is only meaningful when Anchored.
	Balance money.Cents
	// Anchored reports whether a stated balance fixes the opening. Without
	// one the balance is just the sum of the imported movements, which for a
	// card with no previous balance is not what the bank says.
	Anchored bool
	// Holdings is a brokerage account's positions' market value.
	Holdings money.Cents
	// Drift is the latest checkpoint's stated minus computed balance, when a
	// checkpoint other than the anchor exists.
	Drift     *Drift
	Movements int
	LastDay   civil.Date
	// Statement is a card's latest statement, as the owner entered it with its
	// due date. Nil for anything but a card, or a card with no due date yet.
	Statement *CardStatement
	// LastSyncedAt is when a brokerage account last synced with IBKR
	// successfully. Nil when it never has, or for any other account.
	LastSyncedAt *time.Time
	// Pending is what the account's pending captures add up to, signed like
	// Balance, and PendingCount how many there are: purchases noted that no
	// statement has printed yet. They count as spending but are not in
	// Balance until their line arrives.
	Pending      money.Cents
	PendingCount int
	// ToReview counts the noted purchases the owner should look at: a printed
	// line could be them, or a statement passed their day without one.
	ToReview int
	// CaptureKey is the account's live shortcut key, nil when it has none.
	// Never the key itself: only its hint and when it was last used.
	CaptureKey *store.CaptureKey
	// ThisMonth is what a card someone else pays was used for this calendar
	// month in Panama, as a positive amount: its pending purchases, which for
	// such a card are all its purchases. Zero for any other account.
	ThisMonth money.Cents
}

// withCaptures sets what an account's noted purchases come to, beside its
// balance.
func withCaptures(a *Account, c store.CaptureSummary) {
	a.Pending, a.PendingCount, a.ToReview = c.PendingTotal, c.PendingCount, c.ToReview
	if a.PaidByOther {
		a.ThisMonth = -c.MonthTotal
	}
}

// thisMonth is the first day of the current calendar month in Panama, the
// month a card someone else pays shows what it was used for.
func (s *Service) thisMonth() civil.Date {
	today := civil.Of(s.now().In(civil.Panama))
	return civil.Date{Year: today.Year, Month: today.Month, Day: 1}
}

// withKey sets the account's live shortcut key, if it has one.
func withKey(a *Account, keys map[uuid.UUID]store.CaptureKey) {
	if k, ok := keys[a.ID]; ok {
		a.CaptureKey = &k
	}
}

// CardStatement is what a card's statement says the owner must do: when it
// closed, when payment is due and the least they can pay.
type CardStatement struct {
	ClosingOn      civil.Date
	DueOn          *civil.Date
	MinimumPayment *money.Cents
}

// Drift is how far a stated balance is from the computed one.
type Drift struct {
	AsOf     civil.Date
	Stated   money.Cents
	Computed money.Cents
	Source   string
}

// Difference is stated minus computed.
func (d Drift) Difference() money.Cents { return d.Stated - d.Computed }

// List returns the user's accounts with their positions.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Account, error) {
	rows, err := s.db.Q().AccountsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	sums, err := s.db.Q().SumsByAccount(ctx, userID)
	if err != nil {
		return nil, err
	}
	holdings, err := s.db.Q().HoldingsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	checkpoints, err := s.db.Q().CheckpointsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	byAccount := map[uuid.UUID][]store.Checkpoint{}
	for _, c := range checkpoints {
		byAccount[c.AccountID] = append(byAccount[c.AccountID], c)
	}
	synced, err := s.db.Q().LastSyncsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	captures, err := s.db.Q().CaptureSummaries(ctx, userID, s.thisMonth())
	if err != nil {
		return nil, err
	}
	keys, err := s.db.Q().CaptureKeysByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	out := make([]Account, 0, len(rows))
	for _, row := range rows {
		a, err := s.position(ctx, row, sums[row.ID], holdings[row.ID], byAccount[row.ID])
		if err != nil {
			return nil, err
		}
		a.LastSyncedAt = lastSynced(row, synced)
		withCaptures(&a, captures[row.ID])
		withKey(&a, keys)
		out = append(out, a)
	}
	return out, nil
}

// Get returns one of the user's accounts with its position.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Account, error) {
	row, err := s.resolve(ctx, userID, id)
	if err != nil {
		return Account{}, err
	}
	sums, err := s.db.Q().SumsByAccount(ctx, userID)
	if err != nil {
		return Account{}, err
	}
	holdings, err := s.db.Q().HoldingsByUser(ctx, userID)
	if err != nil {
		return Account{}, err
	}
	cps, err := s.db.Q().CheckpointsByAccount(ctx, userID, id)
	if err != nil {
		return Account{}, err
	}
	a, err := s.position(ctx, row, sums[id], holdings[id], cps)
	if err != nil {
		return Account{}, err
	}
	if row.Type == "brokerage" {
		synced, err := s.db.Q().LastSyncsByUser(ctx, userID)
		if err != nil {
			return Account{}, err
		}
		a.LastSyncedAt = lastSynced(row, synced)
	}
	captures, err := s.db.Q().CaptureSummaries(ctx, userID, s.thisMonth())
	if err != nil {
		return Account{}, err
	}
	withCaptures(&a, captures[id])
	keys, err := s.db.Q().CaptureKeysByUser(ctx, userID)
	if err != nil {
		return Account{}, err
	}
	withKey(&a, keys)
	return a, nil
}

// lastSynced is a brokerage account's last successful sync, if it has one.
func lastSynced(row store.Account, synced map[uuid.UUID]time.Time) *time.Time {
	at, ok := synced[row.ID]
	if row.Type != "brokerage" || !ok {
		return nil
	}
	return &at
}

// latestStatement is a card's most recent statement: the latest checkpoint the
// owner gave a due date, its as_of being the day the statement closed. cps must
// be ordered by as_of, as the store returns them.
func latestStatement(row store.Account, cps []store.Checkpoint) *CardStatement {
	if row.Type != "credit_card" {
		return nil
	}
	for i := len(cps) - 1; i >= 0; i-- {
		if c := cps[i]; c.DueOn != nil {
			return &CardStatement{ClosingOn: c.AsOf, DueOn: c.DueOn, MinimumPayment: c.MinimumPayment}
		}
	}
	return nil
}

func (s *Service) position(ctx context.Context, row store.Account, sums store.AccountSums,
	holdings money.Cents, cps []store.Checkpoint) (Account, error) {
	a := Account{Account: row, Balance: sums.Total, Holdings: holdings, Movements: sums.Count, LastDay: sums.LastBooking,
		Statement: latestStatement(row, cps)}

	anchor, ok := Anchor(cps)
	if !ok {
		return a, nil
	}
	opening, err := s.Opening(ctx, anchor)
	if err != nil {
		return Account{}, err
	}
	a.Anchored = true
	a.Balance = opening + sums.Total

	// The latest assertion is the one worth showing: it says whether the
	// account matches the bank now.
	for i := len(cps) - 1; i >= 0; i-- {
		c := cps[i]
		if c.ID == anchor.ID {
			continue
		}
		computed, err := s.balanceAt(ctx, c, opening)
		if err != nil {
			return Account{}, err
		}
		a.Drift = &Drift{AsOf: c.AsOf, Stated: c.Balance, Computed: computed, Source: c.Source}
		break
	}
	return a, nil
}

// Anchor picks the checkpoint the opening balance is derived from: the pinned
// one, otherwise the earliest — with a statement's own opening preferred over
// anything else stated for the same day. cps must be ordered by as_of.
func Anchor(cps []store.Checkpoint) (store.Checkpoint, bool) {
	if len(cps) == 0 {
		return store.Checkpoint{}, false
	}
	for _, c := range cps {
		if c.Pinned {
			return c, true
		}
	}
	sorted := append([]store.Checkpoint(nil), cps...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if c := sorted[i].AsOf.Compare(sorted[j].AsOf); c != 0 {
			return c < 0
		}
		return rank(sorted[i]) < rank(sorted[j])
	})
	return sorted[0], true
}

func rank(c store.Checkpoint) int {
	switch c.Source {
	case store.SourceStatementOpening:
		return 0
	case store.SourceStatementClosing, store.SourceBroker:
		return 1
	default:
		return 2
	}
}

// Opening is the balance before every movement, derived from an anchor.
func (s *Service) Opening(ctx context.Context, anchor store.Checkpoint) (money.Cents, error) {
	before, err := s.sumAt(ctx, anchor)
	if err != nil {
		return 0, err
	}
	return anchor.Balance - before, nil
}

// BalanceAt is what the movements say the balance was at a checkpoint.
func (s *Service) BalanceAt(ctx context.Context, c store.Checkpoint, opening money.Cents) (money.Cents, error) {
	return s.balanceAt(ctx, c, opening)
}

func (s *Service) balanceAt(ctx context.Context, c store.Checkpoint, opening money.Cents) (money.Cents, error) {
	sum, err := s.sumAt(ctx, c)
	if err != nil {
		return 0, err
	}
	return opening + sum, nil
}

// sumAt totals the movements a checkpoint's balance includes: everything
// before its entry, or everything through the end of its day.
func (s *Service) sumAt(ctx context.Context, c store.Checkpoint) (money.Cents, error) {
	return sumAt(ctx, s.db.Q(), c)
}

func sumAt(ctx context.Context, q *store.Queries, c store.Checkpoint) (money.Cents, error) {
	if c.BeforeEntryID != nil {
		return q.SumBefore(ctx, c.AccountID, *c.BeforeEntryID)
	}
	return q.SumThroughDay(ctx, c.AccountID, c.AsOf)
}

// BalanceThrough is what the movements say an account's balance was at the
// end of day: the opening derived from the anchor among its checkpoints cps,
// plus every movement through that day — the computation a balance and its
// drift come from, read through q so it can run inside the caller's
// transaction. ok is false when cps has no anchor: without a stated balance
// there is nothing to derive the opening from.
func BalanceThrough(ctx context.Context, q *store.Queries, accountID uuid.UUID, cps []store.Checkpoint,
	day civil.Date) (balance money.Cents, ok bool, err error) {
	anchor, ok := Anchor(cps)
	if !ok {
		return 0, false, nil
	}
	before, err := sumAt(ctx, q, anchor)
	if err != nil {
		return 0, false, err
	}
	through, err := q.SumThroughDay(ctx, accountID, day)
	if err != nil {
		return 0, false, err
	}
	return anchor.Balance - before + through, true, nil
}

// Balances is what Cuentas shows for each of the user's accounts in ids, by
// account: the balance List gives it, the opening derived from its anchor
// plus every movement, or only their sum when nothing anchors it, and, for a
// brokerage account, its positions on top, the total its card shows. It reads
// what List reads for a balance in a handful of queries whatever the number
// of accounts, for a reader of several balances at once, such as the plan's
// goals. Someone else's account, or one that does not exist, is left out.
func Balances(ctx context.Context, q *store.Queries, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]money.Cents, error) {
	out := make(map[uuid.UUID]money.Cents, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.AccountsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	sums, err := q.SumsByAccount(ctx, userID)
	if err != nil {
		return nil, err
	}
	holdings, err := q.HoldingsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	checkpoints, err := q.CheckpointsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	byAccount := map[uuid.UUID][]store.Checkpoint{}
	for _, c := range checkpoints {
		byAccount[c.AccountID] = append(byAccount[c.AccountID], c)
	}
	wanted := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	anchors := map[uuid.UUID]store.Checkpoint{}
	var anchorIDs []uuid.UUID
	for _, row := range rows {
		if !wanted[row.ID] {
			continue
		}
		if anchor, ok := Anchor(byAccount[row.ID]); ok {
			anchors[row.ID] = anchor
			anchorIDs = append(anchorIDs, anchor.ID)
		}
	}
	before, err := q.SumsAtCheckpoints(ctx, userID, anchorIDs)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if !wanted[row.ID] {
			continue
		}
		// As position does: the sum of the movements, from the opening the
		// anchor gives when there is one.
		balance := sums[row.ID].Total
		if anchor, ok := anchors[row.ID]; ok {
			balance += anchor.Balance - before[anchor.ID]
		}
		if row.Type == "brokerage" {
			balance += holdings[row.ID]
		}
		out[row.ID] = balance
	}
	return out, nil
}

// Rename sets or clears an account's alias. A card someone else pays keeps
// one: it has no bank's name to be known by instead.
func (s *Service) Rename(ctx context.Context, userID, id uuid.UUID, rawAlias string) (Account, error) {
	alias, err := normaliseAlias(rawAlias)
	if err != nil {
		return Account{}, err
	}
	if alias == "" {
		row, err := s.resolve(ctx, userID, id)
		if err != nil {
			return Account{}, err
		}
		if row.KeptByHand() {
			return Account{}, ErrAliasRequired
		}
	}
	if err := s.db.Q().SetAccountAlias(ctx, userID, id, alias); err != nil {
		return Account{}, notFound(err)
	}
	return s.Get(ctx, userID, id)
}

// SetType relabels a bank account as checking or savings.
func (s *Service) SetType(ctx context.Context, userID, id uuid.UUID, typ string) (Account, error) {
	if typ != "checking" && typ != "savings" {
		return Account{}, ErrNotRelabelable
	}
	if _, err := s.resolve(ctx, userID, id); err != nil {
		return Account{}, err
	}
	if err := s.db.Q().SetAccountType(ctx, userID, id, typ); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Account{}, ErrNotRelabelable
		}
		return Account{}, err
	}
	return s.Get(ctx, userID, id)
}

// AddHandCard adds a card someone else pays: one the owner uses while
// somebody else gets its statements and pays it. It is known by the alias the
// owner gives it and its last four digits, and lives only on the purchases
// they note on it.
//
// The digits are one card's, in one place. A card whose statements the owner
// imports already holds its purchases, so it cannot also be kept by hand: its
// lines and the purchases noted on it would count twice, and its debt would
// be both the owner's and not.
// AddHandCard adds a card someone else pays. id, when the caller chooses one,
// makes adding safe to repeat: a second call with the same id returns the card
// the first one added instead of hearing that its digits are taken. A form that
// lost the answer to its first send can send it again. uuid.Nil means a new id.
func (s *Service) AddHandCard(ctx context.Context, userID, id uuid.UUID, rawAlias, last4 string) (Account, error) {
	alias, aliasErr := normaliseAlias(rawAlias)
	if aliasErr == nil && alias == "" {
		aliasErr = ErrAliasRequired
	}
	var last4Err error
	if !isLast4(last4) {
		last4Err = ErrLast4Invalid
	}
	// Both at once, so a form with both fields wrong hears of both.
	if err := errors.Join(aliasErr, last4Err); err != nil {
		return Account{}, err
	}
	var a store.Account
	err := s.db.InTx(ctx, func(q *store.Queries) error {
		// Under the lock an import takes, so none files the card between
		// the check and the insert.
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		if id == uuid.Nil {
			id = uuid.New()
		} else if added, err := q.AccountByID(ctx, userID, id); err == nil {
			// Added already, by an earlier send whose answer was lost. The
			// user's lock makes this check and the insert below one step.
			if !added.PaidByOther {
				return ErrIDTaken
			}
			a = added
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		printed, err := q.PrintedCardEndingIn(ctx, userID, last4)
		if err != nil {
			return err
		}
		if printed {
			return HandCardTakenError{Last4: last4, Printed: true}
		}
		a, err = q.CreateHandCard(ctx, userID, id, alias, last4)
		return err
	})
	if store.IsConstraint(err, "accounts_pkey") {
		return Account{}, fmt.Errorf("%w: %v", ErrIDTaken, err)
	}
	if store.IsConstraint(err, "accounts_user_id_institution_external_number_currency_key") {
		return Account{}, fmt.Errorf("%w: %v", HandCardTakenError{Last4: last4}, err)
	}
	if err != nil {
		return Account{}, err
	}
	return s.Get(ctx, userID, a.ID)
}

// isLast4 reports whether s is exactly four ASCII digits.
func isLast4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Delete removes an account with everything imported into it. Importing its
// files again recreates it with the same movements.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return notFound(s.db.Q().DeleteAccount(ctx, userID, id))
}

// CheckpointInput is a balance the owner states.
type CheckpointInput struct {
	AsOf civil.Date
	// Balance is signed from the holder's view; for a card the caller turns
	// "owed 14.30" into -14.30.
	Balance        money.Cents
	Pin            bool
	DueOn          *civil.Date
	MinimumPayment *money.Cents
	Note           string
}

// AddCheckpoint records a stated balance. Pinned, it becomes the anchor the
// opening is derived from. A card kept by hand takes none: no statement says
// what it owes, and its debt is not the owner's to state.
func (s *Service) AddCheckpoint(ctx context.Context, userID, accountID uuid.UUID, in CheckpointInput) (store.Checkpoint, error) {
	if in.AsOf.IsZero() {
		return store.Checkpoint{}, fmt.Errorf("accounts: a checkpoint needs a date")
	}
	c := store.Checkpoint{
		ID: uuid.New(), UserID: userID, AccountID: accountID, AsOf: in.AsOf, Balance: in.Balance,
		Source: store.SourceManual, DueOn: in.DueOn, MinimumPayment: in.MinimumPayment, Note: in.Note,
	}
	err := s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		account, err := q.AccountByID(ctx, userID, accountID)
		if err != nil {
			return notFound(err)
		}
		if account.KeptByHand() {
			return ErrKeptByHand
		}
		if err := q.CreateCheckpoint(ctx, c); err != nil {
			return err
		}
		if in.Pin {
			c.Pinned = true
			return q.PinCheckpoint(ctx, userID, accountID, &c.ID)
		}
		return nil
	})
	return c, err
}

// DeleteCheckpoint removes a checkpoint the owner entered.
func (s *Service) DeleteCheckpoint(ctx context.Context, userID, id uuid.UUID) error {
	if err := s.db.Q().DeleteManualCheckpoint(ctx, userID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrCheckpointNotFound
		}
		return err
	}
	return nil
}

// Pin makes a checkpoint the account's anchor, or unpins with nil so the
// earliest checkpoint anchors again.
func (s *Service) Pin(ctx context.Context, userID, accountID uuid.UUID, checkpointID *uuid.UUID) error {
	return s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, userID); err != nil {
			return err
		}
		if _, err := q.AccountByID(ctx, userID, accountID); err != nil {
			return notFound(err)
		}
		if err := q.PinCheckpoint(ctx, userID, accountID, checkpointID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrCheckpointNotFound
			}
			return err
		}
		return nil
	})
}

// Checkpoints lists an account's checkpoints.
func (s *Service) Checkpoints(ctx context.Context, userID, accountID uuid.UUID) ([]store.Checkpoint, error) {
	if _, err := s.resolve(ctx, userID, accountID); err != nil {
		return nil, err
	}
	return s.db.Q().CheckpointsByAccount(ctx, userID, accountID)
}

// NetWorth sums every account's balance and holdings. Liability balances are
// negative already, so this is assets minus debts plus investments. A card
// someone else pays is left out: what it owes is not the owner's debt.
func NetWorth(list []Account) (assets, liabilities, holdings money.Cents) {
	for _, a := range list {
		if a.PaidByOther {
			continue
		}
		if a.Class == "liability" {
			liabilities += a.Balance
		} else {
			assets += a.Balance
		}
		holdings += a.Holdings
	}
	return assets, liabilities, holdings
}

func (s *Service) resolve(ctx context.Context, userID, id uuid.UUID) (store.Account, error) {
	a, err := s.db.Q().AccountByID(ctx, userID, id)
	return a, notFound(err)
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return err
}
