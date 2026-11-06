package captures

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/JuanKsPty/chenchen/api/internal/money"
)

var (
	// ErrNoAmount means the shortcut sent no amount, or a zero one: a gas
	// station or a hotel hold that shows no total, or a pass with no payment.
	ErrNoAmount = errors.New("captures: no amount")
	// ErrRefund means the amount is negative: money coming back, which the
	// statement brings and a purchase never is.
	ErrRefund = errors.New("captures: the amount is a refund")
	// ErrForeignCurrency means the amount is in a currency other than the
	// dollar; ForeignCurrencyError says which.
	ErrForeignCurrency = errors.New("captures: the amount is in a foreign currency")
	// ErrMalformed means the amount could not be read as one.
	ErrMalformed = errors.New("captures: malformed amount")
	// ErrImplausible means the amount is more than maxWalletAmount: no tap
	// pays that, a misread one could. ImplausibleError carries the amount
	// read, when it could be.
	ErrImplausible = errors.New("captures: the amount is implausible for a purchase")
)

// ForeignCurrencyError is ErrForeignCurrency with the currency the amount
// came in: an ISO code when the phone wrote one or a symbol stands for one
// ("EUR" for "€"), else the marker as written ("MX$").
type ForeignCurrencyError struct{ Code string }

func (e ForeignCurrencyError) Error() string {
	return fmt.Sprintf("%v: %s", ErrForeignCurrency, e.Code)
}

func (e ForeignCurrencyError) Is(target error) bool { return target == ErrForeignCurrency }

// ImplausibleError is ErrImplausible with the amount read, so the reply can
// say what was understood.
type ImplausibleError struct{ Amount money.Cents }

func (e ImplausibleError) Error() string {
	return fmt.Sprintf("%v: %s", ErrImplausible, e.Amount)
}

func (e ImplausibleError) Is(target error) bool { return target == ErrImplausible }

// maxWalletAmount is the most a purchase the shortcut sends can be. Far above
// any tap, and far below the manual limit (maxAmount): a number misread on its
// way from Wallet — a reference, a card number — must never count as spending
// unseen, while a real purchase this large can still be noted by hand.
const maxWalletAmount money.Cents = 50_000 * 100

// dollarMarkers are what the phone may write around a dollar amount. The
// balboa is at par with the dollar and circulates as one, so "B/." and "PAB"
// are dollars too. Longest first, so "US$" is never read as "US" and "$".
var dollarMarkers = []string{"US$", "U$S", "USD", "B/.", "PAB", "B/", "$"}

// symbolCodes names the currencies whose symbol stands for only one.
var symbolCodes = map[string]string{"€": "EUR", "£": "GBP", "¥": "JPY", "₡": "CRC"}

// ParseWalletAmount reads the amount of a purchase as the phone's Wallet
// wrote it — localized text such as "$4.50", "USD 4.50", "B/.4.50" or "4,50" —
// and returns it in cents, positive. It never goes through a float. Its errors
// never quote what was sent: a shortcut wired wrong can send anything as the
// amount, its key included, and errors end up in the log.
//
//  1. Unicode spaces become spaces, the Unicode minus a hyphen and a typographic
//     apostrophe a plain one.
//  2. Nothing left is ErrNoAmount.
//  3. What is written on either side of the number must be a currency marker,
//     signs and spaces, or nothing; anything else is ErrMalformed, whatever
//     signs it carries — a key pasted into the field is no refund.
//  4. A minus sign or parentheses on either side of the number is ErrRefund.
//  5. A dollar marker (dollarMarkers, any case) before or after the number is
//     dropped; any other letters or currency symbol there is
//     ErrForeignCurrency.
//  6. The number itself holds digits, '.', ',', apostrophes and spaces only;
//     apostrophes and spaces only ever group thousands.
//  7. The decimal mark is decided by what is written: with both '.' and ',',
//     the last one, which must appear once with one or two digits after it;
//     with one kind once, one or two digits after it make it decimal, and
//     exactly three after one to three digits make it thousands ("1,234");
//     with one kind more than once, thousands. Thousands come in groups of
//     three, the first of one to three digits not starting with 0, marked one
//     way throughout ("1 234", never "4 50" or "1'234 567"). Anything else is
//     ErrMalformed: a stray mark read as nothing would make the amount a
//     hundred times larger.
//  8. Zero is ErrNoAmount, and more than maxWalletAmount ErrImplausible.
//
// An amount that came as a JSON number reads by ParseNumberAmount instead.
func ParseWalletAmount(raw string) (money.Cents, error) {
	s := strings.TrimSpace(strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r): // U+00A0, U+202F, U+2009, U+2007 among them
			return ' '
		case r == '\u2212':
			return '-'
		case r == '\u2019':
			return '\''
		}
		return r
	}, raw))
	if s == "" {
		return 0, ErrNoAmount
	}

	first := strings.IndexFunc(s, isDigit)
	if first < 0 {
		// No number at all: a marker alone means no amount was filled in.
		if marker, refund := splitSign(s); !refund && isDollars(marker) {
			return 0, ErrNoAmount
		}
		return 0, ErrMalformed
	}
	last := strings.LastIndexFunc(s, isDigit)
	before, refundBefore := splitSign(s[:first])
	after, refundAfter := splitSign(s[last+1:])
	// The sides are read whole before any sign counts: a minus sign means a
	// refund only beside a number and its currency, not inside text that is
	// no amount at all.
	var foreign error
	for _, marker := range []string{before, after} {
		if marker == "" || isDollars(marker) {
			continue
		}
		// A number started at its decimal mark, "$.50", is not one this reads.
		if bare := strings.TrimRight(marker, ".,"); bare == "" || isDollars(bare) {
			return 0, ErrMalformed
		}
		code, ok := currencyCode(marker)
		if !ok {
			return 0, ErrMalformed
		}
		foreign = ForeignCurrencyError{Code: code}
	}
	switch {
	case refundBefore || refundAfter:
		return 0, ErrRefund
	case foreign != nil:
		return 0, foreign
	}

	number, err := plainDecimal(s[first : last+1])
	if err != nil {
		return 0, err
	}
	return purchaseCents(number)
}

// ParseNumberAmount reads an amount the shortcut sent as a JSON number rather
// than as text. A JSON number means the same in every locale — '.' is always
// the decimal mark and nothing groups thousands — so it is never read as
// ParseWalletAmount reads Wallet's text, which takes "12.345" for twelve
// thousand dollars where a shortcut that split $24.69 in two meant twelve.
// Only a whole number of cents reads: more than two decimals, an exponent or
// anything else is ErrMalformed, never rounded or guessed at. A negative
// number is ErrRefund, zero ErrNoAmount and more than maxWalletAmount
// ErrImplausible.
func ParseNumberAmount(raw string) (money.Cents, error) {
	number, negative := strings.CutPrefix(strings.TrimSpace(raw), "-")
	whole, fraction, decimal := strings.Cut(number, ".")
	if !allDigits(whole) || decimal && (len(fraction) > 2 || !allDigits(fraction)) {
		return 0, ErrMalformed
	}
	cents, err := purchaseCents(number)
	if negative && !errors.Is(err, ErrNoAmount) {
		return 0, ErrRefund
	}
	return cents, err
}

// purchaseCents reads a plain decimal ("1234.56") as the amount of a
// purchase: zero is ErrNoAmount and more than maxWalletAmount ErrImplausible.
func purchaseCents(number string) (money.Cents, error) {
	cents, err := money.ParsePositive(number)
	switch {
	case errors.Is(err, money.ErrNotPositive):
		return 0, ErrNoAmount
	case errors.Is(err, money.ErrOutOfRange):
		return 0, ErrImplausible
	case err != nil:
		return 0, fmt.Errorf("%w: %v", ErrMalformed, err)
	case cents > maxWalletAmount:
		return 0, ImplausibleError{Amount: cents}
	}
	return cents, nil
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// allDigits reports whether s is one or more digits and nothing else.
func allDigits(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool { return !isDigit(r) }) < 0
}

// splitSign takes what is written beside the number apart: the marker, with
// spaces and a plus sign dropped, and whether a minus sign or a parenthesis
// was there.
func splitSign(side string) (marker string, refund bool) {
	var b strings.Builder
	for _, r := range side {
		switch {
		case r == '-' || r == '(' || r == ')':
			refund = true
		case r == '+' || unicode.IsSpace(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String(), refund
}

// isDollars reports whether a marker is nothing but dollar markers, such as
// "US$" or "USD$".
func isDollars(marker string) bool {
	rest := strings.ToUpper(marker)
	for rest != "" {
		found := false
		for _, m := range dollarMarkers {
			if strings.HasPrefix(rest, m) {
				rest, found = rest[len(m):], true
				break
			}
		}
		if !found {
			return false
		}
	}
	return marker != ""
}

// currencyCode names the currency of a marker that is not dollars, when it
// can be one: up to four letters or currency symbols, perhaps with the dots
// and slashes of "Bs." or "S/.". A marker of letters alone is a code and reads
// in capitals. Anything longer is no currency, and is never repeated back.
func currencyCode(marker string) (string, bool) {
	if code, ok := symbolCodes[marker]; ok {
		return code, true
	}
	if utf8.RuneCountInString(marker) > 4 {
		return "", false
	}
	letters, named := true, false
	for _, r := range marker {
		switch {
		case unicode.IsLetter(r):
			named = true
		case unicode.Is(unicode.Sc, r):
			letters, named = false, true
		case r == '.' || r == '/':
			letters = false
		default:
			return "", false
		}
	}
	if letters {
		return strings.ToUpper(marker), named
	}
	return marker, named
}

// plainDecimal rewrites a localized number ("1.234,56") as money.Parse reads
// it ("1234.56"), by the rules in ParseWalletAmount.
func plainDecimal(s string) (string, error) {
	if strings.IndexFunc(s, func(r rune) bool { return !isDigit(r) && !strings.ContainsRune(".,' ", r) }) >= 0 {
		return "", ErrMalformed
	}

	// Apostrophes and spaces only ever group thousands; '.' and ',' may too.
	grouping := "' "
	dot, comma := strings.LastIndexByte(s, '.'), strings.LastIndexByte(s, ',')
	whole, fraction := s, ""
	switch {
	case dot < 0 && comma < 0:
	case dot >= 0 && comma >= 0:
		mark, other := byte('.'), ","
		if comma > dot {
			mark, other = ',', "."
		}
		i := strings.LastIndexByte(s, mark)
		whole, fraction = s[:i], s[i+1:]
		if strings.IndexByte(whole, mark) >= 0 || len(fraction) < 1 || len(fraction) > 2 {
			return "", ErrMalformed
		}
		grouping += other
	default:
		mark := "."
		if comma >= 0 {
			mark = ","
		}
		if parts := strings.Split(s, mark); len(parts) == 2 && len(parts[1]) >= 1 && len(parts[1]) <= 2 {
			whole, fraction = parts[0], parts[1]
		} else {
			grouping += mark
		}
	}
	whole, ok := ungroup(whole, grouping)
	if !ok || fraction != "" && !allDigits(fraction) {
		return "", ErrMalformed
	}
	if fraction == "" {
		return whole, nil
	}
	return whole + "." + fraction, nil
}

// ungroup drops the thousands mark from a whole number written in groups of
// three ("1,234,567"), refusing any other shape. The mark may be any one of
// marks, the same one throughout. A number with no mark at all is returned as
// it is.
func ungroup(s, marks string) (string, bool) {
	i := strings.IndexAny(s, marks)
	if i < 0 {
		return s, allDigits(s)
	}
	groups := strings.Split(s, s[i:i+1])
	lead := groups[0]
	if len(lead) < 1 || len(lead) > 3 || lead[0] == '0' {
		return "", false
	}
	for _, g := range groups[1:] {
		if len(g) != 3 || !allDigits(g) {
			return "", false
		}
	}
	return strings.Join(groups, ""), true
}
