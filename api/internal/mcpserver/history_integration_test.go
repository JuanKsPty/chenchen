package mcpserver_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanKsPty/chenchen/api/internal/accounts"
	"github.com/JuanKsPty/chenchen/api/internal/civil"
	"github.com/JuanKsPty/chenchen/api/internal/mcpserver"
	"github.com/JuanKsPty/chenchen/api/internal/oauth"
)

type historyOut struct {
	Points []struct {
		Day     string `json:"day"`
		Owed    string `json:"owed"`
		Total   string `json:"total"`
		Partial bool   `json:"partial"`
	} `json:"points"`
}

// Claude reads the net worth month by month, today's being the one Cuentas
// shows, and never another person's account.
func TestClaudeReadsTheNetWorthMonthByMonth(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	card := r.card(t, r.owner, "4321")
	lastMonth := civil.Of(time.Now().In(civil.Panama)).AddDays(-40)
	if _, err := r.accounts.AddCheckpoint(ctx, r.owner, card.ID,
		accounts.CheckpointInput{AsOf: lastMonth, Balance: -12345}); err != nil {
		t.Fatal(err)
	}
	owner := r.client(t, first(r.connect(t, r.owner, oauth.ScopeRead)))

	var got historyOut
	mustCall(t, owner, mcpserver.ToolNetWorthHistory, map[string]any{"months": 3}, &got)
	if len(got.Points) < 2 {
		t.Fatalf("points = %+v, want a month's end and today", got.Points)
	}
	today := got.Points[len(got.Points)-1]
	if today.Owed != "123.45" || today.Total != "-123.45" || today.Partial {
		t.Errorf("today = %+v, want owed 123.45, total −123.45, whole", today)
	}

	var one historyOut
	mustCall(t, owner, mcpserver.ToolNetWorthHistory, map[string]any{"account_id": card.ID.String()}, &one)
	if len(one.Points) != len(got.Points) {
		t.Errorf("the card alone = %+v, want the same days as the whole", one.Points)
	}

	stranger := r.client(t, first(r.connect(t, r.stranger, oauth.ScopeRead)))
	for _, args := range []map[string]any{
		{"account_id": card.ID.String()},
		{"account_id": uuid.NewString()},
	} {
		if failed, text := call(t, stranger, mcpserver.ToolNetWorthHistory, args, nil); !failed {
			t.Errorf("the stranger's history(%v) = %s; want a refusal", args, text)
		}
	}
	if failed, text := call(t, owner, mcpserver.ToolNetWorthHistory, map[string]any{"months": 99}, nil); !failed ||
		!strings.Contains(text, "36") {
		t.Errorf("history(months 99) = %v %s; want a refusal that says how far it goes", failed, text)
	}
}
