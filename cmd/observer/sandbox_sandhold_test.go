//go:build !sbo_omit_terminals_sandbox_sandhold && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/keystore"
	"github.com/marmutapp/superbased-observer/internal/sandbox"
	"github.com/marmutapp/superbased-observer/internal/sandboxbackend"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote/sandhold"
	"github.com/marmutapp/superbased-observer/internal/sandboxselect"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
	"github.com/marmutapp/superbased-observer/internal/termsvc"
)

// sandholdTestKey is the key the fake node grants: no output, record or
// error may carry it.
const sandholdTestKey = "sandhold_feedfacefeedfacefeedfacefeedfacefeedface"

// sandholdFakeNode is an httptest SandHold node scripted per test.
type sandholdFakeNode struct {
	srv     *httptest.Server
	mu      sync.Mutex
	tokens  []string // OAuth codes to answer before the grant ("" grants)
	keyID   string
	revoked []string // "<key>|<id>" per DELETE /v1/keys/{id}
}

func newSandholdFakeNode(t *testing.T, keyID string, tokens ...string) *sandholdFakeNode {
	t.Helper()
	f := &sandholdFakeNode{tokens: tokens, keyID: keyID}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/device/code", func(w http.ResponseWriter, _ *http.Request) {
		sandholdWriteJSON(w, 200, map[string]any{
			"device_code": "dev-secret", "user_code": "BCDF-GHJK",
			"verification_uri": f.srv.URL + "/app/#/device", "verification_uri_complete": f.srv.URL + "/app/#/device?code=BCDF-GHJK",
			"expires_in": 600, "interval": 3,
		})
	})
	mux.HandleFunc("POST /v1/device/token", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		next := ""
		if len(f.tokens) > 0 {
			next, f.tokens = f.tokens[0], f.tokens[1:]
		}
		if next != "" {
			sandholdWriteJSON(w, 400, map[string]string{"error": next, "error_description": "the node says " + next})
			return
		}
		sandholdWriteJSON(w, 200, map[string]any{"access_token": sandholdTestKey, "key_id": f.keyID, "expires_at": "2026-12-11T00:00:00Z", "org": "acme", "user": "alice"})
	})
	mux.HandleFunc("GET /v1/whoami", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != sandholdTestKey {
			sandholdWriteJSON(w, 401, map[string]string{"error": "no valid credential", "fix": "sign in again"})
			return
		}
		sandholdWriteJSON(w, 200, map[string]any{"org": "acme", "user": "alice", "keyId": f.keyID})
	})
	mux.HandleFunc("GET /v1/targets", func(w http.ResponseWriter, _ *http.Request) {
		sandholdWriteJSON(w, 200, map[string]any{"targets": []map[string]any{
			{"id": "jail", "tier": "shared", "tierMeans": "a jailed process on the host kernel", "tierRank": 1, "state": "active", "default": true, "capabilities": map[string]any{"isolation": "process", "egressAllowlist": true}},
			{"id": "gvisor", "tier": "sealed", "tierMeans": "its own user-space kernel", "tierRank": 3, "state": "active", "capabilities": map[string]any{"isolation": "user-kernel", "egressAllowlist": true}},
		}})
	})
	mux.HandleFunc("DELETE /v1/keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.revoked = append(f.revoked, r.Header.Get("X-API-Key")+"|"+r.PathValue("id"))
		f.mu.Unlock()
		w.WriteHeader(204)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func sandholdWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// sandholdTestDeps gives the commands a temporary observer dir and a file
// keystore in it (never the OS keychain), no waiting, and a recorded
// browser opener.
func sandholdTestDeps(t *testing.T) (sandholdDeps, *[]string) {
	t.Helper()
	dir := t.TempDir()
	saved := sandholdKeystoreOpen
	sandholdKeystoreOpen = func(observerDir string) keystore.Store {
		return keystore.NewFileStore(sandhold.KeystoreService, hostedCredDir(observerDir))
	}
	t.Cleanup(func() { sandholdKeystoreOpen = saved })
	var opened []string
	return sandholdDeps{
		observerDir: dir,
		openBrowser: func(u string) { opened = append(opened, u) },
		sleep:       func(context.Context, time.Duration) error { return nil },
		now:         func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) },
		hostname:    func() (string, error) { return "devbox", nil },
		lookupEnv:   func(string) (string, bool) { return "", false },
	}, &opened
}

// TestSandholdConnect covers the sign-in outcomes end to end through the
// command: what is printed, what is stored, and that the key never shows.
func TestSandholdConnect(t *testing.T) {
	cases := []struct {
		name      string
		tokens    []string
		noBrowser bool
		wantErr   string
		wantSaved bool
	}{
		{name: "approved", tokens: []string{"authorization_pending", "slow_down", ""}, wantSaved: true},
		{name: "approved_no_browser", tokens: []string{""}, noBrowser: true, wantSaved: true},
		{name: "denied", tokens: []string{"access_denied"}, wantErr: "refused"},
		{name: "expired", tokens: []string{"expired_token"}, wantErr: "not approved in time"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps, opened := sandholdTestDeps(t)
			f := newSandholdFakeNode(t, "key_1", tc.tokens...)
			var out bytes.Buffer
			err := runSandholdConnect(context.Background(), &out, deps, f.srv.URL, tc.noBrowser)
			if strings.Contains(out.String(), sandholdTestKey) || (err != nil && strings.Contains(err.Error(), sandholdTestKey)) {
				t.Fatalf("the key was printed:\n%s\n%v", out.String(), err)
			}
			if !strings.Contains(out.String(), "BCDF-GHJK") || !strings.Contains(out.String(), f.srv.URL+"/app/#/device?code=BCDF-GHJK") {
				t.Errorf("the page and code were not shown:\n%s", out.String())
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("connect: %v\n%s", err, out.String())
			}
			cred, cerr := sandholdLoadCredential(deps.observerDir)
			conn, nerr := sandholdLoadConnection(deps.observerDir)
			if !tc.wantSaved {
				if cred != nil || conn != nil || cerr != nil || nerr != nil {
					t.Fatalf("a failed sign-in kept something: %v %v %v %v", cred, conn, cerr, nerr)
				}
				return
			}
			if cerr != nil || cred == nil || cred.Key != sandholdTestKey || cred.URL != f.srv.URL || cred.KeyID != "key_1" {
				t.Fatalf("credential %v %v", cred, cerr)
			}
			if nerr != nil || conn == nil || conn.User != "alice" || conn.Org != "acme" || len(conn.Offers) != 2 {
				t.Fatalf("connection %+v %v", conn, nerr)
			}
			if conn.Offers[1].Class != vocab.ClassUserspaceKernel {
				t.Errorf("offers %+v", conn.Offers)
			}
			for _, want := range []string{"Connected to " + f.srv.URL + " as alice in acme.", "Shared (a jailed process on the host kernel): Observer class process", "Sealed (its own user-space kernel): Observer class userspace_kernel", "observer sandbox disconnect"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if tc.noBrowser != (len(*opened) == 0) {
				t.Errorf("browser opened %v with --no-browser %v", *opened, tc.noBrowser)
			}
		})
	}
}

// TestSandholdConnectRefusesPlainHTTP: a node address on plain http that is
// not this machine is refused before anything is sent.
func TestSandholdConnectRefusesPlainHTTP(t *testing.T) {
	deps, _ := sandholdTestDeps(t)
	err := runSandholdConnect(context.Background(), &bytes.Buffer{}, deps, "http://sandhold.example.com", false)
	if err == nil || !strings.Contains(err.Error(), "uses http") {
		t.Fatalf("err = %v", err)
	}
}

// TestSandholdCredentialStorage: the key file is owner-only, written
// whole (no temporary file left), and the non-secret record is owner-only
// and holds no key.
func TestSandholdCredentialStorage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	deps, _ := sandholdTestDeps(t)
	f := newSandholdFakeNode(t, "key_1", "")
	if err := runSandholdConnect(context.Background(), &bytes.Buffer{}, deps, f.srv.URL, true); err != nil {
		t.Fatal(err)
	}
	credDir := hostedCredDir(deps.observerDir)
	entries, err := os.ReadDir(credDir)
	if err != nil {
		t.Fatal(err)
	}
	var keyFile string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("a temporary file was left: %s", e.Name())
		}
		if strings.HasPrefix(e.Name(), sandhold.KeystoreService+".") {
			keyFile = filepath.Join(credDir, e.Name())
		}
	}
	if keyFile == "" {
		t.Fatalf("no key file in %s (%v)", credDir, entries)
	}
	for _, p := range []string{keyFile, sandholdConnectionPath(deps.observerDir)} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s is mode %v, want 0600", p, fi.Mode().Perm())
		}
	}
	if fi, _ := os.Stat(credDir); fi.Mode().Perm() != 0o700 {
		t.Errorf("credential dir mode %v", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(sandholdConnectionPath(deps.observerDir))
	if strings.Contains(string(b), sandholdTestKey) {
		t.Error("the connection record holds the key")
	}
	// A record another user could read is refused, never trusted.
	if err := os.Chmod(sandholdConnectionPath(deps.observerDir), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sandholdLoadConnection(deps.observerDir); err == nil {
		t.Error("a world-readable connection record was read")
	}
}

// TestSandholdReconnectRevokesReplacedKey: a new sign-in revokes the key it
// replaces, at the node that issued it, with that node's key.
func TestSandholdReconnectRevokesReplacedKey(t *testing.T) {
	deps, _ := sandholdTestDeps(t)
	oldNode := newSandholdFakeNode(t, "key_old", "")
	if err := runSandholdConnect(context.Background(), &bytes.Buffer{}, deps, oldNode.srv.URL, true); err != nil {
		t.Fatal(err)
	}
	// The old node's key is rewritten so it differs from the new grant.
	if _, err := sandholdSaveCredential(deps.observerDir, sandhold.Credential{URL: oldNode.srv.URL, Key: "old-key-value", KeyID: "key_old"}); err != nil {
		t.Fatal(err)
	}
	newNode := newSandholdFakeNode(t, "key_new", "")
	if err := runSandholdConnect(context.Background(), &bytes.Buffer{}, deps, newNode.srv.URL, true); err != nil {
		t.Fatal(err)
	}
	if len(oldNode.revoked) != 1 || oldNode.revoked[0] != "old-key-value|key_old" {
		t.Errorf("old node revocations %v", oldNode.revoked)
	}
	if len(newNode.revoked) != 0 {
		t.Errorf("the old key was sent to the new node: %v", newNode.revoked)
	}
}

// TestSandholdDisconnect revokes the key at its node and forgets it.
func TestSandholdDisconnect(t *testing.T) {
	deps, _ := sandholdTestDeps(t)
	f := newSandholdFakeNode(t, "key_1", "")
	var out bytes.Buffer
	if err := runSandholdDisconnect(context.Background(), &out, deps); err != nil || !strings.Contains(out.String(), "not connected") {
		t.Fatalf("not connected: %v %q", err, out.String())
	}
	if err := runSandholdConnect(context.Background(), &bytes.Buffer{}, deps, f.srv.URL, true); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runSandholdDisconnect(context.Background(), &out, deps); err != nil {
		t.Fatal(err)
	}
	if len(f.revoked) != 1 || f.revoked[0] != sandholdTestKey+"|key_1" {
		t.Errorf("revocations %v", f.revoked)
	}
	if !strings.Contains(out.String(), "The key is revoked at the node") || strings.Contains(out.String(), sandholdTestKey) {
		t.Errorf("output %q", out.String())
	}
	if c, _ := sandholdLoadCredential(deps.observerDir); c != nil {
		t.Error("the key is still kept")
	}
	if c, _ := sandholdLoadConnection(deps.observerDir); c != nil {
		t.Error("the connection record is still there")
	}
}

// TestSandholdProbeStates is the row's state table: not connected,
// connected (described in Observer's classes, run refused by name), an
// expired key, an unreadable record, and a CI key in the environment.
func TestSandholdProbeStates(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	conn := &sandhold.Connection{Version: 1, URL: "https://node.example.com", User: "alice", Org: "acme", ExpiresAt: now.Add(24 * time.Hour),
		Offers: []sandhold.Offer{{Tier: "shared", Class: vocab.ClassProcess}, {Tier: "vault", Class: vocab.ClassHWVM}}}
	expired := *conn
	expired.ExpiresAt = now.Add(-time.Hour)
	cases := []struct {
		name        string
		conn        *sandhold.Connection
		err         error
		envKey      bool
		wantVerdict string
		wantReason  string
		wantHint    string
	}{
		{name: "not_connected", wantVerdict: verdictSandholdNotConnected, wantReason: "not connected: run `observer sandbox connect <node-url>`", wantHint: "not connected"},
		{name: "connected", conn: conn, wantVerdict: verdictSandholdRunNotBuilt, wantReason: "not built in this release", wantHint: "connected to https://node.example.com as alice in acme; key works until 12 Nov 2026; offers Shared = process, Vault = hw_vm"},
		{name: "expired", conn: &expired, wantVerdict: verdictSandholdKeyExpired, wantReason: "observer sandbox connect https://node.example.com", wantHint: "expired"},
		{name: "unreadable", err: errors.New("mode 0644"), wantVerdict: verdictSandholdNotConnected, wantReason: "could not be read (mode 0644)", wantHint: "connect again"},
		{name: "env_key", envKey: true, wantVerdict: verdictSandholdRunNotBuilt, wantReason: "not built", wantHint: "SANDHOLD_API_KEY"},
		{name: "connected_env_wins", conn: conn, envKey: true, wantVerdict: verdictSandholdRunNotBuilt, wantReason: "not built", wantHint: "SANDHOLD_API_KEY is set and wins"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := sandholdProbeFor(tc.conn, tc.err, tc.envKey, now)
			if !p.Compiled || p.Verdict != tc.wantVerdict || !strings.Contains(p.Reason, tc.wantReason) || !strings.Contains(p.Hint, tc.wantHint) {
				t.Fatalf("probe %+v", p)
			}
			if p.Verdict == sandboxselect.VerdictAvailable {
				t.Fatal("a probe-only row reads available")
			}
		})
	}
}

// TestSandholdBackendRow: the row the dashboard's probe API and `observer
// sandbox status` render, connected and not, enabled and not, carries the
// connection in its note, and never reads available.
func TestSandholdBackendRow(t *testing.T) {
	dir := t.TempDir()
	r := &sandboxRuntime{observerDir: dir}
	drv := &sandholdDriver{gatesProbeOnlyDriver: gatesProbeOnlyDriver{r: r, id: sandboxbackend.BackendSandHold, notBuilt: sandholdNotBuilt}}
	row := func(enabled bool) (out struct{ hint, verdict, reason, whenVerdict, whenReason, class string }) {
		cfg := config.TerminalSandboxConfig{Enabled: true}
		if enabled {
			cfg.Backends = []string{sandbox.BackendBwrap, sandboxbackend.BackendSandHold}
		}
		r.cfg = cfg
		probes := map[string]sandboxselect.Probe{sandboxbackend.BackendSandHold: drv.probe(context.Background())}
		for _, b := range sandboxBackendListWith(cfg, sandbox.Availability{}, nil, vocab.TierNone, probes) {
			if b.ID != sandboxbackend.BackendSandHold {
				continue
			}
			if b.Available {
				t.Fatal("the sandhold row reads available")
			}
			out.hint, out.verdict, out.reason, out.whenVerdict, out.whenReason, out.class = b.Hint, b.Verdict, b.Reason, b.WhenEnabledVerdict, b.WhenEnabledReason, b.Class
			return out
		}
		t.Fatal("no sandhold row in the backend list")
		return out
	}

	// Not connected, not enabled: the note and the "when enabled" answer
	// both say how to connect.
	got := row(false)
	if got.hint != sandhold.NotConnected || got.verdict != sandboxselect.VerdictDisabledByConfig {
		t.Errorf("not connected, not enabled: %+v", got)
	}
	if got.class != string(vocab.ClassProcess) {
		t.Errorf("class %q", got.class)
	}
	if runtime.GOOS == "linux" && !strings.Contains(got.whenReason, "observer sandbox connect") {
		t.Errorf("when enabled: %+v", got)
	}

	// Connected and enabled: the note names the node and its isolation, and
	// the row is dropped with the probe's own refusal.
	if err := sandholdSaveConnection(dir, sandhold.Connection{URL: "https://node.example.com", User: "alice", Org: "acme",
		Offers: []sandhold.Offer{{Tier: "vault", Class: vocab.ClassHWVM}}}); err != nil {
		t.Fatal(err)
	}
	got = row(true)
	if !strings.Contains(got.hint, "connected to https://node.example.com as alice in acme") || !strings.Contains(got.hint, "Vault = hw_vm") {
		t.Errorf("connected hint %q", got.hint)
	}
	if runtime.GOOS == "linux" && (got.verdict != verdictSandholdRunNotBuilt || !strings.Contains(got.reason, "not built")) {
		t.Errorf("connected, enabled: %+v", got)
	}

	// The inactive hint (component not selected) reads the same record.
	reg := sandboxDriverRegs[sandboxbackend.BackendSandHold]
	if reg.inactiveHint == nil || !strings.Contains(reg.inactiveHint(r, config.Config{}), "connected to https://node.example.com") {
		t.Error("the inactive hint does not carry the connection")
	}
	// A run is refused by name.
	if _, err := drv.Create(context.Background(), termsvc.PrepareRequest{}); err == nil || !strings.Contains(err.Error(), "not built") {
		t.Errorf("create: %v", err)
	}
}
