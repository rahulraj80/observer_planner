package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"

	"github.com/marmutapp/superbased-observer/internal/db"
)

// moneyTool is a stand-in tool whose result carries a dollar field, a token
// field and prose with a dollar amount.
type moneyTool struct{}

func (moneyTool) Name() string                { return "money_probe" }
func (moneyTool) Description() string         { return "test" }
func (moneyTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (moneyTool) Invoke(context.Context, json.RawMessage) (any, error) {
	return map[string]any{
		"cost_usd":     1.25,
		"input_tokens": 4000,
		"note":         "routing would have saved about $0.40",
	}, nil
}

// TestToolResultsFollowCostDisplay pins T-204 at the MCP result seam: under
// the token-usage developer cost display a tool result reaches the AI client
// without its dollar-keyed fields and with dollar amounts in its text hidden;
// under dollars it is unchanged.
func TestToolResultsFollowCostDisplay(t *testing.T) {
	database, err := dbtemplate.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "obs.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	for _, mode := range []string{"usd", "tokens"} {
		m := mode
		s, err := New(Options{DB: database, ExtraTools: []Tool{moneyTool{}}, CostDisplay: func() string { return m }})
		if err != nil {
			t.Fatal(err)
		}
		res, rpcErr := s.handleToolsCall(context.Background(), json.RawMessage(`{"name":"money_probe","arguments":{}}`))
		if rpcErr != nil {
			t.Fatalf("%s: %+v", mode, rpcErr)
		}
		text := res.(toolCallResult).Content[0].Text
		var got map[string]any
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Fatalf("%s: decode %q: %v", mode, text, err)
		}
		_, hasUSD := got["cost_usd"]
		switch mode {
		case "usd":
			if !hasUSD || !strings.Contains(text, "$0.40") {
				t.Errorf("usd result changed: %s", text)
			}
		case "tokens":
			if hasUSD || strings.Contains(text, "$") || got["input_tokens"] == nil || got["cost_display"] != "tokens" {
				t.Errorf("tokens result = %s, want no dollars, tokens kept, cost_display tokens", text)
			}
		}
	}
}
