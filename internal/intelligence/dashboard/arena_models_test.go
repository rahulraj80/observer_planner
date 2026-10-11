//go:build !sbo_omit_benchmark

package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestArenaModelsEndpoint pins GET /api/arena/models end to end: every Arena
// harness is listed (vendor catalogues first), a vendor harness offers its own
// history then catalogue models of its family only, an open harness offers
// node history (rewritten onto a routed lane's prefix), and the method and
// harness filters behave.
func TestArenaModelsEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	st := s.remoteManageStore()
	now := time.Now().UTC().Add(-time.Hour)
	ev := func(id, tool, model string, n int) []models.TokenEvent {
		var out []models.TokenEvent
		for i := 0; i < n; i++ {
			out = append(out, models.TokenEvent{
				SourceFile: "f", SourceEventID: id + string(rune('a'+i)), SessionID: "sA",
				Tool: tool, Model: model, Timestamp: now.Add(time.Duration(i) * time.Minute),
				InputTokens: 1, OutputTokens: 1,
			})
		}
		return out
	}
	var seed []models.TokenEvent
	seed = append(seed, ev("cc", "claude-code", "claude-opus-4-8", 3)...)
	seed = append(seed, ev("cx", "codex", "gpt-5.5", 2)...)
	seed = append(seed, ev("hm", "hermes", "anthropic/claude-sonnet-4.6", 1)...)
	if _, err := st.InsertTokenEvents(context.Background(), seed); err != nil {
		t.Fatal(err)
	}

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	rec := get("/api/arena/models")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rec.Code, rec.Body.String())
	}
	var body arenaModelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Routed {
		t.Fatal("arena drives are routed; routed must be true")
	}
	var order []string
	byTool := map[string]arenaHarnessModels{}
	for _, h := range body.Harnesses {
		order = append(order, h.Tool)
		byTool[h.Tool] = h
	}
	wantOrder := []string{"claude-code", "codex", "grok", "aider", "opencode"}
	if len(order) != len(wantOrder) {
		t.Fatalf("harnesses = %v, want %v", order, wantOrder)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("harnesses = %v, want %v", order, wantOrder)
		}
	}

	cc := byTool["claude-code"]
	if !cc.Selectable || len(cc.Options) == 0 || cc.Options[0].Model != "claude-opus-4-8" || cc.Options[0].Count != 3 {
		t.Fatalf("claude-code first option = %+v", cc.Options)
	}
	for _, o := range cc.Options {
		if o.Model == "gpt-5.5" || o.Model == "anthropic/claude-sonnet-4.6" {
			t.Fatalf("claude-code must not offer another tool's history: %+v", o)
		}
	}
	cx := byTool["codex"]
	for _, o := range cx.Options {
		if len(o.Model) >= 6 && o.Model[:6] == "claude" {
			t.Fatalf("codex offers an Anthropic model: %+v", o)
		}
	}

	oc := byTool["opencode"]
	if oc.DefaultModel != "openrouter/stealth/ox-alpha" || oc.Note == "" {
		t.Fatalf("opencode default/note = %q / %q", oc.DefaultModel, oc.Note)
	}
	if len(oc.Options) == 0 || oc.Options[0].Model != "openrouter/anthropic/claude-sonnet-4.6" || oc.Options[0].Disabled {
		t.Fatalf("opencode first option = %+v", oc.Options)
	}
	ad := byTool["aider"]
	if len(ad.Options) != 3 || ad.Options[0].Model != "claude-opus-4-8" {
		t.Fatalf("aider options = %+v", ad.Options)
	}

	if rec := get("/api/arena/models?harness=codex"); rec.Code != http.StatusOK {
		t.Fatalf("filtered GET = %d", rec.Code)
	} else {
		var one arenaModelsResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &one)
		if len(one.Harnesses) != 1 || one.Harnesses[0].Tool != "codex" {
			t.Fatalf("filter = %+v", one.Harnesses)
		}
	}
	if rec := get("/api/arena/models?harness=cursor"); rec.Code != http.StatusNotFound {
		t.Fatalf("non-arena harness = %d, want 404", rec.Code)
	}
	post := httptest.NewRecorder()
	s.Handler().ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/arena/models", nil))
	if post.Code == http.StatusOK {
		t.Fatalf("POST must not succeed, got %d", post.Code)
	}
}
