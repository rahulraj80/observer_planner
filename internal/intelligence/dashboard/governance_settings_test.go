//go:build !sbo_omit_remote

package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/configschema"
	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

// TestEverySettingsRouteDeclaresItsSubsection is the exhaustive gate behind
// Sol A3-01: every route registered under the Settings page declares the
// Settings subsection that owns it (SectionSettings.WithSettings), or is in the
// closed settingsSharedRoutes list with the rule that governs it instead. A
// declared subsection must be a real vocabulary id, and a shared-list row must
// still name a registered Settings route, so neither side can rot.
func TestEverySettingsRouteDeclaresItsSubsection(t *testing.T) {
	s := newRemoteTestServer(t, Options{})
	_, _, sections := s.registerRoutes(nil)
	vocab := map[string]bool{}
	for _, id := range nodegov.SettingsSectionIDs {
		vocab[id] = true
	}
	settingsRoutes := 0
	for pattern, reg := range sections {
		nav, sub := reg.Split()
		if sub != "" && !vocab[sub] {
			t.Errorf("route %q declares Settings subsection %q, which is not in nodegov.SettingsSectionIDs", pattern, sub)
		}
		if nav != SectionSettings {
			continue
		}
		settingsRoutes++
		if sub == "" {
			if _, shared := settingsSharedRoutes[pattern]; !shared {
				t.Errorf("Settings route %q declares no subsection and is not in settingsSharedRoutes: a hidden or read-only Settings section would not govern it", pattern)
			}
		} else if _, shared := settingsSharedRoutes[pattern]; shared {
			t.Errorf("route %q both declares subsection %q and is listed as shared", pattern, sub)
		}
	}
	for pattern := range settingsSharedRoutes {
		nav, sub := sections[pattern].Split()
		if nav != SectionSettings || sub != "" {
			t.Errorf("settingsSharedRoutes names %q, which is not a registered Settings route without a subsection (got %q)", pattern, sections[pattern])
		}
	}
	if settingsRoutes < 60 {
		t.Fatalf("only %d Settings routes registered; the registry may not be wired", settingsRoutes)
	}
}

// TestSettingsSubsectionRoutesRefusedOnBothStacks is the Sol A3-01
// regression: the Settings APIs that used to escape the table (storage, health,
// backfill, shell wrapping, retention's prune, pricing's reprice, connected
// tools and the install ledger) are refused when their subsection is hidden or
// read-only, on the loopback listener AND the remotely exposed one.
func TestSettingsSubsectionRoutesRefusedOnBothStacks(t *testing.T) {
	rc, _ := newReadyRemoteController(t)
	s := newRemoteTestServer(t, Options{
		Remote: rc,
		Governance: governedProvider(t, nil, nil,
			[]string{"storage", "health", "backfill", "terminal"},
			[]string{"retention", "pricing", "tools"}),
	})
	stacks := []struct {
		name string
		h    http.Handler
		req  func(method, path string) *http.Request
	}{
		{"loopback", s.guardedHandler("127.0.0.1:8081"), loopbackRequest},
		{"remote", s.remoteGuardedHandler(rc), func(method, path string) *http.Request {
			r := httptest.NewRequest(method, path, nil)
			r.Host = testRemoteHost
			r.Header.Set("Origin", "https://"+testRemoteHost)
			return r
		}},
	}
	cases := []struct{ method, path, want string }{
		{http.MethodGet, "/api/storage", "governance_hidden"},
		{http.MethodPost, "/api/storage/vacuum", "governance_hidden"},
		{http.MethodPost, "/api/storage/backup", "governance_hidden"},
		{http.MethodGet, "/api/health/doctor", "governance_hidden"},
		{http.MethodGet, "/api/health/failures", "governance_hidden"},
		{http.MethodGet, "/api/backfill/status", "governance_hidden"},
		{http.MethodPost, "/api/backfill/run", "governance_hidden"},
		{http.MethodGet, "/api/backfill/jobs", "governance_hidden"},
		{http.MethodPost, "/api/scan/run", "governance_hidden"},
		{http.MethodGet, "/api/shell-wrap/status", "governance_hidden"},
		{http.MethodPost, "/api/shell-wrap/apply", "governance_hidden"},
		{http.MethodPost, "/api/prune/run", "governance_read_only"},
		{http.MethodPost, "/api/reprice/apply", "governance_read_only"},
		{http.MethodPost, "/api/reprice/revert", "governance_read_only"},
		{http.MethodPost, "/api/tools/launch", "governance_read_only"},
		{http.MethodPost, "/api/terminal/install", "governance_read_only"},
		{http.MethodPost, "/api/terminal/installs/forget", "governance_read_only"},
		{http.MethodGet, "/api/config/backup", "governance_hidden"},
		// Reads of a read-only subsection, and the developer-own update banner,
		// are not governance's to refuse.
		{http.MethodGet, "/api/reprice/status", ""},
		{http.MethodGet, "/api/tools/status", ""},
		{http.MethodGet, "/api/update/status", ""},
	}
	for _, st := range stacks {
		for _, tc := range cases {
			rec := httptest.NewRecorder()
			st.h.ServeHTTP(rec, st.req(tc.method, tc.path))
			if got := governanceRefusalCode(t, rec); got != tc.want {
				t.Errorf("%s: %s %s refusal = %q, want %q (status %d, body %.200s)", st.name, tc.method, tc.path, got, tc.want, rec.Code, rec.Body.String())
			}
		}
	}
}

// TestConfigReadWithholdsHiddenSettings: GET /api/config is the one read every
// Settings subsection shares, so it withholds the hidden subsections' values
// (Sol A3-01) and names them, while every visible value and the enclosing
// objects stay.
func TestConfigReadWithholdsHiddenSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	hidden := map[string]bool{"pricing": true, "retention": true}
	get := func(t *testing.T, opts Options) map[string]any {
		t.Helper()
		opts.ConfigPath = path
		s := newRemoteTestServer(t, opts)
		rec := httptest.NewRecorder()
		s.guardedHandler("127.0.0.1:8081").ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/config"))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/config = %d (%s)", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}
	has := func(cfg map[string]any, fieldPath string) bool {
		var cur any = cfg
		for _, part := range strings.Split(fieldPath, ".") {
			m, ok := cur.(map[string]any)
			if !ok {
				return false
			}
			if cur, ok = m[part]; !ok {
				return false
			}
		}
		return true
	}

	open := get(t, Options{})
	if _, ok := open["withheld_settings"]; ok {
		t.Fatal("an ungoverned node withheld settings")
	}
	governed := get(t, Options{Governance: governedProvider(t, nil, nil, []string{"pricing", "retention"}, nil)})
	if got := governed["withheld_settings"]; !reflect.DeepEqual(got, []any{"pricing", "retention"}) {
		t.Fatalf("withheld_settings = %v, want [pricing retention]", got)
	}
	openCfg, _ := open["config"].(map[string]any)
	govCfg, _ := governed["config"].(map[string]any)
	cut, kept := 0, 0
	for _, leaf := range configschema.Leaves() {
		if !has(openCfg, leaf.FieldPath) {
			continue
		}
		switch {
		case hidden[leaf.Section] && has(govCfg, leaf.FieldPath):
			t.Errorf("hidden %s value %s is still served", leaf.Section, leaf.FieldPath)
		case hidden[leaf.Section]:
			cut++
		case !has(govCfg, leaf.FieldPath):
			t.Errorf("visible %s value %s was withheld", leaf.Section, leaf.FieldPath)
		default:
			kept++
		}
	}
	if cut == 0 || kept == 0 {
		t.Fatalf("the fixture exercised nothing: %d withheld, %d kept", cut, kept)
	}
	// The enclosing objects stay, so a reader of config.Observer.* or
	// config.Intelligence.* still finds an object.
	for _, obj := range []string{"Observer", "Intelligence"} {
		if _, ok := govCfg[obj].(map[string]any); !ok {
			t.Errorf("config.%s is no longer an object", obj)
		}
	}
}

// TestConfigReadWithholdsHiddenSectionSecrets: the redacted_secrets map says
// which credentials exist. For a HIDDEN Settings section even that presence is
// withheld; a read-only section keeps today's answer (the developer can see
// the section, so they can see that a credential is configured there).
func TestConfigReadWithholdsHiddenSectionSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	secrets := func(t *testing.T, gov GovernanceProvider) map[string]any {
		t.Helper()
		s := newRemoteTestServer(t, Options{ConfigPath: path, Governance: gov})
		rec := httptest.NewRecorder()
		s.guardedHandler("127.0.0.1:8081").ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/config"))
		var resp struct {
			RedactedSecrets map[string]any `json:"redacted_secrets"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &resp) != nil {
			t.Fatalf("GET /api/config = %d (%s)", rec.Code, rec.Body.String())
		}
		return resp.RedactedSecrets
	}
	open := secrets(t, nil)
	for _, key := range []string{"routing.key_pool", "observer.process.etw.token"} {
		if _, ok := open[key]; !ok {
			t.Fatalf("fixture: ungoverned redacted_secrets has no %q (%v)", key, open)
		}
	}
	governed := secrets(t, governedProvider(t, nil, nil, []string{"routing"}, []string{"process"}))
	if _, ok := governed["routing.key_pool"]; ok {
		t.Error("redacted_secrets still reveals routing.key_pool, in the hidden Routing section")
	}
	if _, ok := governed["observer.process.etw.token"]; !ok {
		t.Error("redacted_secrets dropped a read-only section's entry; read-only keeps today's behaviour")
	}
	if len(governed) != len(open)-1 {
		t.Errorf("governed redacted_secrets has %d entries, want %d (only the hidden one dropped)", len(governed), len(open)-1)
	}
}
