package captures

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/auth"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/logging"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// duplicateWindow is how close together the same purchase can arrive twice
// and still be one: a terminal repeating its signal, or an automation that
// runs on two devices. A second real purchase of the same thing in the same
// place within it is rare, and its line still arrives with the statement.
const duplicateWindow = 120 * time.Second

// maxCardRunes is the schema's limit on the card name a key remembers.
const maxCardRunes = 80

// What became of a purchase the shortcut sent.
const (
	// IngestTest is a run by hand, with nothing to note: the owner checking
	// that the shortcut reaches the account.
	IngestTest = "test"
	// IngestDuplicate is a purchase already noted moments ago.
	IngestDuplicate = "duplicate"
	// IngestNoted is a purchase noted: counting as spending until its line
	// arrives, or linked to it at once when the line was already imported,
	// or joined to the same purchase noted another way, by hand or by the
	// bank's email, which it then only says it heard of too. The reply never
	// says which. Whoever holds the key could otherwise learn what the
	// owner's statements, or the purchases they noted, contain by sending
	// amounts and reading back which ones were already there.
	IngestNoted = "noted"
)

// Purchase is what the shortcut sent, every field as text and any of them
// possibly empty. Only Amount and Merchant are read: the key decides the
// account and the server's clock the day, so Card and Date are kept as sent
// and never trusted.
type Purchase struct {
	Amount string
	// AmountIsNumber says Amount came as a JSON number, written as it was
	// sent: it reads by JSON's grammar (ParseNumberAmount), never as the
	// localized text Wallet writes.
	AmountIsNumber bool
	Merchant       string
	Card           string
	Date           string
}

// Ingested is what became of a purchase the shortcut sent.
type Ingested struct {
	// Status is one of the Ingest* outcomes.
	Status  string
	Account store.Account
	// Capture is the purchase noted, the one it joined, or the one it
	// repeats; zero for a test. Only its id may reach the shortcut: what it
	// holds may have been noted another way.
	Capture store.Capture
	// Amount is what was paid, positive, Merchant where, tidied as a new
	// purchase's would be, and Day the day it was filed under: what the
	// shortcut is told, joined or not.
	Amount   money.Cents
	Merchant string
	Day      civil.Date
	// OtherCard is the card the purchase came from when it is not the one
	// the key first saw.
	OtherCard string
}

// Ingest notes a purchase the phone's shortcut sent with a capture key, in
// the key's account and nothing else. The day is the server's day in Panama
// when it arrives: the phone's clock may be wrong, and the trigger reports no
// purchase time anyway.
//
// An empty amount and merchant is the owner running the shortcut by hand to
// test it: the key remembers it and nothing is noted. The same purchase sent
// twice within duplicateWindow is noted once. One already noted another way —
// typed by the owner, or told of by the bank's email (JoinSignal) — is only
// told of again, and keeps counting once. Otherwise it is filed by the user's
// rules, noted, and linked at once if its line is already imported and could
// be no other purchase — so it never counts twice.
//
// Everything happens under the user's lock, like any other write to their
// money, and only while the key is live: a key revoked while its purchase was
// on the way notes nothing.
func (s *Service) Ingest(ctx context.Context, scope auth.CaptureScope, p Purchase) (Ingested, error) {
	raw := map[string]string{"amount": asSent(p.Amount), "merchant": asSent(p.Merchant),
		"card": asSent(p.Card), "date": asSent(p.Date)}
	merchant := shorten(squeeze(p.Merchant), maxMerchantRunes)
	card := shorten(squeeze(p.Card), maxCardRunes)
	test := strings.TrimSpace(p.Amount) == "" && merchant == ""
	var amount money.Cents
	if !test {
		parse := ParseWalletAmount
		if p.AmountIsNumber {
			parse = ParseNumberAmount
		}
		var err error
		if amount, err = parse(p.Amount); err != nil {
			return Ingested{}, err
		}
	}
	now := s.now()
	keyGone := func(err error) error {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%w: %v", auth.ErrCaptureKeyInvalid, err)
		}
		return err
	}

	day := civil.Of(now.In(civil.Panama))
	out := Ingested{Amount: amount, Merchant: merchant, Day: day}
	err := s.db.InTx(ctx, func(q *store.Queries) error {
		if err := q.LockUser(ctx, scope.UserID); err != nil {
			return keyGone(err)
		}
		// A deleted account takes its keys with it; one deleted while this
		// purchase was on the way is the same as a revoked key.
		account, err := q.AccountByID(ctx, scope.UserID, scope.AccountID)
		if err != nil {
			return keyGone(err)
		}
		if account.Type == "brokerage" {
			return ErrBrokerage
		}
		out.Account = account
		if test {
			out.Status = IngestTest
			return keyGone(q.MarkCaptureKeyTested(ctx, scope.UserID, scope.KeyID, now))
		}

		dup, found, err := q.RecentDuplicateCapture(ctx, scope.UserID, account.ID, store.ChannelShortcut, -amount,
			raw["merchant"], now.Add(-duplicateWindow))
		if err != nil {
			return err
		}
		if found {
			out.Status, out.Capture = IngestDuplicate, dup
			return nil
		}

		joined, found, err := JoinSignal(ctx, q, scope.UserID, account.ID, amount, day,
			store.CaptureSignal{Channel: store.ChannelShortcut, At: now, Raw: raw})
		if err != nil {
			return err
		}
		id := joined.ID
		if !found {
			c := store.Capture{ID: uuid.New(), UserID: scope.UserID, AccountID: account.ID,
				Source: store.CaptureSourceShortcut, KeyID: &scope.KeyID, ReceivedAt: now,
				BookedOn: day, Amount: -amount, Merchant: merchant, Raw: raw}
			if c.CategoryID, err = ruleCategory(ctx, q, scope.UserID, merchant); err != nil {
				return err
			} else if c.CategoryID != nil {
				src := "rule"
				c.CategorySource = &src
			}
			if err := q.InsertCapture(ctx, c); err != nil {
				return err
			}
			id = c.ID
		}
		// Joined or not, the same steps follow, so the two take the same
		// course and the reply says the same.
		if _, err := AutoLink(ctx, q, scope.UserID, []uuid.UUID{account.ID}); err != nil {
			return err
		}
		if err := keyGone(q.TouchCaptureKey(ctx, scope.UserID, scope.KeyID, now, card)); err != nil {
			return err
		}
		if out.Capture, err = q.CaptureByID(ctx, scope.UserID, id); err != nil {
			return err
		}
		out.Status = IngestNoted
		if card != "" && scope.CardLabel != "" && !strings.EqualFold(card, scope.CardLabel) {
			out.OtherCard = card
		}
		return nil
	})
	if err != nil {
		return Ingested{}, err
	}
	return out, nil
}

// asSent keeps a field as the shortcut sent it, minus what the database's
// JSON cannot hold: a NUL character, or bytes that are not UTF-8.
func asSent(s string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(s, "\uFFFD"), "\x00", "")
}

// ShortcutHandler serves /api/shortcut: the phone's shortcut posting a
// purchase the moment a card is tapped. It is mounted outside the session
// middleware, takes no session and gives none: a capture key is checked here,
// and can do nothing but note purchases in its own account.
type ShortcutHandler struct {
	svc  *Service
	keys *auth.CaptureKeys
	// ipLimiter runs before a key is looked up, so guessing keys costs a
	// request each, per address the proxy saw (httpx.ClientIP). Someone with
	// many addresses still gets many guesses, so it is a speed bump;
	// keyLimiter, per key, is the limit a leaked key cannot get around.
	ipLimiter  *httpx.RateLimiter
	keyLimiter *httpx.RateLimiter
}

func NewShortcutHandler(svc *Service, keys *auth.CaptureKeys, ipLimiter, keyLimiter *httpx.RateLimiter) *ShortcutHandler {
	return &ShortcutHandler{svc: svc, keys: keys, ipLimiter: ipLimiter, keyLimiter: keyLimiter}
}

// Routes returns the /api/shortcut subrouter. A shortcut built by hand with
// the wrong method or address still gets a message it can show.
func (h *ShortcutHandler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/purchases", h.purchase)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		h.fail(w, r, httpx.NotFound("not_found", msgUnreadable), "")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		h.fail(w, r, &httpx.Error{Status: http.StatusMethodNotAllowed, Code: "method_not_allowed",
			Message: msgUnreadable}, "")
	})
	return r
}

func (h *ShortcutHandler) purchase(w http.ResponseWriter, r *http.Request) {
	if ok, wait := h.ipLimiter.Allow(httpx.ClientIP(r)); !ok {
		h.tooMany(w, r, wait)
		return
	}
	scope, err := h.keys.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		h.fail(w, r, err, "")
		return
	}
	if ok, wait := h.keyLimiter.Allow(scope.KeyID.String()); !ok {
		h.tooMany(w, r, wait)
		return
	}
	p, err := readPurchase(w, r)
	if err != nil {
		h.fail(w, r, err, "")
		return
	}
	got, err := h.svc.Ingest(r.Context(), scope, p)
	if err != nil {
		h.fail(w, r, err, p.Amount)
		return
	}

	out := shortcutReply{OK: true, Status: got.Status}
	status := http.StatusOK
	switch got.Status {
	case IngestTest:
		out.Message = msgTest(got.Account)
	case IngestDuplicate:
		out.Message = msgDuplicate(got.Amount, got.Merchant)
	default:
		out.Message, status = msgNoted(got.Amount, got.Merchant), http.StatusCreated
	}
	if status == http.StatusCreated {
		// Built from what the shortcut sent, never from the purchase noted:
		// one it joined may have been noted on another day or under another
		// name, and saying so would tell that it was already there.
		out.Capture = &ShortcutCaptureView{ID: got.Capture.ID, Amount: (-got.Amount).Amount(), Merchant: got.Merchant,
			BookedOn: got.Day}
		if got.OtherCard != "" {
			out.Message += msgOtherCard(got.OtherCard)
		}
	}
	reply(w, r, status, out)
}

func (h *ShortcutHandler) tooMany(w http.ResponseWriter, r *http.Request, wait time.Duration) {
	httpx.SetRetryAfter(w, wait)
	h.fail(w, r, httpx.TooManyRequests(msgTooMany), "")
}

// fail answers a purchase that was not noted. It logs as httpx.Fail does: a
// 5xx with its cause, a 4xx at debug. Neither the key nor the body is ever in
// what is logged.
func (h *ShortcutHandler) fail(w http.ResponseWriter, r *http.Request, err error, amount string) {
	apiErr := shortcutError(err, amount)
	logger := logging.FromContext(r.Context())
	if apiErr.Status >= http.StatusInternalServerError {
		logger.Error("request failed", "code", apiErr.Code, "status", apiErr.Status, "error", err)
	} else {
		logger.Debug("request rejected", "code", apiErr.Code, "status", apiErr.Status, "error", err)
	}
	reply(w, r, apiErr.Status, shortcutReply{Message: apiErr.Message, Error: &shortcutErrorBody{
		Code: apiErr.Code, Message: apiErr.Message, RequestID: httpx.RequestIDFromContext(r.Context())}})
}

// reply writes every answer the shortcut gets, with a real status. Should a
// phone turn out to throw on a 4xx instead of reading the body, this is the
// one place to answer 200 always and let ok tell.
func reply(w http.ResponseWriter, r *http.Request, status int, body shortcutReply) {
	httpx.JSON(w, r, status, body)
}

// shortcutReply is every answer the shortcut gets. It shows message whatever
// happened, so ok and message are always at the top; a failure carries the
// API's usual error envelope beside them.
type shortcutReply struct {
	OK      bool                 `json:"ok"`
	Status  string               `json:"status,omitempty"`
	Message string               `json:"message"`
	Capture *ShortcutCaptureView `json:"capture,omitempty"`
	Error   *shortcutErrorBody   `json:"error,omitempty"`
}

type shortcutErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// ShortcutCaptureView is a purchase as the shortcut is told it was noted:
// only what it sent, the day it was filed under and its id. Nothing else of
// the account's leaves through a key, not even whether the purchase was
// already noted another way.
type ShortcutCaptureView struct {
	ID       uuid.UUID    `json:"id"`
	Amount   money.Amount `json:"amount"`
	Merchant string       `json:"merchant"`
	BookedOn civil.Date   `json:"booked_on"`
}

// maxPurchaseBody caps what the shortcut may send: four short fields, with
// room to spare for a shortcut built by hand.
const maxPurchaseBody = 8 << 10

// readPurchase reads the body the shortcut sent: JSON, or a form, url-encoded
// or multipart, with the same field names. It is more forgiving than
// httpx.Decode on purpose: nothing retries a purchase, so one lost because a
// hand-built shortcut sends a field too many, names one "Amount", or sends
// the amount as a number would be lost for good. Field names are matched
// ignoring case, unknown ones are ignored and every one is optional. Only a
// body that is not an object at all is refused.
func readPurchase(w http.ResponseWriter, r *http.Request) (Purchase, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPurchaseBody)
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mediaType {
	case "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			return Purchase{}, unreadable(err)
		}
		return formPurchase(r.PostForm), nil
	case "multipart/form-data":
		if err := r.ParseMultipartForm(maxPurchaseBody); err != nil {
			return Purchase{}, unreadable(err)
		}
		return formPurchase(r.MultipartForm.Value), nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return Purchase{}, unreadable(err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return Purchase{}, nil
	}
	var fields struct {
		Amount   json.RawMessage `json:"amount"`
		Merchant json.RawMessage `json:"merchant"`
		Card     json.RawMessage `json:"card"`
		Date     json.RawMessage `json:"date"`
	}
	if err := json.Unmarshal(body, &fields); err != nil {
		return Purchase{}, unreadable(err)
	}
	amount := bytes.TrimSpace(fields.Amount)
	return Purchase{Amount: text(fields.Amount),
		AmountIsNumber: len(amount) > 0 && (amount[0] == '-' || isDigit(rune(amount[0]))),
		Merchant:       text(fields.Merchant), Card: text(fields.Card), Date: text(fields.Date)}, nil
}

// text reads one JSON value as text: a string as it reads, escapes decoded;
// anything else — a number above all — exactly as written, so an amount never
// goes through a float.
func text(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if raw[0] == '"' && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func formPurchase(v url.Values) Purchase {
	return Purchase{Amount: formValue(v, "amount"), Merchant: formValue(v, "merchant"),
		Card: formValue(v, "card"), Date: formValue(v, "date")}
}

// formValue is a form field's first value, its name matched ignoring case as
// a JSON body's are.
func formValue(v url.Values, name string) string {
	if got, ok := v[name]; ok && len(got) > 0 {
		return got[0]
	}
	for key, got := range v {
		if strings.EqualFold(key, name) && len(got) > 0 {
			return got[0]
		}
	}
	return ""
}

func unreadable(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return (&httpx.Error{Status: http.StatusRequestEntityTooLarge, Code: "body_too_large",
			Message: msgUnreadable}).WithCause(err)
	}
	return httpx.BadRequest("unreadable_purchase", msgUnreadable).WithCause(err)
}
