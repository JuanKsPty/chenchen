package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/money"
	"github.com/JuanKsPty/chenchen/api/internal/store"
	"github.com/JuanKsPty/chenchen/api/internal/storetest"
)

// Linking a capture to its line carries over what the owner said about the
// purchase, by who said it: never over a choice someone made for the line.
func TestLinkingCarriesCategoryAndNoteByWhoChose(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	cats := map[string]uuid.UUID{}
	for _, name := range []string{"Línea", "Compra"} {
		c, err := db.Q().CreateCategory(ctx, store.Category{ID: uuid.New(), UserID: u.ID, Name: name, Kind: "expense"})
		if err != nil {
			t.Fatal(err)
		}
		cats[name] = c.ID
	}

	tests := []struct {
		name                  string
		lineBy, captureBy     string // who filed each: "" for nobody
		lineNote, captureNote string
		wantCategory, wantBy  string
		wantNote              string
	}{
		{"an unfiled line takes the purchase's category", "", "rule", "", "", "Compra", "rule", ""},
		{"rule against rule keeps the line's", "rule", "rule", "", "", "Línea", "rule", ""},
		{"the owner's choice beats a rule", "rule", "user", "", "", "Compra", "user", ""},
		{"the assistant's choice beats a rule", "rule", "assistant", "", "", "Compra", "assistant", ""},
		{"never over the owner's choice for the line", "user", "user", "", "", "Línea", "user", ""},
		{"never over the assistant's choice for the line", "assistant", "user", "", "", "Línea", "assistant", ""},
		{"an unfiled purchase leaves the line as it was", "rule", "", "", "", "Línea", "rule", ""},
		{"the note goes to a line without one", "", "", "", "con Ana", "", "", "con Ana"},
		{"never over the line's own note", "", "", "desayuno", "con Ana", "", "", "desayuno"},
	}
	for i, tt := range tests {
		on := day.AddDays(i * 10)
		amount := -100 - int64(i)
		lineID := addLine(t, db, card, on, money.Cents(amount), store.KindExpense, nil)
		if tt.lineBy != "" {
			if err := db.Q().SetEntryCategory(ctx, u.ID, lineID, ptr(cats["Línea"]), tt.lineBy); err != nil {
				t.Fatal(err)
			}
		}
		if tt.lineNote != "" {
			if err := db.Q().SetEntryNote(ctx, u.ID, lineID, tt.lineNote); err != nil {
				t.Fatal(err)
			}
		}
		before, err := db.Q().EntryByID(ctx, u.ID, lineID)
		if err != nil {
			t.Fatal(err)
		}
		captureID := addCapture(t, db, card, on, money.Cents(amount))
		if tt.captureBy != "" {
			if err := db.Q().SetCaptureCategory(ctx, u.ID, captureID, ptr(cats["Compra"]), tt.captureBy); err != nil {
				t.Fatal(err)
			}
		}
		if tt.captureNote != "" {
			if err := db.Q().SetCaptureNote(ctx, u.ID, captureID, tt.captureNote); err != nil {
				t.Fatal(err)
			}
		}

		if err := db.Q().LinkCapture(ctx, u.ID, captureID, lineID, store.LinkedByUser); err != nil {
			t.Fatalf("%s: LinkCapture: %v", tt.name, err)
		}
		line, err := db.Q().EntryByID(ctx, u.ID, lineID)
		if err != nil {
			t.Fatal(err)
		}
		var gotCategory, gotBy string
		for name, id := range cats {
			if line.CategoryID != nil && *line.CategoryID == id {
				gotCategory = name
			}
		}
		if line.CategorySource != nil {
			gotBy = *line.CategorySource
		}
		if gotCategory != tt.wantCategory || gotBy != tt.wantBy || line.Note != tt.wantNote {
			t.Errorf("%s: the line is filed under %q by %q with note %q; want %q by %q with %q",
				tt.name, gotCategory, gotBy, line.Note, tt.wantCategory, tt.wantBy, tt.wantNote)
		}
		// What the statement printed is the line's own.
		if line.Amount != before.Amount || line.BookedOn != before.BookedOn || line.Description != before.Description ||
			line.Kind != before.Kind || !reflect.DeepEqual(line.UserKind, before.UserKind) {
			t.Errorf("%s: the line changed from %+v to %+v", tt.name, before, line)
		}
		c, err := db.Q().CaptureByID(ctx, u.ID, captureID)
		if err != nil || c.State != store.CaptureLinked || c.EntryID == nil || *c.EntryID != lineID ||
			c.LinkedBy == nil || *c.LinkedBy != store.LinkedByUser {
			t.Errorf("%s: capture = %+v, %v; want linked by the owner", tt.name, c, err)
		}
	}
}

func TestOnlyAnOpenCaptureLinksAndOnlyToAFreeLineOfItsAccount(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card, checking := newCard(t, db, u.ID), newChecking(t, db, u.ID)
	line := addLine(t, db, card, day, -950, store.KindExpense, nil)
	elsewhere := addLine(t, db, checking, day, -950, store.KindExpense, nil)

	first, second := addCapture(t, db, card, day, -950), addCapture(t, db, card, day, -950)
	err := db.Q().LinkCapture(ctx, u.ID, first, elsewhere, store.LinkedByUser)
	if !errors.Is(err, store.ErrConflict) || store.IsConstraint(err, "captures_one_per_line_idx") {
		t.Errorf("linking to another account's line = %v, want the foreign key to refuse it", err)
	}
	if err := db.Q().LinkCapture(ctx, u.ID, first, line, store.LinkedByAuto); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().LinkCapture(ctx, u.ID, second, line, store.LinkedByUser); !store.IsConstraint(err, "captures_one_per_line_idx") {
		t.Errorf("linking a second purchase to the line = %v, want captures_one_per_line_idx", err)
	}
	// Linked or discarded, it is not open; and someone else's is not found.
	if err := db.Q().LinkCapture(ctx, u.ID, first, line, store.LinkedByUser); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("linking a linked capture = %v, want ErrNotFound", err)
	}
	if err := db.Q().DiscardCapture(ctx, u.ID, second); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().DiscardCapture(ctx, u.ID, second); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("discarding twice = %v, want ErrNotFound", err)
	}
	if err := db.Q().DiscardCapture(ctx, u.ID, first); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("discarding a linked capture = %v, want ErrNotFound", err)
	}
	if err := db.Q().UpdateCapture(ctx, u.ID, second, -100, "x", day); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("editing a discarded capture = %v, want ErrNotFound", err)
	}
	other := newUser(t, db, "other@example.com")
	theirs := addCapture(t, db, newCard(t, db, other.ID), day, -950)
	if err := db.Q().LinkCapture(ctx, u.ID, theirs, line, store.LinkedByUser); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("linking someone else's capture = %v, want ErrNotFound", err)
	}
	if err := db.Q().RejectPair(ctx, other.ID, theirs, line); !errors.Is(err, store.ErrConflict) {
		t.Errorf("rejecting someone else's line = %v, want the foreign key to refuse it", err)
	}

	// Unlinking reopens it and remembers the line, so it is no candidate any
	// more: with nothing else it could be, it counts again.
	if err := db.Q().UnlinkCapture(ctx, u.ID, first); err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, db, u.ID, first); got != store.CapturePending {
		t.Errorf("after unlinking: %s, want pending", got)
	}
	if err := db.Q().UnlinkCapture(ctx, u.ID, first); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unlinking twice = %v, want ErrNotFound", err)
	}
	if err := db.Q().RestoreCapture(ctx, u.ID, second); err != nil {
		t.Fatal(err)
	}
	candidates, err := db.Q().CaptureCandidates(ctx, u.ID, []uuid.UUID{card.ID})
	if err != nil || len(candidates) != 1 || candidates[0].CaptureID != second || candidates[0].Line.ID != line {
		t.Errorf("CaptureCandidates = %+v, %v; want only the restored capture and the line", candidates, err)
	}
	if got, err := db.Q().CaptureCandidates(ctx, u.ID, []uuid.UUID{checking.ID}); err != nil || len(got) != 0 {
		t.Errorf("CaptureCandidates(checking) = %+v, %v; want none", got, err)
	}
	if got, err := db.Q().CaptureCandidates(ctx, u.ID, nil); err != nil || len(got) != 1 {
		t.Errorf("CaptureCandidates(every account) = %+v, %v", got, err)
	}
}

// Unlinking takes back what the link carried to the line while the line still
// holds it, and nothing the line had of its own or was given after the link.
func TestUnlinkingTakesBackWhatTheLinkCarried(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	cats := map[string]uuid.UUID{}
	for _, name := range []string{"Línea", "Compra", "Otra"} {
		c, err := db.Q().CreateCategory(ctx, store.Category{ID: uuid.New(), UserID: u.ID, Name: name, Kind: "expense"})
		if err != nil {
			t.Fatal(err)
		}
		cats[name] = c.ID
	}
	n := 0
	// link pairs a new line with a new capture the owner filed and noted, and
	// returns both.
	link := func(captureBy string) (lineID, captureID uuid.UUID) {
		t.Helper()
		n++
		on, amount := day.AddDays(n*10), money.Cents(-100-n)
		lineID, captureID = addLine(t, db, card, on, amount, store.KindExpense, nil), addCapture(t, db, card, on, amount)
		if err := db.Q().SetCaptureCategory(ctx, u.ID, captureID, ptr(cats["Compra"]), captureBy); err != nil {
			t.Fatal(err)
		}
		if err := db.Q().SetCaptureNote(ctx, u.ID, captureID, "con Ana"); err != nil {
			t.Fatal(err)
		}
		return lineID, captureID
	}
	check := func(name string, lineID uuid.UUID, wantCategory, wantBy, wantNote string) {
		t.Helper()
		line, err := db.Q().EntryByID(ctx, u.ID, lineID)
		if err != nil {
			t.Fatal(err)
		}
		var gotCategory, gotBy string
		for name, id := range cats {
			if line.CategoryID != nil && *line.CategoryID == id {
				gotCategory = name
			}
		}
		if line.CategorySource != nil {
			gotBy = *line.CategorySource
		}
		if gotCategory != wantCategory || gotBy != wantBy || line.Note != wantNote {
			t.Errorf("%s: the line is filed under %q by %q with note %q; want %q by %q with %q",
				name, gotCategory, gotBy, line.Note, wantCategory, wantBy, wantNote)
		}
	}
	unlink := func(lineID, captureID uuid.UUID) {
		t.Helper()
		if err := db.Q().UnlinkCapture(ctx, u.ID, captureID); err != nil {
			t.Fatal(err)
		}
		c, err := db.Q().CaptureByID(ctx, u.ID, captureID)
		if err != nil || c.EntryID != nil || c.CategoryID == nil || *c.CategoryID != cats["Compra"] || c.Note != "con Ana" {
			t.Errorf("capture after unlinking = %+v, %v; want open, as the owner noted it", c, err)
		}
	}

	// No rule files the line: what the purchase carried is cleared.
	lineID, captureID := link("user")
	if err := db.Q().LinkCapture(ctx, u.ID, captureID, lineID, store.LinkedByAuto); err != nil {
		t.Fatal(err)
	}
	check("linked", lineID, "Compra", "user", "con Ana")
	unlink(lineID, captureID)
	check("unlinked, no rule", lineID, "", "", "")

	// A rule files the line again, as the importer did.
	if _, err := db.Q().CreateCategoryRuleIfNew(ctx, store.CategoryRule{ID: uuid.New(), UserID: u.ID,
		MatchText: "linea", CategoryID: ptr(cats["Línea"])}); err != nil {
		t.Fatal(err)
	}
	lineID, captureID = link("assistant")
	if err := db.Q().SetEntryCategory(ctx, u.ID, lineID, ptr(cats["Línea"]), "rule"); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().LinkCapture(ctx, u.ID, captureID, lineID, store.LinkedByAuto); err != nil {
		t.Fatal(err)
	}
	check("linked over a rule", lineID, "Compra", "assistant", "con Ana")
	unlink(lineID, captureID)
	check("unlinked, with a rule", lineID, "Línea", "rule", "")

	// What the owner gave the line after the link is theirs.
	lineID, captureID = link("user")
	if err := db.Q().LinkCapture(ctx, u.ID, captureID, lineID, store.LinkedByUser); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().SetEntryCategory(ctx, u.ID, lineID, ptr(cats["Otra"]), "user"); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().SetEntryNote(ctx, u.ID, lineID, "mío"); err != nil {
		t.Fatal(err)
	}
	unlink(lineID, captureID)
	check("changed after the link", lineID, "Otra", "user", "mío")

	// So is what the line had of its own, which the link never replaced.
	lineID, captureID = link("user")
	if err := db.Q().SetEntryCategory(ctx, u.ID, lineID, ptr(cats["Otra"]), "user"); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().SetEntryNote(ctx, u.ID, lineID, "desayuno"); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().LinkCapture(ctx, u.ID, captureID, lineID, store.LinkedByUser); err != nil {
		t.Fatal(err)
	}
	unlink(lineID, captureID)
	check("the line's own", lineID, "Otra", "user", "desayuno")

	// Someone else's link is not found, and stays.
	other := newUser(t, db, "other@example.com")
	theirCard := newCard(t, db, other.ID)
	theirLine, theirs := addLine(t, db, theirCard, day, -500, store.KindExpense, nil), addCapture(t, db, theirCard, day, -500)
	if err := db.Q().LinkCapture(ctx, other.ID, theirs, theirLine, store.LinkedByAuto); err != nil {
		t.Fatal(err)
	}
	if err := db.Q().UnlinkCapture(ctx, u.ID, theirs); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unlinking someone else's capture = %v, want ErrNotFound", err)
	}
	if got := stateOf(t, db, other.ID, theirs); got != store.CaptureLinked {
		t.Errorf("their capture is %s, want still linked", got)
	}
}

// Filing a capture by the rules never overwrites a category the owner or the
// assistant chose, however recently.
func TestFilingACaptureByRuleLeavesAChosenCategory(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	cats := map[string]uuid.UUID{}
	for _, name := range []string{"Elegida", "Regla"} {
		c, err := db.Q().CreateCategory(ctx, store.Category{ID: uuid.New(), UserID: u.ID, Name: name, Kind: "expense"})
		if err != nil {
			t.Fatal(err)
		}
		cats[name] = c.ID
	}
	for _, by := range []string{"", "rule", "user", "assistant"} {
		id := addCapture(t, db, card, day, -100)
		if by != "" {
			if err := db.Q().SetCaptureCategory(ctx, u.ID, id, ptr(cats["Elegida"]), by); err != nil {
				t.Fatal(err)
			}
		}
		chosen := by == "user" || by == "assistant"
		want := cats["Regla"]
		if chosen {
			want = cats["Elegida"]
		}
		if err := db.Q().FileCaptureByRule(ctx, u.ID, id, ptr(cats["Regla"])); err != nil {
			t.Fatal(err)
		}
		c, err := db.Q().CaptureByID(ctx, u.ID, id)
		if err != nil || c.CategoryID == nil || *c.CategoryID != want {
			t.Errorf("filed by %q: category = %v, %v; want %v", by, c.CategoryID, err, want)
		}
		// No rule clears what a rule filed, never what someone chose.
		if err := db.Q().FileCaptureByRule(ctx, u.ID, id, nil); err != nil {
			t.Fatal(err)
		}
		if c, err := db.Q().CaptureByID(ctx, u.ID, id); err != nil || chosen != (c.CategoryID != nil) {
			t.Errorf("filed by %q, then by no rule: category = %v, %v", by, c.CategoryID, err)
		}
	}
}

// An absurd amount — a card number pasted as one — is never close to a line,
// and never breaks review.
func TestReviewSurvivesAnAbsurdAmount(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	addLine(t, db, card, day, -1000, store.KindExpense, nil)
	huge := addCapture(t, db, card, day, -100_000_000_000_000_000)
	addStatement(t, db, card, day.AddDays(store.CaptureSettleDays+1))
	review, err := db.Q().CapturesForReview(ctx, u.ID)
	if err != nil || len(review) != 1 || review[0].Capture.ID != huge || review[0].Capture.State != store.CaptureMissing ||
		len(review[0].Near) != 0 {
		t.Errorf("CapturesForReview = %+v, %v; want the capture missing, nothing near it", review, err)
	}
}

// AsEntry and captureAsEntryColumns map a capture onto a movement the same
// way, one in Go and one in SQL.
func TestACaptureReadsAsTheSameMovementInGoAndInSQL(t *testing.T) {
	db := storetest.New(t)
	ctx := context.Background()
	u := newUser(t, db, "c@example.com")
	card := newCard(t, db, u.ID)
	key := newKey(t, db, card)
	src := "user"
	cat, err := db.Q().CreateCategory(ctx, store.Category{ID: uuid.New(), UserID: u.ID, Name: "Comida", Kind: "expense"})
	if err != nil {
		t.Fatal(err)
	}
	shortcut := store.Capture{ID: uuid.New(), UserID: u.ID, AccountID: card.ID, Source: store.CaptureSourceShortcut,
		KeyID: &key, ReceivedAt: time.Date(2026, time.October, 15, 12, 41, 0, 0, civil.Panama), BookedOn: day,
		Amount: -450, Merchant: "Café Unido", CategoryID: &cat.ID, CategorySource: &src, Note: "con Ana",
		Raw: map[string]string{"amount": "$4.50"}}
	if err := db.Q().InsertCapture(ctx, shortcut); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{shortcut.ID, addCapture(t, db, card, day, -1200)} {
		sql, err := db.Q().CaptureAsEntry(ctx, u.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		c, err := db.Q().CaptureByID(ctx, u.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.AsEntry(); !reflect.DeepEqual(got, sql) {
			t.Errorf("AsEntry = %+v\nSQL     = %+v", got, sql)
		}
	}
}

func ptr[T any](v T) *T { return &v }
