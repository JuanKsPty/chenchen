package imports

import (
	"strings"
	"unicode"
)

// holderName is how the owner can appear in a bank's description of a
// transfer: their first given name next to their first surname, in either
// order ("Juan Castillo", "CASTILLO PINTO JUAN DAVID"), the surname possibly
// cut short when it is the last thing the bank's column fits ("Juan David
// Cast").
//
// The given name alone is not enough: a relative or a namesake shares one of
// the two, rarely both.
type holderName struct {
	given, surname string
}

// holderNames reads the names the owner goes by — their profile's and the one
// their bank prints — into given name and first surname. A name of four or
// more words is two given names and two surnames; one of three is ambiguous,
// so both readings count.
func holderNames(names ...string) []holderName {
	var out []holderName
	for _, name := range names {
		w := nameWords(name)
		switch {
		case len(w) == 2:
			out = append(out, holderName{w[0], w[1]})
		case len(w) == 3:
			out = append(out, holderName{w[0], w[1]}, holderName{w[0], w[2]})
		case len(w) >= 4:
			out = append(out, holderName{w[0], w[len(w)-2]})
		}
	}
	return out
}

// namesHolder reports whether a description names the owner.
func namesHolder(holders []holderName, description string) bool {
	words := nameWords(description)
	if len(words) == 0 {
		return false
	}
	has := make(map[string]bool, len(words))
	for _, w := range words {
		has[w] = true
	}
	last := words[len(words)-1]
	for _, h := range holders {
		if !has[h.given] {
			continue
		}
		if has[h.surname] || (len(last) >= 3 && strings.HasPrefix(h.surname, last)) {
			return true
		}
	}
	return false
}

// accents folds the accented letters of Spanish names, which banks print both
// with and without them.
var accents = strings.NewReplacer("Á", "A", "É", "E", "Í", "I", "Ó", "O", "Ú", "U", "Ü", "U", "Ñ", "N")

// nameWords splits text into upper-case, accent-free words of letters only.
func nameWords(s string) []string {
	return strings.FieldsFunc(accents.Replace(strings.ToUpper(s)), func(r rune) bool { return !unicode.IsLetter(r) })
}
