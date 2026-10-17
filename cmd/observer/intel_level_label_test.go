package main

import (
	"strings"
	"testing"
)

// TestOrgIntelLevelLabelOneTerm pins ruling Q5 (2026-10-04): the CLI names
// each evidence level exactly as the node dashboard and the org dashboard do,
// and the titles level is never suffixed with the "Title only" purpose name.
func TestOrgIntelLevelLabelOneTerm(t *testing.T) {
	for _, c := range []struct{ level, want string }{
		{"structural", "Metadata only"},
		{"titles", "Metadata and first prompt"},
		{"excerpts", "Metadata and bounded excerpts"},
		{"", "Metadata only"},
	} {
		got := orgIntelLevelLabel(c.level)
		if got != c.want || strings.Contains(got, "Title only") {
			t.Errorf("orgIntelLevelLabel(%q) = %q, want %q", c.level, got, c.want)
		}
	}
}
