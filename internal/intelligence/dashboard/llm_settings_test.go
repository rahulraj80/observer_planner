package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/llmkeyring"
)

// writeLLMTestConfig writes a config.toml carrying one loopback LLM
// connection (id "loop") and returns the opened test server.
func writeLLMTestConfig(t *testing.T, tdir, llmBlock string) (*Server, string) {
	t.Helper()
	cfgPath := filepath.Join(tdir, "config.toml")
	body := "[observer]\nlog_level = \"warn\"\n\n"
	if llmBlock != "" {
		body += llmBlock
	}
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(context.Background(), db.Options{Path: filepath.Join(tdir, "d.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	server, err := New(Options{
		DB:          database,
		ConfigPath:  cfgPath,
		LLMKeyStore: llmkeyring.OpenDir(tdir),
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, cfgPath
}

const loopLLMBlock = `[llm.connections.loop]
name = "loop"
kind = "openai_compat"
base_url = "http://127.0.0.1:1/v1"
enabled = true
`

// TestConfigSectionLLM_RoundTrip verifies PUT /api/config/section/llm
// writes a Connections map and GET /api/config returns it — the section
// the Settings LLM card saves through.
func TestConfigSectionLLM_RoundTrip(t *testing.T) {
	tdir := t.TempDir()
	server, _ := writeLLMTestConfig(t, tdir, "")

	body := `{"Connections":{"freellmapi":{"Name":"FreeLLM API","Kind":"openai_compat","BaseURL":"http://localhost:3001/v1","APIKeyEnv":"FREELMAPI_KEY","Models":["m1","m2"],"DefaultModel":"m1","TaskRoles":["narrative","ask"],"Enabled":true,"AllowRemote":false,"TimeoutMS":0,"MaxTokens":0,"NumCtx":0}}}`
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/config/section/llm", strings.NewReader(body)))
	if rr.Code != 200 {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != 200 {
		t.Fatalf("GET status: %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Config config.Config `json:"config"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	conn, ok := got.Config.LLM.Connections["freellmapi"]
	if !ok {
		t.Fatalf("connection not persisted: %+v", got.Config.LLM.Connections)
	}
	if conn.Kind != "openai_compat" || conn.BaseURL != "http://localhost:3001/v1" || conn.DefaultModel != "m1" {
		t.Errorf("bad round-trip: %+v", conn)
	}
}

// TestConfigSectionLLM_RejectsInvalid verifies a section save that would
// brick the next config load (unknown kind) is refused with the file
// untouched — the config.Validate gate at the section save.
func TestConfigSectionLLM_RejectsInvalid(t *testing.T) {
	tdir := t.TempDir()
	server, cfgPath := writeLLMTestConfig(t, tdir, "")
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"Connections":{"bad":{"Kind":"weird","BaseURL":"http://x"}}}`
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/config/section/llm", strings.NewReader(body)))
	if rr.Code != 400 {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("config.toml was modified by a rejected save")
	}
}

// TestConfigLLMKey_SaveAndDelete verifies the key seam: PUT stores the
// literal in the keychain store and writes the api_key_keychain ref (never
// the literal) to config.toml; DELETE clears both.
func TestConfigLLMKey_SaveAndDelete(t *testing.T) {
	tdir := t.TempDir()
	server, cfgPath := writeLLMTestConfig(t, tdir, loopLLMBlock)

	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/config/llm/key/loop", strings.NewReader(`{"api_key":"sk-sekrit"}`)))
	if rr.Code != 200 {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	var putResp struct {
		Saved          bool   `json:"saved"`
		APIKeyKeychain string `json:"api_key_keychain"`
		Backend        string `json:"backend"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&putResp); err != nil {
		t.Fatal(err)
	}
	if !putResp.Saved || putResp.APIKeyKeychain != "loop" {
		t.Errorf("bad put response: %+v", putResp)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-sekrit") {
		t.Error("config.toml contains the key literal — the seam must only store the ref")
	}
	if !strings.Contains(string(raw), `api_key_keychain = "loop"`) {
		t.Errorf("config.toml missing the keychain ref:\n%s", raw)
	}
	if v, err := llmkeyring.OpenDir(tdir).Load("loop"); err != nil || v != "sk-sekrit" {
		t.Errorf("keychain store: got %q err %v", v, err)
	}

	rr = httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/config/llm/key/loop", nil))
	if rr.Code != 200 {
		t.Fatalf("delete status: %d body=%s", rr.Code, rr.Body.String())
	}
	raw, _ = os.ReadFile(cfgPath)
	if strings.Contains(string(raw), `api_key_keychain = "loop"`) {
		t.Errorf("delete did not clear the ref:\n%s", raw)
	}
	if _, err := llmkeyring.OpenDir(tdir).Load("loop"); err == nil {
		t.Error("key still in store after delete")
	}
}

// TestConfigLLMKey_UnknownConnection verifies the key seam 404s for an id
// the config does not carry (the section save creates connections; the key
// seam only attaches a key to an existing one).
func TestConfigLLMKey_UnknownConnection(t *testing.T) {
	tdir := t.TempDir()
	server, _ := writeLLMTestConfig(t, tdir, loopLLMBlock)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/config/llm/key/nope", strings.NewReader(`{"api_key":"x"}`)))
	if rr.Code != 404 {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestConfigLLMTest_Loopback verifies the Test button path: a loopback
// connection is keyless, probes GET /v1/models on the endpoint, and
// returns the discovered model list.
func TestConfigLLMTest_Loopback(t *testing.T) {
	var gotModels bool
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path: %s", r.URL.Path)
		}
		gotModels = true
		w.Write([]byte(`{"data":[{"id":"m1"},{"id":"m2"}]}`))
	}))
	defer up.Close()

	tdir := t.TempDir()
	block := fmt.Sprintf("[llm.connections.loop]\nkind = \"openai_compat\"\nbase_url = %q\nenabled = true\n", up.URL+"/v1")
	server, _ := writeLLMTestConfig(t, tdir, block)

	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/config/llm/test/loop", nil))
	if rr.Code != 200 {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		OK     bool `json:"ok"`
		Health struct {
			OK      bool   `json:"ok"`
			Message string `json:"message"`
		} `json:"health"`
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || !got.Health.OK || !gotModels {
		t.Errorf("probe failed: ok=%v health=%+v models=%v", got.OK, got.Health, got.Models)
	}
	if len(got.Models) != 2 || got.Models[0] != "m1" {
		t.Errorf("models: %v", got.Models)
	}
}

// TestConfigLLMTest_KeychainCred verifies the Test button resolves an
// OS-keychain credential ref and sends it as the Authorization header.
func TestConfigLLMTest_KeychainCred(t *testing.T) {
	var auth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Write([]byte(`{"data":[]}`))
	}))
	defer up.Close()

	tdir := t.TempDir()
	block := fmt.Sprintf("[llm.connections.loop]\nkind = \"openai_compat\"\nbase_url = %q\napi_key_keychain = \"loop\"\nenabled = true\n", up.URL+"/v1")
	server, _ := writeLLMTestConfig(t, tdir, block)
	if err := llmkeyring.OpenDir(tdir).Save("loop", "sk-keychain"); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/config/llm/test/loop", nil))
	if rr.Code != 200 {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.OK {
		t.Errorf("probe failed: %s", rr.Body.String())
	}
	if auth != "Bearer sk-keychain" {
		t.Errorf("Authorization: %q, want Bearer sk-keychain", auth)
	}
}
