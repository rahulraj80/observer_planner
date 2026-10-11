package copilotcli

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestParseEventsJSONL_SessionShutdownOutputSpanRule pins T-353: the Tier 0
// shutdown row carries modelMetrics' outputTokens only when no Tier 3
// assistant.message in the same span (since the last resume / shutdown)
// reported output. The copilot-cli 1.0.91 "hi" session recorded input from
// this block but output 0 because its assistant.message rows carried none.
func TestParseEventsJSONL_SessionShutdownOutputSpanRule(t *testing.T) {
	uuid := "5c51d44b-5a9f-4b23-85ff-0fddaadf2212"
	shut := func(id string) string {
		return `{"type":"session.shutdown","data":{"modelMetrics":{"mai-code-1.1-flash":{"usage":{"inputTokens":16344,"outputTokens":42,"cacheReadTokens":0,"cacheWriteTokens":0,"reasoningTokens":0}}}},"id":"` + id + `","timestamp":"2026-10-04T09:30:00.000Z","parentId":"e1"}`
	}
	msg := func(id string, out int) string {
		return `{"type":"assistant.message","data":{"messageId":"m-` + id + `","content":"hello","requestId":"req-` + id + `","outputTokens":` + strconv.Itoa(out) + `},"id":"` + id + `","timestamp":"2026-10-04T09:29:00.000Z","parentId":"e1"}`
	}
	resume := `{"type":"session.resume","data":{},"id":"res-1","timestamp":"2026-10-04T09:40:00.000Z","parentId":"e1"}`
	cases := []struct {
		name  string
		lines []string
		want  map[string]int64 // shutdown event id -> OutputTokens on its Tier 0 row
	}{
		{"assistant rows carry no output: shutdown carries it", []string{msg("a1", 0), shut("s1")}, map[string]int64{"s1": 42}},
		{"assistant rows carry output: shutdown stays 0", []string{msg("a1", 40), shut("s1")}, map[string]int64{"s1": 0}},
		{"span resets at resume", []string{msg("a1", 40), shut("s1"), resume, msg("a2", 0), shut("s2")}, map[string]int64{"s1": 0, "s2": 42}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evt := writeMinimalEventsFile(t, uuid, tc.lines...)
			a := NewWithOptions(nil, filepath.Dir(filepath.Dir(evt)))
			res, err := a.ParseSessionFile(context.Background(), evt, 0)
			if err != nil {
				t.Fatalf("ParseSessionFile: %v", err)
			}
			got := map[string]int64{}
			for _, te := range res.TokenEvents {
				if te.Source == models.TokenSourceSessionSummary {
					got[strings.SplitN(te.SourceEventID, ":", 2)[0]] = te.OutputTokens
				}
			}
			for id, w := range tc.want {
				g, ok := got[id]
				if !ok {
					t.Fatalf("no session_summary row for %s (got %v)", id, got)
				}
				if g != w {
					t.Errorf("%s OutputTokens = %d, want %d", id, g, w)
				}
			}
		})
	}
}
