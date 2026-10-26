package captures_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/store"
)

// A card someone else pays lives only on what the owner notes: from the
// phone's shortcut, with a key of its own, and by hand. Both work on it as on
// any card, and the shortcut calls it by the name the owner gave it, the only
// one it has.
func TestACardSomeoneElsePaysTakesTheShortcutAndPurchasesByHand(t *testing.T) {
	r := newRig(t, 60, 12)
	bearer := r.bearer(t, r.user)
	rec := r.serve(t, bearer, http.MethodPost, "/api/accounts", `{"alias": "Tarjeta de papá", "last4": "4321"}`)
	var card struct {
		ID          uuid.UUID `json:"id"`
		PaidByOther bool      `json:"paid_by_other"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil || rec.Code != http.StatusCreated || !card.PaidByOther {
		t.Fatalf("POST /api/accounts = %d %s", rec.Code, rec.Body)
	}

	key := r.createKey(t, r.user, card.ID)
	if got := decodeReply(t, r.post(t, "Bearer "+key, "", "")); got.Status != "test" ||
		got.Message != "Listo: el atajo está conectado a Tarjeta de papá." {
		t.Errorf("test run = %+v", got)
	}
	rec = r.post(t, "Bearer "+key, "application/json", `{"amount": "$4.50", "merchant": "Café Unido", "card": "Visa Papá"}`)
	if got := decodeReply(t, rec); rec.Code != http.StatusCreated || got.Message != "Anotada: $4.50 en Café Unido." {
		t.Errorf("shortcut purchase = %d %s", rec.Code, rec.Body)
	}
	rec = r.serve(t, bearer, http.MethodPost, "/api/captures",
		`{"account_id": "`+card.ID.String()+`", "amount": "12.00", "merchant": "Farmacia", "day": "2026-03-09"}`)
	if rec.Code != http.StatusCreated {
		t.Errorf("purchase by hand = %d %s", rec.Code, rec.Body)
	}

	// Nothing printed will ever stop them counting.
	list, err := r.db.Q().CapturesByAccount(context.Background(), r.user, card.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("captures = %+v, %v", list, err)
	}
	for _, c := range list {
		if c.State != store.CapturePending {
			t.Errorf("%s is %s, want pending", c.Merchant, c.State)
		}
	}
	if spent := r.spent(t); spent != 1650 {
		t.Errorf("spent = %s, want 16.50", spent)
	}
	if view := r.accountView(t, r.user, card.ID); view.Shortcut == nil || !view.Shortcut.Configured ||
		view.Shortcut.CardLabel != "Visa Papá" {
		t.Errorf("shortcut = %+v", view.Shortcut)
	}

	// And it owes the owner nothing, whatever was noted on it.
	rec = r.serve(t, bearer, http.MethodGet, "/api/accounts", "")
	var listed struct {
		NetWorth struct {
			Owed struct {
				Cents int64 `json:"cents"`
			} `json:"owed"`
			Unanchored int `json:"unanchored"`
		} `json:"net_worth"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil || listed.NetWorth.Owed.Cents != 0 ||
		listed.NetWorth.Unanchored != 0 {
		t.Errorf("GET /api/accounts = %d %s", rec.Code, rec.Body)
	}
	rec = r.serve(t, bearer, http.MethodPost, "/api/accounts/"+card.ID.String()+"/checkpoints",
		`{"as_of": "2026-03-01", "balance": "-16.50"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a stated balance = %d %s, want 422", rec.Code, rec.Body)
	}
}
