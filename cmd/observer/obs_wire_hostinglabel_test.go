//go:build !sbo_omit_judge_client

package main

import "testing"

// TestJudgeHostingLabelLoopbackIsExactHost pins the round-4 JUDGE-1 fix on the
// obs judge hosting label: "local" (which skips egress scrubbing) requires an
// exact loopback host, never a substring - localhost.attacker.example is
// "private" (a remote host), and 0.0.0.0 (a bind wildcard reachable from the LAN) is not local.
func TestJudgeHostingLabelLoopbackIsExactHost(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, baseURL, want string
	}{
		{"loopback ipv4", "http://127.0.0.1:11434/v1", "local"},
		{"localhost", "http://localhost:11434", "local"},
		{"loopback ipv6", "http://[::1]:11434/v1", "local"},
		{"substring bypass", "https://localhost.attacker.example/v1", "private"},
		{"ip in path", "https://api.example.com/127.0.0.1/v1", "private"},
		{"bind wildcard", "http://0.0.0.0:11434", "private"},
		{"openrouter", "https://openrouter.ai/api/v1", "aggregator"},
		{"empty means default aggregator", "", "aggregator"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := judgeHostingLabel(tc.baseURL, "some-model"); got != tc.want {
				t.Fatalf("judgeHostingLabel(%q) = %q, want %q", tc.baseURL, got, tc.want)
			}
		})
	}
	if got := judgeHostingLabel("http://127.0.0.1:11434", ""); got != "off" {
		t.Fatalf("no model must be off, got %q", got)
	}
}
