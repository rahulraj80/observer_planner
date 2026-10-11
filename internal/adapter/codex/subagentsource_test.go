package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseSubagentObjectSourceKeepsIDAndLineage pins T-582: a spawned
// subagent's session_meta carries `source` as an OBJECT
// ({"subagent":{"thread_spawn":{...}}}), not the flat string a primary
// session writes. A plain string field failed the whole session_meta
// unmarshal, so every row landed under the filename-derived id and the
// session lost its thread_source / parent_thread_id lineage. The record
// must decode: the real id owns the rows and the lineage is emitted.
func TestParseSubagentObjectSourceKeepsIDAndLineage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
	}{
		{"thread_spawn object", `{"subagent":{"thread_spawn":{"parent_thread_id":"parent-sess","depth":1}}}`},
		{"unit variant object", `{"subagent":"review"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "rollout-2026-10-06T14-21-33-sub-sess.jsonl")
			body := strings.Join([]string{
				`{"timestamp":"2026-10-06T14:21:33.000Z","type":"session_meta","payload":{"id":"sub-sess","session_id":"sub-sess","timestamp":"2026-10-06T14:21:33.000Z","cwd":"/w","originator":"codex_exec","cli_version":"0.160.0","source":` + tc.source + `,"thread_source":"subagent","parent_thread_id":"parent-sess","model":"gpt-5.6","git_branch":"main"}}`,
				`{"timestamp":"2026-10-06T14:21:34.000Z","type":"event_msg","payload":{"type":"user_message","message":"list the files"}}`,
				``,
			}, "\n")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			res, err := NewWithOptions(nil, dir).ParseSessionFile(context.Background(), path, 0)
			if err != nil {
				t.Fatalf("ParseSessionFile: %v", err)
			}
			if len(res.ToolEvents) == 0 {
				t.Fatal("no tool events emitted")
			}
			for _, ev := range res.ToolEvents {
				if ev.SessionID != "sub-sess" {
					t.Errorf("event %s session_id = %q, want sub-sess (not the filename-derived id)", ev.ActionType, ev.SessionID)
				}
			}
			if len(res.SessionLineages) != 1 {
				t.Fatalf("session lineages = %+v, want exactly one", res.SessionLineages)
			}
			lin := res.SessionLineages[0]
			if lin.SessionID != "sub-sess" || lin.ThreadSource != "subagent" || lin.ParentThreadID != "parent-sess" {
				t.Errorf("lineage = %+v, want sub-sess / subagent / parent-sess", lin)
			}
		})
	}
}

// TestSessionMetaSourceDecodes pins the tolerant decoder: a string keeps
// its value, any other JSON type is the "no flat source" case, and none of
// them is an error.
func TestSessionMetaSourceDecodes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{`"cli"`, "cli"},
		{`"exec"`, "exec"},
		{`{"subagent":{"thread_spawn":{}}}`, ""},
		{`{"subagent":"compact"}`, ""},
		{`null`, ""},
		{`7`, ""},
	}
	for _, tc := range cases {
		var s sessionMetaSource
		if err := s.UnmarshalJSON([]byte(tc.in)); err != nil {
			t.Errorf("UnmarshalJSON(%s) error %v", tc.in, err)
		}
		if string(s) != tc.want {
			t.Errorf("UnmarshalJSON(%s) = %q, want %q", tc.in, s, tc.want)
		}
	}
}
