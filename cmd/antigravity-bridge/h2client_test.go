package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// bridgeHandler answers like the language_server: a gRPC frame carrying
// ConvertTrajectoryToMarkdownResponse{markdown = "ok"} with the status in a
// trailer, recording the method and protocol the request arrived on (an
// HTTP/1-only server parses the h2 preface as a "PRI" request, never the POST).
func bridgeHandler(seen *atomic.Value) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.Method + " " + r.Proto)
		w.Header().Set("Trailer", "Grpc-Status")
		_, _ = w.Write(framedGRPC([]byte{0x0a, 0x02, 'o', 'k'}))
		w.Header().Set("Grpc-Status", "0")
	})
}

func seenProto(v *atomic.Value) string {
	s, _ := v.Load().(string)
	return s
}

// TestNewHTTP2ClientSpeaksOnlyHTTP2 pins the SA1019 move off x/net's
// deprecated http2.Transport for the bridge: TLS (self-signed) and h2c both
// still arrive as HTTP/2 with the gRPC trailer read, and the h2c path never
// falls back to HTTP/1.
func TestNewHTTP2ClientSpeaksOnlyHTTP2(t *testing.T) {
	t.Run("h2c convert", func(t *testing.T) {
		var seen atomic.Value
		srv := httptest.NewUnstartedServer(bridgeHandler(&seen))
		srv.Config.Protocols = new(http.Protocols)
		srv.Config.Protocols.SetUnencryptedHTTP2(true)
		srv.Start()
		defer srv.Close()
		md, err := callConvertTrajectory(context.Background(), srv.URL, "csrf", "conv-1", 5*time.Second)
		if err != nil || md != "ok" || seenProto(&seen) != "POST HTTP/2.0" {
			t.Fatalf("md=%q err=%v proto=%q", md, err, seenProto(&seen))
		}
	})
	t.Run("tls raw method", func(t *testing.T) {
		var seen atomic.Value
		srv := httptest.NewUnstartedServer(bridgeHandler(&seen))
		srv.EnableHTTP2 = true
		srv.StartTLS()
		defer srv.Close()
		body, err := callRawMethod(context.Background(), srv.URL, "csrf", "Probe", []byte("conv-1"), 2, 5*time.Second)
		if err != nil || seenProto(&seen) != "POST HTTP/2.0" || string(body) != "\n\x02ok" {
			t.Fatalf("body=%q err=%v proto=%q", body, err, seenProto(&seen))
		}
	})
	t.Run("h2c refuses an HTTP/1-only server", func(t *testing.T) {
		var seen atomic.Value
		srv := httptest.NewServer(bridgeHandler(&seen))
		defer srv.Close()
		if _, err := callConvertTrajectory(context.Background(), srv.URL, "csrf", "conv-1", 5*time.Second); err == nil || strings.HasPrefix(seenProto(&seen), "POST") {
			t.Fatalf("want an error and the POST never served, got err=%v proto=%q", err, seenProto(&seen))
		}
	})
}
