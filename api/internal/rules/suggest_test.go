package rules

import (
	"strings"
	"testing"
)

func TestSuggestTextKeepsTheShopAndDropsWhatChanges(t *testing.T) {
	tests := []struct {
		description string
		text        string
		person      bool
	}{
		// A card line: which card, then the shop, the city and the country.
		{"[Tarjeta **** 1234] CAFE DEL PUERTO CIUDAD PA", "CAFE DEL PUERTO", false},
		{"[Tarjeta **** 1234] PANADERIA CIUDAD PA", "PANADERIA CIUDAD", false},
		{"[Tarjeta **** 1234] TIENDA LA ESQUINA SUC NO.4 CIUDAD PA", "TIENDA LA ESQUINA SUC", false},
		{"[Tarjeta **** 1234] Delivery*Pollos Asados https://www.p PA", "Delivery*Pollos Asados", false},
		{"[Tarjeta **** 1234] AMAZON MKTPL*1A2B3C4D5 Amzn.com/bill WA", "AMAZON MKTPL", false},
		{"[Tarjeta **** 1234] hosting.example Ciudad CY", "hosting.example Ciudad", false},
		// A debit line: 25 characters of shop, 5 of city.
		{"TIENDA DE PRUEBA         CIUDA", "TIENDA DE PRUEBA", false},
		{"EQUIPOS INTERNACIONAL ALBCIUDA", "EQUIPOS INTERNACIONAL ALB", false},
		{"RET. S/TARJETA ATM *0012345678", "RET. S/TARJETA ATM", false},
		// A charge with its card number, a promotion with its month.
		{"PROTECCION ROBO 4000-00**-****-00000", "PROTECCION ROBO", false},
		{"CR. PROMO WALLET SEPT26", "CR. PROMO WALLET", false},
		{"SEGURO DE FRAUDE MENSUAL", "SEGURO DE FRAUDE MENSUAL", false},
		{"SOCIO365", "SOCIO365", false},
		// Phone payments: to a shop, to a person for their shop, to or from a person.
		{"PAGO YAPPY BG A tiendita", "tiendita", false},
		{"YAPPY BG A ANA MARIA PEREZ (PANADERIA LA ESQUINA)", "PANADERIA LA ESQUINA", false},
		{"YAPPY BG DE ANA MARIA PEREZ (PANADERIA LA ESQUINA)", "PANADERIA LA ESQUINA", false},
		{"YAPPY BG A ANA MARIA PEREZ GOMEZ", "ANA MARIA PEREZ GOMEZ", true},
		{"YAPPY BG DE LUIS DE LA CRUZ POR cena", "LUIS DE LA CRUZ", true},
		{"YAPPY BG a CA A ROSA DIAZ", "ROSA DIAZ", true},
		{"YAPPY BG DE ROSA DIAZ (O) LUIS DIAZ (O POR soda", "ROSA DIAZ", true},
		// Spaces stay as printed, so the rule matches its own line.
		{"ACH CRE EMPRESA  S.A.", "ACH CRE EMPRESA  S.A.", false},
		{"[Tarjeta **** 1234] CAFE  DEL PUERTO CIUDAD PA", "CAFE  DEL PUERTO", false},
		{"   ", "", false},
	}
	for _, tt := range tests {
		text, person := SuggestText(tt.description)
		if text != tt.text || person != tt.person {
			t.Errorf("SuggestText(%q) = %q, %v; want %q, %v", tt.description, text, person, tt.text, tt.person)
		}
	}
}

func TestASuggestedTextMatchesItsOwnLine(t *testing.T) {
	for _, d := range []string{
		"[Tarjeta **** 1234] CAFE  DEL PUERTO CIUDAD PA",
		"TIENDA DE PRUEBA         CIUDA",
		"YAPPY BG A ANA  PEREZ (PANADERIA  LA ESQUINA) POR pan",
		"AMAZON MKTPL*1A2B3C4D5 Amzn.com/bill WA",
	} {
		if text, _ := SuggestText(d); text == "" || !strings.Contains(d, text) {
			t.Errorf("SuggestText(%q) = %q, which a rule would not find in it", d, text)
		}
	}
}

func TestSuggestTextFitsARule(t *testing.T) {
	long := "PAGO YAPPY BG A " + "UNA TIENDA CON UN NOMBRE MUY PERO MUY LARGO QUE NO CABE EN UNA REGLA"
	if text, _ := SuggestText(long); len([]rune(text)) > TextLimit || text == "" {
		t.Errorf("SuggestText of a long name = %q (%d characters), want at most %d", text, len([]rune(text)), TextLimit)
	}
}
