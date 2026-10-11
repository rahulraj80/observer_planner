package agentconfig

import (
	"strings"
	"testing"
)

// TestDroidHooksJSONIsRootShaped pins Factory droid's standalone hooks.json
// to its ROOT shape: the event map sits at the top level
// ({"PreToolUse":[...]}), and a {"hooks":{...}} wrapper is ignored by droid
// (live-grounded 2026-10-04 on droid 0.233.0: "Ignoring unknown hook event
// keys", keys ["hooks"]). Only settings.json nests hooks under "hooks".
func TestDroidHooksJSONIsRootShaped(t *testing.T) {
	events := 0
	for _, r := range SettingsFor("droid") {
		if r.FileID != "droid/hooks" {
			continue
		}
		events++
		if len(r.Path) != 1 || r.Path[0] == "hooks" || r.ID != "droid/"+r.Path[0] {
			t.Errorf("hooks.json row %s at %v: the event must be a root key", r.ID, r.Path)
		}
	}
	if events == 0 {
		t.Fatal("no droid hooks.json rows")
	}
	if _, ok := Setting("droid/hooks.PreToolUse"); ok {
		t.Error("the wrapped droid/hooks.PreToolUse row is back")
	}

	spans := 0
	for _, s := range OwnedSpans() {
		if s.Tool == "droid" && s.Shape == "droid/hooks" {
			spans++
			if len(s.Path) != 0 {
				t.Errorf("droid hooks.json span at %v: it must cover the root event map", s.Path)
			}
		}
	}
	if spans == 0 {
		t.Fatal("no owned span on droid hooks.json")
	}

	// The span is live on a root-shaped file and the sanity check refuses a
	// save that drops Observer's entry.
	row, ok := fileRowByID("droid/user-hooks")
	if !ok {
		t.Fatal("no droid/user-hooks file row")
	}
	const file = `{
  "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "/usr/local/bin/observer hook droid user-prompt"}]}],
  "Stop": [{"hooks": [{"type": "command", "command": "notify-send done"}]}]
}
`
	live := false
	for _, st := range SpanStates(SpanEnv{}, row.ID, DecodeTree(row, []byte(file)), []byte(file), true) {
		live = live || st.Live
	}
	if !live {
		t.Fatal("Observer's root-level droid hook is not seen as an owned span")
	}
	stripped := strings.Replace(file, `{"hooks": [{"type": "command", "command": "/usr/local/bin/observer hook droid user-prompt"}]}`, "", 1)
	res := PlanSave(SaveInput{
		File: SaveFile{Row: row, Path: "/h/.factory/hooks.json", Present: true, Bytes: []byte(file)},
		Raw:  &stripped, Access: Access{SecretsPosture: SecretsWarn},
	})
	refused := false
	for _, f := range res.Findings {
		refused = refused || f.Code == "observer_span"
	}
	if res.OK || !refused {
		t.Fatalf("dropping Observer's root-level hook must be refused: %+v", res.Findings)
	}
}

func fileRowByID(id string) (FileRow, bool) {
	for _, f := range Files() {
		if f.ID == id {
			return f, true
		}
	}
	return FileRow{}, false
}
