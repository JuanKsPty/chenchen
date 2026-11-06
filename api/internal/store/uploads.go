package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
)

// What became of a statement Claude sent (migration 00019).
const (
	// UploadPending is an upload whose address was handed out: its bytes have
	// not arrived, or are being read.
	UploadPending = "pending"
	// UploadImported and UploadUnchanged are a file that entered on its own,
	// or that was already in.
	UploadImported  = "imported"
	UploadUnchanged = "unchanged"
	// UploadNeedsReview is a readable file that did not enter on its own: it
	// waits, with its bytes, for the owner.
	UploadNeedsReview = "needs_review"
	// UploadUnreadable is a file no reader recognised or could read.
	UploadUnreadable = "unreadable"
	// UploadDismissed is one the owner set aside, and UploadExpired one whose
	// address or keeping ran out. Neither holds its bytes.
	UploadDismissed = "dismissed"
	UploadExpired   = "expired"
)

// StatementUpload is one file Claude sent, or was given an address to send.
// Its bytes are read apart (UploadData): most reads need only what it says.
type StatementUpload struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	GrantID  *uuid.UUID
	Filename string
	// SHA256 is the hex digest Claude declared, and Size the length in bytes:
	// the address takes no other file.
	SHA256 string
	Size   int64
	// HasData reports whether the row still holds the file.
	HasData     bool
	Status      string
	Reason      string
	AccountID   *uuid.UUID
	ImportRunID *uuid.UUID
	ReceivedAt  *time.Time
	ExpiresAt   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const uploadColumns = `id, user_id, grant_id, filename, sha256, size, data IS NOT NULL, status, reason, account_id,
	import_run_id, received_at, expires_at, created_at, updated_at`

// CreateStatementUpload stores an address just handed out, by the hash of its
// token: the token itself is never kept.
func (q *Queries) CreateStatementUpload(ctx context.Context, u StatementUpload, tokenHash string) error {
	_, err := q.q.Exec(ctx, `
		INSERT INTO statement_uploads (id, user_id, grant_id, token_hash, filename, sha256, size, expires_at, created_at,
		                               updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)`,
		u.ID, u.UserID, u.GrantID, tokenHash, u.Filename, u.SHA256, u.Size, u.ExpiresAt, u.CreatedAt)
	return wrap("store.CreateStatementUpload", err)
}

// OpenUploadCount is how many of the user's addresses still wait for their
// file at now.
func (q *Queries) OpenUploadCount(ctx context.Context, userID uuid.UUID, now time.Time) (int, error) {
	var n int
	err := q.q.QueryRow(ctx, `SELECT count(*) FROM statement_uploads
		WHERE user_id = $1 AND status = 'pending' AND received_at IS NULL AND expires_at > $2`, userID, now).Scan(&n)
	return n, wrap("store.OpenUploadCount", err)
}

// OpenUploadByToken finds the upload a token's hash names while it can still
// take its file: pending, never received, and not expired at now. Anything
// else — spent, expired, unknown — is ErrNotFound, alike.
func (q *Queries) OpenUploadByToken(ctx context.Context, tokenHash string, now time.Time) (StatementUpload, error) {
	u, err := scanUpload(q.q.QueryRow(ctx, `SELECT `+uploadColumns+` FROM statement_uploads
		WHERE token_hash = $1 AND status = 'pending' AND received_at IS NULL AND expires_at > $2`, tokenHash, now))
	return u, wrap("store.OpenUploadByToken", err)
}

// ReceiveUpload spends an upload's address as its file arrives. Only one
// arrival can: a second one, or one after the address expired, is
// ErrNotFound.
func (q *Queries) ReceiveUpload(ctx context.Context, id uuid.UUID, now time.Time) error {
	return q.execOne(ctx, "store.ReceiveUpload", `
		UPDATE statement_uploads SET received_at = $2, updated_at = $2
		WHERE id = $1 AND status = 'pending' AND received_at IS NULL AND expires_at > $2`, id, now)
}

// UploadOutcome is what reading a file came to.
type UploadOutcome struct {
	Status      string
	Reason      string
	AccountID   *uuid.UUID
	ImportRunID *uuid.UUID
	// Data is the file to keep for the owner, or nil to keep none.
	Data []byte
}

// FinishUpload records what one of the user's received uploads came to.
func (q *Queries) FinishUpload(ctx context.Context, userID, id uuid.UUID, o UploadOutcome, now time.Time) error {
	return q.execOne(ctx, "store.FinishUpload", `
		UPDATE statement_uploads
		   SET status = $3, reason = $4, account_id = $5, import_run_id = $6, data = $7, updated_at = $8
		 WHERE id = $1 AND user_id = $2 AND received_at IS NOT NULL`,
		id, userID, o.Status, o.Reason, o.AccountID, o.ImportRunID, o.Data, now)
}

// SettleUpload records that the owner imported or dismissed one of the user's
// uploads still holding its file, and drops the file. One no longer holding
// it — already imported, dismissed or expired — is ErrNotFound.
func (q *Queries) SettleUpload(ctx context.Context, userID, id uuid.UUID, o UploadOutcome, now time.Time) error {
	return q.execOne(ctx, "store.SettleUpload", `
		UPDATE statement_uploads
		   SET status = $3, reason = $4, account_id = $5, import_run_id = $6, data = NULL, updated_at = $7
		 WHERE id = $1 AND user_id = $2 AND data IS NOT NULL`,
		id, userID, o.Status, o.Reason, o.AccountID, o.ImportRunID, now)
}

// UploadByID reads one of the user's uploads. Someone else's is ErrNotFound,
// like one that does not exist.
func (q *Queries) UploadByID(ctx context.Context, userID, id uuid.UUID) (StatementUpload, error) {
	u, err := scanUpload(q.q.QueryRow(ctx, `SELECT `+uploadColumns+` FROM statement_uploads
		WHERE id = $1 AND user_id = $2`, id, userID))
	return u, wrap("store.UploadByID", err)
}

// UploadData reads the file one of the user's uploads still holds. One that
// holds none, or is someone else's, is ErrNotFound.
func (q *Queries) UploadData(ctx context.Context, userID, id uuid.UUID) (StatementUpload, []byte, error) {
	var data []byte
	row := q.q.QueryRow(ctx, `SELECT `+uploadColumns+`, data FROM statement_uploads
		WHERE id = $1 AND user_id = $2 AND data IS NOT NULL`, id, userID)
	u, err := scanUpload(row, &data)
	return u, data, wrap("store.UploadData", err)
}

// UploadsByStatus lists the user's uploads in one state, oldest first: the
// order they arrived to be looked at.
func (q *Queries) UploadsByStatus(ctx context.Context, userID uuid.UUID, status string) ([]StatementUpload, error) {
	rows, err := q.q.Query(ctx, `SELECT `+uploadColumns+` FROM statement_uploads
		WHERE user_id = $1 AND status = $2 ORDER BY created_at, id`, userID, status)
	if err != nil {
		return nil, wrap("store.UploadsByStatus", err)
	}
	defer rows.Close()
	out := []StatementUpload{}
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, wrap("store.UploadsByStatus", err)
		}
		out = append(out, u)
	}
	return out, wrap("store.UploadsByStatus", rows.Err())
}

// ExpireUploads ends what nobody will use: an address whose file never
// arrived by the time it expired (or whose reading stopped halfway, received
// before stalled), and a file kept for review or unreadable since before
// kept. Both lose their bytes. It returns how many it ended.
func (q *Queries) ExpireUploads(ctx context.Context, now, stalled, kept time.Time) (int64, error) {
	tag, err := q.q.Exec(ctx, `
		UPDATE statement_uploads SET status = 'expired', data = NULL, updated_at = $1
		 WHERE (status = 'pending' AND expires_at <= $1 AND (received_at IS NULL OR received_at <= $2))
		    OR (status IN ('needs_review', 'unreadable') AND created_at <= $3)`, now, stalled, kept)
	if err != nil {
		return 0, wrap("store.ExpireUploads", err)
	}
	return tag.RowsAffected(), nil
}

func scanUpload(s scanner, extra ...any) (StatementUpload, error) {
	var u StatementUpload
	dest := []any{&u.ID, &u.UserID, &u.GrantID, &u.Filename, &u.SHA256, &u.Size, &u.HasData, &u.Status, &u.Reason,
		&u.AccountID, &u.ImportRunID, &u.ReceivedAt, &u.ExpiresAt, &u.CreatedAt, &u.UpdatedAt}
	err := s.Scan(append(dest, extra...)...)
	return u, err
}

// AccountByIdentity finds the user's account a statement names, by how its
// institution keys it, without ever creating one: ErrNotFound when the user
// has none.
func (q *Queries) AccountByIdentity(ctx context.Context, userID uuid.UUID, institution, externalNumber,
	currency string) (Account, error) {
	a, err := scanAccount(q.q.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts
		WHERE user_id = $1 AND institution = $2 AND external_number = $3 AND currency = $4`,
		userID, institution, externalNumber, currency))
	return a, wrap("store.AccountByIdentity", err)
}

// ImportRunByID reads one of the user's import runs.
func (q *Queries) ImportRunByID(ctx context.Context, userID, id uuid.UUID) (ImportRun, error) {
	runs, err := q.importRuns(ctx, "store.ImportRunByID", `WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return ImportRun{}, err
	}
	if len(runs) == 0 {
		return ImportRun{}, wrap("store.ImportRunByID", ErrNotFound)
	}
	return runs[0], nil
}

// StatementFacts is what an account's imports say about what to fetch next.
type StatementFacts struct {
	// LastPeriodEnd is the last day any of its statements covers, zero when
	// none was imported.
	LastPeriodEnd civil.Date
	// LastImportAt is when a file last added to it, nil when none did.
	LastImportAt *time.Time
	// LastCut is the cut day of the latest closed card statement imported,
	// zero when none was.
	LastCut civil.Date
}

// StatementFactsByUser returns, per account of the user's, what its imports
// say; an account with none has zero facts.
func (q *Queries) StatementFactsByUser(ctx context.Context, userID uuid.UUID, closedSource string) (map[uuid.UUID]StatementFacts, error) {
	rows, err := q.q.Query(ctx, `
		SELECT a.id,
		       (SELECT max(s.period_end) FROM statements s WHERE s.account_id = a.id),
		       (SELECT max(r.created_at) FROM import_runs r WHERE r.account_id = a.id AND r.status = 'ok'),
		       (SELECT max(r.period_end) FROM import_runs r
		         WHERE r.account_id = a.id AND r.status = 'ok' AND r.source = $2)
		  FROM accounts a WHERE a.user_id = $1`, userID, closedSource)
	if err != nil {
		return nil, wrap("store.StatementFactsByUser", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]StatementFacts{}
	for rows.Next() {
		var (
			id          uuid.UUID
			f           StatementFacts
			end, cut    *civil.Date
			lastImports *time.Time
		)
		if err := rows.Scan(&id, &end, &lastImports, &cut); err != nil {
			return nil, wrap("store.StatementFactsByUser", err)
		}
		if end != nil {
			f.LastPeriodEnd = *end
		}
		if cut != nil {
			f.LastCut = *cut
		}
		f.LastImportAt = lastImports
		out[id] = f
	}
	return out, wrap("store.StatementFactsByUser", rows.Err())
}

// PendingCapture is a purchase noted that no statement has printed yet, with
// the channels that told of it.
type PendingCapture struct {
	Capture
	Channels []string
}

// PendingCaptures lists the user's pending captures, in one account or all of
// them, newest first, each with its signals' channels in the order they
// arrived.
func (q *Queries) PendingCaptures(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID) ([]PendingCapture, error) {
	rows, err := q.q.Query(ctx, `SELECT `+prefixed("c.", captureColumns)+`,
		       ARRAY(SELECT s.channel FROM capture_signals s WHERE s.capture_id = c.id ORDER BY s.at, s.channel)
		  FROM capture_states c
		 WHERE c.user_id = $1 AND c.state = 'pending' AND ($2::uuid IS NULL OR c.account_id = $2)
		 ORDER BY c.booked_on DESC, c.received_at DESC, c.id DESC`, userID, accountID)
	if err != nil {
		return nil, wrap("store.PendingCaptures", err)
	}
	defer rows.Close()
	out := []PendingCapture{}
	for rows.Next() {
		var (
			p        PendingCapture
			amount   int64
			raw      []byte
			channels []string
		)
		c := &p.Capture
		if err := rows.Scan(&c.ID, &c.UserID, &c.AccountID, &c.Source, &c.KeyID, &c.ReceivedAt, &c.BookedOn, &amount,
			&c.Merchant, &c.CategoryID, &c.CategorySource, &c.Note, &c.EntryID, &c.LinkedBy, &c.DiscardedAt, &raw,
			&c.CreatedAt, &c.UpdatedAt, &c.State, &channels); err != nil {
			return nil, wrap("store.PendingCaptures", err)
		}
		c.Amount = money.Cents(amount)
		p.Channels = channels
		out = append(out, p)
	}
	return out, wrap("store.PendingCaptures", rows.Err())
}
