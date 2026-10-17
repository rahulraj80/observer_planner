package agentconfig

import (
	"testing"
)

// Phase P6: a managed node's org grants (D-102 / D-122) and org locks.
func TestAccessGrantsOnly(t *testing.T) {
	str := func(s string) *string { return &s }
	grantsOnly := func(grants ...string) func(*SaveInput) {
		return func(in *SaveInput) {
			in.Access = Access{SecretsPosture: SecretsWarn, GrantsOnly: true, Grants: grants, Reason: "managed"}
		}
	}
	cases := []struct {
		name      string
		edits     []Edit
		raw       *string
		mod       func(*SaveInput)
		ok        bool
		wantCodes []string
	}{
		{name: "granted setting", edits: []Edit{{Path: KeyPath{"model"}, Op: OpSet, Value: "opus"}}, mod: grantsOnly("claude-code/model"), ok: true},
		{name: "tool wildcard grant", edits: []Edit{{Path: KeyPath{"model"}, Op: OpSet, Value: "opus"}}, mod: grantsOnly("claude-code/*"), ok: true},
		{
			name: "ungranted setting", edits: []Edit{{Path: KeyPath{"permissions", "defaultMode"}, Op: OpSet, Value: "plan"}},
			mod: grantsOnly("claude-code/model"), wantCodes: []string{"not_granted"},
		},
		{
			name: "uncatalogued key without a tool grant", edits: []Edit{{Path: KeyPath{"zzNotAKey"}, Op: OpSet, Value: 1}},
			mod: grantsOnly("claude-code/model"), wantCodes: []string{"not_granted"},
		},
		{name: "uncatalogued key with a tool grant", edits: []Edit{{Path: KeyPath{"zzNotAKey"}, Op: OpSet, Value: 1}}, mod: grantsOnly("claude-code/*"), ok: true},
		{name: "no grant for this tool at all", edits: []Edit{{Path: KeyPath{"model"}, Op: OpSet, Value: "opus"}}, mod: grantsOnly("codex/model"), wantCodes: []string{"not_editable"}},
		{
			name: "locked beats a grant", edits: []Edit{{Path: KeyPath{"model"}, Op: OpSet, Value: "opus"}},
			mod: func(in *SaveInput) {
				in.Access = Access{SecretsPosture: SecretsWarn, GrantsOnly: true, Grants: []string{"claude-code/*"}, Locked: map[string]string{"claude-code/model": "Apps baseline"}}
			},
			wantCodes: []string{"org_locked"},
		},
		{
			name: "raw edit of a locked key", raw: str(`{"model": "opus", "permissions": {"defaultMode": "default"}}`),
			mod: func(in *SaveInput) {
				in.Access = Access{SecretsPosture: SecretsWarn, Locked: map[string]string{"claude-code/model": "Apps baseline"}}
			},
			wantCodes: []string{"org_locked"},
		},
		{
			name: "raw delete of a locked key", raw: str(`{"permissions": {"defaultMode": "default"}}`),
			mod: func(in *SaveInput) {
				in.Access = Access{SecretsPosture: SecretsWarn, Locked: map[string]string{"claude-code/model": "Apps baseline"}}
			},
			wantCodes: []string{"org_locked"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := claudeSave(t, c.edits, c.raw, c.mod)
			if r.OK != c.ok {
				t.Fatalf("OK = %v, want %v; findings %+v", r.OK, c.ok, r.Findings)
			}
			got := codes(r)
			for _, w := range c.wantCodes {
				if _, ok := got[w]; !ok {
					t.Fatalf("missing finding %q in %+v", w, r.Findings)
				}
			}
		})
	}
}

// TestLockedPathAncestorReplacement pins review M3-03: a locked setting is
// refused however the save reaches it. Replacing its ancestor object with a
// scalar or an array is one uncatalogued change at the ancestor in the diff,
// which used to skip the org_locked check entirely (an editable node, or a
// whole-tool grant, only warned).
func TestLockedPathAncestorReplacement(t *testing.T) {
	str := func(s string) *string { return &s }
	const before = `{"model": "sonnet", "permissions": {"defaultMode": "plan", "allow": ["Read"]}}`
	cases := []struct {
		name   string
		raw    string
		access Access
		ok     bool
	}{
		{"object to scalar", `{"model": "sonnet", "permissions": "plan"}`, Access{SecretsPosture: SecretsWarn}, false},
		{"object to array", `{"model": "sonnet", "permissions": ["plan"]}`, Access{SecretsPosture: SecretsWarn}, false},
		{"object to scalar under a whole-tool grant", `{"model": "sonnet", "permissions": 1}`, Access{SecretsPosture: SecretsWarn, GrantsOnly: true, Grants: []string{"claude-code/*"}}, false},
		{"ancestor deleted", `{"model": "sonnet"}`, Access{SecretsPosture: SecretsWarn}, false},
		{"locked value unchanged, sibling edited", `{"model": "opus", "permissions": {"defaultMode": "plan", "allow": ["Read"]}}`, Access{SecretsPosture: SecretsWarn}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := claudeSave(t, nil, str(c.raw), func(in *SaveInput) {
				in.File.Bytes = []byte(before)
				in.Access = c.access
				in.Access.Locked = map[string]string{"claude-code/permissions.defaultMode": "Apps baseline"}
			})
			if r.OK != c.ok {
				t.Fatalf("OK = %v, want %v; findings %+v", r.OK, c.ok, r.Findings)
			}
			n := 0
			for _, f := range r.Findings {
				if f.Code == "org_locked" && f.SettingID == "claude-code/permissions.defaultMode" {
					n++
				}
			}
			if (n == 1) == c.ok || n > 1 {
				t.Fatalf("org_locked findings = %d (want exactly one when refused); findings %+v", n, r.Findings)
			}
		})
	}
}

func TestAccessGranted(t *testing.T) {
	a := Access{GrantsOnly: true, Grants: []string{"codex/model", "claude-code/*"}}
	cases := []struct {
		id   string
		want bool
	}{
		{"codex/model", true},
		{"codex/approval_policy", false},
		{"claude-code/permissions.defaultMode", true},
		{"cursor/model", false},
	}
	for _, c := range cases {
		if got := a.Granted(c.id); got != c.want {
			t.Errorf("Granted(%q) = %v, want %v", c.id, got, c.want)
		}
	}
	if !(Access{}).Granted("x/y") {
		t.Error("an individual node (no GrantsOnly) grants everything")
	}
	if !a.ToolGranted("claude-code") || a.ToolGranted("codex") || !a.AnyGrantFor("codex") || a.AnyGrantFor("cursor") {
		t.Error("tool-level grant answers are wrong")
	}
}

// The view says why an ungranted row is read-only and badges org layers.
func TestInspectToolOrgLayers(t *testing.T) {
	user := fileRow(t, "claude-code/user-settings")
	files := []FileSnapshot{{
		Row: user, Resolved: ResolvedPath{Status: PathResolved, Path: "/h/.claude/settings.json"}, State: FilePresent,
		Bytes: []byte(`{"model":"sonnet","permissions":{"defaultMode":"plan"}}`),
	}}
	v := InspectTool(InspectInput{
		Tool: "claude-code", Files: files,
		Access: Access{SecretsPosture: SecretsWarn, GrantsOnly: true, Grants: []string{"claude-code/model"}, Locked: map[string]string{"claude-code/permissions.defaultMode": "Apps baseline"}},
		Layers: []LayerRecord{{FilePath: "/h/.claude/settings.json", Key: "permissions.defaultMode", Layer: ValueO2, ValueHash: ValueHash("plan")}},
	})
	byID := map[string]SettingView{}
	for _, s := range v.Settings {
		byID[s.ID] = s
	}
	if m := byID["claude-code/model"]; !m.Writable {
		t.Fatalf("granted model not writable: %+v", m)
	}
	dm := byID["claude-code/permissions.defaultMode"]
	if dm.Writable || dm.Locked != "Apps baseline" || dm.Effective.Layer != ValueO2 {
		t.Fatalf("locked defaultMode = %+v", dm)
	}
	if other := byID["claude-code/cleanupPeriodDays"]; other.ID != "" && (other.Writable || other.ReadOnlyReason == "") {
		t.Fatalf("ungranted setting = %+v", other)
	}
}

func TestCheckValueExported(t *testing.T) {
	r, ok := Setting("claude-code/permissions.defaultMode")
	if !ok {
		t.Fatal("missing row")
	}
	if CheckValue(r, "plan") != "" || CheckValue(r, "yolo") == "" {
		t.Fatal("CheckValue must accept the enum and refuse others")
	}
}
