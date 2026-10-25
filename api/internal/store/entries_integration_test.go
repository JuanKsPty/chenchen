package store_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

func TestSearchFindsAMovementByItsAmount(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "s@example.com")
	acct, _, err := db.Q().EnsureAccount(ctx, store.Account{ID: uuid.New(), UserID: u.ID, Class: "asset",
		Type: "checking", Institution: "bac", ExternalNumber: "1", Currency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	add := func(desc string, amount money.Cents) {
		t.Helper()
		e := store.Entry{ID: uuid.New(), UserID: u.ID, AccountID: acct.ID,
			BookedOn: civil.Date{Year: 2026, Month: time.October, Day: 1}, Amount: amount,
			Kind: store.KindExpense, Description: desc, DedupKey: desc}
		if amount > 0 {
			e.Kind = store.KindIncome
		}
		if _, err := db.Q().InsertEntryIfNew(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	add("SUPER 99", -2500)
	add("DEPOSITO", 2500)
	add("FARMACIA", -2550)
	add("PLANILLA", 217390)
	add("CINE 25", -1200)

	// Somebody else's 25.00 is not found.
	other := newUser(t, db, "other@example.com")
	theirs, _, err := db.Q().EnsureAccount(ctx, store.Account{ID: uuid.New(), UserID: other.ID, Class: "asset",
		Type: "checking", Institution: "bac", ExternalNumber: "1", Currency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Q().InsertEntryIfNew(ctx, store.Entry{ID: uuid.New(), UserID: other.ID, AccountID: theirs.ID,
		BookedOn: civil.Date{Year: 2026, Month: time.October, Day: 1}, Amount: -2500, Kind: store.KindExpense,
		Description: "AJENO", DedupKey: "k"}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		search string
		want   []string
	}{
		// Both directions of 25.00, plus the description that mentions 25.
		{"25", []string{"CINE 25", "DEPOSITO", "SUPER 99"}},
		{"$25.00", []string{"DEPOSITO", "SUPER 99"}},
		{"−$25.00", []string{"DEPOSITO", "SUPER 99"}},
		{"+25", []string{"DEPOSITO", "SUPER 99"}},
		{"25.50", []string{"FARMACIA"}},
		{"$2,173.90", []string{"PLANILLA"}},
		// Text still matches as text.
		{"super", []string{"SUPER 99"}},
		{"99", []string{"SUPER 99"}},
		// Not an amount, and in no description.
		{"25-30", nil},
	}
	for _, tt := range tests {
		list, err := db.Q().Entries(ctx, store.EntryFilter{UserID: u.ID, Search: tt.search})
		if err != nil {
			t.Fatalf("Entries(%q): %v", tt.search, err)
		}
		var got []string
		for _, e := range list {
			got = append(got, e.Description)
		}
		sort.Strings(got)
		if len(got) != len(tt.want) {
			t.Errorf("Entries(%q) = %v, want %v", tt.search, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("Entries(%q) = %v, want %v", tt.search, got, tt.want)
				break
			}
		}
	}

	// The amount match narrows like any other filter.
	list, err := db.Q().Entries(ctx, store.EntryFilter{UserID: u.ID, Search: "25", Kinds: []string{store.KindIncome}})
	if err != nil || len(list) != 1 || list[0].Description != "DEPOSITO" {
		t.Errorf("Entries(25, income) = %v, %v; want only DEPOSITO", list, err)
	}
}
