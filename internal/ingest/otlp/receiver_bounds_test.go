package otlp

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestShutdown_BoundedByContextWithBlockedGRPCExport pins P3-28: a gRPC
// Export blocked in its handler (a store write that never returns) cannot
// hold Shutdown past its context; the server is stopped at the deadline.
func TestShutdown_BoundedByContextWithBlockedGRPCExport(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	r, err := New(Options{GRPCAddr: "127.0.0.1:0", Handler: func(context.Context, *collogspb.ExportLogsServiceRequest) error {
		close(entered)
		<-release // ignores its context, like a write stuck in SQLite
		return nil
	}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Start()
	conn, err := grpc.NewClient(r.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	go func() { _, _ = collogspb.NewLogsServiceClient(conn).Export(context.Background(), sample("x")) }()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- r.Shutdown(ctx) }()
	select {
	case err := <-stopped:
		if err == nil {
			t.Fatal("Shutdown reported a clean stop although the export was cut off")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return within its context bound while an Export was blocked")
	}
}

// TestReceiver_GzippedJSONHeldToBodyCap pins P3-39: a gzipped OTLP JSON body
// that inflates past the plain body cap is refused (413) before the
// multi-copy JSON decode, while the same payload under the cap is accepted.
func TestReceiver_GzippedJSONHeldToBodyCap(t *testing.T) {
	big := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"` + strings.Repeat("a", 8192) + `"}}]}]}]}`
	for _, tc := range []struct {
		name    string
		maxBody int
		want    int
	}{
		{"inflates past the cap", 2048, http.StatusRequestEntityTooLarge},
		{"within the cap", 64 << 10, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var called atomic.Bool
			r, err := New(Options{HTTPAddr: "127.0.0.1:0", maxBody: tc.maxBody, Handler: func(context.Context, *collogspb.ExportLogsServiceRequest) error {
				called.Store(true)
				return nil
			}})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			r.Start()
			defer func() { _ = r.Shutdown(context.Background()) }()
			var gz bytes.Buffer
			zw := gzip.NewWriter(&gz)
			_, _ = zw.Write([]byte(big))
			_ = zw.Close()
			req, _ := http.NewRequest(http.MethodPost, "http://"+r.HTTPAddr()+"/v1/logs", bytes.NewReader(gz.Bytes()))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", "gzip")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != tc.want || called.Load() != (tc.want == http.StatusOK) {
				t.Fatalf("status %d (handler called %v), want %d", resp.StatusCode, called.Load(), tc.want)
			}
		})
	}
}
