package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestSandboxRunsRoute pins GET /api/terminal/sandbox/runs (the Terminals >
// Sandboxes tab): only sandboxed runs, newest first, the backend from the
// stack, the duration of an ended run, the refusal count as findings (and
// none for a run with no refusal on record), no host in the answer, the
// View class and the owning component.
func TestSandboxRunsRoute(t *testing.T) {
	s, _ := newTestServer(t)
	st := store.New(s.db())
	ctx := context.Background()
	t0 := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	ended := t0.Add(90 * time.Second)
	exit0 := 0
	for _, r := range []store.TerminalRun{
		{RunID: "plain", Tool: "claude-code", Kind: "fresh", LaunchedAt: t0},
		{
			RunID: "local", Tool: "claude-code", Kind: "fresh", LaunchedAt: t0.Add(time.Minute), EndedAt: &ended, ExitCode: &exit0,
			Isolation: store.TerminalRunIsolation{Class: "process", Stack: []string{"bwrap"}, EgressTier: "none", CaptureLevel: "full"},
		},
		{
			RunID: "node", Tool: "claude-code", Kind: "fresh", LaunchedAt: t0.Add(2 * time.Minute),
			Isolation: store.TerminalRunIsolation{Class: "container", Stack: []string{"sandhold"}, EgressTier: "proxy_only"},
		},
	} {
		if err := st.InsertTerminalRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	for i, host := range []string{"secret-host-a.example", "secret-host-b.example"} {
		if _, err := st.InsertSandboxEgressRefusal(ctx, store.SandboxEgressRefusal{RunID: "node", Host: host, Rule: "allow-list-miss", At: t0.Add(time.Duration(3+i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.AddSandboxEgressRefusalDrops(ctx, "node", 1, 2, time.Now()); err != nil {
		t.Fatal(err)
	}

	// What the node said it made (demo screenshots 2026-11-12): the row's
	// recorded class and tier are its floor and the launch's tier.
	s.opts.SandboxBoxWords = func(runID, backend string, running bool) (dashapi.BoxWords, bool) {
		if backend != "sandhold" {
			return dashapi.BoxWords{}, false
		}
		if runID != "node" || !running {
			t.Errorf("box words asked for %s on %s (running %v)", runID, backend, running)
		}
		return dashapi.BoxWords{Isolation: "shielded", Network: "limited", RunsOn: "runc"}, true
	}

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/sandbox/runs", nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-host") {
		t.Fatalf("the answer names a refused host: %s", rec.Body.String())
	}
	var body struct {
		Runs []sandboxRunRow `json:"runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Runs) != 2 || body.Runs[0].RunID != "node" || body.Runs[1].RunID != "local" {
		t.Fatalf("runs %+v, want node then local (the unsandboxed run left out)", body.Runs)
	}
	node, local := body.Runs[0], body.Runs[1]
	if node.Backend != "sandhold" || node.IsolationClass != "container" || node.EgressTier != "proxy_only" || node.DurationSeconds != nil {
		t.Errorf("node run %+v", node)
	}
	if node.Node == nil || *node.Node != (dashapi.BoxWords{Isolation: "shielded", Network: "limited", RunsOn: "runc"}) || local.Node != nil {
		t.Errorf("box words: node %+v, local %+v", node.Node, local.Node)
	}
	if node.Refused == nil || *node.Refused != 3 || !node.RefusedMayBeMore {
		t.Errorf("node findings: refused %v may be more %v, want 3 (2 kept, 1 dropped) and may be more", node.Refused, node.RefusedMayBeMore)
	}
	if local.Backend != "bwrap" || local.State != "ended" || local.DurationSeconds == nil || *local.DurationSeconds != 30 || local.Refused != nil || local.ExitCode == nil {
		t.Errorf("local run %+v (duration %v), want ended after 30s with no findings", local, local.DurationSeconds)
	}

	_, capMap, _ := s.registerRoutes(nil)
	if c := capMap["/api/terminal/sandbox/runs"]; c != CapabilityView {
		t.Fatalf("route class %v, want View", c)
	}
	if got := routeComponent("/api/terminal/sandbox/runs"); got != "terminals.sandbox" {
		t.Fatalf("route owner %q, want terminals.sandbox", got)
	}
}
