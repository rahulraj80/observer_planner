package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestEveryRegistryToolHasDisplayName pins that a new registry row cannot
// ship without a product name for the server-side consumers (T-201).
func TestEveryRegistryToolHasDisplayName(t *testing.T) {
	for _, tool := range Tools() {
		if !HasDisplayName(tool) {
			t.Errorf("registry tool %q has no display name in displayNames", tool)
		}
	}
}

func TestDisplayNameFallsBackToID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"claude-code", "Claude Code"},
		{"kilo-code-cli", "Kilo CLI"},
		{"chatgpt-web", "ChatGPT (web)"},
		{"not-a-tool", "not-a-tool"},
		{"", ""},
	}
	for _, c := range cases {
		if got := DisplayName(c.in); got != c.want {
			t.Errorf("DisplayName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDisplayNamesMatchSharedToolRegistry reads the dashboards' tool table
// (shared/lib/tools.ts) and requires the same id -> label pairs, both ways.
func TestDisplayNamesMatchSharedToolRegistry(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "shared", "lib", "tools.ts"))
	if err != nil {
		t.Fatalf("read shared/lib/tools.ts: %v", err)
	}
	// An entry opens with `<key>: {` (key quoted or bare) and carries
	// `label: "<name>"` before the next entry opens.
	entry := regexp.MustCompile(`(?m)^  "?([a-z0-9-]+)"?: \{[^}]*?label: "([^"]*)"`)
	ts := map[string]string{}
	for _, m := range entry.FindAllStringSubmatch(string(src), -1) {
		ts[m[1]] = m[2]
	}
	if len(ts) < 40 {
		t.Fatalf("parsed only %d tools.ts entries; the parser no longer matches the file", len(ts))
	}
	for id, label := range ts {
		if got, ok := displayNames[id]; !ok {
			t.Errorf("tools.ts names %q (%q) but displayNames has no entry", id, label)
		} else if got != label {
			t.Errorf("display name drift for %q: Go %q, tools.ts %q", id, got, label)
		}
	}
	for id := range displayNames {
		if _, ok := ts[id]; !ok {
			t.Errorf("displayNames names %q but tools.ts has no entry", id)
		}
	}
}
