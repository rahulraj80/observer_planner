package main

import "testing"

// TestVerbOwnersT674 pins the catalog owners T-674 assigned to verbs that
// used to fall through to core: compression profiles and experiments belong
// to proxy.compression, the MCP call-log verb to the MCP server, and the
// two hand-run analysis verbs (Prometheus metrics, session summaries) to
// analytics. One row per verb.
func TestVerbOwnersT674(t *testing.T) {
	owners := verbComponents()
	for verb, want := range map[string]string{
		"profile":    "proxy.compression",
		"experiment": "proxy.compression",
		"mcp-audit":  "integration.mcp-server",
		"metrics":    "analytics",
		"summarize":  "analytics",
	} {
		if got := owners[verb]; got != want {
			t.Errorf("verb %q owner = %q, want %q", verb, got, want)
		}
	}
}
