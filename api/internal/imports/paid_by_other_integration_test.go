package imports_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/imports"
)

// A card someone else pays lives on the purchases the owner notes. A statement
// of it, or of the account it is a card of, is refused whole, from the general
// import page too: filed as a card of its own it would count each of those
// purchases twice, and put another person's debt in what the owner owes.
func TestAStatementOfACardSomeoneElsePaysIsNotImported(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	card, err := e.accounts.AddHandCard(ctx, e.user, uuid.Nil, "Tarjeta de papá", "4321")
	if err != nil {
		t.Fatal(err)
	}
	e.note(t, card.ID, jan(15), -950, "CAFE DE PRUEBA")
	own := e.openCard(t, "3333")
	worth := func() accounts.NetWorthView {
		t.Helper()
		list, err := e.accounts.List(ctx, e.user)
		if err != nil {
			t.Fatal(err)
		}
		return accounts.NewListView(list).NetWorth
	}
	spentBefore, worthBefore := e.spent(t), worth()
	if spentBefore != 950 {
		t.Fatalf("the fixture is not what this test assumes: spent %s", spentBefore)
	}

	for name, file := range map[string]string{
		"its own statement": cardHeader + "\r\n" +
			"**** 4321;**** 4321;15/01/2026;15/01/2026;CAFE DE PRUEBA;R1;Comida y Bebida;-9.5;\r\n",
		"its account's, beside the owner's own card": cardHeader + "\r\n" +
			"**** 3333;**** 3333;10/01/2026;10/01/2026;FARMACIA DE PRUEBA;R2;Salud;-4.25;\r\n" +
			"**** 5555;**** 5555;12/01/2026;12/01/2026;SUPER DE PRUEBA;R3;Supermercado;-30.00;\r\n" +
			"**** 5555;**** 4321;15/01/2026;15/01/2026;CAFE DE PRUEBA;R1;Comida y Bebida;-9.5;\r\n",
	} {
		if _, err := e.imports.Import(ctx, e.user, "papa.txt", []byte(file), nil); !errors.Is(err, imports.ErrPaidByOther) {
			t.Errorf("%s: Import = %v, want ErrPaidByOther", name, err)
		}
		rec := e.upload(t, "papa.txt", []byte(file))
		var body struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != http.StatusUnprocessableEntity || body.Error.Code != "paid_by_other" ||
			body.Error.Message != "La tarjeta que termina en 4321 la paga otra persona: sus estados no se importan." {
			t.Errorf("%s: upload = %d %s", name, rec.Code, rec.Body)
		}
	}

	// Nothing of either file was stored: no card of its own, no line, and the
	// owner's spending, debt and net worth are what they were.
	list, err := e.accounts.List(ctx, e.user)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("the owner has %d accounts, want the card and their own: %+v", len(list), list)
	}
	if got := e.account(t, own); got.Movements != 1 {
		t.Errorf("the owner's card has %d movements, want December's only", got.Movements)
	}
	if spent := e.spent(t); spent != spentBefore {
		t.Errorf("spent = %s, was %s", spent, spentBefore)
	}
	if after := worth(); after != worthBefore {
		t.Errorf("net worth = %+v, was %+v", after, worthBefore)
	}
	if got := e.account(t, card.ID); got.Pending != -950 || got.PendingCount != 1 || got.Movements != 0 {
		t.Errorf("the card = %+v", got)
	}

	// The owner's own card still imports: only the card someone else pays is
	// refused.
	e.importFile(t, "enero-3333.txt", []byte(cardHeader+"\r\n"+
		"**** 3333;**** 3333;10/01/2026;10/01/2026;FARMACIA DE PRUEBA;R2;Salud;-4.25;\r\n"))
	if spent := e.spent(t); spent != 950+425 {
		t.Errorf("spent = %s, want 13.75", spent)
	}
}
