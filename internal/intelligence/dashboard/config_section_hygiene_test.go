package dashboard

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// T-432: PUT /api/config/section/<id> runs through the ONE config.toml save
// pipeline the keys route uses (config_commit.go): dry run with a masked
// diff, secret warnings, the sensitive-tier confirm token, the optional
// base_etag conflict check, a no-op that does not touch the file, and an
// audit row naming keys, never values.

// putSection sends PUT /api/config/section/<path>. confirm=true echoes the
// owner-local confirm token the way the web helper does.
func putSection(t *testing.T, s *Server, path, body string, confirm bool) *httptest.ResponseRecorder {
	t.Helper()
	_, token, cookie := configGet(t, s)
	req := httptest.NewRequest(http.MethodPut, "/api/config/section/"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if confirm {
		req.Header.Set(remoteConfirmHeader, token)
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	return rr
}

// confirmedReq adds the JSON content type and the owner-local confirm token
// (minted by GET /api/config) that the web's saveConfigSection sends on
// every section save. A section save that changes a sensitive-tier key
// demands it since T-432, like the keys route always has.
func confirmedReq(t *testing.T, s *Server, req *http.Request) *http.Request {
	t.Helper()
	_, token, cookie := configGet(t, s)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(remoteConfirmHeader, token)
	req.AddCookie(cookie)
	return req
}

func TestConfigSection_DryRunWritesNothingAndDiffs(t *testing.T) {
	s, path := newSecretTestServer(t, keysCorpus)
	before, _ := os.ReadFile(path)
	rr := putSection(t, s, "intelligence?dry_run=1", `{"MonthlyBudgetUSD": 75, "APIKeyEnv": "", "SummaryModel": ""}`, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("dry run: %d %s", rr.Code, rr.Body.String())
	}
	m := decodeMap(t, rr)
	if m["dry_run"] != true || m["would_write"] != true || m["section"] != "intelligence" {
		t.Fatalf("dry run flags: %v", m)
	}
	if diff, _ := m["diff"].(string); !strings.Contains(diff, "+") || !strings.Contains(diff, "75") {
		t.Fatalf("dry run must carry the diff of the write:\n%v", m["diff"])
	}
	if got := strs(m["changed_keys"]); strings.Join(got, ",") != "intelligence.monthly_budget_usd" {
		t.Fatalf("changed_keys = %v", got)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("a section dry run must not touch the file")
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Fatal("a section dry run must not write a backup")
	}
}

func TestConfigSection_SecretShapedValueWarnsAndIsMasked(t *testing.T) {
	s, _ := newSecretTestServer(t, keysCorpus)
	secretish := "sk-ant-api03-" + strings.Repeat("A", 80)
	body := `{"MonthlyBudgetUSD": 0, "APIKeyEnv": "", "SummaryModel": "` + secretish + `"}`
	rr := putSection(t, s, "intelligence?dry_run=1", body, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("dry run: %d %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), secretish) {
		t.Fatal("the dry run echoes the credential-shaped value")
	}
	m := decodeMap(t, rr)
	warns, _ := m["secret_warnings"].([]any)
	if len(warns) != 1 || !strings.Contains(rr.Body.String(), "intelligence.summary_model") {
		t.Fatalf("want one secret warning naming the key: %v", m["secret_warnings"])
	}
	// The real save (individual node: warn posture) lands and still reports it.
	rr = putSection(t, s, "intelligence", body, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rr.Code, rr.Body.String())
	}
	if warns, _ := decodeMap(t, rr)["secret_warnings"].([]any); len(warns) != 1 {
		t.Fatalf("the save must report the secret warning too: %s", rr.Body.String())
	}
}

func TestConfigSection_SensitiveKeyNeedsConfirm(t *testing.T) {
	s, path := newSecretTestServer(t, keysCorpus)
	before, _ := os.ReadFile(path)
	body := `{"Addr":"127.0.0.1:8099"}`

	// The preview needs no token and says one is needed.
	rr := putSection(t, s, "dashboard?dry_run=1", body, false)
	if rr.Code != http.StatusOK || decodeMap(t, rr)["needs_confirm"] != true {
		t.Fatalf("dry run of a sensitive key: %d %s", rr.Code, rr.Body.String())
	}
	// The save without the token is refused and writes nothing.
	rr = putSection(t, s, "dashboard", body, false)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("sensitive section save without token: %d %s", rr.Code, rr.Body.String())
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("a refused save must not touch the file")
	}
	// With the token it lands.
	rr = putSection(t, s, "dashboard", body, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("sensitive section save with token: %d %s", rr.Code, rr.Body.String())
	}
	if after, _ := os.ReadFile(path); !strings.Contains(string(after), "127.0.0.1:8099") {
		t.Fatalf("addr not written:\n%s", after)
	}
}

func TestConfigSection_BaseEtagConflict(t *testing.T) {
	s, path := newSecretTestServer(t, keysCorpus)
	before, _ := os.ReadFile(path)
	rr := putSection(t, s, "intelligence?base_etag=sha256:stale", `{"MonthlyBudgetUSD": 75}`, false)
	if rr.Code != http.StatusConflict || decodeMap(t, rr)["error"] != "config_changed" {
		t.Fatalf("stale base_etag: %d %s", rr.Code, rr.Body.String())
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("a conflicting save must not touch the file")
	}
	// The etag the preview returned is accepted.
	rr = putSection(t, s, "intelligence?dry_run=1", `{"MonthlyBudgetUSD": 75}`, false)
	etag, _ := decodeMap(t, rr)["config_etag"].(string)
	if etag == "" {
		t.Fatalf("dry run must return the etag: %s", rr.Body.String())
	}
	rr = putSection(t, s, "intelligence?base_etag="+etag, `{"MonthlyBudgetUSD": 75}`, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("save at the previewed etag: %d %s", rr.Code, rr.Body.String())
	}
}

func TestConfigSection_UnchangedSaveIsNoopAndAudited(t *testing.T) {
	s, path := newSecretTestServer(t, keysCorpus)
	body := `{"MonthlyBudgetUSD": 75, "APIKeyEnv": "", "SummaryModel": ""}`
	if rr := putSection(t, s, "intelligence", body, false); rr.Code != http.StatusOK {
		t.Fatalf("first save: %d %s", rr.Code, rr.Body.String())
	}
	written, _ := os.ReadFile(path)
	rr := putSection(t, s, "intelligence", body, false)
	if rr.Code != http.StatusOK {
		t.Fatalf("repeat save: %d %s", rr.Code, rr.Body.String())
	}
	if m := decodeMap(t, rr); m["write_mode"] != "noop" {
		t.Fatalf("an unchanged section save must be a no-op: %v", m)
	}
	if again, _ := os.ReadFile(path); !bytes.Equal(written, again) {
		t.Fatal("a no-op save must not rewrite the file")
	}
	rows, err := store.New(s.opts.DB).RecentRemoteAudit(context.Background(), 5)
	if err != nil || len(rows) == 0 {
		t.Fatalf("audit rows: %v %v", rows, err)
	}
	var found bool
	for _, row := range rows {
		if strings.Contains(row.Detail, "config_section") && strings.Contains(row.Detail, "section=intelligence") &&
			strings.Contains(row.Detail, "intelligence.monthly_budget_usd") {
			found = true
		}
		if strings.Contains(row.Detail, "75") {
			t.Errorf("audit detail must carry key names, not values: %q", row.Detail)
		}
	}
	if !found {
		t.Fatalf("no config_section audit row: %+v", rows)
	}
}
