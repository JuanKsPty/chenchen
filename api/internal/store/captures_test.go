package store

import (
	"strings"
	"testing"
)

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

// captureAsEntryColumns gives a movement its time for the channels in
// timedChannelsSQL, which SQL cannot read from Go: moving a channel in or out
// of timedChannels without the SQL, or the other way round, fails here.
func TestTimedChannelsReadTheSameInSQL(t *testing.T) {
	quoted := make([]string, len(timedChannels))
	for i, c := range timedChannels {
		quoted[i] = "'" + c + "'"
	}
	if got := strings.Join(quoted, ", "); got != timedChannelsSQL {
		t.Errorf("timedChannelsSQL = %s, want %s", timedChannelsSQL, got)
	}
	if !strings.Contains(captureAsEntryColumns, "source IN ("+timedChannelsSQL+")") {
		t.Errorf("captureAsEntryColumns does not read timedChannelsSQL: %s", captureAsEntryColumns)
	}
}
