package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/llmconn"
	"github.com/marmutapp/superbased-observer/internal/llmkeyring"
)

// handleConfigLLMKey serves /api/config/llm/key/<id> — the seam that
// stores an LLM analysis API key OUTSIDE config.toml.
//
//   - PUT {api_key}: writes the literal to the OS keychain (0600-file
//     fallback) under a label equal to the connection id, then sets the
//     connection's api_key_keychain ref to that label and saves config.
//     The literal NEVER reaches config.toml (validateSecretRef in
//     validateLLM rejects paste-through values at the next load).
//   - DELETE: removes the keychain entry and clears the api_key_keychain
//     ref.
//
// Owner-local (CapabilityLocal): writes the keychain and the shared
// config file, so a remote principal must never reach it.
func (s *Server) handleConfigLLMKey(w http.ResponseWriter, r *http.Request) {
	if s.opts.ConfigPath == "" {
		http.Error(w, "config path not configured — server has no file to save to", http.StatusConflict)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/config/llm/key/")
	if id == "" || strings.ContainsAny(id, "/\\") {
		http.Error(w, "connection id required (e.g. /api/config/llm/key/freellmapi)", http.StatusBadRequest)
		return
	}

	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()

	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	conn, ok := cfg.LLM.Connections[id]
	if !ok {
		http.Error(w, "unknown connection "+id+" — create it via the LLM section save first", http.StatusNotFound)
		return
	}

	store := s.llmKeyStore()
	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		var req struct {
			APIKey string `json:"api_key"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.APIKey) == "" {
			http.Error(w, "api_key is required", http.StatusBadRequest)
			return
		}
		if err := store.Save(id, req.APIKey); err != nil {
			writeErr(w, err)
			return
		}
		conn.APIKeyKeychain = id
		cfg.LLM.Connections[id] = conn
		if err := writeConfigToml(s.opts.ConfigPath, cfg); err != nil {
			writeErr(w, err)
			return
		}
		s.notifyConfigSaved()
		writeJSON(w, map[string]any{
			"saved":            true,
			"connection_id":    id,
			"api_key_keychain": id,
			"backend":          store.Backend(),
		})
	case http.MethodDelete:
		if err := store.Delete(id); err != nil && !errors.Is(err, llmkeyring.ErrNoKey) {
			writeErr(w, err)
			return
		}
		conn.APIKeyKeychain = ""
		cfg.LLM.Connections[id] = conn
		if err := writeConfigToml(s.opts.ConfigPath, cfg); err != nil {
			writeErr(w, err)
			return
		}
		s.notifyConfigSaved()
		writeJSON(w, map[string]any{
			"saved":            true,
			"connection_id":    id,
			"api_key_keychain": "",
			"backend":          store.Backend(),
		})
	default:
		http.Error(w, "PUT or DELETE only", http.StatusMethodNotAllowed)
	}
}

// handleConfigLLMTest serves POST /api/config/llm/test/<id> — the
// Settings LLM card's Test button. Resolves the connection's credential
// (env ref, then OS-keychain ref) and runs llmconn.Client.Test against
// the endpoint: reachability + the available-model list. Loopback
// connections are keyless. Never writes anything.
func (s *Server) handleConfigLLMTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/config/llm/test/")
	if id == "" || strings.ContainsAny(id, "/\\") {
		http.Error(w, "connection id required (e.g. /api/config/llm/test/freellmapi)", http.StatusBadRequest)
		return
	}
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	var conn llmconn.Connection
	found := false
	for _, c := range cfg.LLM.ToConnections() {
		if c.ID == id {
			conn, found = c, true
			break
		}
	}
	if !found {
		http.Error(w, "unknown connection "+id, http.StatusNotFound)
		return
	}

	hv := os.Getenv(conn.APIKeyEnv)
	if hv == "" && conn.APIKeyKeychain != "" {
		v, err := s.llmKeyStore().Load(conn.APIKeyKeychain)
		if err != nil {
			writeJSON(w, map[string]any{
				"ok":     false,
				"health": llmconn.Health{OK: false, Message: "keychain load: " + err.Error()},
				"models": []string{},
			})
			return
		}
		hv = v
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	h, models := llmconn.NewClient(conn, hv).Test(ctx)
	writeJSON(w, map[string]any{"ok": h.OK, "health": h, "models": models})
}

// llmKeyStore returns the injected store, or opens one rooted at the
// config dir (the same store the runner resolves api_key_keychain refs
// from in cmd/observer/llm.go).
func (s *Server) llmKeyStore() *llmkeyring.Store {
	if s.opts.LLMKeyStore != nil {
		return s.opts.LLMKeyStore
	}
	return llmkeyring.Open(filepath.Dir(s.opts.ConfigPath))
}
