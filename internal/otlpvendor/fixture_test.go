package otlpvendor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	otlpingest "github.com/marmutapp/superbased-observer/internal/ingest/otlp"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

// fixtureDir is the repo-root testdata/otlp tree (see its README for how each
// fixture was captured and anonymized).
func fixtureDir(t *testing.T, tool string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "otlp", tool)
}

// loadFixture decodes one fixture into msg. Fixtures captured from a JSON
// exporter are kept in the OTLP/HTTP JSON wire form (hex ids) and decoded the
// way the receiver decodes them; the Codex fixture was captured as protobuf
// and is kept as canonical protojson.
func loadFixture(t *testing.T, tool, file string, wire bool, msg proto.Message) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir(t, tool), file))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if wire {
		err = otlpingest.UnmarshalJSON(raw, msg)
	} else {
		err = protojson.Unmarshal(raw, msg)
	}
	if err != nil {
		t.Fatalf("decode fixture %s/%s: %v", tool, file, err)
	}
}

// wantTurn is the normalized shape a live fixture's turns must take. Every
// live capture ran against the same mock upstream, which reports 1234 input
// (1000 of it cached), 56 output and 16 reasoning tokens per call, so every
// vendor must normalize to NET input 234, cache read 1000, and a GROSS
// output of 56 (reasoning inside output) or 72 (reasoning reported beside
// output and added back, the transcript adapters' convention).
type wantTurn struct {
	n         int
	session   string
	model     string
	input     int64
	output    int64
	cacheRead int64
	reasoning int64
	requestID string // prefix; "otel:" means synthesized
}

func checkTurns(t *testing.T, res Result, tool string, w wantTurn) {
	t.Helper()
	var got []Turn
	for _, tr := range res.Turns {
		if tr.Tool == tool {
			got = append(got, tr)
		}
	}
	if len(got) != w.n {
		t.Fatalf("%s: %d turns, want %d: %+v", tool, len(got), w.n, res.Turns)
	}
	seen := map[string]bool{}
	for _, tr := range got {
		if tr.SessionID != w.session || tr.Model != w.model {
			t.Errorf("%s: identity session=%q model=%q, want %q %q", tool, tr.SessionID, tr.Model, w.session, w.model)
		}
		if tr.InputTokens != w.input || tr.OutputTokens != w.output || tr.CacheReadTokens != w.cacheRead || tr.ReasoningTokens != w.reasoning {
			t.Errorf("%s: tokens in=%d out=%d cr=%d reason=%d, want %d %d %d %d", tool,
				tr.InputTokens, tr.OutputTokens, tr.CacheReadTokens, tr.ReasoningTokens, w.input, w.output, w.cacheRead, w.reasoning)
		}
		if !strings.HasPrefix(tr.RequestID, w.requestID) {
			t.Errorf("%s: request id %q, want prefix %q", tool, tr.RequestID, w.requestID)
		}
		if tr.RequestSynthetic != (w.requestID == "otel:") {
			t.Errorf("%s: synthetic=%v for %q", tool, tr.RequestSynthetic, tr.RequestID)
		}
		if seen[tr.RequestID] {
			t.Errorf("%s: duplicate request id %q (two calls would merge into one row)", tool, tr.RequestID)
		}
		seen[tr.RequestID] = true
		if tr.Timestamp.IsZero() {
			t.Errorf("%s: zero timestamp", tool)
		}
		if tr.SourceTag != SourceNativeOTel {
			t.Errorf("%s: source %q", tool, tr.SourceTag)
		}
	}
}

// TestLiveFixtures maps every export captured live from an installed tool
// (testdata/otlp/README.md) and pins the normalized result. These are the
// rows whose attribute names were seen on the wire, not read from docs.
func TestLiveFixtures(t *testing.T) {
	t.Run("codex", func(t *testing.T) {
		var req collogspb.ExportLogsServiceRequest
		loadFixture(t, "codex", "logs.json", false, &req)
		res := ParseLogs(&req)
		checkTurns(t, res, "codex", wantTurn{
			n: 2, session: "01a10e5e-dbc2-7fe3-b64f-4d7d1918389a", model: "gpt-5.5",
			input: 234, output: 56, cacheRead: 1000, reasoning: 16, requestID: "otel:",
		})
		if len(res.Tools) != 1 || res.Tools[0].Name != "shell" || res.Tools[0].CallID != "call_mock1" ||
			!res.Tools[0].SuccessKnown || res.Tools[0].Success {
			t.Errorf("codex tool calls: %+v (want shell call_mock1 failed)", res.Tools)
		}
		kinds := contentKinds(res)
		if kinds[ContentPrompt] != 1 || kinds[ContentToolInput] != 1 || kinds[ContentToolOutput] != 1 {
			t.Errorf("codex content kinds = %v", kinds)
		}
	})
	t.Run("gemini-cli", func(t *testing.T) {
		var req collogspb.ExportLogsServiceRequest
		loadFixture(t, "gemini-cli", "logs.json", true, &req)
		res := ParseLogs(&req)
		// The export ALSO carries gen_ai.client.inference.operation.details
		// events with token counts; the resource is claimed by gemini-cli,
		// so they are never read as a second turn.
		checkTurns(t, res, "gemini-cli", wantTurn{
			n: 2, session: "655fdd60-68db-4734-b0fa-041f5c562a33", model: "gemini-3.5-flash",
			input: 234, output: 72, cacheRead: 1000, reasoning: 16, requestID: "otel:",
		})
		if len(res.Tools) != 1 || res.Tools[0].Name != "list_directory" || !res.Tools[0].Success || !strings.HasPrefix(res.Tools[0].CallID, "otel:gemini-cli:") {
			t.Errorf("gemini tool calls: %+v", res.Tools)
		}
		if contentKinds(res)[ContentPrompt] != 1 {
			t.Errorf("gemini prompt not captured: %+v", res.Contents)
		}
	})
	t.Run("qwen-code", func(t *testing.T) {
		var req collogspb.ExportLogsServiceRequest
		loadFixture(t, "qwen-code", "logs.json", true, &req)
		res := ParseLogs(&req)
		checkTurns(t, res, "qwen-code", wantTurn{
			n: 2, session: "00f3ac91-e678-4ff1-8dfe-b3b89499a366", model: "mock-model",
			// output_token_count 56 already holds the 16 thoughts (total
			// 1290 == 1234 + 56): gross output stays 56 (T-542).
			input: 234, output: 56, cacheRead: 1000, reasoning: 16, requestID: "chatcmpl-mock",
		})
		if len(res.Tools) != 1 || res.Tools[0].Name != "list_directory" || res.Tools[0].Success {
			t.Errorf("qwen tool calls: %+v", res.Tools)
		}
	})
	t.Run("copilot-cli", func(t *testing.T) {
		var req coltracepb.ExportTraceServiceRequest
		loadFixture(t, "copilot-cli", "traces.json", true, &req)
		res := ParseSpans(&req)
		// Two chat spans; the invoke_agent span carries their SUM and must
		// not be read as a third turn.
		checkTurns(t, res, "copilot-cli", wantTurn{
			n: 2, session: "7bb16a66-a714-4265-b119-e9208f02cbff", model: "gpt-5.5",
			input: 234, output: 56, cacheRead: 1000, reasoning: 16, requestID: "chatcmpl-mock",
		})
		if len(res.Tools) != 1 || res.Tools[0].Name != "list_directory" || res.Tools[0].CallID != "call_mock1" || res.Tools[0].Success {
			t.Errorf("copilot-cli tool calls: %+v", res.Tools)
		}
	})
}

func contentKinds(res Result) map[string]int {
	m := map[string]int{}
	for _, c := range res.Contents {
		m[c.Kind]++
	}
	return m
}
