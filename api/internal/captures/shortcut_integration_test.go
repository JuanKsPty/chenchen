package captures_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/auth"
	"github.com/JuanKsPty/chenchen/api/internal/captures"
	"github.com/JuanKsPty/chenchen/api/internal/categories"
	"github.com/JuanKsPty/chenchen/api/internal/chat"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/config"
	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/imports"
	"github.com/JuanKsPty/chenchen/api/internal/investments"
	"github.com/JuanKsPty/chenchen/api/internal/mcpserver"
	"github.com/JuanKsPty/chenchen/api/internal/movements"
	"github.com/JuanKsPty/chenchen/api/internal/proposals"
	"github.com/JuanKsPty/chenchen/api/internal/reports"
	"github.com/JuanKsPty/chenchen/api/internal/rules"
	"github.com/JuanKsPty/chenchen/api/internal/server"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
	"github.com/JuanKsPty/chenchen/api/internal/transfers"
)

// lateNight is 23:30 in Panama on day: already the next day in UTC, and the
// moment a server reading the wrong zone would file a purchase under tomorrow.
var lateNight = time.Date(day.Year, day.Month, day.Day, 23, 30, 0, 0, civil.Panama)

func TestAPurchaseFromTheShortcutIsNotedOnItsDayInPanama(t *testing.T) {
	r := newRig(t, 60, 12)
	ctx := context.Background()
	card := r.numbered(t, r.user, "credit_card", "**** 1111")
	comida := r.category(t, r.user, "Alimentación")
	r.rule(t, "café", comida)
	key := r.createKey(t, r.user, card.ID)

	rec := r.post(t, "Bearer "+key, "application/json",
		`{"amount": "US$4.50", "merchant": " Café\tUnido ", "card": "Visa Oro", "date": "2026-03-10T23:29:58-05:00"}`)
	got := decodeReply(t, rec)
	if rec.Code != http.StatusCreated || !got.OK || got.Status != "noted" || got.Message != "Anotada: $4.50 en Café Unido." ||
		got.Capture == nil || got.Capture.BookedOn != "2026-03-10" || got.Capture.Amount.Cents != -450 ||
		got.Capture.Merchant != "Café Unido" || got.Error != nil {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	if lateNight.UTC().Day() == day.Day {
		t.Fatal("the clock is not past midnight in UTC, so this proves nothing")
	}

	c := r.get(t, r.user, got.Capture.ID)
	if c.AccountID != card.ID || c.Source != store.CaptureSourceShortcut || c.KeyID == nil || c.BookedOn != day ||
		!c.ReceivedAt.Equal(lateNight) || c.State != store.CapturePending || c.CategoryID == nil || *c.CategoryID != comida ||
		*c.CategorySource != "rule" {
		t.Errorf("noted = %+v", c)
	}
	want := map[string]string{"amount": "US$4.50", "merchant": " Café\tUnido ", "card": "Visa Oro",
		"date": "2026-03-10T23:29:58-05:00"}
	if fmt.Sprint(c.Raw) != fmt.Sprint(want) {
		t.Errorf("raw = %v, want what was sent: %v", c.Raw, want)
	}

	// The owner sees the key used, by its hint only.
	view := r.accountView(t, r.user, card.ID)
	if s := view.Shortcut; s == nil || !s.Configured || s.Hint != key[len(key)-4:] || s.CardLabel != "Visa Oro" ||
		s.LastCaptureAt == nil || !s.LastCaptureAt.Equal(lateNight) || s.TestedAt != nil || s.CreatedAt == nil {
		t.Errorf("account shortcut = %+v", view.Shortcut)
	}
	if spent := r.spent(t); spent != 450 {
		t.Errorf("spent = %v, want the purchase", spent)
	}
	if list, err := r.db.Q().CapturesByAccount(ctx, r.user, card.ID); err != nil || len(list) != 1 {
		t.Errorf("captures = %d, %v", len(list), err)
	}
}

func TestTheSamePurchaseTwiceWithinTwoMinutesIsNotedOnce(t *testing.T) {
	r := newRig(t, 60, 12)
	card := r.account(t, r.user, "credit_card")
	key := r.createKey(t, r.user, card.ID)
	body := `{"amount": "$3.50", "merchant": "Café Unido"}`

	if rec := r.post(t, "Bearer "+key, "application/json", body); rec.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", rec.Code, rec.Body)
	}
	r.now = lateNight.Add(120 * time.Second)
	rec := r.post(t, "Bearer "+key, "application/json", `{"amount": "3,50", "merchant": "CAFÉ  unido"}`)
	got := decodeReply(t, rec)
	if rec.Code != http.StatusOK || !got.OK || got.Status != "duplicate" ||
		got.Message != "Ya la teníamos: $3.50 en CAFÉ unido." || got.Capture != nil {
		t.Errorf("at 120 s = %d %s", rec.Code, rec.Body)
	}
	if n := r.count(t, card); n != 1 {
		t.Fatalf("captures after the repeat = %d, want 1", n)
	}

	// A minute past the window from the first is a purchase of its own.
	r.now = lateNight.Add(121 * time.Second)
	rec = r.post(t, "Bearer "+key, "application/json", body)
	if got := decodeReply(t, rec); rec.Code != http.StatusCreated || got.Status != "noted" {
		t.Errorf("at 121 s = %d %s", rec.Code, rec.Body)
	}
	if n := r.count(t, card); n != 2 {
		t.Errorf("captures = %d, want 2", n)
	}
	// A different amount or merchant is never a repeat.
	for _, body := range []string{`{"amount": "$3.51", "merchant": "Café Unido"}`, `{"amount": "$3.50", "merchant": "Café Durán"}`} {
		if rec := r.post(t, "Bearer "+key, "application/json", body); rec.Code != http.StatusCreated {
			t.Errorf("%s = %d %s", body, rec.Code, rec.Body)
		}
	}
}

func TestRunningTheShortcutByHandTestsTheKey(t *testing.T) {
	r := newRig(t, 60, 12)
	card := r.numbered(t, r.user, "credit_card", "4111 **** **** 1111")
	checking := r.numbered(t, r.user, "checking", "04-01-99-123456-7")
	named := r.numbered(t, r.user, "savings", "0401009999")
	if err := r.db.Q().SetAccountAlias(context.Background(), r.user, named.ID, "Ahorros de Ana"); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		account     store.Account
		contentType string
		body        string
		message     string
	}{
		{card, "", "", "Listo: el atajo está conectado a tu tarjeta ···1111."},
		{checking, "application/json", `{"amount": "", "merchant": " ", "card": "", "date": "2026-03-10T12:00:00-05:00"}`,
			"Listo: el atajo está conectado a tu cuenta ···4567."},
		{named, "application/json", "{}", "Listo: el atajo está conectado a Ahorros de Ana."},
	} {
		key := r.createKey(t, r.user, tt.account.ID)
		rec := r.post(t, "Bearer "+key, tt.contentType, tt.body)
		got := decodeReply(t, rec)
		if rec.Code != http.StatusOK || !got.OK || got.Status != "test" || got.Message != tt.message || got.Capture != nil {
			t.Errorf("test run = %d %s; want %q", rec.Code, rec.Body, tt.message)
		}
		if n := r.count(t, tt.account); n != 0 {
			t.Errorf("a test run noted %d purchases", n)
		}
		view := r.accountView(t, r.user, tt.account.ID)
		if s := view.Shortcut; s == nil || s.TestedAt == nil || !s.TestedAt.Equal(lateNight) || s.LastCaptureAt != nil {
			t.Errorf("account shortcut after a test = %+v", view.Shortcut)
		}
	}
}

func TestOnlyALiveKeyOfItsOwnAccountWrites(t *testing.T) {
	r := newRig(t, 60, 12)
	a := r.account(t, r.user, "credit_card")
	b := r.account(t, r.user, "checking")
	other := r.newUser(t, "other@example.com")
	theirs := r.account(t, other, "credit_card")
	key := r.createKey(t, r.user, a.ID)
	theirKey := r.createKey(t, other, theirs.ID)

	// The body cannot name another account, the user's own or anyone's:
	// unknown fields are ignored, and the key decides.
	for i, target := range []uuid.UUID{b.ID, theirs.ID} {
		body := fmt.Sprintf(`{"account_id": %q, "user_id": %q, "amount": "%d.00", "merchant": "Kiosco", "extra": [1, {"x": 2}]}`,
			target, other, i+1)
		if rec := r.post(t, "Bearer "+key, "application/json", body); rec.Code != http.StatusCreated {
			t.Errorf("POST naming %s = %d %s", target, rec.Code, rec.Body)
		}
	}
	if n := r.count(t, a); n != 2 {
		t.Errorf("A's purchases = %d, want both", n)
	}
	if n := r.count(t, b); n != 0 {
		t.Errorf("B's purchases = %d, want none", n)
	}
	if list, err := r.db.Q().CapturesByAccount(context.Background(), other, theirs.ID); err != nil || len(list) != 0 {
		t.Errorf("the other user's purchases = %d, %v", len(list), err)
	}

	// Their key writes to their account only, and never reaches the user's.
	if rec := r.post(t, theirKey, "application/json", `{"amount": "7", "merchant": "Suyo"}`); rec.Code != http.StatusCreated {
		t.Errorf("their bare key = %d %s", rec.Code, rec.Body)
	}
	if n := r.count(t, a); n != 2 {
		t.Errorf("A's purchases after their key = %d", n)
	}

	// Making another key revokes the first; revoking the second leaves none.
	second := r.createKey(t, r.user, a.ID)
	for _, k := range []string{key, "Bearer " + key} {
		rec := r.post(t, k, "application/json", `{"amount": "1", "merchant": "x"}`)
		if got := decodeReply(t, rec); rec.Code != http.StatusUnauthorized || got.OK ||
			got.Message != "Esta clave no sirve. Crea otra en chenchén y pégala en tu atajo." ||
			got.Error == nil || got.Error.Code != "capture_key_invalid" || got.Error.Message != got.Message ||
			got.Error.RequestID == "" {
			t.Errorf("the replaced key = %d %s", rec.Code, rec.Body)
		}
	}
	if rec := r.post(t, "Bearer "+second, "", ""); rec.Code != http.StatusOK {
		t.Errorf("the new key = %d %s", rec.Code, rec.Body)
	}
	if rec := r.serve(t, r.bearer(t, r.user), http.MethodDelete, "/api/captures/keys/"+a.ID.String(), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE key = %d %s", rec.Code, rec.Body)
	}
	if rec := r.post(t, "Bearer "+second, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("the revoked key = %d %s", rec.Code, rec.Body)
	}
	if view := r.accountView(t, r.user, a.ID); view.Shortcut == nil || view.Shortcut.Configured {
		t.Errorf("account shortcut after revoking = %+v", view.Shortcut)
	}

	// Nor can a user make or revoke a key for someone else's account.
	jwt := r.bearer(t, r.user)
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		rec := r.serve(t, jwt, method, "/api/captures/keys/"+theirs.ID.String(), "")
		if rec.Code != http.StatusNotFound || errorCode(t, rec) != "account_not_found" {
			t.Errorf("%s their key = %d %s", method, rec.Code, rec.Body)
		}
	}
	if rec := r.serve(t, jwt, http.MethodPost, "/api/captures/keys/not-an-id", ""); rec.Code != http.StatusNotFound {
		t.Errorf("POST a malformed account = %d %s", rec.Code, rec.Body)
	}
	if rec := r.post(t, theirKey, "", ""); rec.Code != http.StatusOK {
		t.Errorf("their key after the user's attempts = %d %s", rec.Code, rec.Body)
	}
}

func TestAKeyIsNoSessionAndASessionIsNoKey(t *testing.T) {
	r := newRig(t, 60, 12)
	card := r.account(t, r.user, "credit_card")
	key := r.createKey(t, r.user, card.ID)
	jwt := r.bearer(t, r.user)

	for _, header := range []string{"", jwt, "Bearer", "Bearer chn_nope", "Basic " + key} {
		rec := r.post(t, header, "application/json", `{"amount": "4.50", "merchant": "x"}`)
		if got := decodeReply(t, rec); rec.Code != http.StatusUnauthorized || got.OK || got.Error == nil {
			t.Errorf("POST with %q = %d %s", header, rec.Code, rec.Body)
		}
	}
	if n := r.count(t, card); n != 0 {
		t.Errorf("a session noted %d purchases", n)
	}
	for _, target := range []string{"/api/accounts", "/api/accounts/" + card.ID.String(), "/api/me", "/api/captures/review"} {
		rec := r.serve(t, "Bearer "+key, http.MethodGet, target, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s with a key = %d %s", target, rec.Code, rec.Body)
		}
	}
	rec := r.serve(t, "Bearer "+key, http.MethodPost, "/api/captures/keys/"+card.ID.String(), "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a key making a key = %d %s", rec.Code, rec.Body)
	}
	rec = r.serve(t, "Bearer "+key, http.MethodPost, "/api/captures",
		fmt.Sprintf(`{"account_id": %q, "amount": "5", "merchant": "x"}`, card.ID))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a key noting by hand = %d %s", rec.Code, rec.Body)
	}
}

func TestTheShortcutHearsWhatToDo(t *testing.T) {
	r := newRig(t, 60, 100)
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)

	for _, tt := range []struct {
		contentType, body string
		status            int
		code, message     string
	}{
		{"application/json", `{"amount": "", "merchant": "Shell"}`, http.StatusUnprocessableEntity, "no_amount",
			"No llegó el monto. Anótala a mano en chenchén."},
		{"application/json", `{"amount": "$0.00", "merchant": "Shell"}`, http.StatusUnprocessableEntity, "no_amount",
			"No llegó el monto. Anótala a mano en chenchén."},
		{"application/json", `{"amount": "-$5.00", "merchant": "Tienda"}`, http.StatusUnprocessableEntity, "refund",
			"Parece una devolución: llegará con tu estado."},
		{"application/json", `{"amount": -5, "merchant": "Tienda"}`, http.StatusUnprocessableEntity, "refund",
			"Parece una devolución: llegará con tu estado."},
		{"application/json", `{"amount": "€8.00", "merchant": "Louvre"}`, http.StatusUnprocessableEntity, "foreign_currency",
			"Compra en EUR: llegará en dólares con tu estado."},
		{"application/json", `{"amount": "abc", "merchant": "x"}`, http.StatusUnprocessableEntity, "malformed_amount",
			"No entendimos el monto «abc». Anótala a mano en chenchén."},
		// A JSON number reads as JSON writes it, never as Wallet's text: this
		// is twelve dollars and a fraction of a cent, not $12,345.00.
		{"application/json", `{"amount": 12.345, "merchant": "y"}`, http.StatusUnprocessableEntity, "malformed_amount",
			"No entendimos el monto «12.345». Anótala a mano en chenchén."},
		{"application/json", `{"amount": 1e3, "merchant": "y"}`, http.StatusUnprocessableEntity, "malformed_amount",
			"No entendimos el monto «1e3». Anótala a mano en chenchén."},
		{"application/json", `{"amount": "4 50", "merchant": "y"}`, http.StatusUnprocessableEntity, "malformed_amount",
			"No entendimos el monto «4 50». Anótala a mano en chenchén."},
		{"application/json", `{"amount": {"value": 4.5}, "merchant": "x"}`, http.StatusUnprocessableEntity, "malformed_amount",
			`No entendimos el monto «{"value": 4.5}». Anótala a mano en chenchén.`},
		{"application/json", `{"amount": "84000", "merchant": "x"}`, http.StatusUnprocessableEntity, "implausible_amount",
			"$84,000.00 no parece una compra. Si es correcto, anótala a mano."},
		{"application/json", `[{"amount": "4.50"}]`, http.StatusBadRequest, "unreadable_purchase",
			"El atajo mandó algo que no entendemos. Revisa sus pasos en chenchén."},
		{"application/json", `{"amount": "4.50"`, http.StatusBadRequest, "unreadable_purchase",
			"El atajo mandó algo que no entendemos. Revisa sus pasos en chenchén."},
		{"application/json", `{"merchant": "` + strings.Repeat("a", 9000) + `"}`, http.StatusRequestEntityTooLarge, "body_too_large",
			"El atajo mandó algo que no entendemos. Revisa sus pasos en chenchén."},
	} {
		rec := r.post(t, key, tt.contentType, tt.body)
		got := decodeReply(t, rec)
		if rec.Code != tt.status || got.OK || got.Message != tt.message || got.Error == nil ||
			got.Error.Code != tt.code || got.Error.Message != tt.message {
			t.Errorf("%.60s = %d %s; want %d %s %q", tt.body, rec.Code, rec.Body, tt.status, tt.code, tt.message)
		}
	}
	if n := r.count(t, card); n != 0 {
		t.Errorf("refused purchases noted: %d", n)
	}
	// A shortcut built with the wrong method or address hears it too.
	for _, tt := range []struct {
		method, target string
		status         int
	}{
		{http.MethodGet, "/api/shortcut/purchases", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/shortcut/purchase", http.StatusNotFound},
	} {
		rec := r.serve(t, key, tt.method, tt.target, "")
		if got := decodeReply(t, rec); rec.Code != tt.status || got.OK ||
			got.Message != "El atajo mandó algo que no entendemos. Revisa sus pasos en chenchén." {
			t.Errorf("%s %s = %d %s", tt.method, tt.target, rec.Code, rec.Body)
		}
	}

	// What is fine: an amount as a JSON number, names in any case, a merchant
	// tidied and cut to what the schema holds, no merchant at all.
	for _, tt := range []struct {
		body, message, merchant string
		cents                   int64
	}{
		{`{"amount": 4.5, "merchant": "Super 99"}`, "Anotada: $4.50 en Super 99.", "Super 99", -450},
		{`{"amount": 1234.5, "merchant": "Muebles"}`, "Anotada: $1,234.50 en Muebles.", "Muebles", -123450},
		{`{"Amount": "B/. 12,00", "MERCHANT": "Farmacia\u200b Arrocha\u0001"}`, "Anotada: $12.00 en Farmacia Arrocha.", "Farmacia Arrocha", -1200},
		{`{"amount": "1.234,56", "merchant": "` + strings.Repeat("ñ", 200) + `"}`, "", strings.Repeat("ñ", 139) + "…", -123456},
		{`{"amount": "$7.25"}`, "Anotada: $7.25.", "", -725},
	} {
		rec := r.post(t, key, "application/json", tt.body)
		got := decodeReply(t, rec)
		if rec.Code != http.StatusCreated || got.Capture == nil || got.Capture.Merchant != tt.merchant ||
			got.Capture.Amount.Cents != tt.cents || (tt.message != "" && got.Message != tt.message) {
			t.Errorf("%.60s = %d %s", tt.body, rec.Code, rec.Body)
		}
	}
}

func TestAFormBodyWorksToo(t *testing.T) {
	r := newRig(t, 60, 12)
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)

	form := url.Values{"amount": {"4,50"}, "merchant": {"Café Unido"}, "card": {"Visa"}, "colour": {"red"}}
	rec := r.post(t, key, "application/x-www-form-urlencoded", form.Encode())
	if got := decodeReply(t, rec); rec.Code != http.StatusCreated || got.Message != "Anotada: $4.50 en Café Unido." {
		t.Errorf("url-encoded = %d %s", rec.Code, rec.Body)
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for name, value := range map[string]string{"Amount": "US$ 9.99", "merchant": "Super 99", "date": "hoy"} {
		if err := w.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	rec = r.post(t, key, w.FormDataContentType(), body.String())
	if got := decodeReply(t, rec); rec.Code != http.StatusCreated || got.Message != "Anotada: $9.99 en Super 99." {
		t.Errorf("multipart = %d %s", rec.Code, rec.Body)
	}
	if n := r.count(t, card); n != 2 {
		t.Errorf("captures = %d, want 2", n)
	}
}

// The reply is the same as for any purchase: saying the line was already
// there would tell whoever holds the key what the statements contain.
func TestAPurchaseAlreadyInTheStatementIsLinkedAtOnce(t *testing.T) {
	r := newRig(t, 60, 12)
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)
	line := r.line(t, card, day, -950, store.KindExpense, "KIOSCO 123")

	rec := r.post(t, key, "application/json", `{"amount": "$9.50", "merchant": "Kiosco"}`)
	got := decodeReply(t, rec)
	if rec.Code != http.StatusCreated || got.Status != "noted" || got.Message != "Anotada: $9.50 en Kiosco." {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	if c := r.get(t, r.user, got.Capture.ID); c.EntryID == nil || *c.EntryID != line.ID {
		t.Errorf("capture = %+v, want linked to %s", c, line.ID)
	}
	if spent := r.spent(t); spent != 950 {
		t.Errorf("spent = %v, want the line once", spent)
	}
}

func TestADifferentCardNameIsPointedOut(t *testing.T) {
	r := newRig(t, 60, 12)
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)

	for i, tt := range []struct{ card, message string }{
		{"Visa Oro", "Anotada: $1.00 en Tienda."},  // the first name the key sees
		{"visa  oro", "Anotada: $2.00 en Tienda."}, // the same, as typed
		{"", "Anotada: $3.00 en Tienda."},          // none sent
		{"Mastercard", "Anotada: $4.00 en Tienda. Ojo: llegó de «Mastercard», revisa el atajo."},
	} {
		body := fmt.Sprintf(`{"amount": "%d", "merchant": "Tienda", "card": %q}`, i+1, tt.card)
		rec := r.post(t, key, "application/json", body)
		if got := decodeReply(t, rec); rec.Code != http.StatusCreated || got.Message != tt.message {
			t.Errorf("card %q = %d %s; want %q", tt.card, rec.Code, rec.Body, tt.message)
		}
	}
	if view := r.accountView(t, r.user, card.ID); view.Shortcut == nil || view.Shortcut.CardLabel != "Visa Oro" {
		t.Errorf("account shortcut = %+v", view.Shortcut)
	}
}

func TestABrokerageAccountTakesNoKeyAndNoPurchase(t *testing.T) {
	r := newRig(t, 60, 12)
	brokerage := r.account(t, r.user, "brokerage")
	jwt := r.bearer(t, r.user)

	rec := r.serve(t, jwt, http.MethodPost, "/api/captures/keys/"+brokerage.ID.String(), "")
	if rec.Code != http.StatusUnprocessableEntity || errorMessage(t, rec) != "En una cuenta de inversión no se anotan compras." {
		t.Errorf("POST a brokerage key = %d %s", rec.Code, rec.Body)
	}
	if view := r.accountView(t, r.user, brokerage.ID); view.Shortcut != nil {
		t.Errorf("a brokerage account has a shortcut: %+v", view.Shortcut)
	}

	// One that got a key anyway, written straight into the database, is
	// still refused.
	key, hash, err := auth.NewCaptureKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := storetest.Exec(t, r.db, `INSERT INTO capture_keys (id, user_id, account_id, key_hash, hint)
		VALUES ($1, $2, $3, $4, 'abcd')`, uuid.New(), r.user, brokerage.ID, hash); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"amount": "4.50", "merchant": "x"}`, ""} {
		rec := r.post(t, "Bearer "+key, "application/json", body)
		got := decodeReply(t, rec)
		if rec.Code != http.StatusUnprocessableEntity || got.Message != "En una cuenta de inversión no se anotan compras." {
			t.Errorf("POST %q with a brokerage key = %d %s", body, rec.Code, rec.Body)
		}
	}
	if n := r.count(t, brokerage); n != 0 {
		t.Errorf("a brokerage account noted %d purchases", n)
	}
}

func TestTooManyPurchasesWaitAMinute(t *testing.T) {
	r := newRig(t, 60, 2)
	card := r.account(t, r.user, "credit_card")
	key := "Bearer " + r.createKey(t, r.user, card.ID)
	other := "Bearer " + r.createKey(t, r.user, r.account(t, r.user, "checking").ID)

	for i := range 2 {
		if rec := r.post(t, key, "application/json", fmt.Sprintf(`{"amount": "%d", "merchant": "x"}`, i+1)); rec.Code != http.StatusCreated {
			t.Fatalf("purchase %d = %d %s", i, rec.Code, rec.Body)
		}
	}
	rec := r.post(t, key, "application/json", `{"amount": "3", "merchant": "x"}`)
	got := decodeReply(t, rec)
	if rec.Code != http.StatusTooManyRequests || got.OK || got.Message != "Llegaron muchas compras seguidas. Espera un minuto." ||
		got.Error == nil || got.Error.Code != "rate_limited" {
		t.Errorf("the third = %d %s", rec.Code, rec.Body)
	}
	if wait, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || wait < 1 {
		t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	// The limit is per key: another account's shortcut is not held up.
	if rec := r.post(t, other, "application/json", `{"amount": "3", "merchant": "x"}`); rec.Code != http.StatusCreated {
		t.Errorf("another key = %d %s", rec.Code, rec.Body)
	}

	// Per address, before any key is looked up.
	byIP := newRig(t, 1, 12)
	for i, want := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		rec := byIP.post(t, "Bearer chn_nope", "application/json", "{}")
		if rec.Code != want || decodeReply(t, rec).Message == "" {
			t.Errorf("guess %d = %d %s; want %d", i, rec.Code, rec.Body, want)
		}
		if want == http.StatusTooManyRequests && rec.Header().Get("Retry-After") == "" {
			t.Error("no Retry-After per address")
		}
	}
}

func TestTheKeyIsNeverLogged(t *testing.T) {
	r := newRig(t, 60, 2)
	card := r.account(t, r.user, "credit_card")
	key := r.createKey(t, r.user, card.ID)

	for _, rec := range []struct {
		status int
		got    *httptest.ResponseRecorder
	}{
		{http.StatusCreated, r.post(t, "Bearer "+key, "application/json", `{"amount": "4.50", "merchant": "x"}`)},
		// The key pasted into the amount by mistake is quoted back to the
		// phone, which has it anyway, and never written to the log. It is
		// never an amount, whatever its random characters: its prefix is no
		// currency, so even a hyphen beside a digit reads as no refund.
		{http.StatusUnprocessableEntity, r.post(t, key, "application/json", `{"amount": "`+key+`", "merchant": "x"}`)},
		{http.StatusTooManyRequests, r.post(t, "Bearer "+key, "application/json", `{"amount": "4.50", "merchant": "x"}`)},
		{http.StatusUnauthorized, r.post(t, "Bearer "+key+"x", "application/json", "{}")},
		{http.StatusUnauthorized, r.serve(t, "Bearer "+key, http.MethodGet, "/api/accounts", "")},
		{http.StatusUnauthorized, r.serve(t, "", http.MethodPost, "/api/shortcut/purchases?key="+key, "{}")},
		{http.StatusNoContent, r.serve(t, r.bearer(t, r.user), http.MethodDelete, "/api/captures/keys/"+card.ID.String(), "")},
		{http.StatusUnauthorized, r.post(t, "Bearer "+key, "application/json", "{}")},
	} {
		if rec.got.Code != rec.status {
			t.Errorf("request = %d %s; want %d", rec.got.Code, rec.got.Body, rec.status)
		}
	}

	logs := r.logs.String()
	for _, logged := range []string{"path=/api/shortcut/purchases ", "code=malformed_amount",
		"code=capture_key_invalid", "code=rate_limited"} {
		if !strings.Contains(logs, logged) {
			t.Fatalf("the log, at debug, lacks %q, so this proves nothing:\n%s", logged, logs)
		}
	}
	secret := strings.TrimPrefix(key, auth.CaptureKeyPrefix)
	for _, leak := range []string{secret, secret[:12], secret[len(secret)-12:], auth.HashCaptureKey(key)} {
		if strings.Contains(logs, leak) {
			t.Errorf("the log holds %q:\n%s", leak, logs)
		}
	}
}

// --- the whole API --------------------------------------------------------------

// rig is the API as the server builds it, over the fixture's database, with
// the captures service's clock in the test's hands and the log, at debug, in
// a buffer.
type rig struct {
	fixture
	router http.Handler
	tokens *auth.TokenIssuer
	logs   *bytes.Buffer
	now    time.Time
}

func newRig(t *testing.T, ipLimit, keyLimit int) *rig {
	t.Helper()
	f := newFixture(t)
	r := &rig{fixture: f, logs: &bytes.Buffer{}, now: lateNight}
	f.svc.SetNow(func() time.Time { return r.now })

	cfg := config.Config{Env: "development", Auth: config.AuthConfig{
		JWTSecret: []byte(strings.Repeat("s", 32)), AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour,
		BcryptCost: 10, LoginRateLimit: 10, PinLoginRateLimit: 10, DeviceTTL: time.Hour,
		CaptureIPRateLimit: ipLimit, CaptureKeyRateLimit: keyLimit,
	}}
	logger := slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	db, cats := f.db, f.cats
	accountsSvc := accounts.NewService(db)
	authSvc, err := auth.NewService(db, accountsSvc, cats, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	movementsSvc := movements.NewService(db, cats)
	reportsSvc := reports.NewService(db)
	transfersSvc := transfers.NewService(db)
	rulesSvc := rules.NewService(db, cats)
	investmentsSvc := investments.NewService(db, nil)
	proposalsSvc := proposals.NewService(db, accountsSvc, cats, movementsSvc, rulesSvc, transfersSvc)
	chatSvc := chat.NewService(db, mcpserver.Deps{Accounts: accountsSvc, Movements: movementsSvc, Categories: cats,
		Reports: reportsSvc, Transfers: transfersSvc, Investments: investmentsSvc, Rules: rulesSvc,
		Proposals: proposalsSvc}, chat.NewUnavailable(), 1)
	limiter := func(n int) *httpx.RateLimiter { return httpx.NewRateLimiter(n) }

	r.router = server.NewRouter(server.Deps{
		Config: cfg, Logger: logger, Health: map[string]httpx.Checker{"postgres": db},
		Auth:        auth.NewHandler(authSvc, false, limiter(10), limiter(10)),
		Tokens:      authSvc.Tokens(),
		Accounts:    accounts.NewHandler(accountsSvc),
		Movements:   movements.NewHandler(movementsSvc),
		Categories:  categories.NewHandler(cats),
		Investments: investments.NewHandler(investmentsSvc),
		Imports:     imports.NewHandler(imports.NewService(db, cats), db),
		Reports:     reports.NewHandler(reportsSvc),
		Transfers:   transfers.NewHandler(transfersSvc),
		Rules:       rules.NewHandler(rulesSvc),
		Proposals:   proposals.NewHandler(proposalsSvc),
		Chat:        chat.NewHandler(chatSvc, authSvc, limiter(6)),
		Captures:    captures.NewHandler(f.svc),
		Shortcut: captures.NewShortcutHandler(f.svc, auth.NewCaptureKeys(db),
			limiter(cfg.Auth.CaptureIPRateLimit), limiter(cfg.Auth.CaptureKeyRateLimit)),
	})
	r.tokens = authSvc.Tokens()
	return r
}

// post sends a purchase to the shortcut endpoint as the phone would.
func (r *rig) post(t *testing.T, authorization, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/shortcut/purchases", strings.NewReader(body))
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	r.router.ServeHTTP(rec, req)
	return rec
}

// serve sends any request to the API.
func (r *rig) serve(t *testing.T, authorization, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Authorization", authorization)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.router.ServeHTTP(rec, req)
	return rec
}

// bearer is an Authorization header with an access token for user.
func (r *rig) bearer(t *testing.T, user uuid.UUID) string {
	t.Helper()
	token, err := r.tokens.IssueAccessToken(user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + token
}

// createKey makes a key for the account as its owner would, from the app.
func (r *rig) createKey(t *testing.T, user, accountID uuid.UUID) string {
	t.Helper()
	rec := r.serve(t, r.bearer(t, user), http.MethodPost, "/api/captures/keys/"+accountID.String(), "")
	var k struct {
		Key       string    `json:"key"`
		Hint      string    `json:"hint"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &k); err != nil || rec.Code != http.StatusCreated ||
		!strings.HasPrefix(k.Key, auth.CaptureKeyPrefix) || k.Hint != k.Key[len(k.Key)-4:] || k.CreatedAt.IsZero() {
		t.Fatalf("POST key = %d %s", rec.Code, rec.Body)
	}
	return k.Key
}

// accountShortcut is the part of an account's wire shape these tests read.
type accountShortcut struct {
	Shortcut *struct {
		Configured    bool       `json:"configured"`
		Hint          string     `json:"hint"`
		CreatedAt     *time.Time `json:"created_at"`
		TestedAt      *time.Time `json:"tested_at"`
		LastCaptureAt *time.Time `json:"last_capture_at"`
		CardLabel     string     `json:"card_label"`
	} `json:"shortcut"`
}

func (r *rig) accountView(t *testing.T, user, id uuid.UUID) accountShortcut {
	t.Helper()
	rec := r.serve(t, r.bearer(t, user), http.MethodGet, "/api/accounts/"+id.String(), "")
	var v accountShortcut
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET account = %d %s", rec.Code, rec.Body)
	}
	// The list says the same as the account on its own.
	rec = r.serve(t, r.bearer(t, user), http.MethodGet, "/api/accounts", "")
	var list struct {
		Accounts []struct {
			ID uuid.UUID `json:"id"`
			accountShortcut
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET accounts = %d %s", rec.Code, rec.Body)
	}
	for _, a := range list.Accounts {
		if a.ID == id && fmt.Sprint(a.Shortcut) != fmt.Sprint(v.Shortcut) {
			t.Errorf("listed shortcut = %+v, on its own %+v", a.Shortcut, v.Shortcut)
		}
	}
	return v
}

// numbered opens an account with a number of the test's choosing.
func (r *rig) numbered(t *testing.T, user uuid.UUID, typ, number string) store.Account {
	t.Helper()
	a := store.Account{ID: uuid.New(), UserID: user, Class: "asset", Type: typ, Institution: "bac",
		ExternalNumber: number, Currency: "USD", DisplayName: "PRODUCTO DEL BANCO"}
	if typ == "credit_card" {
		a.Class = "liability"
	}
	got, _, err := r.db.Q().EnsureAccount(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (r *rig) count(t *testing.T, a store.Account) int {
	t.Helper()
	list, err := r.db.Q().CapturesByAccount(context.Background(), a.UserID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return len(list)
}

// reply is what the shortcut reads back.
type reply struct {
	OK      bool   `json:"ok"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Capture *struct {
		ID     uuid.UUID `json:"id"`
		Amount struct {
			Cents int64 `json:"cents"`
		} `json:"amount"`
		Merchant string `json:"merchant"`
		BookedOn string `json:"booked_on"`
	} `json:"capture"`
	Error *struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

// decodeReply reads a reply, which must always be JSON with a message: the
// shortcut shows it whatever happened.
func decodeReply(t *testing.T, rec *httptest.ResponseRecorder) reply {
	t.Helper()
	var got reply
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Message == "" ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("reply %d %q is not JSON with a message: %v", rec.Code, rec.Body, err)
	}
	return got
}
