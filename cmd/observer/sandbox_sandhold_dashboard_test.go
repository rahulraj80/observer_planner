//go:build !sbo_omit_terminals_sandbox_sandhold && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/keystore"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote/sandhold"
)

// sandbox_sandhold_dashboard_test.go pins the Terminals > Sandboxes tab's
// SandHold routes against a fake node: the place view reads local facts
// only and never carries the key; the advisor sends the key only to the
// node, in its header; a node refusal comes back in its words; a node that
// echoes the key gets it scrubbed; nothing works without a connection or
// (for the POST) the confirm token; and every route is owner-local.

// sandholdDashEnv gives the routes a temporary home (the observer dir is
// ~/.observer), a file keystore in it, no SANDHOLD_* variables, and a
// connection to a fake node made through the real connect command. It
// returns the node's request log and its address.
func sandholdDashEnv(t *testing.T, adviseRefusal bool) (*[]string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	obs, err := hostedObserverDir()
	if err != nil {
		t.Fatal(err)
	}
	savedKS, savedEnv, savedActive := sandholdKeystoreOpen, sandholdLookupEnv, sandholdComponentActive
	sandholdKeystoreOpen = func(observerDir string) keystore.Store {
		return keystore.NewFileStore(sandhold.KeystoreService, hostedCredDir(observerDir))
	}
	sandholdLookupEnv = func(string) (string, bool) { return "", false }
	sandholdComponentActive = func(string) bool { return true }
	t.Cleanup(func() {
		sandholdKeystoreOpen, sandholdLookupEnv, sandholdComponentActive = savedKS, savedEnv, savedActive
	})

	f := newSandholdFakeNode(t, "key_1", "")
	var calls []string
	q := loadContractAnswer(t, "advise-questions")
	a := loadContractAnswer(t, "advise")
	// The advisor's two calls on the same fake node, through a front that
	// records each call and its key header.
	front := http.NewServeMux()
	front.HandleFunc("/v1/advise", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path+" key="+boolWord(r.Header.Get("X-API-Key") == sandholdTestKey)+" query="+r.URL.RawQuery)
		if r.Header.Get("X-API-Key") != sandholdTestKey {
			sandholdWriteJSON(w, 401, map[string]string{"error": "no valid credential", "fix": "sign in again"})
			return
		}
		switch {
		case r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(q)
		case adviseRefusal:
			// A node that names the key it was sent in its refusal.
			sandholdWriteJSON(w, 400, map[string]string{"error": "trust \"maybe\" is not one of the answers (key " + r.Header.Get("X-API-Key") + ")", "fix": "say how much you trust the code: own, generated or unknown"})
		default:
			body, _ := io.ReadAll(r.Body)
			calls = append(calls, "body "+string(body))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(a)
		}
	})
	plan := bytes.ReplaceAll(loadContractAnswer(t, "offer"), []byte("<time>"), []byte("2026-12-09T12:00:00Z"))
	sandholdOfferCalls.Store(0)
	front.HandleFunc("GET /v1/offer", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != sandholdTestKey {
			sandholdWriteJSON(w, 401, map[string]string{"error": "no valid credential"})
			return
		}
		sandholdOfferCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(plan)
	})
	front.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Everything else (the sign-in) goes to the fake node.
		req, _ := http.NewRequestWithContext(r.Context(), r.Method, f.srv.URL+r.URL.RequestURI(), r.Body)
		req.Header = r.Header.Clone()
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer res.Body.Close()
		for k, v := range res.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	}))
	srv := httptest.NewServer(front)
	t.Cleanup(srv.Close)

	deps, _ := sandholdTestDeps(t)
	deps.observerDir = obs
	sandholdKeystoreOpen = func(observerDir string) keystore.Store {
		return keystore.NewFileStore(sandhold.KeystoreService, hostedCredDir(observerDir))
	}
	var out bytes.Buffer
	if err := runSandholdConnect(context.Background(), &out, deps, srv.URL, true); err != nil {
		t.Fatalf("connect: %v\n%s", err, out.String())
	}
	return &calls, srv.URL
}

// loadContractAnswer is one SandHold contract file's response body.
func loadContractAnswer(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "sandboxremote", "sandhold", "testdata", "contract", "customer", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v.Response
}

func boolWord(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// sandholdDashCall calls one of the routes and returns the status and body.
func sandholdDashCall(t *testing.T, method, path, body, token string) (int, string) {
	t.Helper()
	var h http.HandlerFunc
	for _, rt := range sandholdDashboardRoutes("") {
		if rt.Pattern == method+" "+path {
			h = rt.Handler
		}
	}
	if h == nil {
		t.Fatalf("no route %s %s", method, path)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set(hostedConfirmHeader, token)
		req.AddCookie(&http.Cookie{Name: hostedConfirmCookie, Value: token})
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w.Code, w.Body.String()
}

func TestSandholdDashboardPlaceAndAdvisor(t *testing.T) {
	var logs bytes.Buffer
	savedLog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(savedLog) })

	calls, node := sandholdDashEnv(t, false)

	code, body := sandholdDashCall(t, http.MethodGet, "/api/terminal/sandbox/sandhold", "", "")
	if code != 200 {
		t.Fatalf("place view %d %s", code, body)
	}
	var place sandholdPlaceView
	if err := json.Unmarshal([]byte(body), &place); err != nil {
		t.Fatal(err)
	}
	if !place.Connected || place.Node != node || place.User != "alice" || place.Org != "acme" || !place.ComponentActive || place.ConnectCommand != "observer sandbox connect" {
		t.Errorf("place view %+v", place)
	}
	if len(place.Offers) != 2 || place.Offers[0].Title != "Shared" || place.Offers[1].Title != "Sealed" || len(place.Offers[0].Networks) == 0 {
		t.Errorf("offers %+v, want Shared then Sealed with their networks", place.Offers)
	}
	if strings.Contains(body, "key_1") || place.ConfirmToken == "" {
		t.Errorf("the view carries the key id or no confirm token: %s", body)
	}
	if len(*calls) != 0 {
		t.Fatalf("the place view called the node: %v", *calls)
	}

	if code, _ := sandholdDashCall(t, http.MethodGet, "/api/terminal/sandbox/sandhold/advise", "", ""); code != http.StatusForbidden || len(*calls) != 0 {
		t.Fatalf("questions without the confirm token answered %d and called the node %v", code, *calls)
	}
	tok := strings.Repeat("ab", 32)
	code, body = sandholdDashCall(t, http.MethodGet, "/api/terminal/sandbox/sandhold/advise", "", tok)
	if code != 200 || !strings.Contains(body, `"ask":"What will run in it?"`) || !strings.Contains(body, `"registry.npmjs.org"`) {
		t.Fatalf("questions %d %s", code, body)
	}

	answers := `{"runs":"tests","trust":"generated","reach":{"packages":["pypi"],"hosts":["github.com"]}}`
	if code, _ := sandholdDashCall(t, http.MethodPost, "/api/terminal/sandbox/sandhold/advise", answers, ""); code != http.StatusForbidden {
		t.Fatalf("a POST without the confirm token answered %d", code)
	}
	if code, _ := sandholdDashCall(t, http.MethodPost, "/api/terminal/sandbox/sandhold/advise", `{"runs":"tests","extra":1}`, tok); code != http.StatusBadRequest {
		t.Fatalf("an unknown field answered %d", code)
	}
	if code, _ := sandholdDashCall(t, http.MethodPost, "/api/terminal/sandbox/sandhold/advise", `{"runs":"a\u0007b","trust":"own","reach":{}}`, tok); code != http.StatusBadRequest {
		t.Fatalf("a control character answered %d", code)
	}
	code, body = sandholdDashCall(t, http.MethodPost, "/api/terminal/sandbox/sandhold/advise", answers, tok)
	if code != 200 {
		t.Fatalf("advice %d %s", code, body)
	}
	var adv struct {
		Node   string          `json:"node"`
		Advice sandhold.Advice `json:"advice"`
	}
	if err := json.Unmarshal([]byte(body), &adv); err != nil {
		t.Fatal(err)
	}
	if adv.Node != node || adv.Advice.Isolation != "shielded" || adv.Advice.Network != "limited" || len(adv.Advice.Why) != 3 || !strings.HasPrefix(adv.Advice.Command, "sandhold create") {
		t.Errorf("advice %+v", adv)
	}

	want := []string{
		"GET /v1/advise key=yes query=",
		"POST /v1/advise key=yes query=",
		`body {"runs":"tests","trust":"generated","reach":{"packages":["pypi"],"hosts":["github.com"]}}`,
	}
	if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("node calls:\n%s\nwant:\n%s", strings.Join(*calls, "\n"), strings.Join(want, "\n"))
	}
	if strings.Contains(logs.String(), sandholdTestKey) {
		t.Errorf("the key reached the log:\n%s", logs.String())
	}
}

func TestSandholdDashboardRefusalScrubsTheKey(t *testing.T) {
	sandholdDashEnv(t, true)
	tok := strings.Repeat("cd", 32)
	code, body := sandholdDashCall(t, http.MethodPost, "/api/terminal/sandbox/sandhold/advise", `{"runs":"tests","trust":"maybe","reach":{}}`, tok)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("refusal %d %s", code, body)
	}
	if strings.Contains(body, sandholdTestKey) {
		t.Fatalf("the key reached the browser: %s", body)
	}
	if !strings.Contains(body, `not one of the answers (key [the key])`) || !strings.Contains(body, "own, generated or unknown") {
		t.Errorf("the refusal is not in the node's words: %s", body)
	}
}

// TestSandholdDashboardPlan: the account's first-month offer is kept at
// connect and shown in the place view; the refresh reads it from the node
// only with the confirm token, and keeps it.
func TestSandholdDashboardPlan(t *testing.T) {
	_, node := sandholdDashEnv(t, false)
	code, body := sandholdDashCall(t, http.MethodGet, "/api/terminal/sandbox/sandhold", "", "")
	if code != 200 || !strings.Contains(body, `"skips_left":5`) || !strings.Contains(body, `"days_left":28`) || strings.Contains(body, `"offer":"`) ||
		!strings.Contains(body, `"add_card_url":"`+node+`/app/#/billing"`) || !strings.Contains(body, "5 of 5 sandboxes left before a card is needed") {
		t.Fatalf("place view %d %s", code, body)
	}
	if code, _ := sandholdDashCall(t, http.MethodGet, "/api/terminal/sandbox/sandhold/plan", "", ""); code != http.StatusForbidden || sandholdOfferCalls.Load() != 1 {
		t.Fatalf("plan without the confirm token: %d, node calls %d (want only connect's)", code, sandholdOfferCalls.Load())
	}
	code, body = sandholdDashCall(t, http.MethodGet, "/api/terminal/sandbox/sandhold/plan", "", strings.Repeat("ab", 32))
	if code != 200 || !strings.Contains(body, `"skips_left":5`) || !strings.Contains(body, `"checked_at":"`) || strings.Contains(body, sandholdTestKey) {
		t.Fatalf("plan %d %s", code, body)
	}
	if sandholdOfferCalls.Load() != 2 {
		t.Errorf("node calls %d, want 2", sandholdOfferCalls.Load())
	}
}

// sandholdOfferCalls counts GET /v1/offer at the dashboard tests' node.
var sandholdOfferCalls atomic.Int32

func TestSandholdDashboardNotConnected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	savedKS, savedEnv, savedActive := sandholdKeystoreOpen, sandholdLookupEnv, sandholdComponentActive
	sandholdKeystoreOpen = func(observerDir string) keystore.Store {
		return keystore.NewFileStore(sandhold.KeystoreService, hostedCredDir(observerDir))
	}
	sandholdLookupEnv = func(string) (string, bool) { return "", false }
	sandholdComponentActive = func(string) bool { return false }
	t.Cleanup(func() {
		sandholdKeystoreOpen, sandholdLookupEnv, sandholdComponentActive = savedKS, savedEnv, savedActive
	})
	code, body := sandholdDashCall(t, http.MethodGet, "/api/terminal/sandbox/sandhold", "", "")
	if code != 200 || !strings.Contains(body, `"connected":false`) || !strings.Contains(body, `"component_active":false`) || !strings.Contains(body, `"offers":[]`) ||
		!strings.Contains(body, `"offer":"First month free on SandHold`) || !strings.Contains(body, `"default_node":"https://`) {
		t.Fatalf("place view %d %s", code, body)
	}
	code, body = sandholdDashCall(t, http.MethodGet, "/api/terminal/sandbox/sandhold/advise", "", strings.Repeat("ef", 32))
	if code != http.StatusConflict || !strings.Contains(body, "observer sandbox connect") {
		t.Fatalf("questions without a connection %d %s", code, body)
	}
	for _, rt := range sandholdDashboardRoutes("") {
		if rt.Capability != dashapi.CapabilityLocal || rt.Section != dashapi.Section("terminals") || !strings.Contains(rt.Pattern, "/api/terminal/sandbox/sandhold") {
			t.Errorf("route %s: class %v section %q, want Local under terminals", rt.Pattern, rt.Capability, rt.Section)
		}
	}
}
