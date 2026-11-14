//go:build !sbo_omit_terminals_sandbox_sandhold && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/keystore"
	"github.com/marmutapp/superbased-observer/internal/sandbox"
	"github.com/marmutapp/superbased-observer/internal/sandboxbackend"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote/sandhold"
	"github.com/marmutapp/superbased-observer/internal/sandboxselect"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
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
	// signup and plan, when set, are GET /app/api/signup and GET
	// /v1/offer (404 otherwise, as on a node without the offer).
	signup, plan any
	// who and starter, when set, are GET /v1/whoami's answer and GET
	// /v1/targets' starter limits (a self-serve starter account).
	who, starter any
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
			desc := "the node says " + next
			if next == "expired_token" {
				// SandHold's own words (control/internal/api/device.go).
				desc = "the sign-in was not approved in time; start again"
			}
			sandholdWriteJSON(w, 400, map[string]string{"error": next, "error_description": desc})
			return
		}
		sandholdWriteJSON(w, 200, map[string]any{"access_token": sandholdTestKey, "key_id": f.keyID, "expires_at": "2026-12-11T00:00:00Z", "org": "acme", "user": "alice"})
	})
	mux.HandleFunc("GET /v1/whoami", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != sandholdTestKey {
			sandholdWriteJSON(w, 401, map[string]string{"error": "no valid credential", "fix": "sign in again"})
			return
		}
		if f.who != nil {
			sandholdWriteJSON(w, 200, f.who)
			return
		}
		sandholdWriteJSON(w, 200, map[string]any{"org": "acme", "user": "alice", "keyId": f.keyID})
	})
	mux.HandleFunc("GET /v1/targets", func(w http.ResponseWriter, _ *http.Request) {
		body := map[string]any{"targets": []map[string]any{
			{"id": "jail", "tier": "shared", "tierMeans": "a jailed process on the host kernel", "tierRank": 1, "state": "active", "default": true, "capabilities": map[string]any{"isolation": "process", "egressAllowlist": true}},
			{"id": "gvisor", "tier": "sealed", "tierMeans": "its own user-space kernel", "tierRank": 3, "state": "active", "capabilities": map[string]any{"isolation": "user-kernel", "egressAllowlist": true}},
		}}
		if f.starter != nil {
			body["starter"] = f.starter
		}
		sandholdWriteJSON(w, 200, body)
	})
	mux.HandleFunc("GET /app/api/signup", func(w http.ResponseWriter, _ *http.Request) {
		if f.signup == nil {
			sandholdWriteJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		sandholdWriteJSON(w, 200, f.signup)
	})
	mux.HandleFunc("GET /v1/offer", func(w http.ResponseWriter, _ *http.Request) {
		if f.plan == nil {
			sandholdWriteJSON(w, 404, map[string]string{"error": "no first-month offer", "code": "no_offer"})
			return
		}
		sandholdWriteJSON(w, 200, f.plan)
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
				// Said once: the node's own words for an expiry repeat ours.
				if n := strings.Count(err.Error(), tc.wantErr); n != 1 {
					t.Errorf("%q appears %d times: %v", tc.wantErr, n, err)
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

// TestSandholdConnectDefaultNodeAndOffer: with no node named, connect goes
// to the default node (SANDHOLD_DEFAULT_URL here), shows its first-month
// offer before the browser step, and keeps and prints the account's plan.
func TestSandholdConnectDefaultNodeAndOffer(t *testing.T) {
	deps, opened := sandholdTestDeps(t)
	f := newSandholdFakeNode(t, "key_1", "")
	f.signup = map[string]any{
		"selfServe": true, "signupClosed": true, "inviteAccepted": true,
		"offer": map[string]any{
			"freeDays": 30, "allowanceUsd": 80, "cardSkips": 5, "cardCheckUsd": 1,
			"expect": "The $1.00 card check is refunded at once. Nothing is charged in your free month.",
		},
	}
	f.plan = map[string]any{
		"freeDays": 30, "inFreeMonth": true, "daysLeft": 30, "allowanceUsd": 80, "cardGate": true, "cardSkips": 5,
		"sandboxesCreated": 0, "skipsLeft": 5, "card": "none", "cardCheckUsd": 1, "addCardUrl": f.srv.URL + "/app/#/billing",
		"expect":  "The $1.00 card check is refunded at once. After that, your card is charged $20 each time your balance falls below $5.",
		"summary": "First month free: 30 days left (until 11 Dec 2026), up to $80 of use; 5 of 5 sandboxes left without a card.",
		"freeLimits": map[string]any{
			"kind": "free", "maxRunMinutes": 30, "cpu": "2", "memory": "4Gi", "disk": "10Gi",
			"network": "allowlist", "atOnce": 2, "perDay": 20,
		},
	}
	deps.lookupEnv = func(k string) (string, bool) {
		if k == sandhold.EnvDefaultURL {
			return f.srv.URL, true
		}
		return "", false
	}
	var out bytes.Buffer
	if err := runSandholdConnect(context.Background(), &out, deps, "", false); err != nil {
		t.Fatalf("connect: %v\n%s", err, out.String())
	}
	for _, want := range []string{
		"No node given: connecting to SandHold at " + f.srv.URL,
		"New to SandHold? First month free: 30 days at no charge, up to $80 of use. No card needed for your first 5 sandboxes",
		"by invitation for now",
		"Plan: First month free: 30 days left",
		"Add a card any time at " + f.srv.URL + "/app/#/billing",
		"Free use: each sandbox up to 30 min, 2 vCPU, 4 GiB memory, 10 GiB disk, 2 at a time, 20 a day.",
		"What to expect: The $1.00 card check is refunded at once. Nothing is charged in your free month.",
		"  What to expect: The $1.00 card check is refunded at once. After that, your card is charged $20 each time your balance falls below $5.",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if n := strings.Count(out.String(), "5 of 5 sandboxes left"); n != 1 {
		t.Errorf("the sandboxes-left line appears %d times:\n%s", n, out.String())
	}
	if strings.Index(out.String(), "First month free: 30 days at") > strings.Index(out.String(), "approve this machine") {
		t.Errorf("the offer came after the browser step:\n%s", out.String())
	}
	if len(*opened) != 1 {
		t.Errorf("browser opened %v", *opened)
	}
	conn, err := sandholdLoadConnection(deps.observerDir)
	if err != nil || conn == nil || conn.Plan == nil || conn.Plan.SkipsLeft != 5 || conn.Plan.CheckedAt.IsZero() {
		t.Fatalf("connection %+v %v", conn, err)
	}

	// A node without the offer: nothing about it is printed or kept, and
	// the public node is the default when nothing names another.
	deps2, _ := sandholdTestDeps(t)
	g := newSandholdFakeNode(t, "key_2", "")
	out.Reset()
	if err := runSandholdConnect(context.Background(), &out, deps2, g.srv.URL, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "First month") || strings.Contains(out.String(), "Plan:") {
		t.Errorf("offer shown for a node without one:\n%s", out.String())
	}
	if conn, _ := sandholdLoadConnection(deps2.observerDir); conn == nil || conn.Plan != nil {
		t.Errorf("plan kept: %+v", conn)
	}
	if u, err := sandhold.NodeURL(sandhold.DefaultNode); err != nil || u != sandhold.DefaultNode {
		t.Errorf("default node %q %v", u, err)
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

// TestSandholdProbeStates is the row's connection table: not connected, an
// expired key and an unreadable record refuse by name; a usable connection
// (or a CI key) leaves the verdict to the shared hosted ladder, with the
// connection in the note.
func TestSandholdProbeStates(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	conn := &sandhold.Connection{
		Version: 1, URL: "https://node.example.com", User: "alice", Org: "acme", ExpiresAt: now.Add(24 * time.Hour),
		Offers: []sandhold.Offer{{Tier: "shared", Class: vocab.ClassProcess}, {Tier: "vault", Class: vocab.ClassHWVM}},
	}
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
		{name: "not_connected", wantVerdict: verdictSandholdNotConnected, wantReason: "or `observer sandbox connect <node-url>`", wantHint: "not connected"},
		{name: "connected", conn: conn, wantHint: "connected to https://node.example.com as alice in acme; key works until 12 Nov 2026; offers Shared = process, Vault = hw_vm"},
		{name: "expired", conn: &expired, wantVerdict: verdictSandholdKeyExpired, wantReason: "observer sandbox connect https://node.example.com", wantHint: "expired"},
		{name: "unreadable", err: errors.New("mode 0644"), wantVerdict: verdictSandholdNotConnected, wantReason: "could not be read (mode 0644)", wantHint: "connect again"},
		{name: "env_key", envKey: true, wantHint: "SANDHOLD_API_KEY"},
		{name: "connected_env_wins", conn: conn, envKey: true, wantHint: "SANDHOLD_API_KEY is set and wins"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := sandholdProbeFor(tc.conn, tc.err, tc.envKey, now)
			if !p.Compiled || p.Verdict != tc.wantVerdict || !strings.Contains(p.Reason, tc.wantReason) || !strings.Contains(p.Hint, tc.wantHint) {
				t.Fatalf("probe %+v", p)
			}
		})
	}
}

// sandholdTestRow builds the registered SandHold driver over a temporary
// observer dir, with no SANDHOLD_* variable but those given and a file
// keystore.
func sandholdTestRow(t *testing.T, env map[string]string) (*hostedDriver, *sandboxRuntime) {
	t.Helper()
	deps, _ := sandholdTestDeps(t)
	savedEnv := sandholdLookupEnv
	sandholdLookupEnv = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	t.Cleanup(func() { sandholdLookupEnv = savedEnv; sandholdSetNodeDir(""); hostedListed.Store(false) })
	r := &sandboxRuntime{observerDir: deps.observerDir, observerBin: "/usr/local/bin/observer"}
	full := config.Default()
	r.cfg = full.Terminal.Sandbox
	reg, ok := sandboxDriverRegs[sandboxbackend.BackendSandHold]
	if !ok {
		t.Fatal("no sandhold driver registered")
	}
	d, ok := reg.build(r, full).(*hostedDriver)
	if !ok {
		t.Fatal("the sandhold row is not a hosted driver")
	}
	d.now = func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) }
	return d, r
}

// TestSandholdBackendRow: the row the dashboard's probe API and `observer
// sandbox status` render, connected and not, enabled and not, carries the
// connection in its note, and is the shared hosted ladder's once usable.
func TestSandholdBackendRow(t *testing.T) {
	d, r := sandholdTestRow(t, nil)
	row := func(enabled bool) (out struct{ hint, verdict, reason, whenVerdict, whenReason, class string }) {
		cfg := config.Default().Terminal.Sandbox
		cfg.Enabled = true
		if enabled {
			cfg.Backends = []string{sandbox.BackendBwrap, sandboxbackend.BackendSandHold}
		}
		r.cfg = cfg
		d.r.cfg = cfg
		probes := map[string]sandboxselect.Probe{sandboxbackend.BackendSandHold: d.probe(context.Background())}
		for _, b := range sandboxBackendListWith(cfg, sandbox.Availability{}, nil, vocab.TierNone, probes) {
			if b.ID != sandboxbackend.BackendSandHold {
				continue
			}
			out.hint, out.verdict, out.reason, out.whenVerdict, out.whenReason, out.class = b.Hint, b.Verdict, b.Reason, b.WhenEnabledVerdict, b.WhenEnabledReason, b.Class
			return out
		}
		t.Fatal("no sandhold row in the backend list")
		return out
	}

	// Not connected, not enabled: the note says how to connect.
	got := row(false)
	if got.hint != sandhold.NotConnected || got.verdict != sandboxselect.VerdictDisabledByConfig {
		t.Errorf("not connected, not enabled: %+v", got)
	}
	if got.class != string(vocab.ClassProcess) {
		t.Errorf("class %q", got.class)
	}
	// Not connected, enabled: credential_missing, in the row's own words.
	if p := func() sandboxselect.Probe { row(true); return d.probe(context.Background()) }(); runtime.GOOS == "linux" && (p.Verdict != verdictSandholdNotConnected || !strings.Contains(p.Reason, "observer sandbox connect")) {
		t.Errorf("not connected, enabled: %+v", p)
	}

	// Connected and enabled: the note names the node and its isolation, and
	// the shared ladder decides (no consent yet, so not usable for a run).
	if err := sandholdSaveConnection(r.observerDir, sandhold.Connection{
		URL: "https://node.example.com", User: "alice", Org: "acme",
		Offers: []sandhold.Offer{{Tier: "vault", Class: vocab.ClassHWVM}},
	}); err != nil {
		t.Fatal(err)
	}
	got = row(true)
	if !strings.Contains(got.hint, "connected to https://node.example.com as alice in acme") || !strings.Contains(got.hint, "Vault = hw_vm") {
		t.Errorf("connected hint %q", got.hint)
	}
	if p := d.probe(context.Background()); runtime.GOOS == "linux" && (p.Verdict != sandboxselect.VerdictAvailable || p.ConsentRecorded) {
		t.Errorf("connected, enabled: %+v", p)
	}
	// The inactive hint (component not selected) reads the same record.
	reg := sandboxDriverRegs[sandboxbackend.BackendSandHold]
	if reg.inactiveHint == nil || !strings.Contains(reg.inactiveHint(r, config.Config{}), "connected to https://node.example.com") {
		t.Error("the inactive hint does not carry the connection")
	}
	// An expired key refuses by name, before anything else.
	if err := sandholdSaveConnection(r.observerDir, sandhold.Connection{URL: "https://node.example.com", ExpiresAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if p := d.probe(context.Background()); p.Verdict != verdictSandholdKeyExpired {
		t.Errorf("expired: %+v", p)
	}
}

// TestSandholdHostedCredential: the key comes from the saved sign-in, bound
// to its node; SANDHOLD_URL with SANDHOLD_API_KEY wins; a saved key is never
// bound to another node; the provider table's node is the same one; and the
// source is said without reading the key.
func TestSandholdHostedCredential(t *testing.T) {
	v, _ := sandboxremote.VendorFor(sandboxremote.VendorSandHold)
	d, r := sandholdTestRow(t, nil)
	if _, err := hostedResolveCredential(r.observerDir, v); !errors.Is(err, sandboxremote.ErrCredentialMissing) || !strings.Contains(err.Error(), "observer sandbox connect") {
		t.Fatalf("none: %v", err)
	}
	if src := hostedCredentialSource(r.observerDir, v); src != "" {
		t.Errorf("none: source %q", src)
	}
	if _, err := sandholdSaveCredential(r.observerDir, sandhold.Credential{URL: "https://node.example.com", Key: sandholdTestKey, KeyID: "k1"}); err != nil {
		t.Fatal(err)
	}
	if err := sandholdSaveConnection(r.observerDir, sandhold.Connection{URL: "https://node.example.com"}); err != nil {
		t.Fatal(err)
	}
	cred, err := hostedResolveCredential(r.observerDir, v)
	if err != nil || cred.Get(sandhold.EnvKey) != sandholdTestKey || cred.Endpoint != "https://node.example.com" || cred.Source != sandboxremote.SourceKeystore {
		t.Fatalf("saved: %v %v", cred, err)
	}
	if s := fmt.Sprintf("%v %+v %s", cred, cred, cred); strings.Contains(s, sandholdTestKey) {
		t.Errorf("the credential prints its key: %s", s)
	}
	pc := hostedVendorConfig(d.cfg, v.ID)
	_, endpoint, ok := hostedResolve(v, pc)
	if !ok || endpoint != "https://node.example.com" {
		t.Fatalf("provider table node %q %v", endpoint, ok)
	}
	if err := sandboxremote.CheckBinding(cred, v, endpoint); err != nil {
		t.Errorf("binding: %v", err)
	}
	if err := sandboxremote.CheckBinding(cred, v, "https://other.example.com"); err == nil {
		t.Error("the saved key was bound to another node")
	}
	if src := hostedCredentialSource(r.observerDir, v); src != sandboxremote.SourceKeystore {
		t.Errorf("saved: source %q", src)
	}

	// SANDHOLD_URL names another node, no key in the environment: the saved
	// key stays with its own node.
	sandholdLookupEnv = func(k string) (string, bool) {
		return map[string]string{sandhold.EnvURL: "https://other.example.com"}[k], k == sandhold.EnvURL
	}
	if _, err := hostedResolveCredential(r.observerDir, v); !errors.Is(err, sandboxremote.ErrCredentialMissing) {
		t.Errorf("other node: %v", err)
	}
	if src := hostedCredentialSource(r.observerDir, v); src != "" {
		t.Errorf("other node: source %q", src)
	}
	// Both variables: the environment wins, bound to its node.
	sandholdLookupEnv = func(k string) (string, bool) {
		v, ok := map[string]string{sandhold.EnvURL: "https://ci.example.com", sandhold.EnvKey: "ci-key-value"}[k]
		return v, ok
	}
	cred, err = hostedResolveCredential(r.observerDir, v)
	if err != nil || cred.Get(sandhold.EnvKey) != "ci-key-value" || cred.Endpoint != "https://ci.example.com" || cred.Source != sandboxremote.SourceEnv {
		t.Fatalf("env: %v %v", cred, err)
	}
	if _, endpoint, _ := hostedResolve(v, hostedVendorConfig(d.cfg, v.ID)); endpoint != "https://ci.example.com" {
		t.Errorf("env: provider table node %q", endpoint)
	}
	if src := hostedCredentialSource(r.observerDir, v); src != sandboxremote.SourceEnv {
		t.Errorf("env: source %q", src)
	}
	// A node address Observer would not send a key to is refused by name.
	sandholdLookupEnv = func(k string) (string, bool) {
		return map[string]string{sandhold.EnvURL: "http://node.example.com"}[k], k == sandhold.EnvURL
	}
	pc = hostedVendorConfig(d.cfg, v.ID)
	if _, _, ok := hostedResolve(v, pc); ok {
		t.Error("a plain http node elsewhere was accepted")
	}
	if why := hostedResolveRefusal(v, pc); !strings.Contains(why, "not one Observer sends a key to") {
		t.Errorf("refusal %q", why)
	}
}

// TestSandholdClientAndStoredVerbs: the wire client is built only for an
// address the key may go to, and the shared store's connect and disconnect
// verbs point at the row's own commands.
func TestSandholdClientAndStoredVerbs(t *testing.T) {
	reg := hostedVendorRegs[sandboxremote.VendorSandHold]
	cred := sandboxremote.Credential{Provider: sandboxremote.VendorSandHold, Fields: map[string]string{sandhold.EnvKey: sandholdTestKey}}
	if c, err := reg.newClient(cred, "https://node.example.com"); err != nil || c == nil {
		t.Errorf("https node: %v", err)
	}
	if _, err := reg.newClient(cred, "http://node.example.com"); err == nil {
		t.Error("a client over plain http to another machine was built")
	}
	if _, err := hostedStoredVendorArg(sandboxremote.VendorSandHold); err == nil || !strings.Contains(err.Error(), "observer sandbox connect") {
		t.Errorf("stored verb: %v", err)
	}
}

// TestSandholdFloor: a min_isolation floor above what the row counts is
// refused in plain words, because the node's isolation is its own claim.
func TestSandholdFloor(t *testing.T) {
	d, _ := sandholdTestRow(t, nil)
	cfg := config.Default().Terminal.Sandbox
	cfg.Enabled, cfg.Backends, cfg.MinIsolation = true, []string{sandboxbackend.BackendSandHold}, string(vocab.ClassContainer)
	err := d.floor(cfg, vocab.TierNone, vocab.CustodyInBoxAPIKey, sandboxremote.RegionSandHoldNode)
	if err == nil || !strings.Contains(err.Error(), "the node's own claim") {
		t.Errorf("floor: %v", err)
	}
}

// TestSandholdConnectStarterInWords: a self-serve starter account is named
// as the person ("your own account", no ids), and offered only what it may
// use: no host-kernel tier and no open network, at connect and in the
// Sandboxes panel.
func TestSandholdConnectStarterInWords(t *testing.T) {
	deps, _ := sandholdTestDeps(t)
	f := newSandholdFakeNode(t, "key_1", "")
	f.who = map[string]any{"org": "org_5e87", "user": "u_23e9", "keyId": "key_1", "account": "self", "name": "Dana Example", "email": "dana@example.test", "orgName": "Dana Example"}
	f.starter = map[string]any{"isolation": "sealed", "openNetwork": false}
	var out bytes.Buffer
	if err := runSandholdConnect(context.Background(), &out, deps, f.srv.URL, true); err != nil {
		t.Fatalf("connect: %v\n%s", err, out.String())
	}
	o := out.String()
	if !strings.Contains(o, "Connected to "+f.srv.URL+" as Dana Example (dana@example.test), your own account.") || strings.Contains(o, "u_23e9") || strings.Contains(o, "org_5e87") {
		t.Errorf("connect named ids, not the person:\n%s", o)
	}
	if strings.Contains(o, "Shared (") || !strings.Contains(o, "Sealed (its own user-space kernel)") || strings.Contains(o, "network none, limited, open") || !strings.Contains(o, "starter limits: Sealed or stronger") {
		t.Errorf("connect offered beyond the starter limits:\n%s", o)
	}
	v := sandholdPlace(deps.observerDir, time.Now())
	if v.User != "Dana Example (dana@example.test), your own account" || v.Org != "" {
		t.Errorf("panel: signed in as %q in %q", v.User, v.Org)
	}
	if len(v.Offers) != 1 || v.Offers[0].Tier != "sealed" || slices.Contains(v.Offers[0].Networks, sandhold.NetworkOpen) {
		t.Errorf("panel offers: %+v", v.Offers)
	}
}
