package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProxyDownWarningOnlyForRoutesInPlace pins T-736 (D-548): the
// enrolment's stopped-proxy warning names only the tools whose route the
// wire left in place. A node with no ~/.claude (node 4) used to be told that
// Claude Code now routes through the proxy although nothing was written.
func TestProxyDownWarningOnlyForRoutesInPlace(t *testing.T) {
	requireAdapter(t, "claude-code")
	requireComponents(t, "integration.proxy-route")
	requireActiveAdapters(t, "claude-code")
	cases := []struct {
		name       string
		withClaude bool
		wantRouted bool
	}{
		{name: "no ~/.claude: nothing routed, no warning", withClaude: false, wantRouted: false},
		{name: "~/.claude present: the route is in place", withClaude: true, wantRouted: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.withClaude {
				if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			var report WireReport
			if _, _, _, _, err := wireAIClients(WireAIClientsOptions{
				ProxyPort: 18820, DryRun: true, HomeDir: home, All: true,
				SkipExtension: true, Report: &report,
			}); err != nil {
				t.Fatalf("wireAIClients: %v", err)
			}
			routed := false
			for _, r := range report.RoutesInPlace {
				if r == "claude-code" {
					routed = true
				}
			}
			if routed != tc.wantRouted {
				t.Fatalf("routes in place = %v, want claude-code routed %v", report.RoutesInPlace, tc.wantRouted)
			}
			var out strings.Builder
			printProxyDownWarning(&out, 18820, report.RoutesInPlace)
			if got := strings.Contains(out.String(), "Claude Code now routes through the proxy"); got != tc.wantRouted {
				t.Fatalf("warning printed = %v, want %v:\n%s", got, tc.wantRouted, out.String())
			}
		})
	}
}
