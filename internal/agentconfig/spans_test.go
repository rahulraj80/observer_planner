package agentconfig

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

func ownedBySpan(r SettingRow) bool {
	for _, s := range SpansFor(r.ID) {
		if s.Match != SpanEntries && pathHasPrefix(r.Path, s.Path) && ObserverOwner(s.Owner) == r.Owner {
			return true
		}
	}
	return false
}

// TestOwnedSpanTablesMatchRegistry pins the writer tables against the
// registry in both directions: no table row is dead, and no registry
// writer is silently missing.
func TestOwnedSpanTablesMatchRegistry(t *testing.T) {
	declared := map[integration.HookMechanism]bool{}
	mcpFormats := map[integration.MCPFormat]bool{}
	routeTools := map[string]bool{}
	for _, c := range integration.Capabilities() {
		declared[c.Hook.Mechanism] = true
		if c.MCP != nil && c.MCP.Implemented {
			mcpFormats[c.MCP.Format] = true
		}
		if routeWritesFile(c) {
			routeTools[c.Tool] = true
		}
	}
	for m := range hookSpanWriters {
		if !declared[m] {
			t.Errorf("hookSpanWriters[%q]: no registry row declares the mechanism", m)
		}
		if _, dup := hookNoSpan[m]; dup {
			t.Errorf("%q is in both hookSpanWriters and hookNoSpan", m)
		}
	}
	for m := range declared {
		_, w := hookSpanWriters[m]
		_, n := hookNoSpan[m]
		if !w && !n {
			t.Errorf("mechanism %q has no span writer row and no reason in hookNoSpan", m)
		}
	}
	for f := range mcpFormats {
		if _, ok := mcpSpanWriters[f]; !ok {
			t.Errorf("implemented MCP format %q has no mcpSpanWriters row", f)
		}
	}
	for tool := range proxySpanWriters {
		if !routeTools[tool] {
			t.Errorf("proxySpanWriters[%q]: the registry row's route writes no file", tool)
		}
	}
	for tool := range routeTools {
		_, w := proxySpanWriters[tool]
		_, n := proxyNoSpan[tool]
		if !w && !n {
			t.Errorf("%s: file-writing route with no proxySpanWriters row and no proxyNoSpan reason", tool)
		}
	}
}

// TestOwnedSpansResolve: every computed span lands on a Files row, and
// every writer row whose tool has catalogued files resolves.
func TestOwnedSpansResolve(t *testing.T) {
	catalogOnce.Do(build)
	pending := map[string]bool{}
	for _, c := range Coverage() {
		pending[c.Tool] = c.Status == CoveragePending
	}
	for _, u := range spansUnresolved {
		tool, _, _ := strings.Cut(u, ":")
		if pending[tool] {
			t.Logf("unresolved until the tool's P2 file rows land: %s", u)
			continue
		}
		t.Errorf("unresolved owned span: %s", u)
	}
	files := map[string]FileRow{}
	for _, f := range Files() {
		files[f.ID] = f
	}
	seen := map[string]bool{}
	for _, s := range OwnedSpans() {
		f, ok := files[s.FileID]
		switch {
		case !ok:
			t.Errorf("%s: span file %q not in Files", s.Tool, s.FileID)
		case f.Tool != s.Tool || f.Shape != s.Shape:
			t.Errorf("%s: span file %q belongs to %s/%s", s.Tool, s.FileID, f.Tool, f.Shape)
		}
		if s.Owner == "" || s.Verb == "" || s.From == "" || s.Writer == "" {
			t.Errorf("%s %s: span missing owner / verb / from / writer: %+v", s.Tool, s.Path, s)
		}
		if s.Match == SpanWholeFile && len(s.Path) != 0 {
			t.Errorf("%s: a whole-file span carries a path", s.FileID)
		}
		// A root ENTRIES span is anchored by its owner's own leaf predicate
		// (droid's hooks.json keeps its event map at the root); any other
		// span needs a path or a named entry.
		rootEntries := s.Match == SpanEntries && spanOwnerRules[s.Owner].ownsLeaf != nil
		if s.Match != SpanWholeFile && len(s.Path) == 0 && s.Entry == "" && !rootEntries {
			t.Errorf("%s: span with no path and no entry", s.FileID)
		}
		k := s.FileID + "\x00" + s.Path.String() + "\x00" + s.Owner
		if seen[k] {
			t.Errorf("duplicate span %s %s %s", s.FileID, s.Path, s.Owner)
		}
		seen[k] = true
	}
}

// TestOwnedSpansGolden pins the stamped ownership the editor relies on.
func TestOwnedSpansGolden(t *testing.T) {
	owned := map[string]Owner{
		"claude-code/env.ANTHROPIC_BASE_URL":               ObserverOwner(SpanOwnerProxyRoute),
		"claude-code/env.CLAUDE_CODE_GATEWAY_HINT_HEADERS": ObserverOwner(SpanOwnerProxyRoute),
		"claude-code/statusLine.command":                   ObserverOwner(SpanOwnerStatusline),
		"codex/features.hooks":                             ObserverOwner(SpanOwnerHook),
		"codex/model_provider":                             ObserverOwner(SpanOwnerProxyRoute),
	}
	for id, want := range owned {
		r, ok := Setting(id)
		if !ok {
			t.Errorf("%s: no such row", id)
			continue
		}
		if r.Owner != want || r.Writable {
			t.Errorf("%s: owner %q writable %v, want %q read-only", id, r.Owner, r.Writable, want)
		}
	}
	// Rows that CONTAIN Observer's entries stay the user's; SpansFor names
	// the span so the editor locks only those entries.
	for _, id := range []string{"claude-code/hooks.PreToolUse", "gemini-cli/hooks.BeforeAgent", "codex/mcp_servers", "crush/providers", "opencode/mcp"} {
		r, ok := Setting(id)
		if !ok {
			t.Errorf("%s: no such row", id)
			continue
		}
		if r.Owner != OwnerUser {
			t.Errorf("%s: owner %q, want user (contains Observer entries only)", id, r.Owner)
		}
		if len(SpansFor(id)) == 0 {
			t.Errorf("%s: SpansFor reports no span", id)
		}
	}
	byOwner := map[string]int{}
	for _, s := range OwnedSpans() {
		byOwner[s.Owner]++
	}
	var got []string
	for o, n := range byOwner {
		got = append(got, o+"="+itoa(n))
	}
	sort.Strings(got)
	t.Logf("owned spans by owner: %s", strings.Join(got, " "))
	for _, o := range []string{SpanOwnerHook, SpanOwnerMCP, SpanOwnerProxyRoute, SpanOwnerStatusline, SpanOwnerRoutingApply} {
		if byOwner[o] == 0 {
			t.Errorf("no span for owner %s", o)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// TestDroidHooksSpanFollowsDedicatedWriter pins T-366: the droid hooks.json
// span names the dedicated root-shape writer (b5a15ecc9), not the Codex
// writer it used to reuse, and owns entries at the ROOT of the file, the
// shape the droid catalogue row declares (61a539fc0).
func TestDroidHooksSpanFollowsDedicatedWriter(t *testing.T) {
	rows := hookSpanWriters[integration.HookFactoryJSON]
	if len(rows) != 1 {
		t.Fatalf("droid spans = %d, want 1", len(rows))
	}
	r := rows[0]
	if len(r.Path) != 0 || r.Rel != "~/.factory/hooks.json" {
		t.Errorf("droid span = %s %v, want ~/.factory/hooks.json at the root", r.Rel, r.Path)
	}
	file, _, _ := strings.Cut(r.Writer, " ")
	if file != "internal/hook/register_droid_hooksjson.go" {
		t.Errorf("droid span writer = %q, want the dedicated hooks.json writer", r.Writer)
	}
	if _, err := os.Stat(filepath.Join("..", "..", file)); err != nil {
		t.Errorf("droid span writer file: %v", err)
	}
}
