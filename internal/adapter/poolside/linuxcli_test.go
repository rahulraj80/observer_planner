package poolside

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/platform/crossmount"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// linuxCLIFixture is an anonymized copy of the one trajectory the
// standalone Linux `pool` 1.0.16 TUI wrote on demo node 1 (2026-10-08):
// prompt, reasoning, reply and paths replaced, the vendor's system prompt
// and tool schemas dropped, record ids renumbered. The record shapes, the
// client_metadata, the model and every usage number are as captured.
const linuxCLIFixture = "linux-cli/trajectory-standalone_019a0c00-0000-7000-8000-000000000001.ndjson"

// TestDefaultRootsIncludeLinuxStateHome pins T-706: on a Linux home the
// adapter watches ~/.local/state/poolside/trajectories, where the
// standalone CLI writes. Before the fix no Linux root was declared, so a
// `pool` run on Linux was never captured.
func TestDefaultRootsIncludeLinuxStateHome(t *testing.T) {
	home := crossmount.HomeRoot{Path: filepath.Join("/home", "dev"), OS: crossmount.OSLinux, Origin: "wslhost:x/dev"}
	var got []string
	for _, base := range adapter.AppDataRoots(home, appDataSpec) {
		got = append(got, filepath.Join(base, trajectoryDirName))
	}
	want := filepath.Join(home.Path, ".local", "state", "poolside", "trajectories")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("linux roots = %v, want [%s]", got, want)
	}
	if runtime.GOOS != "linux" {
		return
	}
	found := false
	for _, r := range defaultRoots() {
		if filepath.Base(filepath.Dir(filepath.Dir(r))) == "state" {
			found = true
		}
	}
	if !found {
		t.Errorf("defaultRoots() on linux has no .local/state root: %v", defaultRoots())
	}
}

// TestParseLinuxCLITrajectory parses the node 1 capture: one session, the
// prompt, the reply, the synthetic exit completion, a session end, the
// per-call tokens netted (GROSS 6154 input - 5120 cache read = 1034), the
// model, and the CLI capture surface from client_metadata.host.
func TestParseLinuxCLITrajectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".local", "state", "poolside", "trajectories")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "poolside", filepath.FromSlash(linuxCLIFixture)))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.Base(linuxCLIFixture))
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewWithOptions(nil, root)
	if !a.IsSessionFile(path) {
		t.Fatalf("IsSessionFile(%s) = false", path)
	}
	res, err := a.ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	const sid = "019a0c00-0000-7000-8000-000000000001"
	if len(res.Warnings) != 0 {
		t.Errorf("warnings: %v", res.Warnings)
	}
	kinds := map[string]int{}
	for _, ev := range res.ToolEvents {
		if ev.SessionID != sid || ev.Tool != models.ToolPoolside {
			t.Errorf("event %s session=%q tool=%q", ev.SourceEventID, ev.SessionID, ev.Tool)
		}
		if ev.ProjectRoot != "/home/devuser/work/demo-project" {
			t.Errorf("event %s project root = %q", ev.SourceEventID, ev.ProjectRoot)
		}
		kinds[ev.ActionType]++
	}
	for _, k := range []string{models.ActionSessionStart, models.ActionUserPrompt, models.ActionAssistantMessage, models.ActionTaskComplete, models.ActionSessionEnd} {
		if kinds[k] != 1 {
			t.Errorf("action %s count = %d, want 1 (all: %v)", k, kinds[k], kinds)
		}
	}
	if len(res.TokenEvents) != 1 {
		t.Fatalf("token events = %d, want 1", len(res.TokenEvents))
	}
	tok := res.TokenEvents[0]
	if tok.Model != "poolside/laguna-s-2.1" || tok.InputTokens != 1034 || tok.OutputTokens != 81 ||
		tok.CacheReadTokens != 5120 || tok.CacheCreationTokens != 0 || tok.SessionID != sid {
		t.Errorf("token event = %+v", tok)
	}
	if tok.GenMs <= 0 {
		t.Errorf("GenMs = %d, want the inference start->end span", tok.GenMs)
	}
	if len(res.SessionSurfaces) != 1 {
		t.Fatalf("session surfaces = %+v, want one CLI stamp", res.SessionSurfaces)
	}
	s := res.SessionSurfaces[0]
	if s.SessionID != sid || s.Surface != models.SurfaceCLI || s.SurfaceHost != hostPoolCLI || s.Hosted {
		t.Errorf("surface = %+v", s)
	}
}

// TestParseWindowsACPTrajectoryStampsNoSurface keeps the Windows JetBrains
// capture (no client_metadata) unstamped: its surface comes from the
// JetBrains enricher, never from a guess here.
func TestParseWindowsACPTrajectoryStampsNoSurface(t *testing.T) {
	res, _ := parseFixture(t)
	if len(res.SessionSurfaces) != 0 {
		t.Errorf("surfaces = %+v, want none", res.SessionSurfaces)
	}
}

// TestPoolsideSurfaceHostsShipVerbatim is the producer-side drift guard for
// the org wire's closed surface_host vocabulary: every host this adapter
// can stamp must cross the wire as itself, never coarsen to "other".
func TestPoolsideSurfaceHostsShipVerbatim(t *testing.T) {
	for host, s := range clientHostSurfaces {
		if !store.SurfaceHostShipsVerbatim(s.SurfaceHost) {
			t.Errorf("client host %q -> surface host %q would ship to the org as \"other\"", host, s.SurfaceHost)
		}
	}
}
