package arenamodels

import (
	"reflect"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// catalog is a slice of the compiled snapshot's shape: every family plus the
// rows the family table must exclude.
var catalog = []string{
	"claude-haiku-4-5", "claude-opus-4-8", "claude-opus-5-5", "claude-opus-5-5-fast", "claude-sonnet-5-5",
	"gpt-5.4", "gpt-5.10", "gpt-5.9", "gpt-oss-120b", "gpt-realtime-2.1", "o1-mini", "chat-latest",
	"grok-4.7", "grok-code-fast-1",
	"gemini-3.5-flash", "deepseek-v4", "big-pickle",
}

// nodeUsage is this node's history across tools, most-used first.
var nodeUsage = []Usage{
	{Model: "claude-opus-4-8", Count: 40, LastUsed: "2026-09-30T10:00:00Z", Tools: []string{"claude-code"}},
	{Model: "gpt-5.5", Count: 12, LastUsed: "2026-09-29T10:00:00Z", Tools: []string{"codex"}},
	{Model: "anthropic/claude-sonnet-4.6", Count: 9, LastUsed: "2026-09-20T10:00:00Z", Tools: []string{"hermes"}},
	{Model: "stealth/ox-alpha", Count: 3, LastUsed: "2026-09-28T10:00:00Z", Tools: []string{"opencode"}},
	{Model: "default", Count: 2, LastUsed: "2026-09-01T10:00:00Z", Tools: []string{"cursor"}},
}

func capFor(t *testing.T, tool string) integration.Capability {
	t.Helper()
	c, ok := integration.For(tool)
	if !ok {
		t.Fatalf("no registry row for %q", tool)
	}
	return c
}

func models(h Harness) []string {
	out := make([]string, 0, len(h.Options))
	for _, o := range h.Options {
		out = append(out, o.Model)
	}
	return out
}

func TestComposePerProviderFamily(t *testing.T) {
	cases := []struct {
		name      string
		tool      string
		toolUsage []Usage
		routed    bool
		want      []string
		groups    []Group
		dflt      string
	}{
		{
			name: "claude-code: own history, aliases, anthropic catalogue newest first",
			tool: "claude-code",
			toolUsage: []Usage{
				{Model: "claude-opus-4-8", Count: 40, LastUsed: "2026-09-30T10:00:00Z", Tools: []string{"claude-code"}},
				{Model: "<synthetic>", Count: 5},
			},
			routed: true,
			want:   []string{"claude-opus-4-8", "opus", "sonnet", "fable", "claude-sonnet-5-5", "claude-opus-5-5", "claude-haiku-4-5"},
			groups: []Group{GroupUsed, GroupAvailable, GroupAvailable, GroupAvailable, GroupAvailable, GroupAvailable, GroupAvailable},
		},
		{
			name:   "codex: openai family only, exclusions applied, natural version order",
			tool:   "codex",
			routed: true,
			want:   []string{"o3", "o1-mini", "gpt-5.10", "gpt-5.9", "gpt-5.4"},
		},
		{
			name:   "grok: xai family",
			tool:   "grok",
			routed: true,
			want:   []string{"grok-code-fast-1", "grok-4.7"},
		},
		{
			name:   "aider: open catalogue offers node history only, most-used first",
			tool:   "aider",
			routed: true,
			want:   []string{"claude-opus-4-8", "gpt-5.5", "anthropic/claude-sonnet-4.6", "stealth/ox-alpha"},
		},
		{
			name: "opencode routed: router ids rewritten onto openrouter/, the rest disabled",
			tool: "opencode",
			toolUsage: []Usage{
				{Model: "stealth/ox-alpha", Count: 3, LastUsed: "2026-09-28T10:00:00Z", Tools: []string{"opencode"}},
			},
			routed: true,
			want:   []string{"openrouter/anthropic/claude-sonnet-4.6", "openrouter/stealth/ox-alpha", "claude-opus-4-8", "gpt-5.5"},
			groups: []Group{GroupUsed, GroupUsed, GroupUnroutable, GroupUnroutable},
			dflt:   "openrouter/stealth/ox-alpha",
		},
		{
			name:   "opencode direct: no prefix rule",
			tool:   "opencode",
			routed: false,
			want:   []string{"claude-opus-4-8", "gpt-5.5", "anthropic/claude-sonnet-4.6", "stealth/ox-alpha"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := Compose(Input{
				Tool: tc.tool, Capability: capFor(t, tc.tool),
				ToolUsage: tc.toolUsage, NodeUsage: nodeUsage, Catalog: catalog, Routed: tc.routed,
			})
			if !h.Selectable || h.Reason != "" {
				t.Fatalf("selectable=%v reason=%q", h.Selectable, h.Reason)
			}
			if got := models(h); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("models = %v\nwant     %v", got, tc.want)
			}
			if tc.groups != nil {
				for i, g := range tc.groups {
					if h.Options[i].Group != g {
						t.Errorf("option %d (%s) group = %q, want %q", i, h.Options[i].Model, h.Options[i].Group, g)
					}
					if g == GroupUnroutable && (!h.Options[i].Disabled || h.Options[i].Reason == "") {
						t.Errorf("unroutable option %s must be disabled with a reason", h.Options[i].Model)
					}
				}
			}
			if h.DefaultModel != tc.dflt {
				t.Errorf("DefaultModel = %q, want %q", h.DefaultModel, tc.dflt)
			}
		})
	}
}

func TestComposeHistoryFields(t *testing.T) {
	h := Compose(Input{Tool: "opencode", Capability: capFor(t, "opencode"), NodeUsage: nodeUsage, Routed: true})
	var got Option
	for _, o := range h.Options {
		if o.Model == "openrouter/stealth/ox-alpha" {
			got = o
		}
	}
	want := Option{
		Model: "openrouter/stealth/ox-alpha", Group: GroupUsed, Source: SourceToolHistory,
		Count: 3, LastUsed: "2026-09-28T10:00:00Z", Tools: []string{"opencode"}, UsedAs: "stealth/ox-alpha",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("option = %+v\nwant     %+v", got, want)
	}
	for _, o := range h.Options {
		if o.Model == "openrouter/anthropic/claude-sonnet-4.6" && o.Source != SourceNodeHistory {
			t.Errorf("hermes-only model must be node history, got %q", o.Source)
		}
	}
}

func TestComposeNotSelectable(t *testing.T) {
	cases := []struct {
		name   string
		cap    integration.Capability
		reason string
	}{
		{
			name:   "no model mechanism",
			cap:    integration.Capability{Tool: "x", Headless: &integration.HeadlessSpec{PromptFlag: "-p"}},
			reason: "x has no grounded model flag, so it always runs on its own default model",
		},
		{
			name:   "no headless contract",
			cap:    integration.Capability{Tool: "x", Model: integration.ModelSpec{Kind: integration.ModelArg}},
			reason: "x has no grounded one-shot contract, so the Arena cannot run it",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := Compose(Input{Tool: "x", Capability: tc.cap, NodeUsage: nodeUsage, Catalog: catalog})
			if h.Selectable || h.Reason != tc.reason || len(h.Options) != 0 || h.Options == nil {
				t.Fatalf("harness = %+v", h)
			}
		})
	}
}

func TestMergeUsage(t *testing.T) {
	tool := []Usage{
		{Model: "only-tool", Count: 50, LastUsed: "2026-09-01"},
		{Model: "shared", Count: 2, LastUsed: "2026-09-02"},
	}
	node := []Usage{
		{Model: "shared", Count: 9, LastUsed: "2026-09-03"},
		{Model: "b", Count: 9, LastUsed: "2026-09-04"},
		{Model: "a", Count: 9, LastUsed: "2026-09-04"},
	}
	var got []string
	for _, u := range mergeUsage(tool, node) {
		got = append(got, u.Model)
		if u.Model == "shared" && u.Count != 9 {
			t.Errorf("node-wide count must win for shared, got %d", u.Count)
		}
	}
	want := []string{"only-tool", "a", "b", "shared"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merge order = %v, want %v", got, want)
	}
}

func TestNaturalLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"gpt-5.9", "gpt-5.10", true},
		{"gpt-5.10", "gpt-5.9", false},
		{"claude-opus-4-8", "claude-opus-5-5", true},
		{"grok-4", "grok-4.7", true},
		{"same", "same", false},
		{"a01", "a1", false},
	}
	for _, tc := range cases {
		if got := naturalLess(tc.a, tc.b); got != tc.want {
			t.Errorf("naturalLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestMatchesFamily(t *testing.T) {
	anthropic := familiesFor([]string{"anthropic"})
	openai := familiesFor([]string{"openai"})
	cases := []struct {
		fams  []family
		model string
		want  bool
	}{
		{anthropic, "claude-opus-5-5", true},
		{anthropic, "anthropic/claude-sonnet-4.6", true},
		{anthropic, "claude-opus-5-5-fast", false},
		{anthropic, "gpt-5.5", false},
		{openai, "gpt-5.5", true},
		{openai, "o3", true},
		{openai, "gpt-oss-20b", false},
		{openai, "gpt-realtime-2.1-mini", false},
		{openai, "default", false},
		{openai, "", false},
	}
	for _, tc := range cases {
		if got := matchesFamily(tc.fams, tc.model); got != tc.want {
			t.Errorf("matchesFamily(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

// TestEveryArenaHarnessComposes runs Compose over every registry row the
// Arena can drive: each must be selectable (they all carry a grounded model
// flag today) and declare a catalogue, so the picker is never silently empty
// of meaning.
func TestEveryArenaHarnessComposes(t *testing.T) {
	n := 0
	for _, c := range integration.Capabilities() {
		if c.Headless == nil {
			continue
		}
		n++
		h := Compose(Input{Tool: c.Tool, Capability: c, NodeUsage: nodeUsage, Catalog: catalog, Routed: true})
		if !h.Selectable {
			t.Errorf("%s: not selectable: %s", c.Tool, h.Reason)
		}
		if h.Catalog == "" {
			t.Errorf("%s: no catalogue kind", c.Tool)
		}
	}
	if n == 0 {
		t.Fatal("no Arena harnesses in the registry")
	}
}

// TestNoForbiddenImports pins the package as pure logic (CLAUDE.md #1): the
// dashboard handler loads history and the catalogue and passes them in.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{"database/sql", "net/http", "os", "os/exec", "github.com/fsnotify/fsnotify", "github.com/marmutapp/superbased-observer/internal/store", "github.com/marmutapp/superbased-observer/internal/intelligence/cost"},
		Reason:    "internal/arenamodels must stay pure",
	})
}
