//go:build !sbo_omit_ingest_otlp

package main

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/codex"
	"github.com/marmutapp/superbased-observer/internal/adapter/gemini"
	"github.com/marmutapp/superbased-observer/internal/adapter/qwencode"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	otlpingest "github.com/marmutapp/superbased-observer/internal/ingest/otlp"
	"github.com/marmutapp/superbased-observer/internal/otlpvendor"
	"github.com/marmutapp/superbased-observer/internal/sessionmsg"
	"github.com/marmutapp/superbased-observer/internal/store"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// otlpFixture is one live-captured export under testdata/otlp.
type otlpFixture struct {
	tool    string
	file    string
	path    string // receiver path
	json    bool   // wire form is OTLP/HTTP JSON (else protobuf, kept as protojson)
	session string
	turns   int
	tools   int
}

var liveOTLPFixtures = []otlpFixture{
	{tool: "codex", file: "logs.json", path: "/v1/logs", session: "01a10e5e-dbc2-7fe3-b64f-4d7d1918389a", turns: 2, tools: 1},
	{tool: "gemini-cli", file: "logs.json", path: "/v1/logs", json: true, session: "655fdd60-68db-4734-b0fa-041f5c562a33", turns: 2, tools: 1},
	{tool: "qwen-code", file: "logs.json", path: "/v1/logs", json: true, session: "00f3ac91-e678-4ff1-8dfe-b3b89499a366", turns: 2, tools: 1},
	{tool: "copilot-cli", file: "traces.json", path: "/v1/traces", json: true, session: "7bb16a66-a714-4265-b119-e9208f02cbff", turns: 2, tools: 1},
}

func fixturePath(tool, file string) string {
	return filepath.Join("..", "..", "testdata", "otlp", tool, file)
}

// wireBody returns the fixture in the exact encoding its tool sent.
func wireBody(t *testing.T, f otlpFixture) (body []byte, contentType string) {
	t.Helper()
	raw, err := os.ReadFile(fixturePath(f.tool, f.file))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if f.json {
		return raw, "application/json"
	}
	var msg proto.Message = &collogspb.ExportLogsServiceRequest{}
	if f.path == "/v1/traces" {
		msg = &coltracepb.ExportTraceServiceRequest{}
	}
	if err := protojson.Unmarshal(raw, msg); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	out, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out, "application/x-protobuf"
}

// startOTLPNode opens a fresh node DB and a real loopback receiver wired the
// way `observer start` wires it, returning the DB and the receiver's base URL.
func startOTLPNode(t *testing.T, transcriptCapture func(string) bool, planeA otlpingest.TraceHandler) (*sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "o.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ing := newOTLPIngest(store.New(database), slog.New(slog.NewTextHandler(io.Discard, nil)), true,
		config.DefaultIngestOTelContentMaxBytes, transcriptCapture)
	recv, err := otlpingest.New(otlpingest.Options{HTTPAddr: "127.0.0.1:0", Handler: ing.logs, TraceHandler: ing.traces(planeA)})
	if err != nil {
		t.Fatalf("receiver: %v", err)
	}
	recv.Start()
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = recv.Shutdown(sctx)
	})
	return database, "http://" + recv.HTTPAddr()
}

func postOTLP(t *testing.T, url string, body []byte, contentType string) {
	t.Helper()
	resp, err := http.Post(url, contentType, bytes.NewReader(body)) //nolint:gosec,noctx // loopback test receiver
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: %d %s", url, resp.StatusCode, got)
	}
	if resp.Header.Get("Content-Type") != contentType {
		t.Errorf("response content type %q, want %q", resp.Header.Get("Content-Type"), contentType)
	}
}

func countRows(t *testing.T, database *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := database.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// TestOTLPVendorExportsLandOnNode is the T-475 acceptance: an export from
// every live-captured vendor, sent over HTTP in the exact wire encoding the
// tool used, lands as a Plane B session (with its tool), per-call token
// turns and tool-call actions on a node. With the tool's transcript capture
// ON the same export lands only its dedup-safe kinds.
func TestOTLPVendorExportsLandOnNode(t *testing.T) {
	for _, f := range liveOTLPFixtures {
		t.Run(f.tool+"/export-owns", func(t *testing.T) {
			database, base := startOTLPNode(t, func(string) bool { return false }, nil)
			body, ctype := wireBody(t, f)
			postOTLP(t, base+f.path, body, ctype)
			postOTLP(t, base+f.path, body, ctype) // at-least-once redelivery: no duplicates

			if n := countRows(t, database, `SELECT COUNT(*) FROM sessions s JOIN projects p ON p.id = s.project_id
			      WHERE s.id = ? AND s.tool = ? AND p.root_path = ?`, f.session, f.tool, otlpvendor.PlaceholderProjectRoot(f.tool)); n != 1 {
				t.Errorf("session row for %s: %d", f.session, n)
			}
			if n := countRows(t, database, `SELECT COUNT(*) FROM api_turns WHERE session_id = ? AND source = ?`, f.session, store.SourceNativeOTel); n != f.turns {
				t.Errorf("api_turns = %d, want %d", n, f.turns)
			}
			if n := countRows(t, database, `SELECT COUNT(*) FROM api_turns WHERE session_id = ? AND input_tokens = 234 AND cache_read_tokens = 1000`, f.session); n != f.turns {
				t.Errorf("normalized api_turns = %d, want %d", n, f.turns)
			}
			if n := countRows(t, database, `SELECT COUNT(*) FROM actions WHERE session_id = ? AND tool = ?`, f.session, f.tool); n != f.tools {
				t.Errorf("actions = %d, want %d", n, f.tools)
			}
		})
		t.Run(f.tool+"/transcript-on", func(t *testing.T) {
			database, base := startOTLPNode(t, func(string) bool { return true }, nil)
			body, ctype := wireBody(t, f)
			postOTLP(t, base+f.path, body, ctype)
			v, _ := otlpvendor.ForTool(f.tool)
			wantTurns := 0
			if v.Owner.TokensWithTranscript {
				wantTurns = f.turns
			}
			if n := countRows(t, database, `SELECT COUNT(*) FROM api_turns WHERE session_id = ?`, f.session); n != wantTurns {
				t.Errorf("api_turns = %d, want %d (TokensWithTranscript=%v)", n, wantTurns, v.Owner.TokensWithTranscript)
			}
			if n := countRows(t, database, `SELECT COUNT(*) FROM actions WHERE session_id = ?`, f.session); n != 0 {
				t.Errorf("actions = %d with transcript capture on, want 0 (no cross-path action dedup)", n)
			}
			if n := countRows(t, database, `SELECT COUNT(*) FROM otel_content WHERE session_id = ?`, f.session); n != 0 {
				t.Errorf("otel_content = %d with transcript capture on, want 0", n)
			}
		})
	}
}

// TestOTLPTeamGrainExportRefusedOnNode: a Cursor Enterprise export (sent
// from Cursor's cloud, one per team) never lands on a developer's node.
func TestOTLPTeamGrainExportRefusedOnNode(t *testing.T) {
	database, base := startOTLPNode(t, func(string) bool { return false }, nil)
	req := &collogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "service.name", Value: strVal("cursor")}}},
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{
			TimeUnixNano: uint64(time.Now().UnixNano()),
			Attributes: []*commonpb.KeyValue{
				{Key: "event.name", Value: strVal("cursor.api.request")},
				{Key: "cursor.conversation.id", Value: strVal("bc-team-1")},
				{Key: "cursor.api.request.input_tokens", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 10}}},
			},
		}}}},
	}}}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	postOTLP(t, base+"/v1/logs", body, "application/x-protobuf")
	if n := countRows(t, database, `SELECT COUNT(*) FROM api_turns`); n != 0 {
		t.Errorf("team-grain export landed %d api_turns on a node", n)
	}
	if n := countRows(t, database, `SELECT COUNT(*) FROM sessions WHERE id = 'bc-team-1'`); n != 0 {
		t.Errorf("team-grain export created a session on a node")
	}
}

func strVal(s string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
}

// TestOTLPTracesSplitByPlane: a trace export mixing a mapped coding tool's
// resource and an application's own resource sends the tool's spans to
// Plane B only and the application's to the Plane A handler only.
func TestOTLPTracesSplitByPlane(t *testing.T) {
	var planeA []*coltracepb.ExportTraceServiceRequest
	database, base := startOTLPNode(t, func(string) bool { return false }, func(_ context.Context, req *coltracepb.ExportTraceServiceRequest) error {
		planeA = append(planeA, req)
		return nil
	})
	f := liveOTLPFixtures[3] // copilot-cli
	raw, err := os.ReadFile(fixturePath(f.tool, f.file))
	if err != nil {
		t.Fatal(err)
	}
	var req coltracepb.ExportTraceServiceRequest
	if err := otlpingest.UnmarshalJSON(raw, &req); err != nil {
		t.Fatal(err)
	}
	app := proto.Clone(req.ResourceSpans[0]).(*tracepb.ResourceSpans)
	app.Resource.Attributes = []*commonpb.KeyValue{{Key: "service.name", Value: strVal("my-llm-app")}}
	req.ResourceSpans = append(req.ResourceSpans, app)
	body, err := proto.Marshal(&req)
	if err != nil {
		t.Fatal(err)
	}
	postOTLP(t, base+"/v1/traces", body, "application/x-protobuf")
	if n := countRows(t, database, `SELECT COUNT(*) FROM api_turns WHERE session_id = ?`, f.session); n != 2 {
		t.Errorf("plane B turns = %d, want 2", n)
	}
	if len(planeA) != 1 || len(planeA[0].ResourceSpans) != 1 {
		t.Fatalf("plane A got %d exports", len(planeA))
	}
	if svc := planeA[0].ResourceSpans[0].Resource.Attributes[0].GetValue().GetStringValue(); svc != "my-llm-app" {
		t.Errorf("plane A got resource %q, want only my-llm-app", svc)
	}
}

// TestOTLPTurnsFoldAgainstTranscript proves the dedup contract for every
// live vendor whose row lands tokens alongside the transcript
// (TokensWithTranscript): the export's turns and the SAME session's
// transcript token rows, parsed by the real adapter from the same capture,
// fold one-to-one in internal/sessionmsg (the rule every spend surface
// reads), so the session's tokens are counted once, not twice.
func TestOTLPTurnsFoldAgainstTranscript(t *testing.T) {
	for _, tc := range []struct {
		f          otlpFixture
		adapter    adapter.Adapter
		transcript string
		// layout, when set, is where the adapter expects the transcript to
		// live (relative to a temp HOME); the fixture is copied there.
		layout string
	}{
		{liveOTLPFixtures[0], codex.New(), "rollout-2026-10-06T04-51-00-01a10e5e-dbc2-7fe3-b64f-4d7d1918389a.jsonl", ""},
		{liveOTLPFixtures[1], gemini.New(), "session-2026-10-05T23-24-655fdd60.jsonl", ".gemini/tmp/work-g/chats"},
		{liveOTLPFixtures[2], qwencode.New(), "00f3ac91-e678-4ff1-8dfe-b3b89499a366.jsonl", ""},
	} {
		t.Run(tc.f.tool, func(t *testing.T) {
			v, _ := otlpvendor.ForTool(tc.f.tool)
			path := fixturePath(tc.f.tool, tc.transcript)
			if tc.layout != "" {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				dir := filepath.Join(t.TempDir(), filepath.FromSlash(tc.layout))
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(dir, tc.transcript)
				if err := os.WriteFile(path, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			pr, err := tc.adapter.ParseSessionFile(context.Background(), path, 0)
			if err != nil {
				t.Fatalf("adapter parse: %v", err)
			}
			var tokens []sessionmsg.TokenRow
			var transcriptIn, transcriptOut int64
			for _, e := range pr.TokenEvents {
				if e.SessionID != tc.f.session {
					continue
				}
				tokens = append(tokens, sessionmsg.TokenRow{
					SourceEventID: e.SourceEventID, MessageID: e.MessageID, TurnID: e.TurnID,
					Timestamp: e.Timestamp.UTC().Format(time.RFC3339Nano), Model: e.Model, Tool: e.Tool,
					Input: e.InputTokens, Output: e.OutputTokens, CacheRead: e.CacheReadTokens,
					CacheCreation: e.CacheCreationTokens, Reasoning: e.ReasoningTokens,
				})
				transcriptIn += e.InputTokens
				transcriptOut += e.OutputTokens + e.ReasoningTokens
			}
			if !v.Owner.TokensWithTranscript {
				// The row must say why it cannot fold: the transcript files
				// the same calls under a different session id, so it owns
				// the tokens while its adapter is enabled.
				if len(tokens) != 0 {
					t.Fatalf("%s: transcript shares the export's session id; TokensWithTranscript could be enabled", tc.f.tool)
				}
				if len(pr.TokenEvents) == 0 {
					t.Fatalf("%s: adapter produced no token rows at all", tc.f.tool)
				}
				return
			}
			if len(tokens) == 0 {
				t.Fatalf("adapter produced no token rows for session %s (got %d events)", tc.f.session, len(pr.TokenEvents))
			}

			body, _ := wireBody(t, tc.f)
			var res otlpvendor.Result
			if tc.f.json {
				var req collogspb.ExportLogsServiceRequest
				if err := otlpingest.UnmarshalJSON(body, &req); err != nil {
					t.Fatal(err)
				}
				res = otlpvendor.ParseLogs(&req)
			} else {
				var req collogspb.ExportLogsServiceRequest
				if err := proto.Unmarshal(body, &req); err != nil {
					t.Fatal(err)
				}
				res = otlpvendor.ParseLogs(&req)
			}
			var proxies []sessionmsg.ProxyRow
			for _, tr := range res.Turns {
				at := apiTurnFromOTLP(tr)
				proxies = append(proxies, sessionmsg.ProxyRow{
					RequestID: at.RequestID, Timestamp: at.Timestamp.UTC().Format(time.RFC3339Nano), Model: at.Model,
					Input: at.InputTokens, Output: at.OutputTokens, CacheRead: at.CacheReadTokens, CacheCreation: at.CacheCreationTokens,
				})
			}
			rows := sessionmsg.Derive(sessionmsg.DeriveInput{ProxyRows: proxies, TokenRows: tokens})
			var gotIn, gotOut int64
			for _, r := range rows {
				gotIn += r.Bundle.Input
				gotOut += r.Bundle.Output + r.Bundle.Reasoning
			}
			if gotIn != transcriptIn || gotOut != transcriptOut {
				t.Errorf("derived in=%d out=%d, want the single-capture in=%d out=%d (export turns=%d, transcript rows=%d)",
					gotIn, gotOut, transcriptIn, transcriptOut, len(proxies), len(tokens))
			}
		})
	}
}
