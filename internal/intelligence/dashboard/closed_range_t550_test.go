package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// closedRangeQuery renders a closed [since, until) window the way the
// node dashboard's custom range sends it.
func closedRangeQuery(since, until time.Time) string {
	v := url.Values{}
	v.Set("since", since.UTC().Format(time.RFC3339))
	v.Set("until", until.UTC().Format(time.RFC3339))
	return v.Encode()
}

func getJSONT550(t *testing.T, server *Server, path string) map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s: %d body=%s", path, rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

// TestT550ClosedRangeHealthFailures pins T-550 for /api/health/failures:
// a closed custom range must exclude failures after `until`, not read
// `days` only and widen the window to "until now".
func TestT550ClosedRangeHealthFailures(t *testing.T) {
	server, _ := wizardTestServer(t)
	now := time.Now().UTC()
	seedFailure(t, server, "in", "inside", "s-a", now.Add(-8*24*time.Hour), 0, 0)
	seedFailure(t, server, "after", "after-until", "s-b", now.Add(-time.Hour), 0, 0)
	seedFailure(t, server, "before", "before-since", "s-c", now.Add(-20*24*time.Hour), 0, 0)

	q := closedRangeQuery(now.Add(-10*24*time.Hour), now.Add(-5*24*time.Hour))
	got := getJSONT550(t, server, "/api/health/failures?"+q)
	if got["total"].(float64) != 1 {
		t.Errorf("total: got %v want 1 (closed range)", got["total"])
	}
	fails := got["failures"].([]any)
	if len(fails) != 1 || fails[0].(map[string]any)["command"] != "inside" {
		t.Errorf("failures: got %v want only the in-range command", fails)
	}
}

// TestT550ClosedRangeMCPValue pins T-550 for /api/mcp/value: calls and the
// turn estimate both honour the closed range.
func TestT550ClosedRangeMCPValue(t *testing.T) {
	server, _ := mcpTestServer(t, "[observer]\nlog_level = \"info\"\n")
	now := time.Now().UTC()
	insert := func(at time.Time, tool string, turns int) {
		ts := at.Format(time.RFC3339Nano)
		if _, err := server.opts.DB.Exec(
			`INSERT INTO mcp_audit (ts, tool_name, request_hash, response_size_bytes, response_ok)
			 VALUES (?, ?, ?, 100, 1)`, ts, tool, fmt.Sprintf("h-%s", tool)); err != nil {
			t.Fatal(err)
		}
		for range turns {
			if _, err := server.opts.DB.Exec(
				`INSERT INTO api_turns (session_id, timestamp, provider, model, input_tokens, output_tokens)
				 VALUES ('s', ?, 'anthropic', 'm', 1, 1)`, ts); err != nil {
				t.Fatal(err)
			}
		}
	}
	insert(now.Add(-8*24*time.Hour), "inside", 10)
	insert(now.Add(-time.Hour), "after", 40)

	q := closedRangeQuery(now.Add(-10*24*time.Hour), now.Add(-5*24*time.Hour))
	got := getJSONT550(t, server, "/api/mcp/value?"+q)
	if got["calls"].(float64) != 1 {
		t.Errorf("calls: got %v want 1 (closed range)", got["calls"])
	}
	if got["turns_estimate"].(float64) != 10 {
		t.Errorf("turns_estimate: got %v want 10 (closed range)", got["turns_estimate"])
	}
	byTool := got["by_tool"].([]any)
	if len(byTool) != 1 || byTool[0].(map[string]any)["tool"] != "inside" {
		t.Errorf("by_tool: got %v want only the in-range tool", byTool)
	}
}

// TestT550ClosedRangeLOCSummary pins T-550 for /api/loc/summary: AI code
// lines and the per-day series honour the closed range.
func TestT550ClosedRangeLOCSummary(t *testing.T) {
	s, database := newLOCServer(t)
	now := time.Now().UTC()
	ingestLOCEdit(t, database, "s1", "/repo", "/repo/a.go",
		`{"file_path":"/repo/a.go","old_string":"a := 1","new_string":"a := 2"}`,
		"ev-in", now.Add(-8*24*time.Hour))
	ingestLOCEdit(t, database, "s2", "/repo", "/repo/b.go",
		`{"file_path":"/repo/b.go","old_string":"b := 1","new_string":"b := 2\nc := 3\nd := 4"}`,
		"ev-after", now.Add(-time.Hour))

	q := closedRangeQuery(now.Add(-10*24*time.Hour), now.Add(-5*24*time.Hour))
	got := getJSONT550(t, s, "/api/loc/summary?"+q)
	if got["ai_code_touched"].(float64) != 1 {
		t.Errorf("ai_code_touched: got %v want 1 (closed range)", got["ai_code_touched"])
	}
	for _, d := range got["by_day"].([]any) {
		day := d.(map[string]any)["day"].(string)
		if day >= now.Add(-5*24*time.Hour).Format("2006-01-02") {
			t.Errorf("by_day carries %s, after the closed range", day)
		}
	}
}
