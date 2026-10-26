package store

import "testing"

func TestSharedWordsOnlyCountsRealWords(t *testing.T) {
	tests := []struct {
		merchant, text string
		want           int
	}{
		{"Café Unido", "Comida y Bebida CAFE UNIDO PANAMA PA", 2},
		{"Super 99", "SUPER99 #1234", 1}, // a word that starts the other counts; "99" is too short
		{"Farmacia Arrocha", "Salud FARM ARROCHA", 2},
		{"El Rey", "SUPER REY", 1}, // "el" is too short to count
		{"Uber", "UBER *TRIP", 1},
		{"Cinépolis", "CINEPOLIS ALBROOK", 1},
		{"Café", "PANADERIA", 0},
		{"", "CAFE", 0},
	}
	for _, tt := range tests {
		if got := sharedWords(tt.merchant, tt.text); got != tt.want {
			t.Errorf("sharedWords(%q, %q) = %d, want %d", tt.merchant, tt.text, got, tt.want)
		}
	}
}
