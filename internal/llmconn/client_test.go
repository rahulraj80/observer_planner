package llmconn

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeLLM is a scripted, parameterized upstream that can play any of the three
// dialects. It records the last request it saw so tests can assert on auth,
// body shape, and egress guardrails.
type fakeLLM struct {
	t          *testing.T
	kind       Kind
	status     int
	reply      string
	lastRaw    []byte
	lastHeader http.Header
	authHeader string // expected credential header value ("" = skip check)
}

func (f *fakeLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.lastRaw = body
	f.lastHeader = r.Header.Clone()
	if f.authHeader != "" {
		got := r.Header.Get("Authorization")
		if f.kind == KindAnthropic {
			got = r.Header.Get("x-api-key")
		} else {
			got = strings.TrimPrefix(got, "Bearer ")
		}
		if got != f.authHeader {
			f.t.Errorf("%s: auth mismatch: got %q want %q", f.kind, got, f.authHeader)
		}
	}
	if strings.HasSuffix(r.URL.Path, "/models") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "m1"}, {"id": "m2"}}})
		return
	}
	if f.status != 0 && f.status != http.StatusOK {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch f.kind {
	case KindAnthropic:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1",
			"content": []map[string]string{
				{"type": "text", "text": f.reply},
			},
			"usage": map[string]int{"input_tokens": 11, "output_tokens": 22},
		})
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl_1",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]string{"role": "assistant", "content": f.reply}},
			},
			"usage": map[string]int{"prompt_tokens": 33, "completion_tokens": 44},
		})
	}
}

func (f *fakeLLM) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	return s
}

// newFake returns a client bound to a loopback fake (local classification —
// used for the no-egress dialect). httptest always binds 127.0.0.1, so a
// REMOTE connection is simulated via newRemoteFake with a URL rewrite.
func newFake(t *testing.T, kind Kind, hv string) (*Client, *fakeLLM) {
	t.Helper()
	f := &fakeLLM{t: t, kind: kind, reply: "hello from " + string(kind), authHeader: hv}
	s := f.server(t)
	c := NewClient(Connection{
		ID:        "test",
		Name:      "test",
		Kind:      kind,
		BaseURL:   s.URL,
		APIKeyEnv: "TEST_API_KEY",
	}, hv)
	return c, f
}

// rewriteTransport sends the request to target while preserving the remote
// URL in the request (so URL-derived local/remote classification still works).
type rewriteTransport struct{ target string }

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(r.target)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	req.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

// newRemoteFake is newFake but with a non-loopback BaseURL (remote
// classification) routed to the loopback fake via the rewrite transport.
func newRemoteFake(t *testing.T, kind Kind, hv string) (*Client, *fakeLLM) {
	t.Helper()
	f := &fakeLLM{t: t, kind: kind, reply: "hello from " + string(kind), authHeader: hv}
	s := f.server(t)
	base := "https://api.example.com"
	if kind == KindAnthropic {
		base = "https://anthropic.example"
	}
	c := NewClient(Connection{
		ID:        "test",
		Name:      "test",
		Kind:      kind,
		BaseURL:   base,
		APIKeyEnv: "TEST_API_KEY",
	}, hv)
	c.httpc = &http.Client{Timeout: defaultTimeout, Transport: rewriteTransport{target: s.URL}}
	return c, f
}

func TestCompleteDialects(t *testing.T) {
	tests := []struct {
		name    string
		kind    Kind
		remote  bool
		hv      string
		wantIn  int
		wantOut int
	}{
		{"anthropic", KindAnthropic, true, "sk-remote", 11, 22},
		{"openai_compat", KindOpenAICompat, true, "sk-remote", 33, 44},
		{"local_openai_compat", KindLocalOpenAI, false, "", 33, 44},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				c *Client
				f *fakeLLM
			)
			if tt.remote {
				c, f = newRemoteFake(t, tt.kind, tt.hv)
			} else {
				c, f = newFake(t, tt.kind, tt.hv)
			}
			if tt.remote {
				c = c.WithEgressScrub(redactSecrets, 1<<20)
			}
			res, err := c.Complete(context.Background(), "test-model", "hi")
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if !strings.Contains(res.Text, "hello") {
				t.Errorf("Text = %q", res.Text)
			}
			if res.InputTokens != tt.wantIn || res.OutputTokens != tt.wantOut {
				t.Errorf("usage = %d/%d, want %d/%d", res.InputTokens, res.OutputTokens, tt.wantIn, tt.wantOut)
			}
			if res.LatencyMS < 0 {
				t.Errorf("negative latency")
			}
			// Request path + body shape sanity.
			var msg struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(f.lastRaw, &msg); err != nil || len(msg.Messages) != 1 || msg.Messages[0].Role != "user" {
				t.Errorf("bad messages body: %v body=%s", err, f.lastRaw)
			}
		})
	}
}

// TestExtraHeadersAreSent asserts static connection Headers ride every request
// (Complete + Test) while the dedicated credential still wins for the
// Authorization field and the standard defaults stay intact.
func TestExtraHeadersAreSent(t *testing.T) {
	c, f := newFake(t, KindLocalOpenAI, "sk-local")
	c.conn.Headers = map[string]string{
		"X-API-Key": "custom",
		"X-Routing": "north",
		"X-Title":   "should-not-clobber",
	}
	if _, err := c.Complete(context.Background(), "m", "hi"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := f.lastHeader.Get("X-API-Key"); got != "custom" {
		t.Errorf("X-API-Key = %q, want custom", got)
	}
	if got := f.lastHeader.Get("X-Routing"); got != "north" {
		t.Errorf("X-Routing = %q, want north", got)
	}
	// Attribution default wins over the user header (applyExtraHeaders runs
	// before the defaults? no — defaults are set first, so the user header
	// must NOT clobber X-Title).
	if got := f.lastHeader.Get("X-Title"); got != "superbased-observer analysis" {
		t.Errorf("X-Title clobbered: %q", got)
	}
	// The dedicated credential still wins for Authorization.
	if got := strings.TrimPrefix(f.lastHeader.Get("Authorization"), "Bearer "); got != "sk-local" {
		t.Errorf("Authorization = %q, want sk-local", got)
	}
	// Test also applies extra headers.
	if _, _ = c.Test(context.Background()); f.lastHeader.Get("X-Routing") != "north" {
		t.Error("extra header missing from Test request")
	}
}

// without a resolved credential — never leaking a keyless call upstream.
func TestRemoteRequiresCredential(t *testing.T) {
	c, _ := newRemoteFake(t, KindOpenAICompat, "")
	if _, err := c.Complete(context.Background(), "m", "hi"); err == nil {
		t.Fatal("remote without credential: expected error, got nil")
	}
}

// TestWithSystemPinsTheSystemMessage asserts the system prompt rides in the
// request for both dialects and is omitted when unset.
func TestWithSystemPinsTheSystemMessage(t *testing.T) {
	// OpenAI dialect (local fake).
	c, f := newFake(t, KindLocalOpenAI, "")
	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		System string `json:"system"`
	}
	if _, err := c.Complete(context.Background(), "m", "hi"); err != nil {
		t.Fatalf("Complete without system: %v", err)
	}
	if err := json.Unmarshal(f.lastRaw, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 1 || body.System != "" {
		t.Fatalf("no system → want 1 user message, no top-level system; got messages=%d system=%q", len(body.Messages), body.System)
	}
	if _, err := c.WithSystem("reply only in JSON").Complete(context.Background(), "m", "hi"); err != nil {
		t.Fatalf("Complete with system: %v", err)
	}
	if err := json.Unmarshal(f.lastRaw, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[0].Content != "reply only in JSON" || body.Messages[1].Role != "user" {
		t.Fatalf("system not pinned first: %+v", body.Messages)
	}

	// Anthropic dialect carries system at the top level.
	ac, af := newFake(t, KindAnthropic, "sk-local")
	if _, err := ac.WithSystem("be a strict judge").Complete(context.Background(), "m", "hi"); err != nil {
		t.Fatalf("anthropic Complete: %v", err)
	}
	if err := json.Unmarshal(af.lastRaw, &body); err != nil {
		t.Fatal(err)
	}
	if body.System != "be a strict judge" {
		t.Fatalf("anthropic system = %q", body.System)
	}
}

// TestEgressScrubAppliesOnlyToRemote asserts the scrub+capsule path fires only
// when a caller opts in via WithEgressScrub (as the wiring does for REMOTE
// connections); the client default — the local path — sends the payload
// untouched (loopback, no egress).
func TestEgressScrubAppliesOnlyToRemote(t *testing.T) {
	secret := "sk-secret-abc"
	remote, f := newRemoteFake(t, KindOpenAICompat, "sk-remote")
	remote = remote.WithEgressScrub(redactSecrets, 1<<20)
	if _, err := remote.Complete(context.Background(), "m", "token is "+secret); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(f.lastRaw), secret) {
		t.Error("remote: secret leaked in outbound payload")
	}

	local, fl := newFake(t, KindLocalOpenAI, "")
	if _, err := local.Complete(context.Background(), "m", "token is "+secret); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fl.lastRaw), secret) {
		t.Error("local: payload was scrubbed despite no egress")
	}
}

// TestNumCtxLocalOnly asserts num_ctx is passed through only on a loopback
// connection.
func TestNumCtxLocalOnly(t *testing.T) {
	t.Run("local passes num_ctx", func(t *testing.T) {
		c := NewClient(Connection{ID: "c", Kind: KindOpenAICompat, BaseURL: "http://127.0.0.1:9999"}, "").WithTuning(512, 8192)
		_, _, body, err := c.buildRequest("m", "hi")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"num_ctx":8192`) {
			t.Errorf("local: num_ctx missing: %s", body)
		}
	})
	t.Run("remote omits num_ctx", func(t *testing.T) {
		c := NewClient(Connection{ID: "c", Kind: KindOpenAICompat, BaseURL: "https://api.example.com"}, "k").WithTuning(512, 8192)
		_, _, body, err := c.buildRequest("m", "hi")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "num_ctx") {
			t.Errorf("remote: num_ctx leaked upstream: %s", body)
		}
	})
}

// TestPromptCap asserts the egress cap truncates oversized payloads.
func TestPromptCap(t *testing.T) {
	c, f := newRemoteFake(t, KindOpenAICompat, "k")
	c = c.WithEgressScrub(nil, 16)
	big := strings.Repeat("x", 1000)
	if _, err := c.Complete(context.Background(), "m", big); err != nil {
		t.Fatal(err)
	}
	if len(f.lastRaw) > 256 {
		t.Errorf("payload not capped: %d bytes", len(f.lastRaw))
	}
}

// TestRetriesOn5xxAndNoRetryOn4xx asserts retry policy.
func TestRetriesOn5xxAndNoRetryOn4xx(t *testing.T) {
	t.Run("4xx returns immediately", func(t *testing.T) {
		c, _ := newRemoteFake(t, KindOpenAICompat, "k")
		f := &fakeLLM{t: t, kind: KindOpenAICompat, status: http.StatusBadRequest}
		s := f.server(t)
		c.conn.BaseURL = s.URL
		c.httpc = &http.Client{Timeout: defaultTimeout, Transport: rewriteTransport{target: s.URL}}
		if _, err := c.Complete(context.Background(), "m", "hi"); err == nil {
			t.Fatal("expected error for 400")
		}
		if c.retries != 1 { // single attempt budget untouched by 4xx
			t.Fatalf("retries=%d", c.retries)
		}
	})
	t.Run("5xx retries and recovers", func(t *testing.T) {
		var calls int
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
		}))
		t.Cleanup(s.Close)
		c := NewClient(Connection{ID: "c", Kind: KindOpenAICompat, BaseURL: s.URL, DefaultModel: "m"}, "k").WithRetries(2)
		res, err := c.Complete(context.Background(), "", "")
		if err != nil {
			t.Fatalf("expected recovery: %v", err)
		}
		if res.Text != "ok" {
			t.Errorf("Text = %q", res.Text)
		}
	})
}

// TestTestListsModels asserts the health/models probe for both dialect groups.
func TestTestListsModels(t *testing.T) {
	for _, kind := range []Kind{KindAnthropic, KindOpenAICompat} {
		c, _ := newFake(t, kind, "k")
		h, models := c.Test(context.Background())
		if !h.OK {
			t.Errorf("%s: health = %+v", kind, h)
		}
		if len(models) != 2 || models[0] != "m1" {
			t.Errorf("%s: models = %v", kind, models)
		}
	}
}

// redactSecrets is a tiny stand-in for scrub.New().Scrubber so tests don't
// drag in the scrubbing package.
func redactSecrets(s string) string {
	for _, prefix := range []string{"sk-", "Bearer ", "AIza"} {
		s = strings.ReplaceAll(s, prefix, strings.Repeat("*", 4))
	}
	return s
}
