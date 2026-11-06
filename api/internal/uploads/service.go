// Package uploads takes the statements Claude sends from the owner's computer
// (ADR-0004).
//
// Claude never carries a file through the conversation. It asks for an address
// (Prepare), with the file's name, size and SHA-256; the owner's computer sends
// the bytes there (Receive); and Claude asks how it went (Status). The address
// is the only credential the upload needs: 32 random bytes, kept only as a
// hash, good for ten minutes and for one file, the one declared.
//
// A file enters on its own only when it continues accounts the owner already
// has and agrees with them: every statement in it is of an existing account
// that is neither a card someone else pays nor a brokerage account, reads
// without a warning or a break in its own arithmetic, and, when the bank
// printed the balance it starts from, starts where the account's computed
// balance stands. Anything else waits, with its bytes, for the owner to look at
// in the app, where importing it is the web's import as it always was. Nothing
// that arrives this way ever creates an account: the gate refuses one inside
// the import's own transaction, which rolls it back.
//
// The address answers the same whatever the file turned out to be. Whoever
// holds it learns nothing about the owner's accounts from it: what became of
// the file is read with Claude's own token, as the owner.
package uploads

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/sync/semaphore"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/imports"
	"github.com/JuanKsPty/chenchen/api/internal/logging"
	"github.com/JuanKsPty/chenchen/api/internal/statement"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

const (
	// MaxSize is the largest file an address takes, as the web's import.
	MaxSize = 20 << 20
	// AddressTTL is how long an address waits for its file.
	AddressTTL = 10 * time.Minute
	// KeepFor is how long a file that waits for the owner is kept.
	KeepFor = 30 * 24 * time.Hour
	// stalledAfter is how long after its file arrived an upload still being
	// read is given up on: reading takes seconds, so only a process that
	// stopped halfway leaves one that long.
	stalledAfter = time.Hour
	// readTimeout bounds reading and importing one file, from when it is its
	// turn to be read. It runs on after the sender hangs up so the upload is
	// never left halfway.
	readTimeout = 2 * time.Minute
	// heldBudget is how many bytes of files sent to addresses the process
	// holds at once, waiting for their turn to be read or being read: one
	// file of the largest size, or many of a bank's usual few kilobytes.
	heldBudget = MaxSize + 1
	// maxFilename is the longest name kept, in characters.
	maxFilename = 255
	// maxReason is the longest reason kept, in characters, as the schema.
	maxReason = 500
	// maxOpen is how many addresses a user may have waiting at once. A run
	// over every bank hands out a handful; more is a conversation gone wrong.
	maxOpen = 20
)

var (
	// ErrInvalid wraps what is wrong with an address's request; its message
	// says what, in Spanish.
	ErrInvalid = errors.New("uploads: invalid request")
	// ErrUnknownAddress means no address takes a file there: it never
	// existed, was used, or expired. The three look the same.
	ErrUnknownAddress = errors.New("uploads: no such address")
	// ErrTooLarge means the body is over MaxSize.
	ErrTooLarge = errors.New("uploads: the file is too large")
	// ErrBodyUnreadable means the body stopped arriving before it ended.
	ErrBodyUnreadable = errors.New("uploads: the file did not arrive whole")
	// ErrSizeMismatch and ErrDigestMismatch mean the body is not the file
	// that was declared. Nothing of it is kept.
	ErrSizeMismatch   = errors.New("uploads: the file is not the size declared")
	ErrDigestMismatch = errors.New("uploads: the file is not the one declared")
	// ErrNotFound means no such upload belongs to the caller, or it no longer
	// holds its file: imported, dismissed or expired.
	ErrNotFound = errors.New("uploads: upload not found")
	// ErrDisabled means the app has no public address to give out.
	ErrDisabled = errors.New("uploads: no public URL")
)

// invalid is ErrInvalid with what to tell the caller.
type invalid struct{ message string }

func (e invalid) Error() string        { return "uploads: " + e.message }
func (e invalid) Is(target error) bool { return target == ErrInvalid }

// Message is what to tell whoever asked.
func (e invalid) Message() string { return e.message }

// Service hands out addresses, takes files and keeps the ones that wait.
type Service struct {
	db       *store.DB
	imports  *imports.Service
	accounts *accounts.Service
	// base is where addresses live, with a trailing slash: the public URL's
	// /api/mcp/uploads/. Empty when the app has no public URL.
	base string
	now  func() time.Time
	// reading lets one file be parsed at a time in the process: a workbook can
	// take tens of megabytes to read, and the API has little to spare.
	reading chan struct{}
	// held bounds the bytes of the files sent to addresses that wait for
	// reading, so files sent at once cannot together outgrow the API's
	// memory. The owner's side reads a file only once it holds reading.
	held *semaphore.Weighted
}

// NewService builds the service. publicURL is the app's canonical public
// address, or "" when it has none: then no address can be handed out, and
// only the owner's side works.
func NewService(db *store.DB, importsSvc *imports.Service, accountsSvc *accounts.Service, publicURL string) *Service {
	s := &Service{db: db, imports: importsSvc, accounts: accountsSvc, now: time.Now, reading: make(chan struct{}, 1),
		held: semaphore.NewWeighted(heldBudget)}
	if publicURL != "" {
		s.base = strings.TrimRight(publicURL, "/") + "/api/mcp/uploads/"
	}
	return s
}

// Prepared is an address handed out.
type Prepared struct {
	ID        uuid.UUID
	URL       string
	ExpiresAt time.Time
}

var sha256Shape = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Prepare hands out an address for one file of the user's, asked for through
// grantID: the file named filename, of size bytes, whose SHA-256 is sha in
// hex.
func (s *Service) Prepare(ctx context.Context, userID uuid.UUID, grantID *uuid.UUID, filename, sha string,
	size int64) (Prepared, error) {
	if s.base == "" {
		return Prepared{}, ErrDisabled
	}
	name, err := cleanFilename(filename)
	if err != nil {
		return Prepared{}, err
	}
	sha = strings.ToLower(strings.TrimSpace(sha))
	if !sha256Shape.MatchString(sha) {
		return Prepared{}, invalid{"sha256 debe ser el SHA-256 del archivo en hexadecimal: 64 caracteres."}
	}
	if size < 1 || size > MaxSize {
		return Prepared{}, invalid{fmt.Sprintf("size debe ser el tamaño del archivo en bytes, de 1 a %d.", MaxSize)}
	}
	now := s.now()
	waiting, err := s.db.Q().OpenUploadCount(ctx, userID, now)
	if err != nil {
		return Prepared{}, err
	}
	if waiting >= maxOpen {
		return Prepared{}, invalid{fmt.Sprintf("Ya hay %d entregas esperando su archivo. Manda esos archivos, o espera "+
			"a que venzan (10 minutos), antes de pedir otra.", waiting)}
	}
	token, hash, err := newToken()
	if err != nil {
		return Prepared{}, err
	}
	u := store.StatementUpload{ID: uuid.New(), UserID: userID, GrantID: grantID, Filename: name, SHA256: sha,
		Size: size, ExpiresAt: now.Add(AddressTTL), CreatedAt: now}
	if err := s.db.Q().CreateStatementUpload(ctx, u, hash); err != nil {
		return Prepared{}, err
	}
	return Prepared{ID: u.ID, URL: s.base + token, ExpiresAt: u.ExpiresAt}, nil
}

// cleanFilename keeps a file's own name: no folders, no control characters.
// Its extension is what tells a workbook from a text export.
func cleanFilename(raw string) (string, error) {
	name := strings.TrimSpace(path.Base(strings.ReplaceAll(raw, `\`, "/")))
	if name == "" || name == "." || name == "/" {
		return "", invalid{"filename debe ser el nombre del archivo, con su extensión."}
	}
	if !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		return "", invalid{"filename tiene caracteres que no son de un nombre de archivo."}
	}
	if utf8.RuneCountInString(name) > maxFilename {
		return "", invalid{fmt.Sprintf("filename admite como máximo %d caracteres.", maxFilename)}
	}
	return name, nil
}

// tokenLen is the length of 32 bytes in unpadded base64url.
const tokenLen = 43

// newToken is an address's secret and the hash kept of it.
func newToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("uploads: reading randomness: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// looksLikeToken reports whether s has a token's shape, so anything else is
// turned away before a lookup.
func looksLikeToken(s string) bool {
	if len(s) != tokenLen {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// Receive takes the file an address was handed out for, from body, and reads
// it: it enters on its own, or waits for the owner, or is unreadable. It
// returns the upload's id whatever the file turned out to be. Only a body that
// is not the file declared, or an address that takes no file, is an error, and
// then nothing is kept and the address still takes the right file until it
// expires.
func (s *Service) Receive(ctx context.Context, token string, body io.Reader) (uuid.UUID, error) {
	if !looksLikeToken(token) {
		return uuid.Nil, ErrUnknownAddress
	}
	up, err := s.db.Q().OpenUploadByToken(ctx, hashToken(token), s.now())
	if errors.Is(err, store.ErrNotFound) {
		return uuid.Nil, ErrUnknownAddress
	}
	if err != nil {
		return uuid.Nil, err
	}

	// No more than the declared size and one byte is read, into one buffer:
	// enough to tell a longer body from the file. The buffer waits for room
	// among the files the process already holds; a sender that gives up
	// meanwhile has spent nothing.
	if err := s.held.Acquire(ctx, up.Size+1); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrBodyUnreadable, err)
	}
	defer s.held.Release(up.Size + 1)
	buf := make([]byte, up.Size+1)
	n, err := io.ReadFull(body, buf)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return uuid.Nil, ErrTooLarge
	case err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF):
		return uuid.Nil, fmt.Errorf("%w: %v", ErrBodyUnreadable, err)
	case int64(n) != up.Size:
		return uuid.Nil, ErrSizeMismatch
	}
	data := buf[:n]
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != up.SHA256 {
		return uuid.Nil, ErrDigestMismatch
	}

	// The file waits for its turn to be read before the address is spent,
	// however long the files ahead of it take: a sender that gives up
	// waiting leaves the address for the same file again, and the file's own
	// time to be read starts only when it is its turn.
	if err := s.acquire(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrBodyUnreadable, err)
	}
	defer s.release()

	// The address is spent here, once: a second sender racing this one finds
	// nothing to spend.
	if err := s.db.Q().ReceiveUpload(ctx, up.ID, s.now()); errors.Is(err, store.ErrNotFound) {
		return uuid.Nil, ErrUnknownAddress
	} else if err != nil {
		return uuid.Nil, err
	}

	// From here the upload is finished whether or not the sender waits.
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), readTimeout)
	defer cancel()
	outcome := s.read(work, up, data)
	if err := s.finish(ctx, up, outcome); err != nil {
		return uuid.Nil, err
	}
	logging.FromContext(ctx).Info("statement upload read", "upload_id", up.ID, "user_id", up.UserID,
		"status", outcome.Status)
	return up.ID, nil
}

// finishTries is how long apart finish tries to record an outcome again.
var finishTries = []time.Duration{0, 500 * time.Millisecond, 2 * time.Second}

// finish records what a received file came to. By then its address is spent
// and, if it entered, it is in: a write that fails is tried again, each time
// on a clock of its own, so the upload is not left saying its file never came
// while the file it held is lost.
func (s *Service) finish(ctx context.Context, up store.StatementUpload, o store.UploadOutcome) error {
	ctx = context.WithoutCancel(ctx)
	var err error
	for _, wait := range finishTries {
		time.Sleep(wait)
		try, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = s.db.Q().FinishUpload(try, up.UserID, up.ID, o, s.now())
		cancel()
		if err == nil {
			return nil
		}
		logging.FromContext(ctx).Warn("recording a statement upload failed", "upload_id", up.ID,
			"status", o.Status, "error", err)
	}
	return err
}

// What an upload that waits tells the owner, and Claude, about why. They name
// no bank and no format: the owner opens the file in the app to see the rest.
const (
	reasonNewAccount  = "Es de una cuenta que todavía no está en chenchén. La primera vez se importa desde la app."
	reasonPaidByOther = "Es de una tarjeta que paga otra persona: sus estados no se importan."
	reasonKeptByHand  = "Es de una cuenta que llevas a mano: sus estados no se importan."
	reasonBrokerage   = "Es de una cuenta de inversiones, que se sincroniza por su cuenta."
	reasonChainBreak  = "Los saldos del archivo dejan de cuadrar en la línea %d."
	reasonWarning     = "El archivo trae una advertencia: %s"
	reasonOpening     = "El saldo inicial no coincide con el que tenías: el archivo empieza en %s y chenchén tenía %s."
	reasonFailed      = "No pudimos revisarlo. Ábrelo en chenchén para importarlo."
)

// refusal is the gate holding a file back for the owner.
type refusal struct {
	reason  string
	account *uuid.UUID
}

func (r *refusal) Error() string { return "uploads: held for review: " + r.reason }

// read decides what a received file comes to and, when it may, imports it.
// It never fails: what it cannot do waits for the owner with the file. Its
// caller holds reading.
func (s *Service) read(ctx context.Context, up store.StatementUpload, data []byte) (outcome store.UploadOutcome) {
	defer func() {
		// A reader that panics on a malformed file has rolled its import
		// back; the file waits like any other the app could not read.
		if p := recover(); p != nil {
			outcome = s.failed(ctx, up, fmt.Errorf("panic: %v", p), data)
		}
	}()
	parsed, err := imports.Parse(up.Filename, data)
	if err == nil && len(parsed.Statements) == 0 {
		err = statement.ErrUnrecognised
	}
	if err != nil {
		logging.FromContext(ctx).Warn("statement upload unreadable", "upload_id", up.ID, "error", err)
		return store.UploadOutcome{Status: store.UploadUnreadable, Reason: statement.ParseProblem(err).Message,
			Data: data}
	}

	// A file of one statement is imported as from that account's own page.
	// Its account is looked up, never created: a file of an account the
	// owner does not have waits without even being opened.
	var expect *uuid.UUID
	if len(parsed.Statements) == 1 {
		id := parsed.Statements[0].Identity
		a, err := s.db.Q().AccountByIdentity(ctx, up.UserID, id.Institution, id.ExternalNumber, id.Currency)
		switch {
		case errors.Is(err, store.ErrNotFound):
			return review(reasonNewAccount, nil, data)
		case err != nil:
			return s.failed(ctx, up, err, data)
		}
		expect = &a.ID
	}

	result, err := s.imports.ImportGated(ctx, up.UserID, up.Filename, data, parsed, expect, s.gate)
	var held *refusal
	switch {
	case err == nil:
		o := store.UploadOutcome{Status: store.UploadImported}
		if result.Unchanged {
			o.Status = store.UploadUnchanged
		}
		if len(result.Accounts) > 0 {
			first := result.Accounts[0]
			o.AccountID, o.ImportRunID = &first.AccountID, &first.RunID
		}
		return o
	case errors.As(err, &held):
		return review(held.reason, held.account, data)
	case errors.Is(err, imports.ErrPaidByOther):
		return review(reasonPaidByOther, expect, data)
	case errors.Is(err, imports.ErrUnreadable):
		return store.UploadOutcome{Status: store.UploadUnreadable, Reason: statement.ParseProblem(err).Message,
			Data: data}
	default:
		return s.failed(ctx, up, err, data)
	}
}

// gate is the rule a file enters by on its own (imports.Gate): every
// statement continues an account the owner has, agrees with itself, and
// starts where the account stands.
//
// A statement that prints no opening balance is let through even when its
// account has none: an import only adds stated balances, so it never leaves an
// account that had one without it, and an account that never had one is no
// worse for its movements.
func (s *Service) gate(ctx context.Context, q *store.Queries, account store.Account, created bool,
	st statement.Statement) error {
	id := &account.ID
	switch {
	case created:
		return &refusal{reason: reasonNewAccount}
	case account.KeptByHand():
		return &refusal{reason: reasonKeptByHand, account: id}
	case account.PaidByOther:
		return &refusal{reason: reasonPaidByOther, account: id}
	case account.Type == "brokerage":
		return &refusal{reason: reasonBrokerage, account: id}
	case st.ChainBreak != nil:
		return &refusal{reason: fmt.Sprintf(reasonChainBreak, st.ChainBreak.LineNo), account: id}
	case len(st.Warnings) > 0:
		return &refusal{reason: fmt.Sprintf(reasonWarning, st.Warnings[0]), account: id}
	case st.Opening == nil:
		return nil
	}
	cps, err := q.CheckpointsByAccount(ctx, account.UserID, account.ID)
	if err != nil {
		return err
	}
	// The balance the bank printed before the file's first day against the
	// one the account's own movements reach by the end of the day before.
	// Without a stated balance there is nothing to compare: the file's own
	// opening becomes the account's first.
	computed, ok, err := accounts.BalanceThrough(ctx, q, account.ID, cps, st.PeriodStart.AddDays(-1))
	if err != nil {
		return err
	}
	if ok && computed != *st.Opening {
		return &refusal{reason: fmt.Sprintf(reasonOpening, st.Opening.Display(), computed.Display()), account: id}
	}
	return nil
}

// failed is an upload that could not be read for a reason of the app's own:
// it waits for the owner, who can import it from the app.
func (s *Service) failed(ctx context.Context, up store.StatementUpload, err error, data []byte) store.UploadOutcome {
	logging.FromContext(ctx).Error("reading a statement upload failed", "upload_id", up.ID, "error", err)
	return review(reasonFailed, nil, data)
}

func review(reason string, account *uuid.UUID, data []byte) store.UploadOutcome {
	if utf8.RuneCountInString(reason) > maxReason {
		reason = string([]rune(reason)[:maxReason-1]) + "…"
	}
	return store.UploadOutcome{Status: store.UploadNeedsReview, Reason: reason, AccountID: account, Data: data}
}

func (s *Service) acquire(ctx context.Context) error {
	select {
	case s.reading <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) release() { <-s.reading }

// Status is what became of one of the user's uploads.
type Status struct {
	store.StatementUpload
	// Account is the account it is for, when known, with its position now.
	Account *accounts.Account
	// Run is the import run that brought it in, or the one that already had
	// it.
	Run *store.ImportRun
}

// Status reads what became of one of the user's uploads.
func (s *Service) Status(ctx context.Context, userID, id uuid.UUID) (Status, error) {
	up, err := s.db.Q().UploadByID(ctx, userID, id)
	if errors.Is(err, store.ErrNotFound) {
		return Status{}, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	if err != nil {
		return Status{}, err
	}
	out := Status{StatementUpload: up}
	if up.AccountID != nil {
		a, err := s.accounts.Get(ctx, userID, *up.AccountID)
		if err != nil && !errors.Is(err, accounts.ErrNotFound) {
			return Status{}, err
		}
		if err == nil {
			out.Account = &a
		}
	}
	if up.ImportRunID != nil {
		run, err := s.db.Q().ImportRunByID(ctx, userID, *up.ImportRunID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return Status{}, err
		}
		if err == nil {
			out.Run = &run
		}
	}
	return out, nil
}

// List returns the user's uploads in one state, oldest first, each with the
// account it is for when known.
func (s *Service) List(ctx context.Context, userID uuid.UUID, status string) ([]Listed, error) {
	ups, err := s.db.Q().UploadsByStatus(ctx, userID, status)
	if err != nil {
		return nil, err
	}
	all, err := s.db.Q().AccountsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]store.Account, len(all))
	for _, a := range all {
		byID[a.ID] = a
	}
	out := make([]Listed, 0, len(ups))
	for _, up := range ups {
		l := Listed{StatementUpload: up}
		if up.AccountID != nil {
			if a, ok := byID[*up.AccountID]; ok {
				l.Account = &a
			}
		}
		out = append(out, l)
	}
	return out, nil
}

// Listed is an upload with the account it is for, when known.
type Listed struct {
	store.StatementUpload
	Account *store.Account
}

// file reads the file one of the user's uploads still holds.
func (s *Service) file(ctx context.Context, userID, id uuid.UUID) (store.StatementUpload, []byte, error) {
	up, data, err := s.db.Q().UploadData(ctx, userID, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.StatementUpload{}, nil, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return up, data, err
}

// DryRun reads the file one of the user's uploads holds, writing nothing:
// what the web's dry run says of a file chosen by hand.
//
// The file is loaded only once it is its turn to be read, as every file read
// here is, so the owner's reads add no more than one file to memory.
func (s *Service) DryRun(ctx context.Context, userID, id uuid.UUID) (statement.File, error) {
	if err := s.acquire(ctx); err != nil {
		return statement.File{}, err
	}
	defer s.release()
	up, data, err := s.file(ctx, userID, id)
	if err != nil {
		return statement.File{}, err
	}
	return statement.Parse(up.Filename, data)
}

// Import imports the file one of the user's uploads holds exactly as the
// web's import does — accounts included, created when new: the owner is the
// one importing now — and, once it is in, drops the file. expect is the
// account the owner is importing it into, if they chose one.
func (s *Service) Import(ctx context.Context, userID, id uuid.UUID, expect *uuid.UUID) (imports.Result, error) {
	if err := s.acquire(ctx); err != nil {
		return imports.Result{}, err
	}
	up, data, err := s.file(ctx, userID, id)
	if err != nil {
		s.release()
		return imports.Result{}, err
	}
	result, err := s.imports.Import(ctx, userID, up.Filename, data, expect)
	s.release()
	if err != nil {
		return imports.Result{}, err
	}
	o := store.UploadOutcome{Status: store.UploadImported}
	if result.Unchanged {
		o.Status = store.UploadUnchanged
	}
	if len(result.Accounts) > 0 {
		first := result.Accounts[0]
		o.AccountID, o.ImportRunID = &first.AccountID, &first.RunID
	}
	// Dismissed meanwhile from another tab: the file is in all the same.
	if err := s.db.Q().SettleUpload(ctx, userID, id, o, s.now()); err != nil && !errors.Is(err, store.ErrNotFound) {
		return imports.Result{}, err
	}
	return result, nil
}

// Dismiss sets one of the user's uploads aside and drops its file. Its
// reason stays, for the record.
func (s *Service) Dismiss(ctx context.Context, userID, id uuid.UUID) error {
	up, err := s.db.Q().UploadByID(ctx, userID, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !up.HasData) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return err
	}
	err = s.db.Q().SettleUpload(ctx, userID, id, store.UploadOutcome{Status: store.UploadDismissed, Reason: up.Reason,
		AccountID: up.AccountID}, s.now())
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return err
}

// Expire ends the uploads nobody will use and drops their files: addresses
// whose file never arrived in time, and files that waited for the owner for
// KeepFor. Housekeeping runs it.
func (s *Service) Expire(ctx context.Context) (int64, error) {
	now := s.now()
	return s.db.Q().ExpireUploads(ctx, now, now.Add(-stalledAfter), now.Add(-KeepFor))
}

// Facts returns, per account of the user's, what its imports say about what
// to fetch next: the last day its statements cover, when a file last added to
// it, and the cut of the latest closed card statement.
func (s *Service) Facts(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]store.StatementFacts, error) {
	return s.db.Q().StatementFactsByUser(ctx, userID, string(statement.SourceBGCardStatement))
}

// AccountLabel is what Claude and the owner's list call an account: the
// owner's alias, or what kind of account it is. Never the bank's name for it.
func AccountLabel(a store.Account) string {
	if a.Alias != "" {
		return a.Alias
	}
	switch a.Type {
	case "checking":
		return "Cuenta corriente"
	case "savings":
		return "Cuenta de ahorros"
	case "credit_card":
		return "Tarjeta de crédito"
	case "brokerage":
		return "Inversiones"
	}
	return a.Type
}
