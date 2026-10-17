package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nodeOneLegacyDroidHooks is the user-level ~/.factory/hooks.json the
// installed observer had written on demo node 1 (live 2026-10-04): the
// Codex-style {"hooks":{...}} wrapper. droid 0.233.0 logged "Ignoring
// unknown hook event keys" with keys ["hooks"] for it, so the prompt hook
// never fired.
const nodeOneLegacyDroidHooks = `{
  "hooks": {
    "UserPromptSubmit": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "/usr/local/lib/node_modules/@superbased/observer/node_modules/@superbased/observer-linux-x64/bin/observer hook droid UserPromptSubmit"
          }
        ]
      }
    ]
  }
}
`

// droidRootCommands decodes a droid hooks.json the way droid 0.233.0 does:
// the ROOT object is the event map. It fails the test on a "hooks" key,
// which droid ignores.
func droidRootCommands(t *testing.T, path string) map[string][]string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("hooks.json not valid JSON: %v\n%s", err, body)
	}
	if _, wrapped := root["hooks"]; wrapped {
		t.Fatalf("hooks.json still nests events under \"hooks\" (droid ignores that key):\n%s", body)
	}
	out := map[string][]string{}
	for event, raw := range root {
		var groups []codexHookGroup
		if err := json.Unmarshal(raw, &groups); err != nil {
			t.Fatalf("event %s is not a list of matcher groups: %v\n%s", event, err, body)
		}
		for _, g := range groups {
			for _, h := range g.Hooks {
				out[event] = append(out[event], h.Command)
			}
		}
	}
	return out
}

func TestRegisterFactoryDroid_WritesEventsAtRoot(t *testing.T) {
	t.Parallel()
	r := genericSettingsRegistry(t, ".factory")
	res := r.Register("droid")
	if res.Error != nil {
		t.Fatalf("Register: %v", res.Error)
	}
	got := droidRootCommands(t, res.ConfigPath)
	for _, ev := range []string{"UserPromptSubmit", "PreToolUse"} {
		if len(got[ev]) != 1 || !strings.Contains(got[ev][0], " hook droid "+ev) {
			t.Errorf("%s = %v, want one observer entry at the root", ev, got[ev])
		}
	}
}

func TestRegisterFactoryDroid_RepairsLegacyWrappedFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		legacy      string
		wantAdded   int
		wantAlready int
	}{
		{
			// The live node-1 file: an older binary path, UserPromptSubmit
			// only. The stale entry is refreshed and the tool gate added.
			name: "node 1 user-level file", legacy: nodeOneLegacyDroidHooks,
			wantAdded: 2, wantAlready: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := genericSettingsRegistry(t, ".factory")
			path := filepath.Join(r.opts.HomeDir, ".factory", "hooks.json")
			if err := os.WriteFile(path, []byte(tc.legacy), 0o600); err != nil {
				t.Fatal(err)
			}
			res := r.Register("droid")
			if res.Error != nil {
				t.Fatalf("Register: %v", res.Error)
			}
			if len(res.HooksAdded) != tc.wantAdded || len(res.AlreadySet) != tc.wantAlready {
				t.Errorf("HooksAdded=%v AlreadySet=%v, want %d added / %d already set", res.HooksAdded, res.AlreadySet, tc.wantAdded, tc.wantAlready)
			}
			got := droidRootCommands(t, path)
			if len(got["UserPromptSubmit"]) != 1 || strings.Contains(got["UserPromptSubmit"][0], "node_modules") {
				t.Errorf("UserPromptSubmit = %v, want exactly the refreshed observer entry", got["UserPromptSubmit"])
			}
			if len(got["PreToolUse"]) != 1 {
				t.Errorf("PreToolUse = %v, want the tool gate", got["PreToolUse"])
			}
		})
	}
}

// TestRegisterFactoryDroid_RewritesWrappedFileWhenAlreadySet pins that a
// wrapped file whose entries are all current is still rewritten: droid
// ignores the wrapper, so "already set" there means "never fires".
func TestRegisterFactoryDroid_RewritesWrappedFileWhenAlreadySet(t *testing.T) {
	t.Parallel()
	r := genericSettingsRegistry(t, ".factory")
	path := filepath.Join(r.opts.HomeDir, ".factory", "hooks.json")
	if res := r.Register("droid"); res.Error != nil {
		t.Fatalf("first: %v", res.Error)
	}
	root := droidRootCommands(t, path)
	wrapped := map[string]map[string][]codexHookGroup{"hooks": {}}
	for ev, cmds := range root {
		wrapped["hooks"][ev] = []codexHookGroup{{Matcher: "*", Hooks: []claudeHookCommand{{Type: "command", Command: cmds[0]}}}}
	}
	b, _ := json.Marshal(wrapped)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	res := r.Register("droid")
	if res.Error != nil {
		t.Fatalf("second: %v", res.Error)
	}
	// A current entry that sat in the wrapper was not firing (droid ignores
	// the wrapper), so the repair reports it as added, never as already set:
	// the dry-run currency read (doctor, the node tool gate) must not call a
	// wrapped hook current.
	if len(res.HooksAdded) != 2 || len(res.AlreadySet) != 0 {
		t.Errorf("HooksAdded=%v AlreadySet=%v, want both added by the repair / none already set", res.HooksAdded, res.AlreadySet)
	}
	if got := droidRootCommands(t, path); len(got["UserPromptSubmit"]) != 1 || len(got["PreToolUse"]) != 1 {
		t.Errorf("after repair = %v, want both entries at the root", got)
	}
}

// TestRegisterFactoryDroid_KeepsForeignGroupFields pins that a group
// Observer does not own keeps droid-only fields (commandRegex, timeout).
func TestRegisterFactoryDroid_KeepsForeignGroupFields(t *testing.T) {
	t.Parallel()
	r := genericSettingsRegistry(t, ".factory")
	path := filepath.Join(r.opts.HomeDir, ".factory", "hooks.json")
	pre := `{"PostToolUse":[{"matcher":"Execute","commandRegex":"^git ","hooks":[{"type":"command","command":"/usr/local/bin/audit.sh && true","timeout":30}]}]}`
	if err := os.WriteFile(path, []byte(pre), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := r.Register("droid"); res.Error != nil {
		t.Fatalf("Register: %v", res.Error)
	}
	body, _ := os.ReadFile(path)
	for _, want := range []string{`"commandRegex": "^git "`, `"timeout": 30`, `/usr/local/bin/audit.sh && true`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("hooks.json lost %s:\n%s", want, body)
		}
	}
	if res := r.Unregister("droid"); res.Error != nil || len(res.HooksRemoved) != 2 {
		t.Fatalf("Unregister: removed=%v err=%v", res.HooksRemoved, res.Error)
	}
	got := droidRootCommands(t, path)
	if len(got) != 1 || len(got["PostToolUse"]) != 1 {
		t.Errorf("after unregister = %v, want only the foreign PostToolUse group", got)
	}
}

func TestUnregisterFactoryDroid_LegacyWrappedFile(t *testing.T) {
	t.Parallel()
	r := genericSettingsRegistry(t, ".factory")
	path := filepath.Join(r.opts.HomeDir, ".factory", "hooks.json")
	if err := os.WriteFile(path, []byte(nodeOneLegacyDroidHooks), 0o600); err != nil {
		t.Fatal(err)
	}
	res := r.Unregister("droid")
	if res.Error != nil {
		t.Fatalf("Unregister: %v", res.Error)
	}
	if strings.Join(res.HooksRemoved, ",") != "UserPromptSubmit" {
		t.Errorf("HooksRemoved = %v, want [UserPromptSubmit]", res.HooksRemoved)
	}
	if got := droidRootCommands(t, path); len(got) != 0 {
		t.Errorf("after unregister = %v, want no events", got)
	}
}

func TestReadDroidHooks_Table(t *testing.T) {
	t.Parallel()
	const obs = `{"matcher":"*","hooks":[{"type":"command","command":"/opt/observer/bin/observer hook droid Stop"}]}`
	const user = `{"matcher":"u","hooks":[{"type":"command","command":"/usr/local/bin/mine.sh"}]}`
	all := func(string) bool { return true }
	cases := []struct {
		name       string
		body       string
		lift       func(string) bool
		wantLifted []string
		wantErr    bool
		wantEvents []string
	}{
		{name: "empty file", body: "", lift: all, wantEvents: nil},
		{name: "root events", body: `{"PreToolUse":[{"hooks":[]}]}`, lift: all, wantEvents: []string{"PreToolUse"}},
		{name: "observer group lifted", body: `{"hooks":{"Stop":[` + obs + `]}}`, lift: all, wantLifted: []string{"Stop"}, wantEvents: []string{"Stop"}},
		{name: "observer group merged after root groups", body: `{"Stop":[{"matcher":"a","hooks":[]}],"hooks":{"Stop":[` + obs + `]}}`, lift: all, wantLifted: []string{"Stop"}, wantEvents: []string{"Stop"}},
		// Review M2-15: a group Observer did not write stays wrapped (droid
		// never ran it; lifting would start running it).
		{name: "user group stays wrapped", body: `{"hooks":{"Stop":[` + user + `]}}`, lift: all, wantEvents: []string{"hooks"}},
		{name: "mixed: observer lifted, user stays", body: `{"hooks":{"Stop":[` + obs + `,` + user + `]}}`, lift: all, wantLifted: []string{"Stop"}, wantEvents: []string{"Stop", "hooks"}},
		// Review M2-10: an event the caller will not write is not lifted.
		{name: "skipped event stays wrapped", body: `{"hooks":{"Stop":[` + obs + `]}}`, lift: func(string) bool { return false }, wantEvents: []string{"hooks"}},
		{name: "unknown hooks value kept", body: `{"hooks":"x"}`, lift: all, wantEvents: []string{"hooks"}},
		{name: "garbage", body: `{"PreToolUse":`, lift: all, wantErr: true},
		{name: "byte order mark", body: "\ufeff{}", lift: all, wantEvents: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "hooks.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			doc, lifted, err := readDroidHooks(path, tc.lift)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			var got []string
			for ev := range lifted {
				got = append(got, ev)
			}
			if strings.Join(got, ",") != strings.Join(tc.wantLifted, ",") {
				t.Errorf("lifted = %v, want %v", got, tc.wantLifted)
			}
			if len(doc) != len(tc.wantEvents) {
				t.Errorf("keys = %v, want %v", doc, tc.wantEvents)
			}
			for _, ev := range tc.wantEvents {
				if _, ok := doc[ev]; !ok {
					t.Errorf("missing %s in %v", ev, doc)
				}
			}
			switch tc.name {
			case "observer group merged after root groups":
				gs, _ := doc.groups("Stop")
				if len(gs) != 2 || !strings.Contains(string(gs[0]), `"a"`) || !strings.Contains(string(gs[1]), "observer hook droid") {
					t.Errorf("Stop groups = %s, want root group then lifted group", gs)
				}
			case "mixed: observer lifted, user stays":
				if !strings.Contains(string(doc["hooks"]), "mine.sh") || strings.Contains(string(doc["hooks"]), "observer hook droid") {
					t.Errorf("wrapper = %s, want only the user group left in it", doc["hooks"])
				}
			}
		})
	}
}

// TestRegisterFactoryDroid_NeverActivatesForeignOrSkippedGroups pins reviews
// M2-15 and M2-10 end to end: the repair of a wrapped hooks.json moves only
// Observer's groups of the events this run writes; a user's wrapped group
// and an Observer group for a skipped event stay inert in the wrapper.
func TestRegisterFactoryDroid_NeverActivatesForeignOrSkippedGroups(t *testing.T) {
	t.Parallel()
	r := genericSettingsRegistry(t, ".factory")
	opts := r.opts
	opts.SkipEvent = func(tool, event string) bool { return tool == "droid" && event == "UserPromptSubmit" }
	r, err := NewRegistry(opts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.opts.HomeDir, ".factory", "hooks.json")
	legacy := `{"hooks":{"UserPromptSubmit":[{"matcher":"*","hooks":[{"type":"command","command":"/usr/local/lib/node_modules/@superbased/observer/bin/observer hook droid UserPromptSubmit"}]}],"SessionStart":[{"matcher":"*","hooks":[{"type":"command","command":"/usr/local/bin/never-ran.sh"}]}]}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	res := r.Register("droid")
	if res.Error != nil {
		t.Fatalf("Register: %v", res.Error)
	}
	if strings.Join(res.HooksAdded, ",") != "PreToolUse" {
		t.Errorf("HooksAdded = %v, want only the live PreToolUse gate", res.HooksAdded)
	}
	var root map[string]json.RawMessage
	body, _ := os.ReadFile(path)
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["SessionStart"]; ok {
		t.Errorf("the user's never-run SessionStart group was activated:\n%s", body)
	}
	if _, ok := root["UserPromptSubmit"]; ok {
		t.Errorf("a skipped event's Observer group was activated:\n%s", body)
	}
	if !strings.Contains(string(root["hooks"]), "never-ran.sh") || !strings.Contains(string(root["hooks"]), "hook droid UserPromptSubmit") {
		t.Errorf("wrapper lost its inert groups:\n%s", body)
	}
	if _, ok := root["PreToolUse"]; !ok {
		t.Errorf("the live gate was not written at the root:\n%s", body)
	}
}
