package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/telemetry"
	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// selfHostRegisterServer mimics the self-hosted observer_reporter
// POST /v1/register handler: the only request field is enrollment_code, an
// empty code is 400 enrollment_required, an unknown one 403
// enrollment_invalid.
func selfHostRegisterServer(t *testing.T, goodCode string, got *map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/register" {
			http.NotFound(w, r)
			return
		}
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		*got = req
		w.Header().Set("Content-Type", "application/json")
		switch code := strings.TrimSpace(req["enrollment_code"]); {
		case code == "":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"enrollment_required","detail":"enrollment code required"}`))
		case code != goodCode:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"enrollment_invalid","detail":"unknown code"}`))
		default:
			_, _ = w.Write([]byte(`{"install_id":"inst-0123456789ab","install_token":"tok_test"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func registerTestConfig(t *testing.T, endpoint string) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Observer.DBPath = filepath.Join(t.TempDir(), "obs.db")
	cfg.Telemetry.Endpoint = endpoint
	return cfg
}

// TestTelemetryRegisterEnrollmentCode pins register against a self-hosted
// telemetry server: --code is sent as enrollment_code, and the server's
// enrollment_required refusal becomes a message that says a code is needed
// and who issues it (it used to print "server returned no identity").
func TestTelemetryRegisterEnrollmentCode(t *testing.T) {
	var got map[string]string
	srv := selfHostRegisterServer(t, "enr-good", &got)

	cfg := registerTestConfig(t, srv.URL)
	_, _, err := telemetryRegister(cfg, "")
	if err == nil || !strings.Contains(err.Error(), "enrollment code") || !strings.Contains(err.Error(), "operator") {
		t.Fatalf("refusal error = %v, want a message naming the enrollment code and the operator", err)
	}
	if strings.Contains(err.Error(), "no identity") {
		t.Errorf("refusal still reports %q", err)
	}
	if _, serr := os.Stat(telemetryTokenPath(cfg)); serr == nil {
		t.Error("a refused register stored a token")
	}

	if _, _, err := telemetryRegister(cfg, "enr-bad"); err == nil || !strings.Contains(err.Error(), "refused the enrollment code") {
		t.Errorf("invalid code error = %v", err)
	}

	token, iid, err := telemetryRegister(cfg, " enr-good ")
	if err != nil {
		t.Fatalf("register with a good code: %v", err)
	}
	if got["enrollment_code"] != "enr-good" {
		t.Errorf("request carried %v, want enrollment_code=enr-good", got)
	}
	if token != "tok_test" || iid != "inst-0123456789ab" {
		t.Errorf("identity = %q/%q", token, iid)
	}
	if b, _ := os.ReadFile(telemetryTokenPath(cfg)); strings.TrimSpace(string(b)) != "tok_test" {
		t.Errorf("stored token = %q", b)
	}
}

// TestConsentNeedsServer pins which consent changes wait for the server:
// only a raise above baseline. Turning the default rung back on after
// `consent off` used to fail whenever the telemetry server was unreachable.
func TestConsentNeedsServer(t *testing.T) {
	for _, tc := range []struct {
		prev, next string
		want       bool
	}{
		{"off", "baseline", false},
		{"off", "usage", false},
		{"error", "baseline", false},
		{"baseline", "off", false},
		{"deep", "baseline", false},
		{"enrichment", "baseline", false},
		{"off", "enrichment", true},
		{"baseline", "enrichment", true},
		{"enrichment", "enrichment", false},
	} {
		if got := consentNeedsServer(tc.prev, tc.next); got != tc.want {
			t.Errorf("consentNeedsServer(%q, %q) = %v, want %v", tc.prev, tc.next, got, tc.want)
		}
	}
}

// TestReporterSenderSyncsLocalTier pins that a node registered with a
// self-hosted reporter tells it the local base tier before the first batch.
// The reporter registers installs at off and refuses their batches, and a
// default (baseline) install never runs a consent command, so before this
// every batch of a freshly registered default install was refused.
func TestReporterSenderSyncsLocalTier(t *testing.T) {
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/consent" {
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if r.Header.Get("Authorization") != "Bearer tok_test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			posts = append(posts, req["tier"])
			_, _ = w.Write([]byte(`{"tier":"` + req["tier"] + `"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	cfg := registerTestConfig(t, srv.URL)
	if err := telemetryStoreToken(cfg, "tok_test"); err != nil {
		t.Fatal(err)
	}
	collector := telemetry.NewCollector(nil, telemetry.CollectorConfig{Tier: contract.TierBaseline})
	sender, err := newTelemetrySender(cfg, collector, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	rs, ok := sender.(*reporterSender)
	if !ok {
		t.Fatalf("sender is %T, want the reporter sender (an install token is stored)", sender)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := rs.prepare(ctx); err != nil {
			t.Fatalf("prepare %d: %v", i, err)
		}
	}
	if len(posts) != 1 || posts[0] != "baseline" {
		t.Fatalf("consent posts = %v, want exactly one, baseline", posts)
	}
	// A lowered tier is told once; off and deep are not synced here.
	collector.SetTier(contract.TierOff)
	if err := rs.prepare(ctx); err != nil {
		t.Fatal(err)
	}
	collector.SetTier(contract.TierDeep)
	if err := rs.prepare(ctx); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 {
		t.Fatalf("off or deep was posted: %v", posts)
	}
}
