package mcpserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/captures"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/mcpserver"
	"github.com/JuanKsPty/chenchen/api/internal/oauth"
	"github.com/JuanKsPty/chenchen/api/internal/plan"
)

// planItemOut is what save_plan_item answers.
type planItemOut struct {
	Item struct {
		ItemID    string  `json:"item_id"`
		Kind      string  `json:"kind"`
		Name      string  `json:"name"`
		Month     string  `json:"month"`
		Amount    string  `json:"amount"`
		Day       int     `json:"day"`
		From      string  `json:"from"`
		To        *string `json:"to"`
		MatchText string  `json:"match_text"`
		Category  string  `json:"category"`
	} `json:"item"`
	AlreadyPlanned bool   `json:"already_planned"`
	Note           string `json:"note"`
}

// monthPlanOut is what get_month_plan answers, as far as the tests read it.
type monthPlanOut struct {
	Month          string  `json:"month"`
	Today          string  `json:"today"`
	State          string  `json:"state"`
	Planned        bool    `json:"planned"`
	Editable       bool    `json:"editable"`
	DaysLeft       int     `json:"days_left"`
	Free           string  `json:"free"`
	Spent          string  `json:"spent"`
	Left           string  `json:"left"`
	PerDay         *string `json:"per_day"`
	Level          string  `json:"level"`
	Spending       string  `json:"spending"`
	FixedTotal     string  `json:"fixed_total"`
	SavingsTotal   string  `json:"savings_total"`
	PurchasesTotal string  `json:"purchases_total"`
	Income         struct {
		Expected  string `json:"expected"`
		Received  string `json:"received"`
		Unplanned string `json:"unplanned"`
		Items     []struct {
			ItemID string `json:"item_id"`
			Amount string `json:"amount"`
			DueOn  string `json:"due_on"`
			Status string `json:"status"`
		} `json:"items"`
	} `json:"income"`
	Fixed []struct {
		ItemID    string  `json:"item_id"`
		Name      string  `json:"name"`
		Amount    string  `json:"amount"`
		Status    string  `json:"status"`
		Paid      *string `json:"paid"`
		WentOutOn string  `json:"went_out_on"`
		MatchText string  `json:"match_text"`
		Category  string  `json:"category"`
	} `json:"fixed"`
	Savings []struct {
		ItemID string `json:"item_id"`
		Amount string `json:"amount"`
		Status string `json:"status"`
	} `json:"savings"`
	Purchases []struct {
		ItemID         string `json:"item_id"`
		Price          string `json:"price"`
		TargetMonth    string `json:"target_month"`
		Share          string `json:"share"`
		SetAsideBefore string `json:"set_aside_before"`
		Status         string `json:"status"`
	} `json:"purchases"`
	Caps []struct {
		Category string `json:"category"`
		Amount   string `json:"amount"`
		Spent    string `json:"spent"`
		Level    string `json:"level"`
	} `json:"caps"`
	Uncategorized struct {
		Spent string `json:"spent"`
		Count int    `json:"count"`
	} `json:"uncategorized"`
	Quincenas []struct {
		Free           string `json:"free"`
		Spent          string `json:"spent"`
		IncomeExpected string `json:"income_expected"`
		FixedDue       string `json:"fixed_due"`
	} `json:"quincenas"`
	Alerts []struct {
		Kind  string `json:"kind"`
		Level string `json:"level"`
	} `json:"alerts"`
	Note string `json:"note"`
}

// Claude's connection to one person never reaches another's plan: an item of
// someone else's is not there to change or remove, under its id or any
// other, and what it reads of the plan is its own owner's only.
func TestClaudeCannotTouchAnotherPersonsPlanItem(t *testing.T) {
	r := newRig(t)
	owner := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	food := r.category(t, r.owner)
	var theirs planItemOut
	mustCall(t, owner, mcpserver.ToolSavePlanItem, map[string]any{"kind": "fixed", "name": "Yappy de cada mes",
		"amount": "32.67", "day": 5, "match_text": "YAPPY A PERSONA UNO", "category_id": food.String()}, &theirs)

	stranger := r.client(t, first(r.connect(t, r.stranger, oauth.ScopeRead, oauth.ScopeWrite)))
	for _, tt := range []struct {
		tool string
		args map[string]any
	}{
		{mcpserver.ToolSavePlanItem, map[string]any{"item_id": theirs.Item.ItemID, "amount": "1.00"}},
		{mcpserver.ToolSavePlanItem, map[string]any{"item_id": theirs.Item.ItemID, "kind": "fixed", "name": "Mío",
			"amount": "1.00", "day": 1}},
		{mcpserver.ToolRemovePlanItem, map[string]any{"item_id": theirs.Item.ItemID}},
		{mcpserver.ToolRemovePlanItem, map[string]any{"item_id": theirs.Item.ItemID, "scope": "only"}},
		// Nor is the owner's category one the stranger can plan under.
		{mcpserver.ToolSavePlanItem, map[string]any{"kind": "fixed", "name": "Mío", "amount": "1.00", "day": 1,
			"category_id": food.String()}},
	} {
		failed, text := call(t, stranger, tt.tool, tt.args, nil)
		if !failed {
			t.Errorf("the stranger's %s(%v) = %s; want a refusal", tt.tool, tt.args, text)
			continue
		}
		if !strings.Contains(mcpserver.CustomerPart(text), "no está") {
			t.Errorf("the stranger's %s(%v) = %q; want it told it is not theirs", tt.tool, tt.args, text)
		}
	}
	if n := r.count(t, "plan_items WHERE user_id = $1", r.stranger); n != 0 {
		t.Errorf("the stranger has %d plan items, want none", n)
	}

	var strangers monthPlanOut
	mustCall(t, stranger, mcpserver.ToolMonthPlan, nil, &strangers)
	if strangers.Planned || len(strangers.Fixed) != 0 || strangers.FixedTotal != "0.00" {
		t.Errorf("the stranger reads %+v; want nothing planned", strangers)
	}
	var owners monthPlanOut
	mustCall(t, owner, mcpserver.ToolMonthPlan, nil, &owners)
	if len(owners.Fixed) != 1 || owners.Fixed[0].ItemID != theirs.Item.ItemID || owners.Fixed[0].Amount != "32.67" ||
		owners.Fixed[0].MatchText != "YAPPY A PERSONA UNO" {
		t.Errorf("the owner's plan = %+v; want their payment as they saved it", owners.Fixed)
	}
}

// Claude saving the same thing twice, because the answer to the first was
// lost or it asked again in other words, saves it once, under an id chenchén
// chose, and says so the second time. The salary's other paycheck, which
// chenchén suggests by the same name, has its own day and is a second item;
// and the caller never chooses an id.
func TestSavingThePlanTwiceSavesItOnce(t *testing.T) {
	r := newRig(t)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))

	var firstSave, again planItemOut
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "income", "name": "Sueldo de quincena",
		"amount": "650.00", "day": 16, "match_text": "ACH EMPRESA X"}, &firstSave)
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "income", "name": "  sueldo  DE quincena ",
		"amount": 650, "day": 16}, &again)
	if firstSave.AlreadyPlanned || firstSave.Item.Amount != "650.00" || firstSave.Item.Day != 16 ||
		firstSave.Item.MatchText != "ACH EMPRESA X" || firstSave.Item.To != nil {
		t.Errorf("first = %+v", firstSave)
	}
	if !again.AlreadyPlanned || again.Item.ItemID != firstSave.Item.ItemID || again.Item.MatchText != "ACH EMPRESA X" {
		t.Errorf("again = %+v; want the first, already planned and as it was", again)
	}
	id, err := uuid.Parse(firstSave.Item.ItemID)
	if err != nil || id.Version() != 4 {
		t.Errorf("the item's id %q is not one chenchén chose at random", firstSave.Item.ItemID)
	}
	if n := r.count(t, "plan_items"); n != 1 {
		t.Errorf("plan items = %d, want 1", n)
	}

	var second planItemOut
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "income", "name": "Sueldo de quincena",
		"amount": "650.00", "day": 30, "match_text": "ACH EMPRESA X"}, &second)
	if second.AlreadyPlanned || second.Item.ItemID == firstSave.Item.ItemID || second.Item.Day != 30 {
		t.Errorf("the second paycheck = %+v; want an item of its own", second)
	}
	var p monthPlanOut
	mustCall(t, cs, mcpserver.ToolMonthPlan, nil, &p)
	if len(p.Income.Items) != 2 || p.Income.Expected != "1300.00" {
		t.Errorf("income = %+v; want both paychecks, 1300.00", p.Income)
	}

	// The caller cannot choose the id, by name or by an item_id chenchén never
	// gave out.
	if failed, text := call(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"id": uuid.NewString(), "kind": "fixed",
		"name": "Luz", "amount": "20.00", "day": 3}, nil); !failed {
		t.Errorf("save_plan_item took an id: %s", text)
	}
	if failed, text := call(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"item_id": uuid.NewString(),
		"kind": "fixed", "name": "Luz", "amount": "20.00", "day": 3}, nil); !failed {
		t.Errorf("save_plan_item made an item under an id it was handed: %s", text)
	}
	if n := r.count(t, "plan_items"); n != 2 {
		t.Errorf("plan items = %d, want 2", n)
	}

	// Removing it twice removes it once; the second time says it is done.
	var removed struct {
		Removed bool `json:"removed"`
	}
	mustCall(t, cs, mcpserver.ToolRemovePlanItem, map[string]any{"item_id": second.Item.ItemID}, &removed)
	if !removed.Removed {
		t.Errorf("remove_plan_item = %+v", removed)
	}
	if failed, text := call(t, cs, mcpserver.ToolRemovePlanItem, map[string]any{"item_id": second.Item.ItemID},
		nil); !failed || !strings.Contains(text, "ya está hecho") {
		t.Errorf("removing it again = %v %q; want it told it is done", failed, text)
	}
	if n := r.count(t, "plan_items"); n != 1 {
		t.Errorf("plan items = %d, want 1", n)
	}

	// Planned from next month on, it is already planned for a save from this
	// month on, and is shown from the month it starts.
	next := plan.MonthOf(civil.Of(time.Now().In(civil.Panama))).AddMonths(1)
	var light, lightAgain planItemOut
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "fixed", "name": "Luz", "amount": "20.00",
		"day": 20, "month": next.String()}, &light)
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "fixed", "name": "Luz", "amount": "20.00",
		"day": 20}, &lightAgain)
	if !lightAgain.AlreadyPlanned || lightAgain.Item.ItemID != light.Item.ItemID || lightAgain.Item.From != next.String() {
		t.Errorf("Luz again from this month = %+v; want the one from %s", lightAgain, next)
	}
	var nextPlan monthPlanOut
	mustCall(t, cs, mcpserver.ToolMonthPlan, map[string]any{"month": next.String()}, &nextPlan)
	if len(nextPlan.Fixed) != 1 || nextPlan.FixedTotal != "20.00" {
		t.Errorf("next month's payments = %+v (%s); want Luz once", nextPlan.Fixed, nextPlan.FixedTotal)
	}

	// Both say a repeat changes nothing, so a client may send one again.
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list.Tools {
		if (tool.Name == mcpserver.ToolSavePlanItem || tool.Name == mcpserver.ToolRemovePlanItem) &&
			(tool.Annotations == nil || !tool.Annotations.IdempotentHint) {
			t.Errorf("%s is not marked idempotent: %+v", tool.Name, tool.Annotations)
		}
	}
}

// A change names what changes: what it leaves out stays as the item has it,
// from the month it is for, and the months before keep their plan.
func TestChangingAPlanItemKeepsWhatTheChangeLeavesOut(t *testing.T) {
	r := newRig(t)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	food := r.category(t, r.owner)
	this := plan.MonthOf(civil.Of(time.Now().In(civil.Panama)))
	next := this.AddMonths(1)

	var saved, changed planItemOut
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "fixed", "name": "Cafe de cada mes",
		"amount": "3.50", "day": 18, "match_text": "CAFE DE PRUEBA", "category_id": food.String()}, &saved)
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"item_id": saved.Item.ItemID, "amount": "3.68",
		"month": next.String()}, &changed)
	if changed.Item.ItemID != saved.Item.ItemID || changed.Item.Amount != "3.68" || changed.Item.From != next.String() ||
		changed.Item.Day != 18 || changed.Item.MatchText != "CAFE DE PRUEBA" || changed.Item.Category == "" ||
		changed.Item.Category != saved.Item.Category || changed.Item.Name != "Cafe de cada mes" {
		t.Errorf("changed = %+v; want 3.68 from %s and the rest as saved (%+v)", changed, next, saved)
	}
	for month, want := range map[plan.Month]string{this: "3.50", next: "3.68"} {
		var p monthPlanOut
		mustCall(t, cs, mcpserver.ToolMonthPlan, map[string]any{"month": month.String()}, &p)
		if len(p.Fixed) != 1 || p.Fixed[0].Amount != want || p.Fixed[0].Category != saved.Item.Category {
			t.Errorf("%s = %+v; want %s", month, p.Fixed, want)
		}
	}

	// An item's kind never changes, which is said before what the new kind
	// would make wrong, such as a saving with a text.
	if failed, text := call(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"item_id": saved.Item.ItemID,
		"kind": "saving"}, nil); !failed || !strings.Contains(text, "no cambia de tipo") || strings.Contains(text, "match_text") {
		t.Errorf("changing its kind = %v %q", failed, text)
	}
	// An empty value removes what it names.
	var cleared planItemOut
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"item_id": saved.Item.ItemID, "category_id": ""}, &cleared)
	if cleared.Item.Category != "" || cleared.Item.MatchText != "CAFE DE PRUEBA" || cleared.Item.Amount != "3.50" {
		t.Errorf("without a category = %+v", cleared)
	}
}

// A change that does not say for when leaves the months before the item
// alone: a purchase or a payment planned from next month is changed from
// next month, and this month's figures stay as they were.
func TestAChangeWithNoMonthLeavesTheMonthsBeforeTheItemAlone(t *testing.T) {
	r := newRig(t)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	this := plan.MonthOf(civil.Of(time.Now().In(civil.Panama)))
	next := this.AddMonths(1)

	var laptop, light planItemOut
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "purchase", "name": "Laptop", "amount": "600.00",
		"month": next.String(), "target_month": next.AddMonths(2).String(), "funding": "monthly"}, &laptop)
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "fixed", "name": "Luz", "amount": "20.00",
		"day": 20, "month": next.String()}, &light)
	var before monthPlanOut
	mustCall(t, cs, mcpserver.ToolMonthPlan, nil, &before)

	var renamed, changed planItemOut
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"item_id": laptop.Item.ItemID, "name": "Laptop nueva"},
		&renamed)
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"item_id": light.Item.ItemID, "amount": "22.00"}, &changed)
	if renamed.Item.Name != "Laptop nueva" || renamed.Item.From != next.String() || renamed.Item.Amount != "600.00" {
		t.Errorf("the renamed purchase = %+v; want it still from %s", renamed.Item, next)
	}
	if changed.Item.Amount != "22.00" || changed.Item.From != next.String() || changed.Item.Day != 20 {
		t.Errorf("the changed payment = %+v; want 22.00 from %s", changed.Item, next)
	}

	var after monthPlanOut
	mustCall(t, cs, mcpserver.ToolMonthPlan, nil, &after)
	if len(after.Purchases) != 0 || len(after.Fixed) != 0 || after.PurchasesTotal != before.PurchasesTotal ||
		after.FixedTotal != before.FixedTotal || after.Free != before.Free {
		t.Errorf("this month after the changes = %+v; want it as before, %+v", after, before)
	}
}

// A month taken out of an item comes back by saving the item again for that
// month, with its id, as the note on taking it out says; what it had comes
// back with it. An item left with no month is gone, and the note says to add
// it again.
func TestAMonthTakenOutOfThePlanComesBackWithItsID(t *testing.T) {
	r := newRig(t)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	this := plan.MonthOf(civil.Of(time.Now().In(civil.Panama)))

	var saved planItemOut
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "income", "name": "Sueldo", "amount": "650.00",
		"day": 15, "match_text": "ACH EMPRESA X"}, &saved)
	var removed struct {
		Removed bool   `json:"removed"`
		Note    string `json:"note"`
	}
	mustCall(t, cs, mcpserver.ToolRemovePlanItem, map[string]any{"item_id": saved.Item.ItemID, "scope": "only"}, &removed)
	if !strings.Contains(removed.Note, "month "+this.String()+" y scope only") {
		t.Errorf("the note = %q; want it to say how to undo it", removed.Note)
	}

	// Saved again without its id, it is the one planned from next month.
	var again planItemOut
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "income", "name": "Sueldo", "amount": "650.00",
		"day": 15}, &again)
	if !again.AlreadyPlanned || again.Item.ItemID != saved.Item.ItemID {
		t.Errorf("saved again without its id = %+v; want the one already planned", again)
	}

	var back planItemOut
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"item_id": saved.Item.ItemID, "month": this.String(),
		"scope": "only"}, &back)
	if back.Item.Amount != "650.00" || back.Item.Day != 15 || back.Item.MatchText != "ACH EMPRESA X" ||
		back.Item.Month != this.String() {
		t.Errorf("this month back = %+v", back.Item)
	}
	for _, m := range []plan.Month{this, this.AddMonths(1)} {
		var p monthPlanOut
		mustCall(t, cs, mcpserver.ToolMonthPlan, map[string]any{"month": m.String()}, &p)
		if len(p.Income.Items) != 1 || p.Income.Expected != "650.00" {
			t.Errorf("%s: income = %+v; want the paycheck once", m, p.Income)
		}
	}

	// Taken out of every month, it is gone, and coming back is adding it.
	mustCall(t, cs, mcpserver.ToolRemovePlanItem, map[string]any{"item_id": saved.Item.ItemID}, &removed)
	if !strings.Contains(removed.Note, "sin item_id") {
		t.Errorf("the note = %q; want it to say to add it again", removed.Note)
	}
	if failed, text := call(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"item_id": saved.Item.ItemID,
		"amount": "650.00"}, nil); !failed || strings.Contains(text, "ya está hecho") || !strings.Contains(text, "sin item_id") {
		t.Errorf("saving it with its id = %v %q; want it told to add it again", failed, text)
	}
	if n := r.count(t, "plan_items"); n != 0 {
		t.Errorf("plan items = %d, want none", n)
	}
}

// A connection that may only read reads the plan, and is not even offered
// what would change it.
func TestAReadOnlyConnectionCannotEditThePlan(t *testing.T) {
	r := newRig(t)
	this := plan.MonthOf(civil.Of(time.Now().In(civil.Panama)))
	id := uuid.New()
	r.putPlan(t, "/api/plan/items/"+id.String(), fmt.Sprintf(`{"kind": "fixed", "month": %q, "scope": "from",
		"name": "Luz", "amount": "20.00", "day": 3}`, this), http.StatusCreated)

	token, _ := r.connect(t, r.owner, oauth.ScopeRead)
	cs := r.client(t, token)
	if got := cs.InitializeResult().Instructions; !strings.Contains(got, "editar el plan del mes") {
		t.Errorf("a read-only connection is not told how to edit the plan: %q", got)
	}
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	if !slices.Contains(names, mcpserver.ToolMonthPlan) || slices.Contains(names, mcpserver.ToolSavePlanItem) ||
		slices.Contains(names, mcpserver.ToolRemovePlanItem) {
		t.Errorf("a read-only connection is offered %v", names)
	}
	for _, tt := range []struct {
		tool string
		args map[string]any
	}{
		{mcpserver.ToolSavePlanItem, map[string]any{"kind": "fixed", "name": "Agua", "amount": "9.00", "day": 8}},
		{mcpserver.ToolSavePlanItem, map[string]any{"item_id": id.String(), "amount": "1.00"}},
		{mcpserver.ToolRemovePlanItem, map[string]any{"item_id": id.String()}},
	} {
		// A tool the server does not have ends the client's session, so each
		// try has its own.
		if failed, text := call(t, r.client(t, token), tt.tool, tt.args, nil); !failed {
			t.Errorf("%s with a read-only token: %s", tt.tool, text)
		}
	}
	var p monthPlanOut
	mustCall(t, cs, mcpserver.ToolMonthPlan, nil, &p)
	if len(p.Fixed) != 1 || p.Fixed[0].ItemID != id.String() || p.Fixed[0].Amount != "20.00" {
		t.Errorf("the plan = %+v; want the payment as the owner saved it", p.Fixed)
	}
	if n := r.count(t, "plan_items"); n != 1 {
		t.Errorf("plan items = %d, want 1", n)
	}
}

// get_month_plan says what the app's plan says, figure for figure, for the
// current month with every kind of item and a cap, and for a past one; and
// what it says was spent is what spending_by_category adds up.
func TestGetMonthPlanSaysWhatThePlanSays(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	today := civil.Of(time.Now().In(civil.Panama))
	this := plan.MonthOf(today)
	card := r.card(t, r.owner, "1111")
	var food uuid.UUID
	var foodName string
	if err := r.db.QueryRow(ctx, `SELECT id, name FROM categories WHERE user_id = $1 AND kind = 'expense'
		AND parent_id IS NULL ORDER BY name LIMIT 1`, r.owner).Scan(&food, &foodName); err != nil {
		t.Fatal(err)
	}
	for _, c := range []captures.AddInput{
		{AccountID: card.ID, Amount: 350, Merchant: "CAFE DE PRUEBA"},
		{AccountID: card.ID, Amount: 1250, Merchant: "KIOSCO DE PRUEBA", CategoryID: &food},
		{AccountID: card.ID, Amount: 4000, Merchant: "FERRETERIA DE PRUEBA"},
	} {
		if _, err := r.captures.Add(ctx, r.owner, c); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{
		`{"kind": "income", "name": "Sueldo", "amount": "650.00", "day": 16, "match_text": "ACH EMPRESA X"}`,
		fmt.Sprintf(`{"kind": "fixed", "name": "Cafe de cada mes", "amount": "3.50", "day": %d,
			"match_text": "CAFE DE PRUEBA"}`, today.Day),
		`{"kind": "saving", "name": "Fondo", "amount": "50.00"}`,
		fmt.Sprintf(`{"kind": "purchase", "name": "Laptop", "amount": "600.00", "target_month": %q,
			"funding": "monthly"}`, this.AddMonths(2)),
	} {
		body = fmt.Sprintf(`{"month": %q, "scope": "from", `, this) + body[1:]
		r.putPlan(t, "/api/plan/items/"+uuid.NewString(), body, http.StatusCreated)
	}
	r.putPlan(t, "/api/plan/caps/"+food.String(), fmt.Sprintf(`{"month": %q, "scope": "from", "amount": "10.00"}`, this),
		http.StatusOK)
	r.importAsOwner(t, r.owner, "junio.csv", june)

	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead)))
	for _, month := range []string{"", "2026-06"} {
		target := "/api/plan"
		args := map[string]any{}
		if month != "" {
			target += "?month=" + month
			args["month"] = month
		}
		want := r.readPlan(t, target)
		var got monthPlanOut
		mustCall(t, cs, mcpserver.ToolMonthPlan, args, &got)

		s := want.Summary
		if got.Month != want.Month || got.Today != want.Today || got.State != want.State ||
			got.Planned != want.Planned || got.Editable != want.Editable || got.DaysLeft != s.DaysLeft ||
			got.Free != s.Free.F || got.Spent != s.Spent.F || got.Left != s.Left.F || got.Level != s.Level ||
			!sameDollars(got.PerDay, s.PerDay) || got.FixedTotal != s.Fixed.F || got.SavingsTotal != s.Savings.F ||
			got.PurchasesTotal != s.Purchases.F {
			t.Errorf("%q: the summary = %+v\nthe app's = %+v %+v", month, got, want, s)
		}
		if got.Income.Expected != want.Income.Expected.F || got.Income.Received != want.Income.Actual.F ||
			got.Income.Unplanned != want.Income.Unplanned.F || len(got.Income.Items) != len(want.Income.Items) {
			t.Errorf("%q: income = %+v, the app's %+v", month, got.Income, want.Income)
		}
		for i := range min(len(got.Income.Items), len(want.Income.Items)) {
			g, w := got.Income.Items[i], want.Income.Items[i]
			if g.ItemID != w.ID || g.Amount != w.Amount.F || g.DueOn != w.DueOn || g.Status != w.Status {
				t.Errorf("%q: paycheck %d = %+v, the app's %+v", month, i, g, w)
			}
		}
		if len(got.Fixed) != len(want.Fixed) {
			t.Errorf("%q: fixed = %+v, the app's %+v", month, got.Fixed, want.Fixed)
		}
		for i := range min(len(got.Fixed), len(want.Fixed)) {
			g, w := got.Fixed[i], want.Fixed[i]
			wentOut := ""
			if len(w.Movements) > 0 {
				wentOut = w.Movements[0].BookedOn
			}
			if g.ItemID != w.ID || g.Amount != w.Amount.F || g.Status != w.Status || !sameDollars(g.Paid, w.Paid) ||
				g.WentOutOn != wentOut {
				t.Errorf("%q: payment %d = %+v, the app's %+v", month, i, g, w)
			}
		}
		if len(got.Savings) != len(want.Savings) || len(got.Purchases) != len(want.Purchases) {
			t.Errorf("%q: savings %+v and purchases %+v, the app's %+v and %+v", month, got.Savings, got.Purchases,
				want.Savings, want.Purchases)
		}
		for i := range min(len(got.Savings), len(want.Savings)) {
			if g, w := got.Savings[i], want.Savings[i]; g.ItemID != w.ID || g.Amount != w.Amount.F || g.Status != w.Status {
				t.Errorf("%q: saving %d = %+v, the app's %+v", month, i, g, w)
			}
		}
		for i := range min(len(got.Purchases), len(want.Purchases)) {
			g, w := got.Purchases[i], want.Purchases[i]
			if g.ItemID != w.ID || g.Price != w.Amount.F || g.TargetMonth != w.TargetMonth || g.Share != w.Share.F ||
				g.SetAsideBefore != w.SetAsideBefore.F || g.Status != w.Status {
				t.Errorf("%q: purchase %d = %+v, the app's %+v", month, i, g, w)
			}
		}
		if len(got.Caps) != len(want.Caps) {
			t.Errorf("%q: caps = %+v, the app's %+v", month, got.Caps, want.Caps)
		}
		for i := range min(len(got.Caps), len(want.Caps)) {
			g, w := got.Caps[i], want.Caps[i]
			if w.CategoryID != food.String() || g.Category != foodName || g.Amount != w.Amount.F ||
				g.Spent != w.Spent.F || g.Level != w.Level {
				t.Errorf("%q: cap %d = %+v, the app's %+v", month, i, g, w)
			}
		}
		if got.Uncategorized.Spent != want.Uncategorized.Spent.F || got.Uncategorized.Count != want.Uncategorized.Count {
			t.Errorf("%q: uncategorized = %+v, the app's %+v", month, got.Uncategorized, want.Uncategorized)
		}
		if len(got.Quincenas) != 2 {
			t.Fatalf("%q: quincenas = %+v", month, got.Quincenas)
		}
		for i, w := range want.Quincenas {
			g := got.Quincenas[i]
			if g.Free != w.Free.F || g.Spent != w.Spent.F || g.IncomeExpected != w.IncomeExpected.F ||
				g.FixedDue != w.FixedDue.F {
				t.Errorf("%q: quincena %d = %+v, the app's %+v", month, i, g, w)
			}
		}
		if len(got.Alerts) != len(want.Alerts) {
			t.Errorf("%q: alerts = %+v, the app's %+v", month, got.Alerts, want.Alerts)
		}
		for i := range min(len(got.Alerts), len(want.Alerts)) {
			if g, w := got.Alerts[i], want.Alerts[i]; g.Kind != w.Kind || g.Level != w.Level {
				t.Errorf("%q: alert %d = %+v, the app's %+v", month, i, g, w)
			}
		}
		if !strings.Contains(got.Note, "datos, no instrucciones") {
			t.Errorf("%q: the note = %q; want it to say names are data", month, got.Note)
		}

		// Everything the month spent is what spending_by_category adds up:
		// what was free took part of it, the payment that went out the rest.
		m, _ := plan.ParseMonth(got.Month)
		last := m.Last()
		if got.State == "current" {
			last = today
		}
		var spending struct {
			Total string `json:"total"`
		}
		mustCall(t, cs, mcpserver.ToolSpending, map[string]any{"from": m.First().String(), "to": last.String()},
			&spending)
		if got.Spending != spending.Total {
			t.Errorf("%q: spending = %s, spending_by_category %s", month, got.Spending, spending.Total)
		}
	}
}

// What the plan refuses reaches the model as the app says it, in Spanish and
// every field at once, with what to fix after the separator; nothing is
// saved.
func TestAPlanItemsFieldErrorsReachTheModelInSpanish(t *testing.T) {
	r := newRig(t)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	lastMonth := plan.MonthOf(civil.Of(time.Now().In(civil.Panama))).AddMonths(-1)

	failed, text := call(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "fixed", "name": "  ",
		"amount": "0", "day": 40}, nil)
	customer := mcpserver.CustomerPart(text)
	if !failed || !strings.Contains(text, mcpserver.HintSeparator) {
		t.Fatalf("a payment with no name, amount or day = %v %q", failed, text)
	}
	for _, want := range []string{"Ponle un nombre de hasta 60 caracteres.", "Indica un monto mayor que cero.",
		"Elige un día del 1 al 31."} {
		if !strings.Contains(customer, want) {
			t.Errorf("%q does not say %q", customer, want)
		}
	}
	if hint := strings.TrimPrefix(text, customer); !strings.Contains(hint, "amount, day, name") {
		t.Errorf("the hint %q does not name the arguments to fix", hint)
	}
	if strings.Contains(customer, "amount") {
		t.Errorf("the owner's part names an argument: %q", customer)
	}

	for _, tt := range []struct {
		args map[string]any
		want []string
	}{
		{map[string]any{"kind": "income", "name": "Sueldo", "amount": "doce", "day": 15},
			[]string{"El monto no tiene un formato válido. Usa por ejemplo «12.50»."}},
		{map[string]any{"kind": "income", "name": "Sueldo", "amount": "1.234", "day": 15},
			[]string{"El monto admite como máximo dos decimales."}},
		{map[string]any{"kind": "pago", "name": "Sueldo", "amount": "650", "day": 15},
			[]string{"Elige si es un ingreso, un pago fijo, un ahorro o una compra."}},
		{map[string]any{"kind": "fixed", "name": "Luz", "amount": "20", "day": 3, "target": "100"},
			[]string{"Solo un ahorro tiene una meta."}},
		{map[string]any{"kind": "saving", "name": "Fondo", "amount": "50", "target": "cero"},
			[]string{"El monto no tiene un formato válido. Usa por ejemplo «12.50»."}},
		{map[string]any{"kind": "purchase", "name": "Laptop", "amount": "600"},
			[]string{"Elige para cuándo, de aquí a 24 meses como mucho.", "Elige cómo la apartas"}},
		{map[string]any{"kind": "income", "name": "Sueldo", "amount": "650", "day": 15, "month": lastMonth.String()},
			[]string{"Ese mes ya pasó; su plan queda como estaba."}},
	} {
		failed, text := call(t, cs, mcpserver.ToolSavePlanItem, tt.args, nil)
		for _, want := range tt.want {
			if !failed || !strings.Contains(mcpserver.CustomerPart(text), want) {
				t.Errorf("save_plan_item(%v) = %v %q; want it to say %q", tt.args, failed, text, want)
			}
		}
	}
	if n := r.count(t, "plan_items"); n != 0 {
		t.Errorf("plan items = %d, want none", n)
	}
}

// --- helpers ----------------------------------------------------------------------

// putPlan sends a plan write to the API as the owner, and wants status back.
func (r *rig) putPlan(t *testing.T, path, body string, status int) {
	t.Helper()
	res, raw := r.do(t, http.MethodPut, path, r.session(t, r.owner), "application/json", strings.NewReader(body))
	if res.StatusCode != status {
		t.Fatalf("PUT %s %s = %d %s; want %d", path, body, res.StatusCode, raw, status)
	}
}

// dollarsView is an amount as the API writes it; F is its formatted text, the
// same text the tools give.
type dollarsView struct {
	F string `json:"formatted"`
}

// planView is GET /api/plan, as far as the tests compare it.
type planView struct {
	Month    string `json:"month"`
	Today    string `json:"today"`
	State    string `json:"state"`
	Planned  bool   `json:"planned"`
	Editable bool   `json:"editable"`
	Summary  struct {
		Fixed     dollarsView  `json:"fixed"`
		Savings   dollarsView  `json:"savings"`
		Purchases dollarsView  `json:"purchases"`
		Free      dollarsView  `json:"free"`
		Spent     dollarsView  `json:"spent"`
		Left      dollarsView  `json:"left"`
		DaysLeft  int          `json:"days_left"`
		PerDay    *dollarsView `json:"per_day"`
		Level     string       `json:"level"`
	} `json:"summary"`
	Income struct {
		Expected  dollarsView `json:"expected"`
		Actual    dollarsView `json:"actual"`
		Unplanned dollarsView `json:"unplanned"`
		Items     []struct {
			ID     string      `json:"id"`
			Amount dollarsView `json:"amount"`
			DueOn  string      `json:"due_on"`
			Status string      `json:"status"`
		} `json:"items"`
	} `json:"income"`
	Fixed []struct {
		ID        string       `json:"id"`
		Amount    dollarsView  `json:"amount"`
		Status    string       `json:"status"`
		Paid      *dollarsView `json:"paid"`
		Movements []struct {
			BookedOn string `json:"booked_on"`
		} `json:"movements"`
	} `json:"fixed"`
	Savings []struct {
		ID     string      `json:"id"`
		Amount dollarsView `json:"amount"`
		Status string      `json:"status"`
	} `json:"savings"`
	Purchases []struct {
		ID             string      `json:"id"`
		Amount         dollarsView `json:"amount"`
		TargetMonth    string      `json:"target_month"`
		Share          dollarsView `json:"share"`
		SetAsideBefore dollarsView `json:"set_aside_before"`
		Status         string      `json:"status"`
	} `json:"purchases"`
	Caps []struct {
		CategoryID string      `json:"category_id"`
		Amount     dollarsView `json:"amount"`
		Spent      dollarsView `json:"spent"`
		Level      string      `json:"level"`
	} `json:"caps"`
	Uncategorized struct {
		Spent dollarsView `json:"spent"`
		Count int         `json:"count"`
	} `json:"uncategorized"`
	Quincenas []struct {
		Free           dollarsView `json:"free"`
		Spent          dollarsView `json:"spent"`
		IncomeExpected dollarsView `json:"income_expected"`
		FixedDue       dollarsView `json:"fixed_due"`
	} `json:"quincenas"`
	Alerts []struct {
		Kind  string `json:"kind"`
		Level string `json:"level"`
	} `json:"alerts"`
}

// readPlan reads the owner's plan from the API, as the app does.
func (r *rig) readPlan(t *testing.T, target string) planView {
	t.Helper()
	res, raw := r.do(t, http.MethodGet, target, r.session(t, r.owner), "", nil)
	var v planView
	if res.StatusCode != http.StatusOK || json.Unmarshal(raw, &v) != nil {
		t.Fatalf("GET %s = %d %s", target, res.StatusCode, raw)
	}
	return v
}

// sameDollars reports whether a tool's amount and the API's say the same,
// none included.
func sameDollars(tool *string, api *dollarsView) bool {
	if tool == nil || api == nil {
		return tool == nil && api == nil
	}
	return *tool == api.F
}
