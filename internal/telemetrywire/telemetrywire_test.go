package telemetrywire

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

const addr = "127.0.0.1:4318"

func plan(t *testing.T, k integration.OTLPWireKind, op Op, in string) Result {
	t.Helper()
	r, err := Plan(k, op, []byte(in), addr, Endpoint(addr))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return r
}

// TestRoundTripIsByteExact pins the core promise for every writer shape and
// both line-ending styles: apply then remove gives back the original bytes,
// a second apply changes nothing, and every byte outside Observer's subtree
// (including a credential) survives the apply unchanged.
func TestRoundTripIsByteExact(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind integration.OTLPWireKind
		in   string
	}{
		{"claude env added beside other keys", integration.OTLPWireEnvSettingsJSON, "{\n  \"model\": \"opus\",\n  \"hooks\": {}\n}\n"},
		{"claude env merged into an existing env", integration.OTLPWireEnvSettingsJSON, "{\n  \"env\": {\n    \"ANTHROPIC_API_KEY\": \"sk-secret-value\"\n  },\n  \"z\": 1\n}\n"},
		{"claude CRLF file", integration.OTLPWireEnvSettingsJSON, "{\r\n  \"zeta\": true,\r\n  \"alpha\": 2\r\n}\r\n"},
		{"qwen settings with a provider key", integration.OTLPWireTelemetryJSON, "{\n    \"security\": {\"auth\": {\"apiKey\": \"sk-secret-value\"}},\n    \"model\": {\"name\": \"qwen3\"}\n}\n"},
		{"gemini CRLF settings", integration.OTLPWireTelemetryJSON, "{\r\n  \"theme\": \"dark\"\r\n}\r\n"},
		{"codex config with a provider", integration.OTLPWireCodexTOML, "model = \"gpt-5\"\n\n[model_providers.x]\nbase_url = \"https://x\"\nhttp_headers = { Authorization = \"Bearer sk-secret-value\" }\n"},
		{"codex CRLF config", integration.OTLPWireCodexTOML, "model = \"gpt-5\"\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ap := plan(t, tc.kind, OpApply, tc.in)
			if !ap.Changed || ap.Conflict != "" {
				t.Fatalf("apply: changed=%v conflict=%q", ap.Changed, ap.Conflict)
			}
			got := string(ap.Content)
			if strings.Contains(tc.in, "sk-secret-value") && !strings.Contains(got, "sk-secret-value") {
				t.Fatal("apply lost an unrelated value")
			}
			if strings.Contains(tc.in, "\r\n") && strings.Count(got, "\n") != strings.Count(got, "\r\n") {
				t.Fatalf("apply mixed line endings into a CRLF file:\n%q", got)
			}
			for _, s := range ap.Summary {
				if strings.Contains(s, "sk-secret-value") {
					t.Fatal("a summary carried a value from the file")
				}
			}
			if tc.kind != integration.OTLPWireCodexTOML {
				var v map[string]any
				if err := json.Unmarshal(ap.Content, &v); err != nil {
					t.Fatalf("apply produced invalid JSON: %v\n%s", err, got)
				}
			}
			if !Wired(tc.kind, ap.Content, Endpoint(addr)) {
				t.Fatal("Wired is false after apply")
			}
			again := plan(t, tc.kind, OpApply, got)
			if again.Changed {
				t.Fatalf("second apply changed the file:\n%s", again.Content)
			}
			rm := plan(t, tc.kind, OpRemove, got)
			if !rm.Changed {
				t.Fatalf("remove changed nothing: %v", rm.Summary)
			}
			if string(rm.Content) != tc.in {
				t.Fatalf("remove did not restore the original bytes:\n got %q\nwant %q", rm.Content, tc.in)
			}
			if Wired(tc.kind, rm.Content, Endpoint(addr)) {
				t.Fatal("Wired is true after remove")
			}
		})
	}
}

// TestMissingFileGetsAMinimalDocument pins the create case.
func TestMissingFileGetsAMinimalDocument(t *testing.T) {
	for _, k := range []integration.OTLPWireKind{integration.OTLPWireEnvSettingsJSON, integration.OTLPWireTelemetryJSON, integration.OTLPWireCodexTOML} {
		r := plan(t, k, OpApply, "")
		if !r.Changed || !Wired(k, r.Content, Endpoint(addr)) {
			t.Errorf("%s: apply on a missing file: changed=%v content=%q", k, r.Changed, r.Content)
		}
	}
	env := plan(t, integration.OTLPWireEnvSettingsJSON, OpApply, "")
	var v struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(env.Content, &v); err != nil || v.Env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://"+addr || v.Env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Fatalf("env = %+v (%v)", v, err)
	}
}

// TestUserSettingsAreNeverOverwritten is one row per conflict rule.
func TestUserSettingsAreNeverOverwritten(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind integration.OTLPWireKind
		in   string
	}{
		{"claude: user endpoint elsewhere", integration.OTLPWireEnvSettingsJSON, `{"env": {"OTEL_EXPORTER_OTLP_ENDPOINT": "https://collector.example.com"}}`},
		{"claude: user logs exporter", integration.OTLPWireEnvSettingsJSON, `{"env": {"OTEL_LOGS_EXPORTER": "console"}}`},
		{"gemini: user telemetry to gcp", integration.OTLPWireTelemetryJSON, `{"telemetry": {"enabled": true, "target": "gcp"}}`},
		{"gemini: user telemetry with extra keys", integration.OTLPWireTelemetryJSON, `{"telemetry": {"enabled": true, "target": "local", "otlpEndpoint": "http://127.0.0.1:4318", "otlpProtocol": "http", "logPrompts": false}}`},
		{"codex: user [otel] table", integration.OTLPWireCodexTOML, "[otel]\nexporter = \"none\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := plan(t, tc.kind, OpApply, tc.in)
			if r.Changed || r.Conflict == "" || string(r.Content) != tc.in {
				t.Fatalf("changed=%v conflict=%q", r.Changed, r.Conflict)
			}
			if strings.Contains(r.Conflict, "example.com") || strings.Contains(r.Conflict, "console") || strings.Contains(r.Conflict, "gcp") {
				t.Fatalf("conflict message carries a value from the file: %q", r.Conflict)
			}
			rm := plan(t, tc.kind, OpRemove, tc.in)
			if rm.Changed {
				t.Fatalf("remove touched a user setting: %q", rm.Content)
			}
		})
	}
}

// TestPortChangeUpdatesOwnWiring pins that Observer's own wiring follows a
// receiver port change instead of conflicting with itself.
func TestPortChangeUpdatesOwnWiring(t *testing.T) {
	for _, k := range []integration.OTLPWireKind{integration.OTLPWireTelemetryJSON, integration.OTLPWireCodexTOML} {
		first, _ := Plan(k, OpApply, nil, "127.0.0.1:4318", "")
		moved, err := Plan(k, OpApply, first.Content, "127.0.0.1:5318", Endpoint("127.0.0.1:4318"))
		if err != nil || !moved.Changed || moved.Conflict != "" || !strings.Contains(string(moved.Content), "5318") {
			t.Errorf("%s: port change: %+v %v", k, moved.Summary, err)
		}
	}
}

// TestUserLoopbackCollectorIsNotObservers pins P3-12 (c): ownership is the
// recorded endpoint, never "any loopback URL". A user's own local collector
// (a loopback endpoint Observer has no record of installing) is a conflict on
// apply, untouched by remove and not reported as wired, even when its port
// differs from the receiver's by one.
func TestUserLoopbackCollectorIsNotObservers(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind integration.OTLPWireKind
		in   string
	}{
		{"claude env to a local collector", integration.OTLPWireEnvSettingsJSON, `{"env": {"CLAUDE_CODE_ENABLE_TELEMETRY": "1", "OTEL_LOGS_EXPORTER": "otlp", "OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4317"}}`},
		{"gemini telemetry to a local collector", integration.OTLPWireTelemetryJSON, `{"telemetry": {"enabled": true, "target": "local", "otlpEndpoint": "http://localhost:4317", "otlpProtocol": "http"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, owned := range []string{"", Endpoint(addr)} {
				ap, err := Plan(tc.kind, OpApply, []byte(tc.in), addr, owned)
				if err != nil || ap.Changed || ap.Conflict == "" {
					t.Fatalf("owned=%q apply over a user collector: changed=%v conflict=%q err=%v", owned, ap.Changed, ap.Conflict, err)
				}
				rm, err := Plan(tc.kind, OpRemove, []byte(tc.in), addr, owned)
				if err != nil || rm.Changed {
					t.Fatalf("owned=%q remove touched a user collector: %q", owned, rm.Content)
				}
				if Wired(tc.kind, []byte(tc.in), owned) {
					t.Fatalf("owned=%q a user collector reads as Observer's wiring", owned)
				}
			}
		})
	}
}

// TestRemoveWithoutRecordKeepsEverything pins that a removal with no
// ownership record touches nothing, even a value equal to what Observer
// would write today.
func TestRemoveWithoutRecordKeepsEverything(t *testing.T) {
	for _, k := range []integration.OTLPWireKind{integration.OTLPWireEnvSettingsJSON, integration.OTLPWireTelemetryJSON} {
		ap, _ := Plan(k, OpApply, nil, addr, "")
		rm, err := Plan(k, OpRemove, ap.Content, addr, "")
		if err != nil || rm.Changed {
			t.Errorf("%s: remove without a record changed the file: %q", k, rm.Content)
		}
	}
}

// TestReceiverAddrIsAlwaysLoopback pins P3-12 (b): the endpoint written into
// a tool's config is a loopback host and the bind's port, never the bind
// address itself.
func TestReceiverAddrIsAlwaysLoopback(t *testing.T) {
	for _, tc := range []struct {
		bind, want string
		bad        bool
	}{
		{bind: "127.0.0.1:4318", want: "127.0.0.1:4318"},
		{bind: "0.0.0.0:4318", want: "127.0.0.1:4318"},
		{bind: ":4318", want: "127.0.0.1:4318"},
		{bind: "[::]:4318", want: "127.0.0.1:4318"},
		{bind: "localhost:4318", want: "127.0.0.1:4318"},
		{bind: "[::1]:4318", want: "[::1]:4318"},
		{bind: "192.168.1.5:4318", bad: true},
		{bind: "127.0.0.1:0", bad: true},
		{bind: "127.0.0.1", bad: true},
		{bind: "", bad: true},
	} {
		got, err := ReceiverAddr(tc.bind)
		if tc.bad {
			if err == nil {
				t.Errorf("ReceiverAddr(%q) = %q, want an error", tc.bind, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ReceiverAddr(%q) = %q, %v; want %q", tc.bind, got, err, tc.want)
		}
	}
}
