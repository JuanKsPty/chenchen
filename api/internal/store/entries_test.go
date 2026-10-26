package store

import (
	"testing"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/money"
)

func TestSearchAmount(t *testing.T) {
	tests := []struct {
		in   string
		want money.Cents
		ok   bool
	}{
		{"25", 2500, true},
		{"25.5", 2550, true},
		{"$25.00", 2500, true},
		{"  $25.00  ", 2500, true},
		{"+25", 2500, true},
		{"-25", 2500, true},
		{"−25.00", 2500, true}, // the typographic minus the app prints
		{"−$1,234.50", 123450, true},
		{"+$2,173.90", 217390, true},
		{"$ 25", 2500, true},
		{"1,000", 100000, true},
		{".50", 50, true},
		{"0", 0, true},

		{"", 0, false},
		{"$", 0, false},
		{"−", 0, false},
		{".", 0, false},
		{"SUPER 99", 0, false},
		{"25-30", 0, false},
		{"25$", 0, false},
		{"25.005", 0, false},
		{"1 000", 0, false},
		{",-25", 0, false},
		{"12/10", 0, false},
		{"99999999999999999999", 0, false},
	}
	for _, tt := range tests {
		got, ok := searchAmount(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("searchAmount(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSearchMatchesAmountsOnlyWhenTheTextIsOne(t *testing.T) {
	where, args, err := EntryFilter{UserID: uuid.New(), Search: "$25.00"}.where()
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 3 || args[1] != "%$25.00%" || args[2] != int64(2500) {
		t.Errorf("args = %#v", args)
	}
	const want = "e.user_id = $1 AND (e.description ILIKE $2 OR e.note ILIKE $2 OR e.bank_category ILIKE $2 OR abs(e.amount_cents) = $3)"
	if where != want {
		t.Errorf("where =\n  %s\nwant\n  %s", where, want)
	}

	where, args, err = EntryFilter{UserID: uuid.New(), Search: "super"}.where()
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || where != "e.user_id = $1 AND (e.description ILIKE $2 OR e.note ILIKE $2 OR e.bank_category ILIKE $2)" {
		t.Errorf("where = %s, args = %#v", where, args)
	}
}
