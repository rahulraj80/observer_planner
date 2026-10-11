package modelfilter_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/modelfilter"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"all", ""},
		{"All", ""},
		{"claude-opus-4-7", "claude-opus-4-7"},
		{" gpt-5.1\t", "gpt-5.1"},
		{strings.Repeat("x", modelfilter.MaxLen), strings.Repeat("x", modelfilter.MaxLen)},
		// Over-long is KEPT (Sol F5): dropping it made a filter the viewer
		// set silently become no filter, widening the answer. Kept, it is an
		// exact match that narrows to nothing; the org API refuses it.
		{strings.Repeat("x", modelfilter.MaxLen+1), strings.Repeat("x", modelfilter.MaxLen+1)},
	}
	for _, tc := range cases {
		if got := modelfilter.Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTooLong(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"", false},
		{"claude-opus-4-7", false},
		{strings.Repeat("x", modelfilter.MaxLen), false},
		{strings.Repeat("x", modelfilter.MaxLen+1), true},
	} {
		if got := modelfilter.TooLong(tc.in); got != tc.want {
			t.Errorf("TooLong(%d chars) = %v, want %v", len(tc.in), got, tc.want)
		}
	}
}

// TestMatches: the in-memory predicate, one row per case.
func TestMatches(t *testing.T) {
	for _, tc := range []struct {
		name   string
		filter string
		models []string
		want   bool
	}{
		{"no filter matches a row with models", "", []string{"a"}, true},
		{"no filter matches a row with none", "", nil, true},
		{"first column", "gpt-5.1", []string{"gpt-5.1", "claude-opus-4-7"}, true},
		{"second column", "claude-opus-4-7", []string{"gpt-5.1", "claude-opus-4-7"}, true},
		{"neither column", "claude-haiku-4-5", []string{"gpt-5.1", "claude-opus-4-7"}, false},
		{"exact only, never a prefix", "gpt-5", []string{"gpt-5.1"}, false},
		{"a filter never matches a row with no models", "gpt-5.1", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelfilter.Matches(tc.filter, tc.models...); got != tc.want {
				t.Fatalf("Matches(%q, %v) = %v, want %v", tc.filter, tc.models, got, tc.want)
			}
		})
	}
}

// TestEmptyModelIsNoFilter: every builder returns no fragment and no args for
// "" so a caller can append unconditionally.
func TestEmptyModelIsNoFilter(t *testing.T) {
	for name, f := range map[string]func() (string, []any){
		"SessionsUsing":    func() (string, []any) { return modelfilter.SessionsUsing("s.id", "") },
		"ProjectsUsing":    func() (string, []any) { return modelfilter.ProjectsUsing("p.id", "") },
		"RowIs":            func() (string, []any) { return modelfilter.RowIs("t.model", "") },
		"AndSessionsUsing": func() (string, []any) { return modelfilter.AndSessionsUsing("s.id", "") },
		"AndRowIs":         func() (string, []any) { return modelfilter.AndRowIs("t.model", "") },
	} {
		if frag, args := f(); frag != "" || args != nil {
			t.Errorf("%s(\"\") = %q, %v; want no filter", name, frag, args)
		}
	}
}

// openSeeded builds a migrated store with:
//
//	s1 claude-code  proxy turn claude-opus-4-7, transcript row claude-haiku-4-5   project /p1
//	s2 codex        transcript row gpt-5.1                                         project /p2
//	s3 claude-code  no turns, sessions.model = claude-opus-4-7 (a label only)      project /p2
//	s4 codex        proxy turn gpt-5.1                                             project /p3
func openSeeded(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "m.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	st := store.New(database)
	now := time.Now().UTC()
	for _, ev := range []models.ToolEvent{
		{SourceFile: "f", SourceEventID: "1", SessionID: "s1", ProjectRoot: "/p1", Timestamp: now, Tool: models.ToolClaudeCode, ActionType: models.ActionReadFile, Target: "a", Success: true},
		{SourceFile: "f", SourceEventID: "2", SessionID: "s2", ProjectRoot: "/p2", Timestamp: now, Tool: models.ToolCodex, ActionType: models.ActionReadFile, Target: "b", Success: true},
		{SourceFile: "f", SourceEventID: "3", SessionID: "s3", ProjectRoot: "/p2", Timestamp: now, Tool: models.ToolClaudeCode, ActionType: models.ActionReadFile, Target: "c", Success: true},
		{SourceFile: "f", SourceEventID: "4", SessionID: "s4", ProjectRoot: "/p3", Timestamp: now, Tool: models.ToolCodex, ActionType: models.ActionReadFile, Target: "d", Success: true},
	} {
		if _, err := st.Ingest(ctx, []models.ToolEvent{ev}, nil, store.IngestOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, turn := range []models.APITurn{
		{SessionID: "s1", Provider: models.ProviderAnthropic, Model: "claude-opus-4-7", InputTokens: 10, OutputTokens: 1, Timestamp: now},
		{SessionID: "s4", Provider: models.ProviderOpenAI, Model: "gpt-5.1", InputTokens: 10, OutputTokens: 1, Timestamp: now},
	} {
		if _, err := st.InsertAPITurn(ctx, turn); err != nil {
			t.Fatal(err)
		}
	}
	ts := now.Format(time.RFC3339Nano)
	for _, row := range [][]any{
		{"s1", ts, "claude-code", "claude-haiku-4-5", "jsonl", "f", "t1"},
		{"s2", ts, "codex", "gpt-5.1", "jsonl", "f", "t2"},
	} {
		if _, err := database.ExecContext(ctx,
			`INSERT INTO token_usage (session_id, timestamp, tool, model, input_tokens, output_tokens, source, source_file, source_event_id)
			 VALUES (?, ?, ?, ?, 10, 1, ?, ?, ?)`, row...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `UPDATE sessions SET model = 'claude-opus-4-7' WHERE id = 's3'`); err != nil {
		t.Fatal(err)
	}
	return database
}

func ids(t *testing.T, database *sql.DB, q string, args []any) []string {
	t.Helper()
	rows, err := database.QueryContext(context.Background(), q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// TestPredicatesAgainstStore runs every builder as a real WHERE clause over a
// migrated store, one row per (grain, model) case.
func TestPredicatesAgainstStore(t *testing.T) {
	database := openSeeded(t)
	cases := []struct {
		name  string
		query string // %s is the WHERE fragment
		build func(string) (string, []any)
		model string
		want  []string
	}{
		// Session grain: a proxy turn or a transcript row with the model.
		// sessions.model alone is NOT evidence the session used it (D-154,
		// Sol A3-04): s3 carries only the label and must not match.
		{
			"sessions opus (proxy turn; s3's sessions.model label ignored)", "SELECT id FROM sessions WHERE %s",
			func(m string) (string, []any) { return modelfilter.SessionsUsing("id", m) }, "claude-opus-4-7",
			[]string{"s1"},
		},
		{
			"sessions haiku (transcript only)", "SELECT id FROM sessions WHERE %s",
			func(m string) (string, []any) { return modelfilter.SessionsUsing("id", m) }, "claude-haiku-4-5",
			[]string{"s1"},
		},
		{
			"sessions gpt (transcript + proxy)", "SELECT id FROM sessions WHERE %s",
			func(m string) (string, []any) { return modelfilter.SessionsUsing("id", m) }, "gpt-5.1",
			[]string{"s2", "s4"},
		},
		{
			"sessions unknown", "SELECT id FROM sessions WHERE %s",
			func(m string) (string, []any) { return modelfilter.SessionsUsing("id", m) }, "nope", nil,
		},
		{
			"actions via session_id", "SELECT session_id FROM actions WHERE %s",
			func(m string) (string, []any) { return modelfilter.SessionsUsing("session_id", m) }, "gpt-5.1",
			[]string{"s2", "s4"},
		},
		// Project grain: projects with a session that used the model.
		{
			"projects opus (not /p2: only s3's label)", "SELECT root_path FROM projects WHERE %s",
			func(m string) (string, []any) { return modelfilter.ProjectsUsing("id", m) }, "claude-opus-4-7",
			[]string{"/p1"},
		},
		{
			"projects gpt", "SELECT root_path FROM projects WHERE %s",
			func(m string) (string, []any) { return modelfilter.ProjectsUsing("id", m) }, "gpt-5.1",
			[]string{"/p2", "/p3"},
		},
		// Turn grain: the row's own model only.
		{
			"api_turns opus", "SELECT session_id FROM api_turns WHERE %s",
			func(m string) (string, []any) { return modelfilter.RowIs("model", m) }, "claude-opus-4-7",
			[]string{"s1"},
		},
		{
			"token_usage opus (none: opus was proxy-only)", "SELECT session_id FROM token_usage WHERE %s",
			func(m string) (string, []any) { return modelfilter.RowIs("model", m) }, "claude-opus-4-7", nil,
		},
		{
			"token_usage gpt", "SELECT session_id FROM token_usage WHERE %s",
			func(m string) (string, []any) { return modelfilter.RowIs("model", m) }, "gpt-5.1",
			[]string{"s2"},
		},
		// And-suffix forms compose after an existing predicate.
		{
			"and-suffix session grain", "SELECT id FROM sessions WHERE tool = 'codex'%s",
			func(m string) (string, []any) { return modelfilter.AndSessionsUsing("id", m) }, "gpt-5.1",
			[]string{"s2", "s4"},
		},
		{
			"and-suffix turn grain", "SELECT session_id FROM api_turns WHERE 1=1%s",
			func(m string) (string, []any) { return modelfilter.AndRowIs("model", m) }, "gpt-5.1",
			[]string{"s4"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frag, args := tc.build(tc.model)
			if frag == "" {
				t.Fatalf("no fragment for model %q", tc.model)
			}
			got := ids(t, database, strings.Replace(tc.query, "%s", frag, 1), args)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
