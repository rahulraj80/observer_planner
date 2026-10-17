//go:build !sbo_omit_ingest_otlp

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	otlpingest "github.com/marmutapp/superbased-observer/internal/ingest/otlp"
	"github.com/marmutapp/superbased-observer/internal/store"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// TestOTLPLogsHandler_TransientWriteFailureIsRetryable pins P3-10: an export
// whose turn write fails transiently (here a cancelled write) must NOT be
// acknowledged as stored; the handler returns ErrRetryLater so the receiver
// answers with the OTLP-retryable status and the exporter resends. The resend
// then lands the row exactly once (writes are idempotent).
func TestOTLPLogsHandler_TransientWriteFailureIsRetryable(t *testing.T) {
	ctx := context.Background()
	database, err := dbtemplate.Open(ctx, db.Options{Path: t.TempDir() + "/o.db"})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	st := store.New(database)
	h := otlpLogsHandler(st, slog.New(slog.NewTextHandler(io.Discard, nil)), true, config.DefaultIngestOTelContentMaxBytes)
	req := &collogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{ScopeLogs: []*logspb.ScopeLogs{{
		LogRecords: []*logspb.LogRecord{{
			EventName: "claude_code.api_request",
			Attributes: []*commonpb.KeyValue{
				{Key: "request_id", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "req_retry"}}},
				{Key: "model", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-opus-4-8"}}},
				{Key: "input_tokens", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 7}}},
			},
		}},
	}}}}}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := h(cancelled, req); !errors.Is(err, otlpingest.ErrRetryLater) {
		t.Fatalf("transient write failure returned %v, want ErrRetryLater", err)
	}
	// The exporter's resend succeeds, twice, and lands one row.
	for i := 0; i < 2; i++ {
		if err := h(ctx, req); err != nil {
			t.Fatalf("resend %d: %v", i, err)
		}
	}
	var n int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_turns WHERE request_id = 'req_retry'`).Scan(&n); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 1 {
		t.Fatalf("api_turns rows = %d, want 1", n)
	}
}
