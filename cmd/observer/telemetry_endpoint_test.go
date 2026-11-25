package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestTelemetryShowNamesEndpointSource: `observer telemetry show` says which
// endpoint is in force and why, and warns when the override is refused.
func TestTelemetryShowNamesEndpointSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cases := []struct {
		env  string
		want []string
	}{
		{"", []string{"endpoint:      " + config.DefaultTelemetryEndpoint + " (default)"}},
		{"https://dev.example.test:48189", []string{"endpoint:      https://dev.example.test:48189 (OBSERVER_TELEMETRY_ENDPOINT)"}},
		{"http://dev.example.test", []string{"endpoint:      " + config.DefaultTelemetryEndpoint + " (default)", "WARN:", "refused"}},
	}
	for _, tc := range cases {
		t.Setenv(config.TelemetryEndpointEnv, tc.env)
		cmd := newTelemetryShowCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(nil)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("show (env %q): %v", tc.env, err)
		}
		for _, w := range tc.want {
			if !strings.Contains(out.String(), w) {
				t.Fatalf("show (env %q) output lacks %q:\n%s", tc.env, w, out.String())
			}
		}
	}
}
