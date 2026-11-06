package store

import (
	"testing"

	"github.com/google/uuid"
)

func TestMatchRuleTakesTheFirstRuleThatDecidesEach(t *testing.T) {
	food, shops := uuid.New(), uuid.New()
	transfer := KindTransfer
	// In the order CategoryRulesByUser returns them.
	rules := []CategoryRule{
		{MatchText: "  "},                                    // says nothing: never matches
		{MatchText: "Pago", SetKind: &transfer},              // a kind, no category
		{MatchText: "super 99", CategoryID: &food},           // the first category that matches
		{MatchText: "super", CategoryID: &shops},             // too late for the category
		{MatchText: "99", SetKind: new(string)},              // too late for the kind
		{MatchText: "farmacia", CategoryID: &shops},          // does not match
		{MatchText: "PAGO SUPER", SetKind: new(string)},      // matches, the kind is taken
		{MatchText: "otro", CategoryID: &shops, Priority: 9}, // does not match
	}
	m := MatchRule("PAGO SUPER 99 Panamá", rules)
	if m.CategoryID == nil || *m.CategoryID != food {
		t.Errorf("category = %v, want the first rule that names one", m.CategoryID)
	}
	if m.SetKind == nil || *m.SetKind != KindTransfer {
		t.Errorf("kind = %v, want the first rule that sets one", m.SetKind)
	}

	// Case does not matter, on either side; nothing matching decides nothing.
	if m := MatchRule("Farmacia Arrocha", rules); m.CategoryID == nil || *m.CategoryID != shops || m.SetKind != nil {
		t.Errorf("MatchRule(Farmacia Arrocha) = %+v", m)
	}
	if m := MatchRule("Café Unido", rules); m.CategoryID != nil || m.SetKind != nil {
		t.Errorf("MatchRule(Café Unido) = %+v, want nothing", m)
	}
}

// What the phone notes carries accents; what the bank prints does not. One
// rule answers to both, written either way.
func TestMatchRuleIgnoresAccents(t *testing.T) {
	coffee := uuid.New()
	for _, text := range []string{"café unido", "CAFE UNIDO", "Café Unido"} {
		rules := []CategoryRule{{MatchText: text, CategoryID: &coffee}}
		for _, haystack := range []string{"Comida y Bebida CAFE UNIDO OBARRIO", "Café Unido", "CAFÉ ÚNIDO"} {
			if m := MatchRule(haystack, rules); m.CategoryID == nil {
				t.Errorf("rule %q on %q matched nothing", text, haystack)
			}
		}
	}
	if m := MatchRule("Panadería Niño", []CategoryRule{{MatchText: "panaderia nino", CategoryID: &coffee}}); m.CategoryID == nil {
		t.Errorf("ñ and í should fold")
	}
}
