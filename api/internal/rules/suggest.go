package rules

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TextLimit is how long a rule's text may be, in characters.
const TextLimit = 60

// SuggestText is the rule text a movement's description suggests: the shop it
// names, without what changes from one purchase to the next there (which
// card paid, the city, an order or card number), so a rule made from one line
// also files the next.
//
// person is true when the description names a person paid, or paying, by
// phone rather than a shop. What is paid to a person varies, so a rule for one
// is only right for something fixed, like a monthly payment: the app never
// offers one on its own.
//
// It reads the shapes the statements print:
//
//	[Tarjeta **** 1234] CAFE DEL PUERTO CIUDAD PA    a card line: shop, city, country
//	AMAZON MKTPL*1A2B3C4D5 Amzn.com/bill WA          an order number after the asterisk
//	TIENDA DE PRUEBA         CIUDA                   a 30-character debit line: 25 of shop, 5 of city
//	PAGO YAPPY BG A tiendita                         a phone payment to a shop
//	YAPPY BG A ANA PEREZ (PANADERIA LA ESQUINA)      to a person, for their shop
//	YAPPY BG DE ANA PEREZ POR cena                   from a person, with what they wrote
//	PROTECCION ROBO 4000-00**-****-00000             a charge with a card number
func SuggestText(description string) (text string, person bool) {
	s := strings.TrimSpace(description)
	// Which card paid: "[Tarjeta **** 1234] ".
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "]"); i >= 0 {
			s = strings.TrimSpace(s[i+1:])
		}
	}
	if m := phonePayment.FindStringSubmatch(s); m != nil {
		return phoneText(m[1] != "", m[2])
	}

	// A debit line is 25 characters of shop and 5 of city, padded with spaces.
	if r := []rune(s); len(r) == 30 && isLetters(r[25:]) {
		s = string(r[:25])
	}
	// The text kept is always a stretch of the description as printed, spaces
	// and all, so the rule made from it matches the line it came from.
	words := fields(s)
	word := func(i int) string { return s[words[i].start:words[i].end] }
	// An order number after an asterisk: AMAZON MKTPL*1A2B3C4D5.
	for i := range words {
		w := word(i)
		if j := strings.Index(w, "*"); j >= 0 && hasDigit(w[j+1:]) {
			start := words[i].start
			words = words[:i]
			if j > 0 {
				words = append(words, span{start, start + j})
			}
			break
		}
	}
	// A card line ends in the country's two letters, after the city: both
	// change with where the shop is, not which shop it is.
	if n := len(words); n >= 3 && isCountry(word(n-1)) {
		words = words[:n-1]
		if len(words) >= 3 {
			words = words[:len(words)-1]
		}
	}
	// Trailing numbers: a card, a reference, a date.
	for len(words) > 1 && hasDigit(word(len(words)-1)) {
		words = words[:len(words)-1]
	}
	if len(words) == 0 {
		return "", false
	}
	return clamp(s[:words[len(words)-1].end]), false
}

// span is where a word is in a text, in bytes.
type span struct{ start, end int }

// fields is strings.Fields that remembers where each word is.
func fields(s string) []span {
	var out []span
	start := -1
	for i, c := range s {
		switch {
		case unicode.IsSpace(c) && start >= 0:
			out = append(out, span{start, i})
			start = -1
		case !unicode.IsSpace(c) && start < 0:
			start = i
		}
	}
	if start >= 0 {
		out = append(out, span{start, len(s)})
	}
	return out
}

// phonePayment is a phone payment's line: "PAGO YAPPY BG A <shop>" to a shop,
// "YAPPY BG A|DE <person>" to or from a person (the bank sometimes puts a word
// of its own before the A).
var phonePayment = regexp.MustCompile(`^(PAGO )?YAPPY BG (?:.*? )??(?:A|DE) (.+)$`)

func phoneText(toShop bool, rest string) (string, bool) {
	// What the payer wrote comes after " POR ".
	if i := strings.Index(rest, " POR "); i >= 0 {
		rest = rest[:i]
	}
	if toShop {
		return clamp(rest), false
	}
	// A person's shop, in parentheses after their name. "(O)" is not a shop:
	// it joins the names on a shared account.
	if i := strings.Index(rest, "("); i >= 0 {
		shop := rest[i+1:]
		if j := strings.Index(shop, ")"); j >= 0 {
			shop = shop[:j]
		}
		if shop = strings.TrimSpace(shop); utf8.RuneCountInString(shop) > 1 {
			return clamp(shop), false
		}
		rest = rest[:i]
	}
	return clamp(rest), true
}

// clamp fits a text in a rule, keeping its spaces as printed.
func clamp(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > TextLimit {
		s = strings.TrimSpace(string(r[:TextLimit]))
	}
	return s
}

func isLetters(r []rune) bool {
	for _, c := range r {
		if !unicode.IsLetter(c) {
			return false
		}
	}
	return true
}

func isCountry(w string) bool {
	return len(w) == 2 && unicode.IsUpper(rune(w[0])) && unicode.IsUpper(rune(w[1]))
}

func hasDigit(s string) bool {
	return strings.ContainsFunc(s, unicode.IsDigit)
}
