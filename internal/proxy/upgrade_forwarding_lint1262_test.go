package proxy

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// LINT-126-2: serveUpgradePassthrough moved from the deprecated
// httputil.ReverseProxy.Director to Rewrite. Rewrite strips the inbound
// Forwarded / X-Forwarded-* headers before it runs and never appends the
// client address to X-Forwarded-For itself, so a bare migration would change
// what the upstream sees on every WebSocket upgrade. These rows pin the
// headers the Director version sent (each expectation was first run green
// against it), so the Rewrite version is held to the same bytes.

type upgradeForwardingCase struct {
	name string
	// extra is the inbound header block (CRLF-terminated lines) beyond the
	// upgrade basics.
	extra string
	// connection is the inbound Connection header value.
	connection string
	// want maps a header to its exact upstream value; "" means absent.
	want map[string]string
}

var upgradeForwardingCases = []upgradeForwardingCase{
	{
		name: "inbound forwarding headers kept, client address appended",
		extra: "X-Forwarded-For: 203.0.113.7\r\nX-Forwarded-Host: app.example\r\nX-Forwarded-Proto: https\r\n" +
			"Forwarded: for=203.0.113.7;proto=https\r\nUser-Agent: ua-test\r\nX-Custom-Debug: keep-me\r\n",
		connection: "keep-alive, Upgrade",
		want: map[string]string{
			"X-Forwarded-For":   "203.0.113.7, 127.0.0.1",
			"X-Forwarded-Host":  "app.example",
			"X-Forwarded-Proto": "https",
			"Forwarded":         "for=203.0.113.7;proto=https",
			"User-Agent":        "ua-test",
			"X-Custom-Debug":    "keep-me",
			"X-Session-Id":      "",
			"Upgrade":           "websocket",
			"Connection":        "Upgrade",
		},
	},
	{
		name:       "no inbound forwarding headers: only X-Forwarded-For is added",
		extra:      "User-Agent: ua-test\r\n",
		connection: "Upgrade",
		want: map[string]string{
			"X-Forwarded-For":   "127.0.0.1",
			"X-Forwarded-Host":  "",
			"X-Forwarded-Proto": "",
			"Forwarded":         "",
		},
	},
	{
		name:       "a forwarding header named in Connection is hop-by-hop and dropped",
		extra:      "X-Forwarded-Host: app.example\r\nX-Forwarded-For: 203.0.113.7\r\n",
		connection: "keep-alive, Upgrade, X-Forwarded-Host, X-Forwarded-For",
		want: map[string]string{
			"X-Forwarded-Host": "",
			"X-Forwarded-For":  "127.0.0.1",
		},
	},
}

func TestLINT1262UpgradePassthroughForwardingHeaders(t *testing.T) {
	for _, tc := range upgradeForwardingCases {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan *http.Request, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				clone := r.Clone(r.Context())
				clone.Header = r.Header.Clone()
				seen <- clone
				hj, ok := w.(http.Hijacker)
				if !ok {
					t.Error("upstream response writer does not support hijacking")
					return
				}
				conn, rw, err := hj.Hijack()
				if err != nil {
					t.Errorf("hijack upstream: %v", err)
					return
				}
				defer conn.Close()
				_, _ = fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: test\r\n\r\n")
				_ = rw.Flush()
			}))
			defer upstream.Close()

			p, err := New(Options{
				AnthropicUpstream: "https://api.anthropic.com",
				OpenAIUpstream:    upstream.URL + "/root",
				Sink:              &fakeSink{},
			})
			if err != nil {
				t.Fatalf("proxy.New: %v", err)
			}
			ts := httptest.NewServer(p.Handler())
			defer ts.Close()
			proxyURL, err := url.Parse(ts.URL)
			if err != nil {
				t.Fatal(err)
			}
			upURL, err := url.Parse(upstream.URL)
			if err != nil {
				t.Fatal(err)
			}
			conn, err := net.Dial("tcp", proxyURL.Host)
			if err != nil {
				t.Fatalf("dial proxy: %v", err)
			}
			defer conn.Close()
			_, err = fmt.Fprintf(conn, "GET /v1/chat/completions?sb_session=s1&conversation=abc HTTP/1.1\r\n"+
				"Host: %s\r\nConnection: %s\r\nUpgrade: websocket\r\n"+
				"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n"+
				"X-Session-Id: local-session\r\n%s\r\n", proxyURL.Host, tc.connection, tc.extra)
			if err != nil {
				t.Fatalf("write upgrade request: %v", err)
			}
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatalf("read upgrade response: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusSwitchingProtocols {
				t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusSwitchingProtocols)
			}
			select {
			case req := <-seen:
				if req.URL.Path != "/root/v1/chat/completions" {
					t.Errorf("upstream path = %q", req.URL.Path)
				}
				if req.URL.RawQuery != "conversation=abc" {
					t.Errorf("upstream query = %q, want conversation=abc", req.URL.RawQuery)
				}
				if req.Host != upURL.Host {
					t.Errorf("upstream Host = %q, want %q", req.Host, upURL.Host)
				}
				for name, want := range tc.want {
					got := req.Header.Values(name)
					switch {
					case want == "" && len(got) != 0:
						t.Errorf("%s = %q, want absent", name, got)
					case want != "" && (len(got) != 1 || got[0] != want):
						t.Errorf("%s = %q, want %q", name, got, want)
					}
				}
			case <-time.After(2 * time.Second):
				t.Fatal("upstream did not receive the upgrade request")
			}
		})
	}
}
