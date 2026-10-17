package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestLanesEndpointReportsLiveTable pins T-308: GET /-/lanes lists the ids
// of the LIVE lane table (a table installed after construction, as the org's
// gateway.providers rail does) plus the coding-agent lanes, never forwards,
// and refuses other methods.
func TestLanesEndpointReportsLiveTable(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream backend was hit for %s %s; /-/lanes must never forward", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer backend.Close()

	p, err := New(Options{
		AnthropicUpstream: backend.URL,
		OpenAIUpstream:    backend.URL,
		Sink:              &fakeSink{},
		Upstreams:         map[string]string{"local-cfg": backend.URL},
		AgentUpstreams:    map[string]string{"grok": backend.URL},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(p.Handler())
	defer ts.Close()

	get := func() LaneInfo {
		t.Helper()
		resp, err := http.Get(ts.URL + lanesPath)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var li LaneInfo
		if err := json.NewDecoder(resp.Body).Decode(&li); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return li
	}
	if got, want := get().Lanes, []string{"auto", "grok", "local-cfg"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("config lanes = %v, want %v", got, want)
	}
	// An org-distributed table replaces the hosted lanes live.
	if err := p.SetLaneTable(map[string]string{"org-lane": backend.URL}, "org-lane"); err != nil {
		t.Fatalf("SetLaneTable: %v", err)
	}
	li := get()
	if want := []string{"auto", "grok", "org-lane"}; !reflect.DeepEqual(li.Lanes, want) {
		t.Fatalf("org lanes = %v, want %v", li.Lanes, want)
	}
	if li.AutoDefaultLane != "org-lane" {
		t.Errorf("auto default = %q, want org-lane", li.AutoDefaultLane)
	}
	resp, err := http.Post(ts.URL+lanesPath, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", resp.StatusCode)
	}
}

// TestLanesEndpointLoopbackOnly: a proxy bound beyond loopback (a hosted
// gateway) never lists its lanes to a non-loopback peer.
func TestLanesEndpointLoopbackOnly(t *testing.T) {
	p, err := New(Options{Sink: &fakeSink{}, Upstreams: map[string]string{"x": "https://example.test"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, tc := range []struct {
		addr string
		want int
	}{
		{"10.0.0.5:4242", http.StatusForbidden},
		{"[2001:db8::1]:4242", http.StatusForbidden},
		{"127.0.0.1:4242", http.StatusOK},
		{"[::1]:4242", http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, lanesPath, nil)
		req.RemoteAddr = tc.addr
		rec := httptest.NewRecorder()
		p.serveLanes(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", tc.addr, rec.Code, tc.want)
		}
	}
}
