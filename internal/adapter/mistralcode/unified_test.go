package mistralcode

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/models"
)

const (
	richUUID   = "4f3c2a10-5b6d-4e8f-9a0b-1c2d3e4f5a6b"
	failedUUID = "7d1e0f22-3a4b-4c5d-8e6f-a1b2c3d4e5f6"
)

func unifiedFixture(uuid string, elem ...string) string {
	return fixturePath(append([]string{"vibe-home", "logs", "session", "unified", uuid}, elem...)...)
}

// copyUnifiedSession copies one fixture session into <tmp>/.vibe/logs/session/
// unified/<uuid> and returns (watch root, CURRENT path).
func copyUnifiedSession(t *testing.T, uuid string) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".vibe", "logs", "session")
	src := unifiedFixture(uuid)
	dst := filepath.Join(root, "unified", uuid)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		copyFile(t, p, filepath.Join(dst, rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(dst, unifiedPointerName)
}

func appendJournal(t *testing.T, sessDir, segment, data string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(sessDir, "journal", segment), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
}

func eventIDs(res adapter.ParseResult) []string {
	var ids []string
	for _, ev := range res.ToolEvents {
		ids = append(ids, ev.SourceEventID)
	}
	return ids
}

func TestUnified_IsSessionFileShapes(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".vibe", "logs", "session")
	a := NewWithOptions(nil, root)
	sess := filepath.Join(root, "unified", richUUID)
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(sess, "CURRENT"), true},
		{filepath.Join(sess, "meta.json"), false},
		{filepath.Join(sess, "journal", "0000000000000010.jsonl"), false},
		{filepath.Join(sess, "chunks", strings.Repeat("a", 64)+".json"), false},
		{filepath.Join(sess, "generations", "0000000000000009", "manifest.json"), false},
		{filepath.Join(root, "active", richUUID+".lock"), false},
		{filepath.Join(root, "CURRENT"), false},                             // not inside unified/<id>/
		{filepath.Join(root, "other", richUUID, "CURRENT"), false},          // wrong parent
		{filepath.Join(t.TempDir(), "unified", richUUID, "CURRENT"), false}, // outside every watch root
		{filepath.Join(root, "session_20261002_181833_9d7020e1", "messages.jsonl"), true},
	}
	for _, tc := range cases {
		if got := a.IsSessionFile(tc.path); got != tc.want {
			t.Errorf("IsSessionFile(%s) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestUnified_RichSession is the regression for the live finding: vibe 2.25.8
// writes ONLY the unified store, and nothing in it was captured.
func TestUnified_RichSession(t *testing.T) {
	root, current := copyUnifiedSession(t, richUUID)
	a := NewWithOptions(nil, root)
	if !a.IsSessionFile(current) {
		t.Fatalf("IsSessionFile(CURRENT) = false: the unified store is not captured")
	}
	res, err := a.ParseSessionFile(context.Background(), current, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 || res.RetrySuggested {
		t.Fatalf("warnings=%v retry=%v", res.Warnings, res.RetrySuggested)
	}
	fi, _ := os.Stat(current)
	if res.NewOffset != fi.Size() {
		t.Errorf("NewOffset = %d, want CURRENT size %d", res.NewOffset, fi.Size())
	}

	type want struct {
		id, action, target, raw string
		success                 bool
	}
	wants := []want{
		{"user:4f3c2a10:a0c1d2e3-0000-4000-8000-000000000001", models.ActionUserPrompt, "Add a --verbose flag to the CLI and run the tests", "", true},
		{"tool:4f3c2a10:effect-act-0002", models.ActionReadFile, "src/cli.py", "file_system.read_file", true},
		{"tool:4f3c2a10:effect-act-0003", models.ActionEditFile, "src/cli.py", "file_system.search_replace", true},
		{"tool:4f3c2a10:effect-act-0004", models.ActionRunCommand, "python -m pytest -q", "file_system.bash", false},
		{"asst:4f3c2a10:assistant-act-0005", models.ActionAssistantMessage, "Added the --verbose flag; one test still fails.", "", true},
		{"user:4f3c2a10:a0c1d2e3-0000-4000-8000-000000000002", models.ActionUserPrompt, "Fix the failing test", "", true},
		{"tool:4f3c2a10:effect-act-0007", models.ActionTodoUpdate, "", "vibe.todo", true},
		{"asst:4f3c2a10:assistant-act-0008", models.ActionAssistantMessage, "Fixed the assertion; all 12 tests pass.", "", true},
	}
	if len(res.ToolEvents) != len(wants) {
		t.Fatalf("got %d events %v, want %d", len(res.ToolEvents), eventIDs(res), len(wants))
	}
	for i, w := range wants {
		ev := res.ToolEvents[i]
		if ev.SourceEventID != w.id || ev.ActionType != w.action || ev.Target != w.target ||
			ev.RawToolName != w.raw || ev.Success != w.success {
			t.Errorf("event %d = {%s %s %q %s %v}, want %+v", i, ev.SourceEventID, ev.ActionType, ev.Target, ev.RawToolName, ev.Success, w)
		}
		if ev.SessionID != "4f3c2a10" || ev.SourceFile != current || ev.Tool != models.ToolMistralCode {
			t.Errorf("event %d identity = %s %s %s", i, ev.SessionID, ev.SourceFile, ev.Tool)
		}
		if ev.ProjectRoot != "/home/dev/work/sample-app" {
			t.Errorf("event %d project root = %q", i, ev.ProjectRoot)
		}
	}
	if got := res.ToolEvents[1].PrecedingReasoning; got != "The flag parsing lives in src/cli.py; read it first." {
		t.Errorf("reasoning not carried onto the first tool call: %q", got)
	}
	if got := res.ToolEvents[0].Timestamp; !got.Equal(time.UnixMilli(1791000000000)) {
		t.Errorf("user timestamp = %v, want the entry's createdAt", got)
	}
	if out := res.ToolEvents[3].ToolOutput; out != "exit code 1" {
		t.Errorf("failed bash output = %q, want the error message", out)
	}
	for _, ev := range res.ToolEvents {
		if strings.Contains(ev.Target, "Partial answer") {
			t.Errorf("a discarded assistant entry was recorded: %s", ev.SourceEventID)
		}
	}

	if len(res.TokenEvents) != 1 {
		t.Fatalf("token events = %d, want 1", len(res.TokenEvents))
	}
	tk := res.TokenEvents[0]
	if tk.SourceEventID != "tokens:session:4f3c2a10" || tk.InputTokens != 60400-45000 ||
		tk.OutputTokens != 3900 || tk.CacheReadTokens != 45000 || tk.Model != "devstral-medium-2" {
		t.Errorf("token event = %+v", tk)
	}
	if len(res.SessionSurfaces) != 1 || res.SessionSurfaces[0].Surface != models.SurfaceCLI {
		t.Errorf("surfaces = %+v", res.SessionSurfaces)
	}
}

// TestUnified_FailedLiveShape mirrors the node-1 session: one prompt, the
// completion failed with a 429, a superseded generation and journal remain.
func TestUnified_FailedLiveShape(t *testing.T) {
	root, current := copyUnifiedSession(t, failedUUID)
	res, err := NewWithOptions(nil, root).ParseSessionFile(context.Background(), current, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := eventIDs(res)
	want := []string{
		"user:7d1e0f22:b21a52ef-0000-4000-8000-0000000000aa",
		"turnerr:7d1e0f22:turn-c0ffee00c0ffee00c0ffee00c0ffee00",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v (the superseded journal must not replay)", got, want)
	}
	errEv := res.ToolEvents[1]
	if errEv.ActionType != models.ActionAPIError || errEv.Success || errEv.Target != "model_stream_failed" ||
		!strings.Contains(errEv.ErrorMessage, "429") {
		t.Errorf("turn error event = %+v", errEv)
	}
	if len(res.TokenEvents) != 0 {
		t.Errorf("a session with no tokenUsage emitted %d token events", len(res.TokenEvents))
	}
}

// TestUnified_JournalGrowth: entries journaled after the snapshot appear on
// the next pass, earlier ids stay stable, and a half-written tail waits.
func TestUnified_JournalGrowth(t *testing.T) {
	root, current := copyUnifiedSession(t, richUUID)
	a := NewWithOptions(nil, root)
	first, err := a.ParseSessionFile(context.Background(), current, 0)
	if err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{
		"type": "message", "id": "a0c1d2e3-0000-4000-8000-000000000003", "role": "user",
		"turnId": "turn-3", "createdAt": 1791000200000, "generationStatus": "completed",
		"source": "turn_start", "content": []map[string]string{{"type": "text", "text": "Now update the README"}},
	}
	rec := map[string]any{
		"type": "projection_delta", "sequence": 18,
		"payload": map[string]any{"delta": []map[string]any{{"op": "append_entry", "entry": entry}}, "watermark": 11},
	}
	line, _ := json.Marshal(rec)
	sessDir := filepath.Dir(current)
	half := len(line) / 2
	appendJournal(t, sessDir, "0000000000000010.jsonl", string(line[:half]))

	second, err := a.ParseSessionFile(context.Background(), current, first.NewOffset)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(eventIDs(second), ",") != strings.Join(eventIDs(first), ",") {
		t.Fatalf("a half-written journal line changed the events: %v", eventIDs(second))
	}

	appendJournal(t, sessDir, "0000000000000010.jsonl", string(line[half:])+"\n")
	third, err := a.ParseSessionFile(context.Background(), current, second.NewOffset)
	if err != nil {
		t.Fatal(err)
	}
	ids := eventIDs(third)
	if len(ids) != len(eventIDs(first))+1 {
		t.Fatalf("events after growth = %v", ids)
	}
	for i, id := range eventIDs(first) {
		if ids[i] != id {
			t.Errorf("id %d changed across passes: %s -> %s", i, id, ids[i])
		}
	}
	if last := ids[len(ids)-1]; last != "user:4f3c2a10:a0c1d2e3-0000-4000-8000-000000000003" {
		t.Errorf("new entry id = %s", last)
	}
}

// TestUnified_CollectedGenerationRetries: a generation collected between the
// CURRENT read and the manifest read is a retry, never a warning.
func TestUnified_CollectedGenerationRetries(t *testing.T) {
	root, current := copyUnifiedSession(t, richUUID)
	if err := os.RemoveAll(filepath.Join(filepath.Dir(current), "generations", "0000000000000009")); err != nil {
		t.Fatal(err)
	}
	res, err := NewWithOptions(nil, root).ParseSessionFile(context.Background(), current, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.RetrySuggested || len(res.Warnings) != 0 || len(res.ToolEvents) != 0 {
		t.Errorf("retry=%v warnings=%v events=%d", res.RetrySuggested, res.Warnings, len(res.ToolEvents))
	}
}

// TestUnified_ManifestPathEscapeRefused: a manifest naming a document outside
// the session directory is refused, not read.
func TestUnified_ManifestPathEscapeRefused(t *testing.T) {
	root, current := copyUnifiedSession(t, richUUID)
	manifest := filepath.Join(filepath.Dir(current), "generations", "0000000000000009", "manifest.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(data), `"path":"projection-state.json"`, `"path":"../../../../outside.json"`, 1)
	if err := os.WriteFile(manifest, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := NewWithOptions(nil, root).ParseSessionFile(context.Background(), current, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 || len(res.ToolEvents) != 0 {
		t.Errorf("escape not refused: warnings=%v events=%d", res.Warnings, len(res.ToolEvents))
	}
}

func TestUnified_ImportedLegacyHistorySkipped(t *testing.T) {
	st := unifiedStore{sessionUUID: richUUID}
	st.runtime.ImportProvenance = &struct {
		Source struct {
			Backend string `json:"backend"`
		} `json:"source"`
		ImportedAt string `json:"imported_at"`
	}{ImportedAt: time.UnixMilli(1791000050000).UTC().Format(time.RFC3339Nano)}
	st.runtime.ImportProvenance.Source.Backend = "legacy"
	for _, e := range []map[string]any{
		{"type": "message", "id": "old", "role": "user", "createdAt": 1791000000000, "content": []map[string]string{{"type": "text", "text": "imported"}}},
		{"type": "message", "id": "new", "role": "user", "createdAt": 1791000060000, "content": []map[string]string{{"type": "text", "text": "after resume"}}},
	} {
		raw, _ := json.Marshal(e)
		st.entries = append(st.entries, raw)
	}
	var res adapter.ParseResult
	New().emitUnified("/x/logs/session/unified/"+richUUID+"/CURRENT", st, &res)
	if got := eventIDs(res); len(got) != 1 || got[0] != "user:4f3c2a10:new" {
		t.Errorf("events = %v, want only the post-import prompt", got)
	}
}

func TestUnified_ModelLadder(t *testing.T) {
	explicit := "mistral-medium-3.5"
	cases := []struct {
		name       string
		st         unifiedStore
		lastChange string
		want       string
	}{
		{"nothing known", unifiedStore{}, "", ""},
		{"turn error names the model", func() unifiedStore {
			var st unifiedStore
			st.envelope.LatestTurn = &unifiedTurn{Error: &struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Details struct {
					Model string `json:"model"`
				} `json:"details"`
			}{}}
			st.envelope.LatestTurn.Error.Details.Model = "mistral-vibe-cli-latest"
			return st
		}(), "", "mistral-vibe-cli-latest"},
		{"model change beats the error", unifiedStore{}, "devstral-medium-2", "devstral-medium-2"},
		{"active model wins", func() unifiedStore {
			var st unifiedStore
			st.runtime.SessionMetadata.ActiveModel = &explicit
			return st
		}(), "devstral-medium-2", explicit},
	}
	for _, tc := range cases {
		if got := unifiedModel(tc.st, tc.lastChange); got != tc.want {
			t.Errorf("%s: model = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestUnified_CursorSemantics(t *testing.T) {
	a := New()
	if k := a.CursorSemanticsFor("/h/.vibe/logs/session/unified/" + richUUID + "/CURRENT").Kind; k != adapter.CursorWatermark {
		t.Errorf("unified pointer kind = %v, want watermark", k)
	}
	if k := a.CursorSemanticsFor("/h/.vibe/logs/session/session_20261002_181833_9d7020e1/messages.jsonl").Kind; k != adapter.CursorByteOffset {
		t.Errorf("legacy transcript kind = %v, want byte offset", k)
	}
}

func TestUnified_ToolMapping(t *testing.T) {
	cases := map[string]string{
		"file_system.read_file":           models.ActionReadFile,
		"read_file":                       models.ActionReadFile,
		"file_system.search_replace":      models.ActionEditFile,
		"file_system.bash":                models.ActionRunCommand,
		"process.write":                   models.ActionStdinWrite,
		"skill.read":                      models.ActionSkillInvoke,
		"ui.ask_user_question":            models.ActionAskUser,
		"subagent.spawn":                  models.ActionSpawnSubagent,
		"search_tool_functions":           models.ActionToolSearch,
		"vibe.todo":                       models.ActionTodoUpdate,
		"vibe.unified_harness_scratchpad": models.ActionUnknown,
		"connector_web_search.open_url":   models.ActionUnknown,
	}
	for name, want := range cases {
		if got, _ := mapUnifiedTool(name, `{"path":"x"}`); got != want {
			t.Errorf("mapUnifiedTool(%s) = %s, want %s", name, got, want)
		}
	}
}
