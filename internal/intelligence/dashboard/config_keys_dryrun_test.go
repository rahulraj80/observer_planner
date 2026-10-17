package dashboard

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// The config.toml dry run and secret posture (adapter-config plan section
// 5.5): the UI previews every save first; nothing is written by a preview,
// and a credential never appears in its diff.

func TestConfigKeys_DryRunWritesNothingAndDiffs(t *testing.T) {
	s, path := newSecretTestServer(t, keysCorpus)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	etag, _, _ := configGet(t, s)
	rr := putKeys(t, s, `{"base_etag":"`+etag+`","dry_run":true,"patches":[{"key":"observer.log_level","value":"debug"},{"key":"observer.secrets.enable_scrubbing","value":false}]}`, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("dry run: %d %s", rr.Code, rr.Body.String())
	}
	m := decodeMap(t, rr)
	if m["dry_run"] != true || m["would_write"] != true || m["needs_confirm"] != true {
		t.Fatalf("dry run flags: %v", m)
	}
	diff, _ := m["diff"].(string)
	if !strings.Contains(diff, `+log_level = "debug"`) || !strings.Contains(diff, `-log_level = "warn"`) {
		t.Fatalf("diff:\n%s", diff)
	}
	if got := strs(m["changed_keys"]); strings.Join(got, ",") != "observer.log_level,observer.secrets.enable_scrubbing" {
		t.Fatalf("changed_keys = %v", got)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("a dry run must not touch the file")
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Fatal("a dry run must not write a backup")
	}
	// The real save of a sensitive key still demands the confirm token.
	rr = putKeys(t, s, keysBody(etag, `{"key":"observer.secrets.enable_scrubbing","value":false}`), false)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("sensitive save without token: %d", rr.Code)
	}
}

func TestConfigKeys_DryRunMasksCredentials(t *testing.T) {
	corpus := keysCorpus + `
[email]
password = "plain-config-password"
`
	s, _ := newSecretTestServer(t, corpus)
	etag, _, _ := configGet(t, s)
	// A list edit re-serializes the whole file, so every line, including the
	// credential, lands in the diff: it must be masked on both sides.
	rr := putKeys(t, s, `{"base_etag":"`+etag+`","dry_run":true,"patches":[{"key":"terminal.launch.allowed_tools","value":["claude","codex"]}]}`, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("dry run: %d %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "plain-config-password") {
		t.Fatalf("dry-run response leaks the credential: %s", rr.Body.String())
	}
}

func TestConfigKeys_SecretShapedValueWarns(t *testing.T) {
	s, _ := newSecretTestServer(t, keysCorpus)
	etag, _, _ := configGet(t, s)
	secretish := "sk-ant-api03-" + strings.Repeat("A", 80)
	rr := putKeys(t, s, `{"base_etag":"`+etag+`","dry_run":true,"patches":[{"key":"profiles.default","value":"`+secretish+`"}]}`, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("dry run: %d %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), secretish) {
		t.Fatal("the response echoes the credential-shaped value")
	}
	m := decodeMap(t, rr)
	warns, _ := m["secret_warnings"].([]any)
	if len(warns) != 1 {
		t.Fatalf("want one secret warning: %v", m["secret_warnings"])
	}
}

func TestConfigSection_ReadBackRestoresOnFailure(t *testing.T) {
	s, path := newSecretTestServer(t, keysCorpus)
	before, _ := os.ReadFile(path)
	orig := readBackConfig
	t.Cleanup(func() { readBackConfig = orig })
	readBackConfig = func(string) (config.Config, error) { return config.Config{}, errors.New("forced read-back failure") }
	req := httptest.NewRequest(http.MethodPut, "/api/config/section/intelligence",
		strings.NewReader(`{"MonthlyBudgetUSD": 75, "APIKeyEnv": "", "SummaryModel": ""}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), "read_back_failed") {
		t.Fatalf("section save with a failing read-back: %d %s", rr.Code, rr.Body.String())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatalf("the previous file must be restored:\n%s", after)
	}
}
