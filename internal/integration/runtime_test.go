package integration

import (
	"regexp"
	"strings"
	"testing"
)

// TestRuntimeRequirementsGrounded pins every row's minimum host runtime to the
// vendor's published engines.node (npm registry, `npm view <pkg> engines`,
// read 2026-10-04). A row whose package publishes NO engines field carries a
// nil Runtime: zero means unknown, and an unknown is never checked. Changing a
// value here means re-reading the vendor's metadata, not editing to match.
func TestRuntimeRequirementsGrounded(t *testing.T) {
	cases := []struct {
		tool string
		min  string // "" = no engines field published: Runtime must be nil
	}{
		{"gemini-cli", "20"},
		{"qwen-code", "22.0.0"},
		{"codex", "16"},
		{"claude-code", "22.0.0"},
		{"openclaw", "24.16.0"},
		{"pi", "22.19.0"},
		{"grok", "20"},
		{"kimi-code", "22.19.0"},
		{"qoder", "20.0.0"},
		{"droid", "20.0.0"},
		{"command-code", "22"},
		{"junie", "20"},
		{"freebuff", "16"},
		// No engines field in the published package.json (2026-10-04).
		{"opencode", ""},
		{"copilot-cli", ""},
		{"kilo-code-cli", ""},
		{"cline-cli", ""},
		{"crush", ""},
	}
	for _, tc := range cases {
		c, ok := For(tc.tool)
		if !ok || c.Binary == nil {
			t.Fatalf("%s: no registry row with a Binary spec", tc.tool)
		}
		got := c.Binary.Runtime
		if tc.min == "" {
			if got != nil {
				t.Errorf("%s: Runtime = %+v, want nil (the vendor publishes no engines.node)", tc.tool, *got)
			}
			continue
		}
		if got == nil {
			t.Errorf("%s: Runtime = nil, want node >= %s", tc.tool, tc.min)
			continue
		}
		if got.Runtime != RuntimeNode || got.MinVersion != tc.min {
			t.Errorf("%s: Runtime = %s >= %s, want node >= %s", tc.tool, got.Runtime, got.MinVersion, tc.min)
		}
	}
}

var runtimeVersionShape = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)

// TestRuntimeRequirementsWellFormed: every declared requirement names a runtime
// in the closed vocabulary, a dotted numeric floor, a product for the user
// sentence and a source, and sits on a row that has an install channel or a
// launcher the check can protect.
func TestRuntimeRequirementsWellFormed(t *testing.T) {
	known := map[string]bool{RuntimeNode: true}
	n := 0
	for _, c := range Capabilities() {
		if c.Binary == nil || c.Binary.Runtime == nil {
			continue
		}
		n++
		r := c.Binary.Runtime
		if !known[r.Runtime] {
			t.Errorf("%s: runtime %q outside the closed vocabulary", c.Tool, r.Runtime)
		}
		if !runtimeVersionShape.MatchString(r.MinVersion) {
			t.Errorf("%s: MinVersion %q is not dotted numeric", c.Tool, r.MinVersion)
		}
		if strings.TrimSpace(r.Product) == "" {
			t.Errorf("%s: Product is empty (the user sentence names it)", c.Tool)
		}
		if !strings.Contains(r.Source, "2026-") {
			t.Errorf("%s: Source %q carries no read date", c.Tool, r.Source)
		}
	}
	if n == 0 {
		t.Fatal("no runtime requirement declared; gemini-cli's node >= 20 must be")
	}
}
