package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/auth"
	"github.com/JuanKsPty/chenchen/api/internal/captures"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/mcpserver"
	"github.com/JuanKsPty/chenchen/api/internal/oauth"
	"github.com/JuanKsPty/chenchen/api/internal/store"
)

var (
	readTools = []string{
		mcpserver.ToolListAccounts, mcpserver.ToolListMovements, mcpserver.ToolListCategories, mcpserver.ToolSpending,
		mcpserver.ToolCashFlow, mcpserver.ToolExplainDrift, mcpserver.ToolTransferSuggest, mcpserver.ToolPortfolio,
		mcpserver.ToolListRules, mcpserver.ToolListPending, mcpserver.ToolSyncPlan, mcpserver.ToolImportStatus,
		mcpserver.ToolMonthPlan,
	}
	writeTools = []string{
		mcpserver.ToolNotePurchase, mcpserver.ToolSetCategory, mcpserver.ToolSetNote, mcpserver.ToolPrepareImport,
		mcpserver.ToolProposeRule, mcpserver.ToolProposeTransfer, mcpserver.ToolProposeCheckpoint,
		mcpserver.ToolSavePlanItem, mcpserver.ToolRemovePlanItem,
	}
)

// Without a token the endpoint says where to sign in, and asks for both
// scopes: a client asks for what the refusal names, and the owner switches
// write off on the consent page if they want.
func TestTheEndpointAsksForAToken(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	key, err := r.captures.CreateKey(ctx, r.owner, r.card(t, r.owner, "1111").ID)
	if err != nil {
		t.Fatal(err)
	}
	_, refresh, err := auth.NewOAuthRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
		`"capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`
	for name, authorization := range map[string]string{
		"no token":         "",
		"a web session":    r.session(t, r.owner),
		"a shortcut key":   "Bearer " + key.Key,
		"a refresh token":  "Bearer " + refresh,
		"a made-up access": "Bearer " + auth.OAuthAccessTokenPrefix + strings.Repeat("a", 43),
	} {
		res, body := r.do(t, http.MethodPost, "/api/mcp", authorization, "application/json", strings.NewReader(initialize))
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: %d %s, want 401", name, res.StatusCode, body)
			continue
		}
		want := `Bearer resource_metadata="` + r.srv.URL + `/.well-known/oauth-protected-resource/api/mcp", scope="read write"`
		if got := res.Header.Values("WWW-Authenticate"); len(got) != 1 || got[0] != want {
			t.Errorf("%s: WWW-Authenticate = %q, want %q", name, got, want)
		}
	}

	// The metadata it points at is there.
	res, body := r.do(t, http.MethodGet, "/.well-known/oauth-protected-resource/api/mcp", "", "", nil)
	var prm map[string]any
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &prm) != nil || prm["resource"] != r.srv.URL+"/api/mcp" {
		t.Errorf("protected resource metadata = %d %s", res.StatusCode, body)
	}
	// And a token of Claude's opens nothing a session opens.
	token, _ := r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)
	if res, _ := r.do(t, http.MethodGet, "/api/accounts", "Bearer "+token, "", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("Claude's token on /api/accounts = %d, want 401", res.StatusCode)
	}
	if res, _ := r.do(t, http.MethodGet, "/api/imports/uploads", "Bearer "+token, "", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("Claude's token on the owner's uploads = %d, want 401", res.StatusCode)
	}
}

// What a connection may do is what its token carries: the tools that write
// are not even listed without write, so they cannot be called.
func TestToolsFollowTheTokensScopes(t *testing.T) {
	r := newRig(t)
	readOnly, _ := r.connect(t, r.owner, oauth.ScopeRead)
	readWrite, _ := r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)

	for _, tt := range []struct {
		name  string
		token string
		want  []string
	}{
		{"read", readOnly, readTools},
		{"read and write", readWrite, append(slices.Clone(readTools), writeTools...)},
	} {
		cs := r.client(t, tt.token)
		// Claude is told only of what its connection can do.
		got := cs.InitializeResult().Instructions
		writes := slices.Contains(tt.want, mcpserver.ToolPrepareImport)
		if got == "" || strings.Contains(got, "prepare_import") != writes || strings.Contains(got, "solo puede leer") == writes {
			t.Errorf("%s: instructions = %q", tt.name, got)
		}
		list, err := cs.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tool := range list.Tools {
			names = append(names, tool.Name)
			a := tool.Annotations
			switch {
			case a == nil || a.Title == "":
				t.Errorf("%s has no title", tool.Name)
			case slices.Contains(readTools, tool.Name) && !a.ReadOnlyHint:
				t.Errorf("%s reads but is not marked read-only", tool.Name)
			case slices.Contains(writeTools, tool.Name) && (a.ReadOnlyHint || a.DestructiveHint == nil || !*a.DestructiveHint):
				t.Errorf("%s writes but is not marked destructive: %+v", tool.Name, a)
			}
		}
		slices.Sort(names)
		want := slices.Sorted(slices.Values(tt.want))
		if !slices.Equal(names, want) {
			t.Errorf("%s: tools = %v\nwant %v", tt.name, names, want)
		}
	}

	cs := r.client(t, readOnly)
	if failed, text := call(t, cs, mcpserver.ToolNotePurchase, map[string]any{"account_id": uuid.NewString(),
		"amount": "1.00", "merchant": "x"}, nil); !failed {
		t.Errorf("note_purchase with a read-only token: %s", text)
	}
	if n := r.count(t, "captures"); n != 0 {
		t.Errorf("captures = %d", n)
	}
}

// Each connection acts as its owner and nobody else: whatever another
// person's id it is handed, it finds nothing.
func TestAConnectionSeesOnlyItsOwner(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.importAsOwner(t, r.owner, "junio.csv", june)
	ownerAccount := r.accountOf(t, r.owner, product)
	theirs, err := r.captures.Add(ctx, r.owner, captures.AddInput{AccountID: ownerAccount, Amount: 1250, Merchant: "Kiosco"})
	if err != nil {
		t.Fatal(err)
	}
	ownerCS := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	upload := prepare(t, ownerCS, "julio.csv", july)

	strangerCard := r.card(t, r.stranger, "2222")
	cs := r.client(t, first(r.connect(t, r.stranger, oauth.ScopeRead, oauth.ScopeWrite)))

	var accountsOut struct {
		Accounts []struct {
			AccountID string `json:"account_id"`
		} `json:"accounts"`
	}
	mustCall(t, cs, mcpserver.ToolListAccounts, nil, &accountsOut)
	if len(accountsOut.Accounts) != 1 || accountsOut.Accounts[0].AccountID != strangerCard.ID.String() {
		t.Errorf("list_accounts = %+v; want only the stranger's card", accountsOut)
	}
	var pending struct {
		Purchases []any `json:"purchases"`
	}
	mustCall(t, cs, mcpserver.ToolListPending, nil, &pending)
	if len(pending.Purchases) != 0 {
		t.Errorf("list_pending_purchases = %+v", pending)
	}
	movement := r.lineOf(t, r.owner)
	for _, tt := range []struct {
		tool string
		args map[string]any
	}{
		{mcpserver.ToolListMovements, map[string]any{"account_id": ownerAccount.String()}},
		{mcpserver.ToolListPending, map[string]any{"account_id": ownerAccount.String()}},
		{mcpserver.ToolExplainDrift, map[string]any{"account_id": ownerAccount.String()}},
		{mcpserver.ToolImportStatus, map[string]any{"upload_id": upload.UploadID}},
		{mcpserver.ToolNotePurchase, map[string]any{"account_id": ownerAccount.String(), "amount": "5.00", "merchant": "x"}},
		{mcpserver.ToolSetNote, map[string]any{"movement_id": movement.String(), "note": "mía"}},
		{mcpserver.ToolSetNote, map[string]any{"movement_id": theirs.ID.String(), "note": "mía"}},
		{mcpserver.ToolSetCategory, map[string]any{"movement_id": movement.String(), "category_id": nil}},
	} {
		var out map[string]any
		failed, text := call(t, cs, tt.tool, tt.args, &out)
		if tt.tool == mcpserver.ToolListMovements && !failed {
			if list, _ := out["movements"].([]any); len(list) == 0 {
				continue
			}
		}
		if !failed {
			t.Errorf("the stranger's %s(%v) = %s; want a refusal", tt.tool, tt.args, text)
		}
	}
	if e, err := r.db.Q().EntryByID(ctx, r.owner, movement); err != nil || e.Note != "" {
		t.Errorf("the owner's movement = %+v, %v", e, err)
	}
	if n := r.count(t, "captures WHERE user_id = $1", r.owner); n != 1 {
		t.Errorf("the owner's purchases = %d, want 1", n)
	}
}

// Each connection gets its own budget a minute, whoever shares its address.
func TestEachConnectionHasItsOwnLimit(t *testing.T) {
	r := newRigWith(t, 3)
	busy, _ := r.connect(t, r.owner, oauth.ScopeRead)
	other, _ := r.connect(t, r.owner, oauth.ScopeRead)
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	post := func(token string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, r.srv.URL+"/api/mcp", strings.NewReader(list))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res
	}
	for i := range 3 {
		if res := post(busy); res.StatusCode != http.StatusOK {
			t.Fatalf("call %d = %d", i+1, res.StatusCode)
		}
	}
	res := post(busy)
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Errorf("the fourth call = %d, Retry-After %q; want 429 with a wait", res.StatusCode, res.Header.Get("Retry-After"))
	}
	if res := post(other); res.StatusCode != http.StatusOK {
		t.Errorf("another connection's call = %d, want 200", res.StatusCode)
	}
}

// The assistant noting a purchase it read twice notes it once, under an id
// chenchén chose, and says so the second time.
func TestNotingAPurchaseTwiceNotesItOnce(t *testing.T) {
	r := newRig(t)
	card := r.card(t, r.owner, "1111")
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	day := civil.Of(time.Now().In(civil.Panama)).AddDays(-1).String()

	type noted struct {
		Purchase struct {
			ID     string `json:"id"`
			Amount string `json:"amount"`
			State  string `json:"state"`
		} `json:"purchase"`
		AlreadyNoted bool `json:"already_noted"`
	}
	var firstNote, again noted
	mustCall(t, cs, mcpserver.ToolNotePurchase, map[string]any{"account_id": card.ID.String(), "amount": 12.5,
		"merchant": "SUPER DE PRUEBA", "day": day}, &firstNote)
	mustCall(t, cs, mcpserver.ToolNotePurchase, map[string]any{"account_id": card.ID.String(), "amount": "12.50",
		"merchant": "super de prueba", "day": day}, &again)
	if firstNote.AlreadyNoted || firstNote.Purchase.Amount != "12.50" || firstNote.Purchase.State != store.CapturePending {
		t.Errorf("first = %+v", firstNote)
	}
	if !again.AlreadyNoted || again.Purchase.ID != firstNote.Purchase.ID {
		t.Errorf("again = %+v; want the first, already noted", again)
	}
	id, err := uuid.Parse(firstNote.Purchase.ID)
	if err != nil || id.Version() != 4 {
		t.Errorf("the purchase's id %q is not one chenchén chose at random", firstNote.Purchase.ID)
	}
	if n := r.count(t, "captures"); n != 1 {
		t.Errorf("captures = %d, want 1", n)
	}
	if c, err := r.db.Q().CaptureByID(context.Background(), r.owner, id); err != nil || c.Source != store.ChannelAgent {
		t.Errorf("noted as %+v, %v; want the assistant's channel", c, err)
	}

	// The caller cannot choose the id.
	if failed, text := call(t, cs, mcpserver.ToolNotePurchase, map[string]any{"id": uuid.NewString(),
		"account_id": card.ID.String(), "amount": "3.00", "merchant": "OTRO"}, nil); !failed {
		t.Errorf("note_purchase took an id: %s", text)
	}
	for _, amount := range []any{"0", -3, "12.505", "doce"} {
		if failed, _ := call(t, cs, mcpserver.ToolNotePurchase, map[string]any{"account_id": card.ID.String(),
			"amount": amount, "merchant": "OTRO"}, nil); !failed {
			t.Errorf("note_purchase took the amount %v", amount)
		}
	}
	if n := r.count(t, "captures"); n != 1 {
		t.Errorf("captures = %d, want 1", n)
	}
}

// Two purchases alike in amount, merchant and day are two when the bank's
// references for them differ, and each is found again by its own. A category
// the assistant gives one is the assistant's choice, not the owner's.
func TestTwoLikePurchasesWithTheirOwnReferencesAreTwo(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	card := r.card(t, r.owner, "1111")
	food := r.category(t, r.owner)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))
	day := civil.Of(time.Now().In(civil.Panama)).AddDays(-1).String()

	type noted struct {
		Purchase struct {
			ID string `json:"id"`
		} `json:"purchase"`
		AlreadyNoted bool `json:"already_noted"`
	}
	note := func(reference string) noted {
		t.Helper()
		args := map[string]any{"account_id": card.ID.String(), "amount": "3.50", "merchant": "CAFE DE PRUEBA",
			"day": day, "category_id": food.String()}
		if reference != "" {
			args["reference"] = reference
		}
		var out noted
		mustCall(t, cs, mcpserver.ToolNotePurchase, args, &out)
		return out
	}
	one, two := note("000123"), note("000124")
	if one.AlreadyNoted || two.AlreadyNoted || one.Purchase.ID == two.Purchase.ID {
		t.Fatalf("two references noted %+v and %+v; want two purchases", one, two)
	}
	for _, tt := range []struct {
		reference string
		want      string
	}{{"000123", one.Purchase.ID}, {"000124", two.Purchase.ID}} {
		if again := note(tt.reference); !again.AlreadyNoted || again.Purchase.ID != tt.want {
			t.Errorf("%s again = %+v; want %s, already noted", tt.reference, again, tt.want)
		}
	}
	// Without a reference it is one of them told again, never a third.
	if again := note(""); !again.AlreadyNoted {
		t.Errorf("no reference = %+v; want one already noted", again)
	}
	if n := r.count(t, "captures"); n != 2 {
		t.Errorf("captures = %d, want 2", n)
	}
	id, _ := uuid.Parse(one.Purchase.ID)
	if c, err := r.db.Q().CaptureByID(ctx, r.owner, id); err != nil || c.CategorySource == nil ||
		*c.CategorySource != "assistant" {
		t.Errorf("the purchase = %+v, %v; want its category the assistant's", c, err)
	}
	if failed, text := call(t, cs, mcpserver.ToolNotePurchase, map[string]any{"account_id": card.ID.String(),
		"amount": "1.00", "merchant": "OTRO", "reference": strings.Repeat("9", 65)}, nil); !failed {
		t.Errorf("note_purchase took a 65-character reference: %s", text)
	}
}

// set_category and set_note reach a printed line and a pending purchase
// alike, and are said to be the assistant's choice.
func TestFilingAndAnnotatingAMovement(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.importAsOwner(t, r.owner, "junio.csv", june)
	account := r.accountOf(t, r.owner, product)
	line := r.lineOf(t, r.owner)
	purchase, err := r.captures.Add(ctx, r.owner, captures.AddInput{AccountID: account, Amount: 800, Merchant: "Kiosco"})
	if err != nil {
		t.Fatal(err)
	}
	food := r.category(t, r.owner)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))

	for _, id := range []uuid.UUID{line, purchase.ID} {
		var out struct {
			Movement struct {
				ID         string `json:"id"`
				Category   string `json:"category"`
				CategoryBy string `json:"category_by"`
				Note       string `json:"note"`
				Pending    bool   `json:"pending"`
			} `json:"movement"`
		}
		mustCall(t, cs, mcpserver.ToolSetCategory, map[string]any{"movement_id": id.String(),
			"category_id": food.String()}, &out)
		if out.Movement.ID != id.String() || out.Movement.Category == "" || out.Movement.CategoryBy != "assistant" {
			t.Errorf("set_category(%s) = %+v", id, out)
		}
		mustCall(t, cs, mcpserver.ToolSetNote, map[string]any{"movement_id": id.String(), "note": "para la casa"}, &out)
		if out.Movement.Note != "para la casa" || out.Movement.Pending != (id == purchase.ID) {
			t.Errorf("set_note(%s) = %+v", id, out)
		}
		out.Movement.Category = ""
		mustCall(t, cs, mcpserver.ToolSetCategory, map[string]any{"movement_id": id.String(), "category_id": nil}, &out)
		if out.Movement.Category != "" {
			t.Errorf("set_category(%s, null) = %+v", id, out)
		}
	}
	if c, err := r.db.Q().CaptureByID(ctx, r.owner, purchase.ID); err != nil || c.Note != "para la casa" {
		t.Errorf("the purchase = %+v, %v", c, err)
	}
	if e, err := r.db.Q().EntryByID(ctx, r.owner, line); err != nil || e.Note != "para la casa" || e.CategoryID != nil {
		t.Errorf("the line = %+v, %v", e, err)
	}
}

// What Gastos shows as Sin categoría Claude can list, each movement with the
// text a rule for it would take.
func TestClaudeListsWhatHasNoCategory(t *testing.T) {
	r := newRig(t)
	r.importAsOwner(t, r.owner, "junio.csv", june)
	r.importAsOwner(t, r.owner, "julio.csv", july)
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead)))
	var out struct {
		Movements []struct {
			Description string `json:"description"`
			Kind        string `json:"kind"`
			Category    string `json:"category"`
			RuleHint    string `json:"rule_hint"`
		} `json:"movements"`
	}
	mustCall(t, cs, mcpserver.ToolListMovements, map[string]any{"uncategorized": true, "limit": 50}, &out)
	found := false
	for _, m := range out.Movements {
		if m.Category != "" || !slices.Contains([]string{"expense", "refund", "fee", "interest"}, m.Kind) {
			t.Errorf("listed as without a category: %+v", m)
		}
		if m.Description == "SUPERMERCADO DE PRUEBA" {
			found = m.RuleHint == "SUPERMERCADO DE PRUEBA"
		}
	}
	if !found {
		t.Errorf("the supermarket is not listed with its rule text: %+v", out.Movements)
	}
}

// The app shows the same: each movement with its rule text, and what a rule
// with a text would reach before the owner makes it.
func TestTheAppSuggestsARuleAndSaysWhatItWouldReach(t *testing.T) {
	r := newRig(t)
	r.importAsOwner(t, r.owner, "julio.csv", july)
	res, raw := r.do(t, http.MethodGet, "/api/entries", r.session(t, r.owner), "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"rule_hint":"SUPERMERCADO DE PRUEBA"`) {
		t.Errorf("GET /api/entries = %d %s, want the supermarket's rule text", res.StatusCode, raw)
	}
	res, raw = r.do(t, http.MethodGet, "/api/category-rules/preview?text=supermercado", r.session(t, r.owner), "", nil)
	var reach struct {
		Matches       int `json:"matches"`
		Uncategorized int `json:"uncategorized"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(raw, &reach) != nil || reach.Matches != 1 || reach.Uncategorized != 1 {
		t.Errorf("GET /api/category-rules/preview = %d %s, want one match without a category", res.StatusCode, raw)
	}
	if res, _ := r.do(t, http.MethodGet, "/api/category-rules/preview?text=", r.session(t, r.owner), "", nil); res.StatusCode == http.StatusOK {
		t.Error("a preview without text was answered")
	}
}

// What Claude writes never anchors a balance or rewrites movements in bulk:
// the stated balance and the rule stay proposals for the owner, and nothing
// else it can call touches either. Keeping the plan writes no line and no
// noted purchase either: the plan is only what the owner expects.
func TestNoWriteToolTouchesCheckpointsOrRules(t *testing.T) {
	r := newRig(t)
	r.importAsOwner(t, r.owner, "junio.csv", june)
	account := r.accountOf(t, r.owner, product)
	line := r.lineOf(t, r.owner)
	food := r.category(t, r.owner)
	checkpoints, rules := r.count(t, "balance_checkpoints"), r.count(t, "category_rules")
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)))

	entries, captures := r.count(t, "entries"), r.count(t, "captures")
	var saved planItemOut
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"kind": "fixed", "name": "Cafe de cada mes",
		"amount": "3.50", "day": 4, "match_text": "CAFE DE PRUEBA", "category_id": food.String()}, &saved)
	mustCall(t, cs, mcpserver.ToolSavePlanItem, map[string]any{"item_id": saved.Item.ItemID, "amount": "3.75"}, nil)
	mustCall(t, cs, mcpserver.ToolRemovePlanItem, map[string]any{"item_id": saved.Item.ItemID}, nil)
	if e, c := r.count(t, "entries"), r.count(t, "captures"); e != entries || c != captures {
		t.Errorf("keeping the plan wrote lines %d → %d, purchases %d → %d", entries, e, captures, c)
	}

	day := civil.Of(time.Now().In(civil.Panama)).String()
	for _, tt := range []struct {
		tool string
		args map[string]any
	}{
		{mcpserver.ToolNotePurchase, map[string]any{"account_id": account.String(), "amount": "4.00", "merchant": "PAN"}},
		{mcpserver.ToolSetCategory, map[string]any{"movement_id": line.String(), "category_id": food.String()}},
		{mcpserver.ToolSetNote, map[string]any{"movement_id": line.String(), "note": "x"}},
		{mcpserver.ToolPrepareImport, map[string]any{"filename": "julio.csv", "sha256": digest(july), "size": len(july)}},
		{mcpserver.ToolProposeRule, map[string]any{"match_text": "DEPOSITO", "category_id": food.String(),
			"apply_to_existing": true}},
		{mcpserver.ToolProposeCheckpoint, map[string]any{"account_id": account.String(), "as_of": day,
			"balance": "1.00"}},
	} {
		mustCall(t, cs, tt.tool, tt.args, nil)
	}
	if n := r.count(t, "balance_checkpoints"); n != checkpoints {
		t.Errorf("checkpoints %d → %d", checkpoints, n)
	}
	if n := r.count(t, "category_rules"); n != rules {
		t.Errorf("rules %d → %d", rules, n)
	}
	if n := r.count(t, "ai_proposals WHERE status = 'pending'"); n != 2 {
		t.Errorf("pending proposals = %d, want the rule and the balance", n)
	}
}

// Every call is recorded — the tool, the connection, the owner, how long and
// whether it failed — and none of what it carried.
func TestEachCallIsAuditedWithoutItsArguments(t *testing.T) {
	r := newRig(t)
	card := r.card(t, r.owner, "1111")
	token, grant := r.connect(t, r.owner, oauth.ScopeRead, oauth.ScopeWrite)
	cs := r.client(t, token)
	mustCall(t, cs, mcpserver.ToolNotePurchase, map[string]any{"account_id": card.ID.String(), "amount": "7.25",
		"merchant": "FARMACIA SECRETA", "note": "nota muy privada"}, nil)
	p := prepare(t, cs, "julio.csv", july)
	call(t, cs, mcpserver.ToolSetNote, map[string]any{"movement_id": uuid.NewString(), "note": "x"}, nil)

	logs := r.logs.String()
	for _, line := range []string{
		"tool=" + mcpserver.ToolNotePurchase + " grant_id=" + grant.String() + " user_id=" + r.owner.String(),
		"tool=" + mcpserver.ToolPrepareImport + " grant_id=" + grant.String(),
	} {
		if !strings.Contains(logs, line) {
			t.Errorf("no audit line with %q in:\n%s", line, logs)
		}
	}
	if !strings.Contains(logs, "tool="+mcpserver.ToolSetNote) || !strings.Contains(logs, "error=true") {
		t.Errorf("the failed call is not recorded as one:\n%s", logs)
	}
	token = p.UploadURL[strings.LastIndex(p.UploadURL, "/")+1:]
	for _, secret := range []string{"FARMACIA SECRETA", "nota muy privada", "7.25", token, digest(july)} {
		if strings.Contains(logs, secret) {
			t.Errorf("the log holds %q", secret)
		}
	}
}

// sync_plan says, for each account a statement reaches, from when to
// download, what is pending, and whether a card's closed statement is due.
func TestSyncPlan(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.importAsOwner(t, r.owner, "junio.csv", june)
	bank := r.accountOf(t, r.owner, product)
	card := r.card(t, r.owner, "1111")
	if _, err := r.accounts.AddHandCard(ctx, r.owner, uuid.Nil, "La de otra persona", "4444"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.db.Q().EnsureAccount(ctx, store.Account{ID: uuid.New(), UserID: r.owner, Class: "asset",
		Type: "brokerage", Institution: "ibkr", ExternalNumber: "U0000001", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.captures.Add(ctx, r.owner, captures.AddInput{AccountID: card.ID, Amount: 3901, Merchant: "Kiosco"}); err != nil {
		t.Fatal(err)
	}
	cs := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead)))

	var plan struct {
		Today    string `json:"today"`
		Accounts []struct {
			AccountID     string `json:"account_id"`
			Institution   string `json:"institution"`
			Type          string `json:"type"`
			Last4         string `json:"last4"`
			Name          string `json:"name"`
			LastPeriodEnd string `json:"last_period_end"`
			From          string `json:"from"`
			LastImportAt  string `json:"last_import_at"`
			Pending       struct {
				Count  int    `json:"count"`
				Amount string `json:"amount"`
			} `json:"pending"`
			ClosedDue *bool  `json:"closed_statement_due"`
			LastCut   string `json:"last_cut"`
		} `json:"accounts"`
	}
	mustCall(t, cs, mcpserver.ToolSyncPlan, nil, &plan)
	if len(plan.Accounts) != 2 {
		t.Fatalf("sync_plan = %+v; want the bank account and the card", plan)
	}
	today, _ := civil.Parse(plan.Today)
	b, c := plan.Accounts[0], plan.Accounts[1]
	if b.AccountID != bank.String() || b.LastPeriodEnd != "2026-06-30" || b.From != "2026-06-23" ||
		b.LastImportAt == "" || b.Last4 != "0001" || b.Name != "Cuenta corriente" || b.ClosedDue != nil {
		t.Errorf("the bank account = %+v", b)
	}
	if c.AccountID != card.ID.String() || c.LastPeriodEnd != "" || c.From != today.AddDays(-60).String() ||
		c.Pending.Count != 1 || c.Pending.Amount != "39.01" || c.ClosedDue == nil || !*c.ClosedDue || c.LastCut != "" ||
		c.Name != "Tarjeta de crédito" || c.Last4 != "1111" {
		t.Errorf("the card = %+v", c)
	}
}

// --- helpers ----------------------------------------------------------------------

func first[A, B any](a A, _ B) A { return a }

// card gives user a card ending in last4, as importing its statement would.
func (r *rig) card(t *testing.T, user uuid.UUID, last4 string) store.Account {
	t.Helper()
	a, _, err := r.db.Q().EnsureAccount(context.Background(), store.Account{ID: uuid.New(), UserID: user,
		Class: "liability", Type: "credit_card", Institution: "banco_general", ExternalNumber: "**** " + last4,
		Currency: "USD", DisplayName: "Tarjeta **** " + last4})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// accountOf is user's account with this number.
func (r *rig) accountOf(t *testing.T, user uuid.UUID, number string) uuid.UUID {
	t.Helper()
	a, err := r.db.Q().AccountByIdentity(context.Background(), user, "bac", number, "USD")
	if err != nil {
		t.Fatalf("the account %s: %v", number, err)
	}
	return a.ID
}

// lineOf is one of user's printed lines.
func (r *rig) lineOf(t *testing.T, user uuid.UUID) uuid.UUID {
	t.Helper()
	list, err := r.db.Q().Entries(context.Background(), store.EntryFilter{UserID: user, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.Origin == store.OriginStatement {
			return e.ID
		}
	}
	t.Fatalf("%s has no printed line", user)
	return uuid.Nil
}

// category is one of user's spending categories.
func (r *rig) category(t *testing.T, user uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := r.db.QueryRow(context.Background(),
		`SELECT id FROM categories WHERE user_id = $1 AND kind = 'expense' ORDER BY name LIMIT 1`, user).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
