package integration

import (
	"strings"
	"testing"
)

// TestModelCatalogShape pins the model-catalogue declarations the Arena's
// model pickers dispatch on: every Arena harness that can take a model
// override states which catalogue it draws from (so a new headless row cannot
// land with a picker that silently shows nothing), a vendor catalogue names
// at least one known provider family, and an open catalogue names none.
func TestModelCatalogShape(t *testing.T) {
	knownProviders := map[string]bool{"anthropic": true, "openai": true, "xai": true, "google": true}
	want := map[string]ModelCatalogKind{
		"claude-code": ModelCatalogVendor,
		"codex":       ModelCatalogVendor,
		"grok":        ModelCatalogVendor,
		"opencode":    ModelCatalogOpen,
		"aider":       ModelCatalogOpen,
	}
	for _, c := range Capabilities() {
		cat := c.Model.Catalog
		if c.Headless != nil && c.Model.Kind != ModelNone && cat.Kind == ModelCatalogNone {
			t.Errorf("tool %q: Arena harness with a model override must declare Model.Catalog", c.Tool)
		}
		if k, ok := want[c.Tool]; ok && cat.Kind != k {
			t.Errorf("tool %q: Model.Catalog.Kind = %q, want %q", c.Tool, cat.Kind, k)
		}
		switch cat.Kind {
		case ModelCatalogNone:
			if len(cat.Providers) > 0 {
				t.Errorf("tool %q: providers without a catalogue kind", c.Tool)
			}
		case ModelCatalogVendor:
			if len(cat.Providers) == 0 {
				t.Errorf("tool %q: vendor catalogue names no provider family", c.Tool)
			}
			for _, p := range cat.Providers {
				if !knownProviders[p] {
					t.Errorf("tool %q: unknown provider family %q", c.Tool, p)
				}
			}
		case ModelCatalogOpen:
			if len(cat.Providers) > 0 {
				t.Errorf("tool %q: an open catalogue must not name provider families", c.Tool)
			}
		default:
			t.Errorf("tool %q: unknown Model.Catalog.Kind %q", c.Tool, cat.Kind)
		}
		if cat.Kind != ModelCatalogNone && c.Model.Kind == ModelNone {
			t.Errorf("tool %q: a catalogue without a model mechanism can never be selected", c.Tool)
		}
	}
}

// TestHeadlessGroundedSet pins the exact set of registry rows carrying a
// live-verified headless one-shot contract (Capability.Headless). A row is
// added here ONLY after a real drive proves the argv — the same grounding
// rule LaunchSpec rows follow. Adding a tool to the arena means adding it to
// this pin AND landing the drive evidence, so the set can never grow by
// accident.
func TestHeadlessGroundedSet(t *testing.T) {
	want := map[string]bool{
		"claude-code": true,
		"codex":       true,
		"grok":        true, // 2026-08-22: -p + --output-format json --always-approve (file edit + usage envelope)
		"opencode":    true, // 2026-08-22: run <prompt> --format json (file edit + sessionID events)
		"aider":       true, // 2026-08-22: --message --yes --no-auto-commits (file edit + Tokens/Cost prose)
	}
	for _, c := range Capabilities() {
		if want[c.Tool] && c.Headless == nil {
			t.Errorf("tool %q: expected grounded Headless row, found nil", c.Tool)
			continue
		}
		if !want[c.Tool] && c.Headless != nil {
			t.Errorf("tool %q: Headless declared but not in the grounded pin — verify a live one-shot drive first, then extend this test", c.Tool)
		}
		if c.Headless == nil {
			continue
		}
		// Shape invariants: a grounded headless row must have binary
		// resolution (the arena drives the real binary directly, not a
		// launcher) and must name a result extraction contract.
		if c.Binary == nil {
			t.Errorf("tool %q: Headless without Binary resolution", c.Tool)
		}
		if len(c.Headless.OutputArgs) == 0 {
			t.Errorf("tool %q: headless row without OutputArgs (every grounded lane needs its machine-readable/approval flags)", c.Tool)
		}
		if c.Headless.PromptFlag == "" && len(c.Headless.Lead) == 0 {
			t.Errorf("tool %q: headless spec has neither PromptFlag nor Lead — prompt delivery undefined", c.Tool)
		}
		switch c.Headless.Result {
		case HeadlessResultStdoutJSON, HeadlessResultGrokJSON,
			HeadlessResultOpenCodeEvents, HeadlessResultStdoutText:
			// extraction contracts implemented in internal/arena/driver.go
		case HeadlessResultOutputFile:
			if c.Headless.ResultFlag == "" {
				t.Errorf("tool %q: output_file result requires ResultFlag", c.Tool)
			}
		default:
			t.Errorf("tool %q: Headless.Result unset or unknown (%q)", c.Tool, c.Headless.Result)
		}
		switch c.Headless.ContextMode {
		case HeadlessContextNone:
			if c.Tool == "aider" {
				t.Errorf("tool %q: aider must declare its grounded positional context-file argv", c.Tool)
			}
		case HeadlessContextPositional:
			if c.Tool != "aider" {
				t.Errorf("tool %q: positional context-file argv is grounded only for aider", c.Tool)
			}
		default:
			t.Errorf("tool %q: unknown Headless.ContextMode %q", c.Tool, c.Headless.ContextMode)
		}
		if c.Headless.ProxyModelPrefix != "" {
			if c.Headless.ProxyDefaultModel == "" {
				t.Errorf("tool %q: ProxyModelPrefix requires ProxyDefaultModel", c.Tool)
			} else if !strings.HasPrefix(c.Headless.ProxyDefaultModel, c.Headless.ProxyModelPrefix) {
				t.Errorf("tool %q: proxy default model %q does not match prefix %q", c.Tool, c.Headless.ProxyDefaultModel, c.Headless.ProxyModelPrefix)
			}
		}
		if c.Tool == "opencode" {
			if c.Headless.ProxyModelPrefix != "openrouter/" || c.Headless.ProxyDefaultModel != "openrouter/stealth/ox-alpha" {
				t.Errorf("tool %q: routed model contract drifted: prefix=%q default=%q", c.Tool, c.Headless.ProxyModelPrefix, c.Headless.ProxyDefaultModel)
			}
		} else if c.Headless.ProxyModelPrefix != "" || c.Headless.ProxyDefaultModel != "" {
			t.Errorf("tool %q: provider-scoped proxy model is grounded only for opencode", c.Tool)
		}
	}
}
