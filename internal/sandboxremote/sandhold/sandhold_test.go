package sandhold

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// fakeKey is the key the fake node grants; no output may carry it.
const fakeKey = "sandhold_0123456789abcdef0123456789abcdef01234567"

// fakeNode is an httptest SandHold node: the device grant answers a scripted
// list of token replies, then the key's whoami and targets.
type fakeNode struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	tokens   []string // the OAuth error codes to answer, then "" for the grant
	polls    int
	page     string // overrides verification_uri_complete ("" = on this node)
	interval int
	keySeen  []string // X-API-Key values seen
	codeBody map[string]string
	revoked  []string
}

func newFakeNode(t *testing.T, tokens ...string) *fakeNode {
	f := &fakeNode{t: t, tokens: tokens, interval: 3}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/device/code", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "" {
			t.Errorf("the sign-in start carried a key")
		}
		_ = json.NewDecoder(r.Body).Decode(&f.codeBody)
		page := f.page
		if page == "" {
			page = f.srv.URL + "/app/#/device?code=BCDF-GHJK"
		}
		writeJSON(w, 200, map[string]any{
			"device_code": "dev-secret", "user_code": "BCDF-GHJK",
			"verification_uri": f.srv.URL + "/app/#/device", "verification_uri_complete": page,
			"expires_in": 600, "interval": f.interval,
		})
	})
	mux.HandleFunc("POST /v1/device/token", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.polls++
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["device_code"] != "dev-secret" {
			writeJSON(w, 400, map[string]string{"error": "invalid_grant", "error_description": "unknown device code"})
			return
		}
		next := ""
		if len(f.tokens) > 0 {
			next, f.tokens = f.tokens[0], f.tokens[1:]
		}
		if next != "" {
			writeJSON(w, 400, map[string]string{"error": next, "error_description": "node says " + next + "\x1b[31m"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"access_token": fakeKey, "token_type": "Bearer", "expires_in": 2592000,
			"key_id": "key_42", "expires_at": "2026-12-11T00:00:00Z", "org": "acme", "user": "alice\x1b]0;pwned\x07",
		})
	})
	mux.HandleFunc("GET /v1/whoami", func(w http.ResponseWriter, r *http.Request) {
		f.see(r)
		if r.Header.Get("X-API-Key") != fakeKey {
			writeJSON(w, 401, map[string]string{"error": "no valid credential", "fix": "sign in again"})
			return
		}
		writeJSON(w, 200, map[string]any{"org": "acme", "user": "alice", "keyId": "key_42", "via": "api_key"})
	})
	mux.HandleFunc("GET /v1/targets", func(w http.ResponseWriter, r *http.Request) {
		f.see(r)
		writeJSON(w, 200, map[string]any{"targets": []map[string]any{
			{
				"id": "kata", "tier": "vault", "tierMeans": "its own kernel in a virtual machine", "tierRank": 4, "state": "active",
				"capabilities": map[string]any{"isolation": "microvm", "egressAllowlist": true},
			},
			{
				"id": "jail", "tier": "shared", "tierMeans": "a jailed process on the host kernel", "tierRank": 1, "state": "active", "default": true,
				"capabilities": map[string]any{"isolation": "process", "egressAllowlist": true},
			},
			{"id": "old", "tier": "shielded", "tierRank": 2, "state": "suspended"},
		}})
	})
	mux.HandleFunc("DELETE /v1/keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.see(r)
		f.mu.Lock()
		f.revoked = append(f.revoked, r.PathValue("id"))
		f.mu.Unlock()
		w.WriteHeader(204)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeNode) see(r *http.Request) {
	f.mu.Lock()
	f.keySeen = append(f.keySeen, r.Header.Get("X-API-Key"))
	f.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// recordSleep records each wait instead of sleeping.
type recordSleep struct{ waits []time.Duration }

func (r *recordSleep) sleep(_ context.Context, d time.Duration) error {
	r.waits = append(r.waits, d)
	return nil
}

func TestSignIn(t *testing.T) {
	cases := []struct {
		name      string
		tokens    []string
		page      string // "" = on the node; "other" = another host
		wantErr   error
		wantWaits []time.Duration
		wantSame  bool
	}{
		{name: "approved", tokens: []string{"authorization_pending", "authorization_pending", ""}, wantWaits: []time.Duration{3 * time.Second, 3 * time.Second, 3 * time.Second}, wantSame: true},
		{name: "slow_down", tokens: []string{"slow_down", "authorization_pending", "slow_down", ""}, wantWaits: []time.Duration{3 * time.Second, 8 * time.Second, 8 * time.Second, 13 * time.Second}, wantSame: true},
		{name: "denied", tokens: []string{"authorization_pending", "access_denied"}, wantErr: ErrDenied, wantWaits: []time.Duration{3 * time.Second, 3 * time.Second}, wantSame: true},
		{name: "expired", tokens: []string{"expired_token"}, wantErr: ErrExpired, wantWaits: []time.Duration{3 * time.Second}, wantSame: true},
		{name: "page_elsewhere", tokens: []string{""}, page: "other", wantWaits: []time.Duration{3 * time.Second}, wantSame: false},
		{name: "page_not_web", tokens: []string{""}, page: "javascript:alert(1)", wantErr: ErrBadPage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeNode(t, tc.tokens...)
			switch tc.page {
			case "other":
				f.page = "https://evil.example/app/#/device?code=BCDF-GHJK"
			default:
				f.page = tc.page
			}
			rs := &recordSleep{}
			var shown Start
			var same bool
			s := &SignIn{
				Client: NewClient(f.srv.URL, ""), ClientID: "Observer on test", Sleep: rs.sleep,
				Shown: func(st Start, sameNode bool) { shown, same = st, sameNode },
			}
			g, err := s.Run(context.Background())
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if strings.Contains(fmt.Sprint(err), "\x1b") {
					t.Errorf("a node's control character reached the error: %q", err)
				}
			} else {
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				if g.Key != fakeKey || g.KeyID != "key_42" || g.Org != "acme" {
					t.Errorf("grant %+v", g)
				}
				if strings.ContainsAny(g.User, "\x1b\x07") {
					t.Errorf("control characters kept in the user name: %q", g.User)
				}
				if shown.UserCode != "BCDF-GHJK" || same != tc.wantSame {
					t.Errorf("shown %+v same %v, want same %v", shown, same, tc.wantSame)
				}
			}
			if fmt.Sprint(rs.waits) != fmt.Sprint(tc.wantWaits) {
				t.Errorf("waits %v, want %v", rs.waits, tc.wantWaits)
			}
			if f.codeBody["client_id"] != "Observer on test" && tc.page != "javascript:alert(1)" {
				t.Errorf("client_id %q", f.codeBody["client_id"])
			}
		})
	}
}

// TestSignInDeadline: a node that keeps answering pending ends the wait
// once the code's lifetime has passed.
func TestSignInDeadline(t *testing.T) {
	pending := make([]string, 300)
	for i := range pending {
		pending[i] = "authorization_pending"
	}
	f := newFakeNode(t, pending...)
	clock := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	s := &SignIn{
		Client: NewClient(f.srv.URL, ""), Now: func() time.Time { return clock },
		Sleep: func(_ context.Context, d time.Duration) error { clock = clock.Add(d); return nil },
	}
	if _, err := s.Run(context.Background()); !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
	if f.polls < 190 || f.polls > 201 {
		t.Errorf("polled %d times in 600 s at 3 s", f.polls)
	}
}

// TestSignInNoBrowserLogin: a node without the device grant answers 404.
func TestSignInNoBrowserLogin(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	s := &SignIn{Client: NewClient(srv.URL, ""), Sleep: (&recordSleep{}).sleep}
	if _, err := s.Run(context.Background()); !errors.Is(err, ErrNoBrowserSignIn) {
		t.Fatalf("err = %v", err)
	}
}

// TestRedirectRefused: a redirect is never followed, so the key never
// reaches the address it points to, plain http or not.
func TestRedirectRefused(t *testing.T) {
	var elsewhere []string
	var mu sync.Mutex
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		elsewhere = append(elsewhere, r.Header.Get("X-API-Key"))
		mu.Unlock()
	}))
	t.Cleanup(other.Close)
	for _, status := range []int{301, 302, 307, 308} {
		node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL+r.URL.Path, status)
		}))
		c := NewClient(node.URL, fakeKey)
		_, err := c.Whoami(context.Background())
		node.Close()
		if !errors.Is(err, ErrRedirect) {
			t.Errorf("%d: err = %v, want ErrRedirect", status, err)
		}
		if strings.Contains(fmt.Sprint(err), fakeKey) {
			t.Errorf("%d: the key is in the error", status)
		}
	}
	if len(elsewhere) != 0 {
		t.Fatalf("the redirect target was called %d times (keys %q)", len(elsewhere), elsewhere)
	}
}

func TestNodeURL(t *testing.T) {
	cases := []struct {
		in, want string
		bad      bool
	}{
		{in: "https://Node.Example.com/", want: "https://node.example.com"},
		{in: "node.example.com", want: "https://node.example.com"},
		{in: "https://node.example.com:8443/sandhold/", want: "https://node.example.com:8443/sandhold"},
		{in: "http://localhost:8080", want: "http://localhost:8080"},
		{in: "http://127.0.0.1:9", want: "http://127.0.0.1:9"},
		{in: "http://[::1]:9", want: "http://[::1]:9"},
		{in: "http://node.example.com", bad: true},
		{in: "http://10.0.0.5", bad: true},
		{in: "http://localhost.evil.com", bad: true},
		{in: "ftp://node.example.com", bad: true},
		{in: "https://user:pw@node.example.com", bad: true},
		{in: "https://node.example.com/?x=1", bad: true},
		{in: "https://node.example.com/#frag", bad: true},
		{in: "", bad: true},
	}
	for _, tc := range cases {
		got, err := NodeURL(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("NodeURL(%q) = %q, want a refusal", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("NodeURL(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

// TestRefusalCarriesNodeWords: a refusal's error and fix reach the caller
// unchanged but for control characters.
func TestRefusalCarriesNodeWords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 403, map[string]string{"error": "the org's ceiling does not allow pypi.org\x1b[2J", "fix": "ask an org admin to widen [policy.org_ceiling]"})
	}))
	t.Cleanup(srv.Close)
	_, err := NewClient(srv.URL, fakeKey).Targets(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 403 {
		t.Fatalf("err = %v", err)
	}
	if ae.Msg != "the org's ceiling does not allow pypi.org[2J" || ae.Fix != "ask an org admin to widen [policy.org_ceiling]" {
		t.Errorf("msg %q fix %q", ae.Msg, ae.Fix)
	}
}

func TestRevokeKeyChecksID(t *testing.T) {
	f := newFakeNode(t)
	c := NewClient(f.srv.URL, fakeKey)
	if err := c.RevokeKey(context.Background(), "../admin"); err == nil {
		t.Fatal("a crafted key id reached the address")
	}
	if err := c.RevokeKey(context.Background(), "key_42"); err != nil {
		t.Fatal(err)
	}
	if len(f.revoked) != 1 || f.revoked[0] != "key_42" {
		t.Errorf("revoked %v", f.revoked)
	}
}

func TestPlain(t *testing.T) {
	cases := map[string]string{
		"plain words":            "plain words",
		"a\x1b[31mred":           "a[31mred",
		"bell\x07 and del\x7f":   "bell and del",
		"c1 \u009b2J":            "c1 2J",
		"raw 8-bit \x9b2J":       "raw 8-bit �2J",
		"tab\tand\nnewline kept": "tab\tand\nnewline kept",
	}
	for in, want := range cases {
		if got := Plain(in); got != want {
			t.Errorf("Plain(%q) = %q, want %q", in, got, want)
		}
	}
	if got := PlainLine("two\nlines\tand  tabs"); got != "two lines and tabs" {
		t.Errorf("PlainLine = %q", got)
	}
}

// TestVocabularyMapping is the mapping table: a target's level when it names
// one, else its tier, one to one; the node's network words to Observer's
// tiers.
func TestVocabularyMapping(t *testing.T) {
	levels := []struct {
		level string
		want  vocab.IsoClass
	}{
		{"process", vocab.ClassProcess},
		{"container", vocab.ClassContainer},
		{"user-kernel", vocab.ClassUserspaceKernel},
		{"microvm", vocab.ClassHWVM},
	}
	for _, tc := range levels {
		if got, ok := ClassForLevel(tc.level); !ok || got != tc.want {
			t.Errorf("level %s = %q, want %q", tc.level, got, tc.want)
		}
	}
	tiers := []struct {
		tier string
		want vocab.IsoClass
	}{
		{"shared", vocab.ClassProcess},
		{"shielded", vocab.ClassContainer},
		{"sealed", vocab.ClassUserspaceKernel},
		{"vault", vocab.ClassHWVM},
		{"Vault", vocab.ClassHWVM},
	}
	for _, tc := range tiers {
		if got, ok := ClassForTier(tc.tier); !ok || got != tc.want {
			t.Errorf("tier %s = %q, want %q", tc.tier, got, tc.want)
		}
	}
	for _, unknown := range []string{"", "auto", "kata", "hw_vm"} {
		if _, ok := ClassForTier(unknown); ok {
			t.Errorf("tier %q mapped", unknown)
		}
		if _, ok := ClassForLevel(unknown); ok {
			t.Errorf("level %q mapped", unknown)
		}
	}
	nets := []struct {
		word string
		want vocab.Tier
	}{{"none", vocab.TierNone}, {"limited", vocab.TierProxyOnly}, {"open", vocab.TierInternet}}
	for _, tc := range nets {
		if got, ok := TierForNetwork(tc.word); !ok || got != tc.want {
			t.Errorf("network %s = %q, want %q", tc.word, got, tc.want)
		}
	}
	if _, ok := TierForNetwork("host"); ok {
		t.Error("host mapped: a hosted box never shares this machine's network")
	}

	// The level wins over the tier: a Shielded target whose level is
	// user-kernel is userspace_kernel; one with no level falls back to the
	// tier; neither known gives no class.
	var tg Target
	tg.Tier, tg.Capabilities.Isolation = "shielded", "user-kernel"
	if c, from, _ := TargetClass(tg); c != vocab.ClassUserspaceKernel || from != ClassFromLevel {
		t.Errorf("level first: %s from %s", c, from)
	}
	tg.Capabilities.Isolation = ""
	if c, from, _ := TargetClass(tg); c != vocab.ClassContainer || from != ClassFromTier {
		t.Errorf("tier fallback: %s from %s", c, from)
	}
	tg.Tier = "something-new"
	if _, _, ok := TargetClass(tg); ok {
		t.Error("an unknown isolation got a class")
	}
	if got := TargetTiers(Target{}); fmt.Sprint(got) != "[internet]" {
		t.Errorf("no allow list: %v", got)
	}
	tg.Capabilities.EgressAllowlist = true
	if got := TargetTiers(tg); fmt.Sprint(got) != "[none proxy_only internet]" {
		t.Errorf("allow list: %v", got)
	}
}

func TestOffers(t *testing.T) {
	f := newFakeNode(t)
	ts, err := NewClient(f.srv.URL, fakeKey).Targets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	offers := Offers(ts)
	if len(offers) != 2 {
		t.Fatalf("offers %+v (a suspended target is not offered)", offers)
	}
	if offers[0].Tier != "shared" || offers[0].Class != vocab.ClassProcess || !offers[0].Default || offers[1].Class != vocab.ClassHWVM {
		t.Errorf("offers %+v", offers)
	}
	want := "Vault (its own kernel in a virtual machine): Observer class hw_vm; network none, limited, open; runs on kata"
	if got := offers[1].Line(); got != want {
		t.Errorf("line %q\nwant %q", got, want)
	}
}

func TestResolve(t *testing.T) {
	saved := &Credential{URL: "https://node.example.com", Key: fakeKey, KeyID: "key_42"}
	env := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	cases := []struct {
		name    string
		saved   *Credential
		env     map[string]string
		wantURL string
		wantKey string
		fromEnv bool
		bad     bool
	}{
		{name: "saved", saved: saved, wantURL: saved.URL, wantKey: fakeKey},
		{name: "env key wins", saved: saved, env: map[string]string{EnvKey: "ci-key"}, wantURL: saved.URL, wantKey: "ci-key", fromEnv: true},
		{name: "env url elsewhere drops saved key", saved: saved, env: map[string]string{EnvURL: "https://other.example.com"}, wantURL: "https://other.example.com", wantKey: ""},
		{name: "env url same node keeps saved key", saved: saved, env: map[string]string{EnvURL: "https://NODE.example.com/"}, wantURL: saved.URL, wantKey: fakeKey},
		{name: "env both", env: map[string]string{EnvURL: "https://ci.example.com", EnvKey: "ci-key"}, wantURL: "https://ci.example.com", wantKey: "ci-key", fromEnv: true},
		{name: "env key without node", env: map[string]string{EnvKey: "ci-key"}, bad: true},
		{name: "env http url refused", env: map[string]string{EnvURL: "http://ci.example.com"}, bad: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Resolve(tc.saved, env(tc.env))
			if tc.bad {
				if err == nil {
					t.Fatalf("want a refusal, got %+v", s)
				}
				return
			}
			if err != nil || s.URL != tc.wantURL || s.Key != tc.wantKey || s.FromEnv != tc.fromEnv {
				t.Fatalf("got %+v %v", s, err)
			}
		})
	}
}

// TestCredentialNeverPrintsItsKey: every fmt verb and the JSON of the
// non-secret half leave the key out.
func TestCredentialNeverPrintsItsKey(t *testing.T) {
	c := Credential{URL: "https://node.example.com", Key: fakeKey, KeyID: "key_42"}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		if got := fmt.Sprintf(verb, c); strings.Contains(got, fakeKey) || strings.Contains(got, "sandhold_0123") {
			t.Errorf("%s printed the key: %s", verb, got)
		}
	}
	b, err := EncodeCredential(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeCredential(b)
	if err != nil || back.Key != fakeKey || back.URL != c.URL || back.KeyID != "key_42" {
		t.Fatalf("round trip %v %v", back.URL, err)
	}
	if _, err := DecodeCredential([]byte(`{"version":1,"url":"x","key":"` + fakeKey + `"`)); err == nil || strings.Contains(err.Error(), fakeKey) {
		t.Errorf("a broken value: %v", err)
	}
	conn := Connection{Version: 1, URL: c.URL, KeyID: c.KeyID, User: "alice", Org: "acme"}
	j, _ := json.Marshal(conn)
	if strings.Contains(string(j), fakeKey) {
		t.Error("the connection record holds the key")
	}
}

func TestConnectionSummary(t *testing.T) {
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	var none *Connection
	if got := none.Summary(now); got != NotConnected {
		t.Errorf("nil: %q", got)
	}
	c := &Connection{
		URL: "https://node.example.com", User: "alice", Org: "acme", ExpiresAt: now.Add(48 * time.Hour),
		Offers: []Offer{{Tier: "shared", Class: vocab.ClassProcess}, {Tier: "vault", Class: vocab.ClassHWVM}},
	}
	want := "connected to https://node.example.com as alice in acme; key works until 13 Nov 2026; offers Shared = process, Vault = hw_vm"
	if got := c.Summary(now); got != want {
		t.Errorf("summary %q\nwant %q", got, want)
	}
	if !c.Expired(now.Add(72*time.Hour)) || c.Expired(now) {
		t.Error("expiry")
	}
	if got := c.Summary(now.Add(72 * time.Hour)); !strings.Contains(got, "expired") {
		t.Errorf("expired summary %q", got)
	}
}
