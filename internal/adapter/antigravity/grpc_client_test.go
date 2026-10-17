package antigravity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// grpcLikeHandler answers like the language_server: an HTTP/2-only gRPC
// reply whose status travels in a trailer. It records the method and protocol the
// request arrived on so the test can prove no HTTP/1 fallback happened (an
// HTTP/1-only server parses the h2 preface as a "PRI" request, never the POST).
func grpcLikeHandler(seen *atomic.Value) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.Method + " " + r.Proto)
		w.Header().Set("Trailer", "Grpc-Status")
		w.Header().Set("Content-Type", "application/grpc+proto")
		_, _ = w.Write(framedGRPC([]byte{0x0a, 0x02, 'o', 'k'}))
		w.Header().Set("Grpc-Status", "0")
	})
}

// TestBuildHTTP2ClientSpeaksOnlyHTTP2 pins the SA1019 move off x/net's
// deprecated http2.Transport: the net/http client built through
// Transport.Protocols still speaks HTTP/2 on both paths (TLS with a
// self-signed cert, and h2c with prior knowledge), still carries the gRPC
// trailer, and on the h2c path never falls back to HTTP/1.
func TestBuildHTTP2ClientSpeaksOnlyHTTP2(t *testing.T) {
	t.Run("h2c", func(t *testing.T) {
		var seen atomic.Value
		srv := httptest.NewUnstartedServer(grpcLikeHandler(&seen))
		srv.Config.Protocols = new(http.Protocols)
		srv.Config.Protocols.SetUnencryptedHTTP2(true)
		srv.Start()
		defer srv.Close()
		body, err := callLanguageServerMethod(context.Background(), srv.URL, "csrf", "Probe", 2, "conv-1", 5*time.Second)
		if err != nil {
			t.Fatalf("h2c call: %v", err)
		}
		if proto(&seen) != "POST HTTP/2.0" {
			t.Fatalf("request arrived as %q, want POST HTTP/2.0", proto(&seen))
		}
		if got := string(body[5:]); got != "\n\x02ok" {
			t.Fatalf("body = %q", got)
		}
	})
	t.Run("tls", func(t *testing.T) {
		var seen atomic.Value
		srv := httptest.NewUnstartedServer(grpcLikeHandler(&seen))
		srv.EnableHTTP2 = true
		srv.StartTLS()
		defer srv.Close()
		md, err := callConvertTrajectory(context.Background(), srv.URL, "csrf", "conv-1", 5*time.Second)
		if err != nil {
			t.Fatalf("tls call: %v", err)
		}
		if proto(&seen) != "POST HTTP/2.0" || md != "ok" {
			t.Fatalf("proto=%q markdown=%q", proto(&seen), md)
		}
	})
	t.Run("h2c refuses an HTTP/1-only server", func(t *testing.T) {
		var seen atomic.Value
		srv := httptest.NewServer(grpcLikeHandler(&seen))
		defer srv.Close()
		_, err := callLanguageServerMethod(context.Background(), srv.URL, "csrf", "Probe", 2, "conv-1", 5*time.Second)
		if err == nil || strings.HasPrefix(proto(&seen), "POST") {
			t.Fatalf("want an error and the POST never served, got err=%v proto=%q", err, proto(&seen))
		}
	})
	t.Run("grpc status trailer surfaces", func(t *testing.T) {
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Trailer", "Grpc-Status, Grpc-Message")
			_, _ = w.Write(framedGRPC(nil))
			w.Header().Set("Grpc-Status", "7")
			w.Header().Set("Grpc-Message", "denied")
		}))
		srv.Config.Protocols = new(http.Protocols)
		srv.Config.Protocols.SetUnencryptedHTTP2(true)
		srv.Start()
		defer srv.Close()
		_, err := callLanguageServerMethod(context.Background(), srv.URL, "csrf", "Probe", 2, "conv-1", 5*time.Second)
		if err == nil || !strings.Contains(err.Error(), "grpc-status=7") {
			t.Fatalf("want grpc-status=7, got %v", err)
		}
	})
}

// proto reads the protocol grpcLikeHandler recorded ("" when it never ran).
func proto(v *atomic.Value) string {
	s, _ := v.Load().(string)
	return s
}
