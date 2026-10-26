package captures

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/JuanKsPty/chenchen/api/internal/auth"
	"github.com/JuanKsPty/chenchen/api/internal/httpx"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// What the shortcut shows the owner after each purchase it sends, as a phone
// notification: short, and saying what to do when something went wrong. A
// failure the shortcut cannot fix ends in noting the purchase by hand, since
// nothing retries it.
const (
	msgKeyInvalid  = "Esta clave no sirve. Crea otra en chenchén y pégala en tu atajo."
	msgUnreadable  = "El atajo mandó algo que no entendemos. Revisa sus pasos en chenchén."
	msgNoAmount    = "No llegó el monto. Anótala a mano en chenchén."
	msgRefund      = "Parece una devolución: llegará con tu estado."
	msgTooMany     = "Llegaron muchas compras seguidas. Espera un minuto."
	msgServerError = "No pudimos anotarla. Anótala a mano en chenchén."
	msgBrokerage   = "En una cuenta de inversión no se anotan compras."
)

func msgNoted(amount money.Cents, merchant string) string {
	return "Anotada: " + purchasePhrase(amount, merchant) + "."
}

func msgDuplicate(amount money.Cents, merchant string) string {
	return "Ya la teníamos: " + purchasePhrase(amount, merchant) + "."
}

func msgTest(a store.Account) string {
	return "Listo: el atajo está conectado a " + accountLabel(a) + "."
}

// msgOtherCard is added to a noted purchase that came from a card other than
// the one the key first saw: the owner may have pointed this account's
// shortcut at the wrong card.
func msgOtherCard(card string) string {
	return " Ojo: llegó de «" + card + "», revisa el atajo."
}

func msgForeign(code string) string {
	return "Compra en " + code + ": llegará en dólares con tu estado."
}

func msgMalformed(raw string) string {
	return "No entendimos el monto «" + shorten(squeeze(raw), 30) + "». Anótala a mano en chenchén."
}

func msgImplausible(err error) string {
	var implausible ImplausibleError
	if errors.As(err, &implausible) {
		return dollars(implausible.Amount) + " no parece una compra. Si es correcto, anótala a mano."
	}
	return "Ese monto no parece una compra. Si es correcto, anótala a mano."
}

// purchasePhrase says a purchase the way the notification does: "$4.50 en
// Café Unido", or the amount alone when no merchant came.
func purchasePhrase(amount money.Cents, merchant string) string {
	if merchant == "" {
		return dollars(amount)
	}
	return dollars(amount) + " en " + merchant
}

func dollars(amount money.Cents) string { return "$" + amount.Display() }

// accountLabel names an account in a reply: the owner's alias, or what it is
// and the last four digits of its number. Never display_name, which is the
// bank's own name for its product: a reply must not name a bank, and only
// says which account the key writes to, nothing about it.
func accountLabel(a store.Account) string {
	if a.Alias != "" {
		return a.Alias
	}
	label := "tu cuenta"
	if a.Type == "credit_card" {
		label = "tu tarjeta"
	}
	var digits strings.Builder
	for _, r := range a.ExternalNumber {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	if d := digits.String(); len(d) >= 4 {
		label += " ···" + d[len(d)-4:]
	}
	return label
}

// shorten cuts s to at most n runes, marking the cut.
func shorten(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// shortcutError maps what can go wrong with a purchase the shortcut sent to
// the reply it gets. raw is the amount as sent, for the message that quotes
// it back.
func shortcutError(err error, raw string) *httpx.Error {
	var apiErr *httpx.Error
	var foreign ForeignCurrencyError
	switch {
	case errors.As(err, &apiErr):
		return apiErr
	case errors.Is(err, auth.ErrCaptureKeyInvalid):
		return (&httpx.Error{Status: http.StatusUnauthorized, Code: "capture_key_invalid",
			Message: msgKeyInvalid}).WithCause(err)
	case errors.Is(err, ErrNoAmount):
		return httpx.Unprocessable("no_amount", msgNoAmount).WithCause(err)
	case errors.Is(err, ErrRefund):
		return httpx.Unprocessable("refund", msgRefund).WithCause(err)
	case errors.As(err, &foreign):
		return httpx.Unprocessable("foreign_currency", msgForeign(foreign.Code)).WithCause(err)
	case errors.Is(err, ErrMalformed):
		return httpx.Unprocessable("malformed_amount", msgMalformed(raw)).WithCause(err)
	case errors.Is(err, ErrImplausible):
		return httpx.Unprocessable("implausible_amount", msgImplausible(err)).WithCause(err)
	case errors.Is(err, ErrBrokerage):
		return httpx.Unprocessable("brokerage_account", msgBrokerage).WithCause(err)
	default:
		// Not the API's usual "try again": nothing will send this purchase
		// again, so the owner must hear to note it by hand.
		return (&httpx.Error{Status: http.StatusInternalServerError, Code: "internal_error",
			Message: msgServerError}).WithCause(fmt.Errorf("captures: shortcut: %w", err))
	}
}
