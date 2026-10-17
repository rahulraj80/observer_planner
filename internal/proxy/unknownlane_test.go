package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestUnknownUpstreamLaneNeverForwardsCredentials (Sol A5-02, D-032): a
// request addressed to an `/up/<id>/` lane this node cannot route must be
// refused locally. Before the fix it fell through to the fixed upstream the
// path shape matched, carrying the credential meant for the lane's provider
// (an OpenCode Zen key sent to OpenAI, say), and every row below reached a
// fixed upstream.
//
// Each row counts hits on EVERY upstream the proxy knows (the three fixed
// providers and a configured lane) and requires zero, then checks the local
// refusal names the lane.
func TestUnknownUpstreamLaneNeverForwardsCredentials(t *testing.T) {
	const secret = "sk-lane-secret-123"
	cases := []struct {
		name        string
		lanes       map[string]string // nil = no [proxy.upstreams]
		autoDefault bool              // configure auto_default_lane = "lane-a"
		method      string
		path        string
		header      map[string]string
		body        string
		wantLane    string
	}{
		{
			name: "unknown id, Authorization, lanes configured", lanes: map[string]string{"lane-a": ""},
			method: http.MethodPost, path: "/up/opencode-zen/v1/chat/completions",
			header: map[string]string{"Authorization": "Bearer " + secret},
			body:   `{"model":"m","messages":[]}`, wantLane: "opencode-zen",
		},
		{
			name: "unknown id, Authorization, NO lanes configured", lanes: nil,
			method: http.MethodPost, path: "/up/opencode-zen/v1/chat/completions",
			header: map[string]string{"Authorization": "Bearer " + secret},
			body:   `{"model":"m","messages":[]}`, wantLane: "opencode-zen",
		},
		{
			name: "unknown id, X-Api-Key on an Anthropic path", lanes: map[string]string{"lane-a": ""},
			method: http.MethodPost, path: "/up/typo/v1/messages",
			header: map[string]string{"X-Api-Key": secret},
			body:   `{"model":"m","messages":[]}`, wantLane: "typo",
		},
		{
			name: "unknown id, X-Goog-Api-Key on a Gemini path", lanes: map[string]string{"lane-a": ""},
			method: http.MethodPost, path: "/up/typo/v1beta/models/gemini-2.5-pro:generateContent",
			header: map[string]string{"X-Goog-Api-Key": secret},
			body:   `{"contents":[]}`, wantLane: "typo",
		},
		{
			name: "unknown id, Azure Api-Key", lanes: map[string]string{"lane-a": ""},
			method: http.MethodPost, path: "/up/azure-east/v1/chat/completions",
			header: map[string]string{"Api-Key": secret},
			body:   `{"model":"m","messages":[]}`, wantLane: "azure-east",
		},
		{
			name: "unknown id, credential in ?key= query", lanes: nil,
			method: http.MethodPost, path: "/up/typo/v1beta/models/gemini-2.5-pro:generateContent?key=" + secret,
			body: `{"contents":[]}`, wantLane: "typo",
		},
		{
			name: "auto lane, no default and no matching prefix", lanes: map[string]string{"lane-a": ""},
			method: http.MethodPost, path: "/up/auto/v1/chat/completions",
			header: map[string]string{"Authorization": "Bearer " + secret},
			body:   `{"model":"unmatched/gpt-mini","messages":[]}`, wantLane: "auto",
		},
		{
			name: "auto lane, NO lanes configured", lanes: nil,
			method: http.MethodPost, path: "/up/auto/v1/chat/completions",
			header: map[string]string{"Authorization": "Bearer " + secret},
			body:   `{"model":"gpt-mini","messages":[]}`, wantLane: "auto",
		},
		{
			name: "auto lane websocket upgrade, no default", lanes: map[string]string{"lane-a": ""},
			// Not a Responses path: that one is answered 426 (HTTP fallback)
			// before lane resolution, which forwards nothing either.
			method: http.MethodGet, path: "/up/auto/v1/realtime",
			header: map[string]string{
				"Authorization": "Bearer " + secret, "Connection": "Upgrade", "Upgrade": "websocket",
				"Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ==",
			},
			wantLane: "auto",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			counting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				t.Errorf("upstream dialed: %s %s (auth=%q x-api-key=%q goog=%q api-key=%q query=%q)",
					r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"),
					r.Header.Get("X-Goog-Api-Key"), r.Header.Get("Api-Key"), r.URL.RawQuery)
				w.WriteHeader(http.StatusNotFound)
			}))
			defer counting.Close()

			var lanes map[string]string
			if tc.lanes != nil {
				lanes = map[string]string{}
				for id := range tc.lanes {
					lanes[id] = counting.URL
				}
			}
			opts := Options{
				AnthropicUpstream: counting.URL,
				OpenAIUpstream:    counting.URL,
				GeminiUpstream:    counting.URL,
				Upstreams:         lanes,
				Sink:              &fakeSink{},
				Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			if tc.autoDefault {
				opts.AutoDefaultLane = "lane-a"
			}
			p, err := New(opts)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			p.Handler().ServeHTTP(rec, req)

			if n := hits.Load(); n != 0 {
				t.Fatalf("upstream was dialed %d time(s); an unresolvable lane must never leave the node", n)
			}
			if rec.Code != unknownLaneStatus {
				t.Fatalf("status = %d, want %d (local refusal): %s", rec.Code, unknownLaneStatus, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("refusal body is not JSON: %v: %s", err, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `\"`+tc.wantLane+`\"`) {
				t.Errorf("refusal does not name lane %q: %s", tc.wantLane, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), secret) {
				t.Errorf("refusal echoed the credential: %s", rec.Body.String())
			}
		})
	}
}

// TestMalformedUpstreamLaneNeverForwardsCredentials (pass-2 review C156-18):
// a lane address with no id or no path after its id used to be read as "no
// prefix" and forwarded to the fixed upstream its path shape matched, with
// the caller's credential (isOpenAIPath matches /chat/completions anywhere).
// Every row must be refused locally with zero upstream hits, whether or not
// the named lane exists.
func TestMalformedUpstreamLaneNeverForwardsCredentials(t *testing.T) {
	const secret = "sk-lane-secret-456"
	cases := []struct {
		name   string
		path   string
		header map[string]string
	}{
		{"empty id before a chat path", "/up//v1/chat/completions", map[string]string{"Authorization": "Bearer " + secret}},
		{"empty id before a messages path", "/up//v1/messages", map[string]string{"X-Api-Key": secret}},
		{"empty id, key in the query", "/up//v1beta/models/gemini-2.5-pro:generateContent", nil},
		{"configured id with no API path", "/up/lane-a", map[string]string{"Authorization": "Bearer " + secret}},
		{"bare /up/", "/up/", map[string]string{"X-Api-Key": secret}},
		{"bare /up", "/up", map[string]string{"X-Api-Key": secret}},
		{"leading double slash, empty id", "//up//v1/chat/completions", map[string]string{"Authorization": "Bearer " + secret}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			counting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				t.Errorf("upstream dialed: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}))
			defer counting.Close()
			p, err := New(Options{
				AnthropicUpstream: counting.URL,
				OpenAIUpstream:    counting.URL,
				GeminiUpstream:    counting.URL,
				Upstreams:         map[string]string{"lane-a": counting.URL},
				Sink:              &fakeSink{},
				Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8820/", strings.NewReader(`{"model":"m","messages":[]}`))
			req.URL.Path = tc.path // set raw: "//up/..." must not parse as a host
			if tc.header == nil {
				req.URL.RawQuery = "key=" + secret
			}
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			p.Handler().ServeHTTP(rec, req)
			if n := hits.Load(); n != 0 {
				t.Fatalf("upstream was dialed %d time(s); a malformed lane address must never leave the node", n)
			}
			if rec.Code != malformedLaneStatus {
				t.Fatalf("status = %d, want %d (local refusal): %s", rec.Code, malformedLaneStatus, rec.Body.String())
			}
			if !json.Valid(rec.Body.Bytes()) {
				t.Fatalf("refusal body is not JSON: %s", rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), secret) {
				t.Errorf("refusal echoed the credential: %s", rec.Body.String())
			}
		})
	}
}

// TestLeadingDoubleSlashLaneStillRoutes: a base URL with a trailing slash
// yields `//up/<id>/...`; a well-formed lane behind it reaches its own
// upstream, never a fixed one.
func TestLeadingDoubleSlashLaneStillRoutes(t *testing.T) {
	var laneHits, fixedHits atomic.Int32
	okBody := `{"id":"x","model":"m","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	serve := func(c *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			c.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(okBody))
		}))
	}
	lane, fixed := serve(&laneHits), serve(&fixedHits)
	defer lane.Close()
	defer fixed.Close()
	p, err := New(Options{
		AnthropicUpstream: fixed.URL,
		OpenAIUpstream:    fixed.URL,
		Upstreams:         map[string]string{"lane-a": lane.URL},
		Sink:              &fakeSink{},
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8820/", strings.NewReader(`{"model":"m","messages":[]}`))
	req.URL.Path = "//up/lane-a/v1/chat/completions" // set raw: it must not parse as a host
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || laneHits.Load() != 1 || fixedHits.Load() != 0 {
		t.Fatalf("status=%d lane=%d fixed=%d, want 200/1/0: %s", rec.Code, laneHits.Load(), fixedHits.Load(), rec.Body.String())
	}
}

// TestKnownLanesStillRoute guards the other side of the refusal: a configured
// lane, a configured agent lane and a resolvable auto lane all still reach
// their own upstream.
func TestKnownLanesStillRoute(t *testing.T) {
	var laneHits, agentHits, fixedHits atomic.Int32
	okBody := `{"id":"x","model":"m","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	serve := func(c *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			c.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(okBody))
		}))
	}
	lane, agent, fixed := serve(&laneHits), serve(&agentHits), serve(&fixedHits)
	defer lane.Close()
	defer agent.Close()
	defer fixed.Close()

	p, err := New(Options{
		AnthropicUpstream: fixed.URL,
		OpenAIUpstream:    fixed.URL,
		Upstreams:         map[string]string{"lane-a": lane.URL},
		AgentUpstreams:    map[string]string{"zen": agent.URL},
		AutoDefaultLane:   "lane-a",
		Sink:              &fakeSink{},
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, path := range []string{"/up/lane-a/v1/chat/completions", "/up/zen/v1/chat/completions", "/up/auto/v1/chat/completions"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"m","messages":[]}`))
		rec := httptest.NewRecorder()
		p.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s -> %d, want 200: %s", path, rec.Code, rec.Body.String())
		}
	}
	if laneHits.Load() != 2 || agentHits.Load() != 1 || fixedHits.Load() != 0 {
		t.Errorf("hits lane=%d agent=%d fixed=%d, want 2/1/0", laneHits.Load(), agentHits.Load(), fixedHits.Load())
	}
}
