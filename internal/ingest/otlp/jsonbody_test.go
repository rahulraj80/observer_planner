package otlp

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

// TestHTTPAcceptsOTLPJSON pins the OTLP/HTTP JSON encoding (T-475): Gemini
// CLI and Qwen Code "http", GitHub Copilot CLI and OpenCode all send JSON,
// which the receiver used to reject as "bad protobuf". JSON ids are HEX on
// the wire and must decode to the same bytes; the response is JSON too.
func TestHTTPAcceptsOTLPJSON(t *testing.T) {
	var gotLogs *collogspb.ExportLogsServiceRequest
	var gotTraces *coltracepb.ExportTraceServiceRequest
	r, err := New(Options{
		HTTPAddr: "127.0.0.1:0",
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler: func(_ context.Context, req *collogspb.ExportLogsServiceRequest) error {
			gotLogs = req
			return nil
		},
		TraceHandler: func(_ context.Context, req *coltracepb.ExportTraceServiceRequest) error {
			gotTraces = req
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = r.Shutdown(ctx)
	}()
	base := "http://" + r.HTTPAddr()

	post := func(path, ctype, body string) (int, string, string) {
		resp, err := http.Post(base+path, ctype, strings.NewReader(body)) //nolint:gosec,noctx // loopback test
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
	}

	const traceHex, spanHex = "ac646a8262095ad4a37e418def2a4ef4", "d72b54fd39c0ac9c"
	logs := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"qwen-code"}}]},
	  "scopeLogs":[{"logRecords":[{"timeUnixNano":"1791242591947000000","traceId":"` + traceHex + `","spanId":"` + spanHex + `",
	  "attributes":[{"key":"event.name","value":{"stringValue":"qwen-code.api_response"}},{"key":"input_token_count","value":{"intValue":"1234"}}],
	  "futureField":true}]}]}]}`
	code, ctype, body := post("/v1/logs", "application/json", logs)
	if code != http.StatusOK || ctype != "application/json" {
		t.Fatalf("json logs: %d %q %s", code, ctype, body)
	}
	rec := gotLogs.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords()[0]
	if hex.EncodeToString(rec.GetTraceId()) != traceHex || hex.EncodeToString(rec.GetSpanId()) != spanHex {
		t.Errorf("hex ids decoded wrong: trace=%x span=%x", rec.GetTraceId(), rec.GetSpanId())
	}
	if rec.GetAttributes()[1].GetValue().GetIntValue() != 1234 {
		t.Errorf("int64-as-string attribute decoded wrong: %v", rec.GetAttributes()[1])
	}

	traces := `{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"` + traceHex + `","spanId":"` + spanHex + `","parentSpanId":"` + spanHex + `","name":"chat gpt-5.5","kind":3}]}]}]}`
	if code, ctype, body := post("/v1/traces", "application/json; charset=utf-8", traces); code != http.StatusOK || ctype != "application/json" {
		t.Fatalf("json traces: %d %q %s", code, ctype, body)
	}
	sp := gotTraces.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans()[0]
	if !bytes.Equal(sp.GetParentSpanId(), mustHex(t, spanHex)) || sp.GetName() != "chat gpt-5.5" {
		t.Errorf("span decoded wrong: %+v", sp)
	}

	if code, _, body := post("/v1/logs", "application/json", `{"resourceLogs":[`); code != http.StatusBadRequest || !strings.Contains(body, "bad json") {
		t.Errorf("malformed json: %d %q", code, body)
	}
	if code, _, body := post("/v1/logs", "application/x-protobuf", "\xff\xff\xff"); code != http.StatusBadRequest || !strings.Contains(body, "bad protobuf") {
		t.Errorf("malformed protobuf: %d %q", code, body)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
