package captures_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/identity"
	"github.com/JuanKsPty/chenchen/api/internal/movements"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// A printed line a noted purchase was linked to says which purchase it is in
// the movement lists, so Categorizar can say «La anotaste como «Café Unido».»
// and undo the link. Nothing else carries one, another user never reads it,
// and undoing the link takes it away.
func TestALinkedLineSaysWhichPurchaseItIs(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	moves := movements.NewService(f.db, f.cats)
	card := f.account(t, f.user, "credit_card")
	line := f.line(t, card, day, -950, store.KindExpense, "CAFE UNIDO PANAMA")
	alone := f.line(t, card, day, -2000, store.KindExpense, "FARMACIA ARROCHA")
	c := f.add(t, card, day, 950, "Café Unido")
	pending := f.add(t, card, day, 300, "Kiosco")
	if c.State != store.CaptureLinked || pending.State != store.CapturePending {
		t.Fatalf("fixture: %s and %s; want the café linked at once and the kiosk pending", c.State, pending.State)
	}
	want := store.NotedPurchase{CaptureID: c.ID, Merchant: "Café Unido", Source: store.CaptureSourceManual,
		LinkedBy: store.LinkedByAuto}

	noted := func(t *testing.T) map[uuid.UUID]*store.NotedPurchase {
		t.Helper()
		page, err := moves.List(ctx, f.user, movements.Query{})
		if err != nil {
			t.Fatal(err)
		}
		out := map[uuid.UUID]*store.NotedPurchase{}
		for _, e := range page.Entries {
			out[e.ID] = e.Noted
		}
		return out
	}

	got := noted(t)
	if len(got) != 3 {
		t.Fatalf("listed %d movements, want the two lines and the pending purchase", len(got))
	}
	if n := got[line.ID]; n == nil || *n != want {
		t.Errorf("the linked line's purchase = %+v, want %+v", n, want)
	}
	if got[alone.ID] != nil || got[pending.ID] != nil {
		t.Errorf("a line with no purchase or a pending purchase names one: %+v, %+v", got[alone.ID], got[pending.ID])
	}

	// On the wire, only the linked line has the field.
	entries := f.listEntries(t, f.user)
	if len(entries) != 3 {
		t.Fatalf("GET /api/entries listed %d movements, want 3", len(entries))
	}
	seen := false
	for _, e := range entries {
		var id uuid.UUID
		if err := json.Unmarshal(e["id"], &id); err != nil {
			t.Fatal(err)
		}
		raw, has := e["noted"]
		if id != line.ID {
			if has {
				t.Errorf("movement %s: noted = %s, want none", id, raw)
			}
			continue
		}
		seen = true
		if !has {
			t.Fatal("the linked line has no noted field")
		}
		var view movements.NotedView
		if err := json.Unmarshal(raw, &view); err != nil {
			t.Fatalf("noted = %s: %v", raw, err)
		}
		if view != (movements.NotedView{CaptureID: c.ID, Merchant: "Café Unido", Source: "manual", LinkedBy: "auto"}) {
			t.Errorf("noted = %s", raw)
		}
	}
	if !seen {
		t.Error("GET /api/entries did not list the linked line")
	}

	// Another user, even naming the line, reads nothing of it.
	other := f.newUser(t, "b@example.com")
	if n, err := f.db.Q().NotedPurchases(ctx, other, []uuid.UUID{line.ID}); err != nil || len(n) != 0 {
		t.Errorf("another user's NotedPurchases = %+v, %v; want nothing", n, err)
	}
	if len(f.listEntries(t, other)) != 0 {
		t.Error("another user lists the owner's movements")
	}

	// "No es esta compra": the line no longer names it.
	if _, err := f.svc.Unlink(ctx, f.user, c.ID); err != nil {
		t.Fatal(err)
	}
	if n := noted(t)[line.ID]; n != nil {
		t.Errorf("after unlinking, the line names %+v", n)
	}

	// Linked again by the owner, it says so.
	if _, err := f.svc.Link(ctx, f.user, c.ID, line.ID); err != nil {
		t.Fatal(err)
	}
	want.LinkedBy = store.LinkedByUser
	if n := noted(t)[line.ID]; n == nil || *n != want {
		t.Errorf("after linking by hand, the line names %+v, want %+v", n, want)
	}
}

// listEntries lists user's movements through the movements router, each as
// its raw JSON fields.
func (f fixture) listEntries(t *testing.T, user uuid.UUID) []map[string]json.RawMessage {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(identity.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	movements.NewHandler(movements.NewService(f.db, f.cats)).Routes().ServeHTTP(rec, req)
	var page struct {
		Entries []map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET /api/entries = %d %s", rec.Code, rec.Body)
	}
	return page.Entries
}
