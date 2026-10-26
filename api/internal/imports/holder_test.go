package imports

import "testing"

func TestNamesHolder(t *testing.T) {
	holders := holderNames("Ana Pérez", "ANA MARIA PEREZ GOMEZ")
	for desc, want := range map[string]bool{
		"ACH BCOGENERAL Ana Perez":                         true,
		"ACH XPR: Ana Maria Perez G":                       true,
		"ACH XPRESS - PEREZ GOMEZ  ANA MARIA":              true,
		"ACH BCOGENERAL Ana Maria Pere":                    true, // the bank's column cut the surname short
		"BANCA MOVIL OTROBANCO 0000000000 Ana María Pérez": true,
		"TEF DE:LUIS MARIA PEREZ GO":                       false, // a relative: same surname, not her
		"ACH - ANA LUCIA RAMOS PEREIRA":                    false, // a namesake: same given name, not her
		"ACH CRE EMPRESA DE PRUEBA S.A.":                   false,
		"ACH BCOGENERAL Ana Pe":                            false, // two letters say nothing
	} {
		if got := namesHolder(holders, desc); got != want {
			t.Errorf("namesHolder(%q) = %v, want %v", desc, got, want)
		}
	}
}

func TestHolderNamesReadsGivenNameAndFirstSurname(t *testing.T) {
	for name, want := range map[string][]holderName{
		"Ana Pérez":             {{"ANA", "PEREZ"}},
		"Ana Pérez Gómez":       {{"ANA", "PEREZ"}, {"ANA", "GOMEZ"}},
		"Ana María Pérez Gómez": {{"ANA", "PEREZ"}},
		"Ana":                   nil,
	} {
		got := holderNames(name)
		if len(got) != len(want) {
			t.Errorf("holderNames(%q) = %v, want %v", name, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("holderNames(%q) = %v, want %v", name, got, want)
			}
		}
	}
}
