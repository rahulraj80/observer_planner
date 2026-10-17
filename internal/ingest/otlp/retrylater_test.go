package otlp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// TestReceiver_RetryLaterIsOTLPRetryable pins P3-10: a handler error wrapping
// ErrRetryLater is answered with the OTLP-retryable status on both transports
// (HTTP 503 with Retry-After, gRPC Unavailable), so the exporter resends the
// batch instead of dropping it.
func TestReceiver_RetryLaterIsOTLPRetryable(t *testing.T) {
	failing := func(context.Context, *collogspb.ExportLogsServiceRequest) error {
		return fmt.Errorf("store busy: %w", ErrRetryLater)
	}
	r, err := New(Options{HTTPAddr: "127.0.0.1:0", GRPCAddr: "127.0.0.1:0", Handler: failing})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Start()
	defer func() { _ = r.Shutdown(context.Background()) }()

	resp, err := http.Post("http://"+r.HTTPAddr()+"/v1/logs", "application/x-protobuf", bytes.NewReader(mustMarshal(t, sample("x"))))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("HTTP retry-later = %d (Retry-After %q), want 503 with Retry-After", resp.StatusCode, resp.Header.Get("Retry-After"))
	}

	conn, err := grpc.NewClient(r.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_, rpcErr := collogspb.NewLogsServiceClient(conn).Export(context.Background(), sample("x"))
	if status.Code(rpcErr) != codes.Unavailable {
		t.Fatalf("gRPC retry-later code = %v, want Unavailable", status.Code(rpcErr))
	}
}
