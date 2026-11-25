package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTelemetryEndpointPrecedence(t *testing.T) {
	const dev = "https://dev.example.test:48189"
	cases := []struct {
		name       string
		configured string
		env        string
		wantURL    string
		wantSource string
		wantWarn   bool
	}{
		{"absent: shipped default", DefaultTelemetryEndpoint, "", DefaultTelemetryEndpoint, TelemetryEndpointSourceDefault, false},
		{"blank env: shipped default", DefaultTelemetryEndpoint, "   ", DefaultTelemetryEndpoint, TelemetryEndpointSourceDefault, false},
		{"env replaces default", DefaultTelemetryEndpoint, dev, dev, TelemetryEndpointSourceEnv, false},
		{"env trailing slash trimmed", DefaultTelemetryEndpoint + "/", dev + "/", dev, TelemetryEndpointSourceEnv, false},
		{"env replaces an empty config", "", dev, dev, TelemetryEndpointSourceEnv, false},
		{"explicit config wins over env", "https://reporter.corp.test", dev, "https://reporter.corp.test", TelemetryEndpointSourceConfig, false},
		{"explicit config, no env", "https://reporter.corp.test", "", "https://reporter.corp.test", TelemetryEndpointSourceConfig, false},
		{"loopback http accepted", DefaultTelemetryEndpoint, "http://127.0.0.1:8080", "http://127.0.0.1:8080", TelemetryEndpointSourceEnv, false},
		{"localhost http accepted", DefaultTelemetryEndpoint, "http://localhost:8080", "http://localhost:8080", TelemetryEndpointSourceEnv, false},
		{"plain http off loopback refused", DefaultTelemetryEndpoint, "http://dev.example.test", DefaultTelemetryEndpoint, TelemetryEndpointSourceDefault, true},
		{"no scheme refused", DefaultTelemetryEndpoint, "dev.example.test", DefaultTelemetryEndpoint, TelemetryEndpointSourceDefault, true},
		{"credentials refused", DefaultTelemetryEndpoint, "https://u:p@dev.example.test", DefaultTelemetryEndpoint, TelemetryEndpointSourceDefault, true},
		{"query refused", DefaultTelemetryEndpoint, "https://dev.example.test/?x=1", DefaultTelemetryEndpoint, TelemetryEndpointSourceDefault, true},
		{"ftp refused", DefaultTelemetryEndpoint, "ftp://dev.example.test", DefaultTelemetryEndpoint, TelemetryEndpointSourceDefault, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(TelemetryEndpointEnv, tc.env)
			got := TelemetryConfig{Endpoint: tc.configured}.ResolveEndpoint()
			if strings.TrimRight(got.URL, "/") != tc.wantURL || got.Source != tc.wantSource {
				t.Fatalf("ResolveEndpoint() = %+v, want url %q source %q", got, tc.wantURL, tc.wantSource)
			}
			if (got.Warning != "") != tc.wantWarn {
				t.Fatalf("warning = %q, want warning %v", got.Warning, tc.wantWarn)
			}
			if tc.wantWarn && (!strings.Contains(got.Warning, TelemetryEndpointEnv) || !strings.Contains(got.Warning, "refused") || !strings.Contains(got.Warning, "using the default")) {
				t.Fatalf("warning %q does not say the override was refused and the default is used", got.Warning)
			}
			if got.URL != (TelemetryConfig{Endpoint: tc.configured}).EffectiveEndpoint() {
				t.Fatal("EffectiveEndpoint disagrees with ResolveEndpoint")
			}
		})
	}
}

func TestResolveTelemetryEndpointUnsetKeepsDefault(t *testing.T) {
	t.Setenv(TelemetryEndpointEnv, "")
	got := Default().Telemetry.ResolveEndpoint()
	if got.URL != DefaultTelemetryEndpoint || got.Source != TelemetryEndpointSourceDefault || got.Warning != "" {
		t.Fatalf("default config resolves to %+v, want the shipped default", got)
	}
	if d := got.Describe(); d != DefaultTelemetryEndpoint+" (default)" {
		t.Fatalf("Describe() = %q", d)
	}
}

// TestLoadKeepsTelemetryEndpointFromTheGenericEnvWalker: the generic
// OBSERVER_<SECTION>_<KEY> walker must not turn OBSERVER_TELEMETRY_ENDPOINT
// into [telemetry].endpoint (that would beat an explicit self-hosted
// reporter and skip validation); ResolveEndpoint applies it instead.
func TestLoadKeepsTelemetryEndpointFromTheGenericEnvWalker(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(TelemetryEndpointEnv, "https://dev.example.test:48189")

	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Telemetry.Endpoint != DefaultTelemetryEndpoint {
		t.Fatalf("[telemetry].endpoint = %q, want the shipped default kept", cfg.Telemetry.Endpoint)
	}
	if got := cfg.Telemetry.ResolveEndpoint(); got.URL != "https://dev.example.test:48189" || got.Source != TelemetryEndpointSourceEnv {
		t.Fatalf("ResolveEndpoint() = %+v, want the override", got)
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[telemetry]\nendpoint = \"https://reporter.corp.test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(LoadOptions{GlobalPath: path})
	if err != nil {
		t.Fatalf("Load %s: %v", path, err)
	}
	if got := cfg.Telemetry.ResolveEndpoint(); got.URL != "https://reporter.corp.test" || got.Source != TelemetryEndpointSourceConfig {
		t.Fatalf("ResolveEndpoint() = %+v, want the configured reporter", got)
	}
}
