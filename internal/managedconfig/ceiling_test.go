package managedconfig

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

func tomlProbe(doc string) Probe {
	return Probe{Found: true, Source: "/etc/superbased/managed.toml", Format: "toml", Data: []byte(doc)}
}

const validDoc = `[ceiling]
catalog_version = 1
components = ["codeintel", "adapter.codex"]
`

// TestDecideTable is one row per decideRules row (fourth-pass M1).
func TestDecideTable(t *testing.T) {
	lkg := &Ceiling{IDs: []string{"terminals"}, CatalogVersion: 1, SourceHash: "abc"}
	cases := []struct {
		name        string
		probe       Probe
		lkg         *Ceiling
		status      Status
		ids         []string
		updates     bool
		persist     bool
		failedCheck string
	}{
		{name: "no source: no ceiling, updates allowed", probe: Probe{}, status: StatusNone, updates: true},
		{name: "no source ignores a stored set", probe: Probe{}, lkg: lkg, status: StatusNone, updates: true},
		{name: "valid source", probe: tomlProbe(validDoc), status: StatusValid, ids: []string{"adapter.codex", "codeintel"}, updates: true, persist: true},
		{name: "valid source replaces a stored set", probe: tomlProbe(validDoc), lkg: lkg, status: StatusValid, ids: []string{"adapter.codex", "codeintel"}, updates: true, persist: true},
		{name: "ownership failure, never read valid: core only", probe: Probe{Found: true, FailedCheck: "not_root_owned"}, status: StatusInvalidCoreOnly, ids: []string{}, failedCheck: "not_root_owned"},
		{name: "malformed, read valid before: last known", probe: tomlProbe("[ceiling]\ncomponents = 3\n"), lkg: lkg, status: StatusInvalidLastKnown, ids: []string{"terminals"}, failedCheck: "parse"},
		{name: "malformed, never read valid: core only", probe: tomlProbe("not toml"), status: StatusInvalidCoreOnly, ids: []string{}, failedCheck: "parse"},
	}
	covered := map[Status]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Decide(tc.probe, tc.lkg)
			if r.Status != tc.status || r.UpdatesAllowed != tc.updates || r.Persist != tc.persist || r.FailedCheck != tc.failedCheck {
				t.Fatalf("got status=%s updates=%v persist=%v failed=%q err=%q", r.Status, r.UpdatesAllowed, r.Persist, r.FailedCheck, r.Err)
			}
			pinned, ids := r.Pinned()
			if tc.status == StatusNone {
				if pinned {
					t.Fatal("no source must not pin a ceiling")
				}
				return
			}
			if !pinned || strings.Join(ids, ",") != strings.Join(tc.ids, ",") {
				t.Fatalf("pinned=%v ids=%v, want %v", pinned, ids, tc.ids)
			}
		})
		covered[tc.status] = true
	}
	for _, r := range decideRules {
		if !covered[r.status] {
			t.Errorf("rule %q has no row", r.name)
		}
	}
}

func TestDecideNoPersistWhenUnchanged(t *testing.T) {
	first := Decide(tomlProbe(validDoc), nil)
	again := Decide(tomlProbe(validDoc), first.Ceiling)
	if again.Persist {
		t.Fatal("an unchanged ceiling asks to be persisted again")
	}
	// The stored last-known set is copied, never aliased.
	bad := Decide(Probe{Found: true, FailedCheck: "symlink"}, first.Ceiling)
	bad.Ceiling.IDs[0] = "changed"
	if first.Ceiling.IDs[0] == "changed" {
		t.Fatal("result aliases the caller's last-known set")
	}
}

func TestParseTOML(t *testing.T) {
	cases := []struct {
		name    string
		doc     string
		ids     string
		note    string
		wantErr string
	}{
		{name: "valid", doc: validDoc, ids: "adapter.codex,codeintel"},
		{name: "empty list is core only", doc: "[ceiling]\ncatalog_version = 1\ncomponents = []\n", ids: ""},
		{name: "duplicates collapse", doc: "[ceiling]\ncatalog_version = 1\ncomponents = [\"codeintel\", \"codeintel\"]\n", ids: "codeintel"},
		{name: "unknown id ignored with a note", doc: "[ceiling]\ncatalog_version = 1\ncomponents = [\"codeintel\", \"adapter.future\"]\n", ids: "codeintel", note: "adapter.future: not a component"},
		{name: "org id ignored with a note", doc: "[ceiling]\ncatalog_version = 1\ncomponents = [\"org.digest\"]\n", ids: "", note: "org server component"},
		{name: "newer catalog noted", doc: "[ceiling]\ncatalog_version = 99\ncomponents = []\n", note: "newer than this build"},
		{name: "unknown key", doc: "[ceiling]\ncatalog_version = 1\ncomponents = []\ndistro = \"managed\"\n", wantErr: "unknown keys ceiling.distro"},
		{name: "unknown table", doc: "[ceiling]\ncatalog_version = 1\ncomponents = []\n[extra]\nx = 1\n", wantErr: "unknown keys"},
		{name: "components required", doc: "[ceiling]\ncatalog_version = 1\n", wantErr: "components is required"},
		{name: "catalog version required", doc: "[ceiling]\ncomponents = []\n", wantErr: "catalog_version is required"},
		{name: "catalog version positive", doc: "[ceiling]\ncatalog_version = 0\ncomponents = []\n", wantErr: "positive integer"},
		{name: "selectors are not ids", doc: "[ceiling]\ncatalog_version = 1\ncomponents = [\"adapter.*\"]\n", wantErr: "not a component id"},
		{name: "classes are not ids", doc: "[ceiling]\ncatalog_version = 1\ncomponents = [\"class:adapter\"]\n", wantErr: "not a component id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, notes, err := ParseTOML([]byte(tc.doc))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(c.IDs, ","); got != tc.ids {
				t.Errorf("ids = %q, want %q", got, tc.ids)
			}
			if tc.note != "" && !strings.Contains(strings.Join(notes, "\n"), tc.note) {
				t.Errorf("notes %v, want %q", notes, tc.note)
			}
			if c.SourceHash == "" {
				t.Error("no source hash")
			}
		})
	}
}

func TestParsePlistJSONAndRegistry(t *testing.T) {
	c, _, err := parseProbe(Probe{Format: "json", Data: []byte(`{"Components":["codeintel"],"CatalogVersion":1}`)})
	if err != nil || strings.Join(c.IDs, ",") != "codeintel" {
		t.Fatalf("json: %v %v", c, err)
	}
	for _, bad := range []string{`{"Components":["codeintel"]}`, `{"CatalogVersion":1}`, `{"Components":[],"CatalogVersion":1,"Extra":1}`} {
		if _, _, err := parseProbe(Probe{Format: "json", Data: []byte(bad)}); err == nil {
			t.Errorf("json %s parsed", bad)
		}
	}
	c, _, err = parseProbe(Probe{Format: "registry", Registry: &RegistryValues{HaveComponents: true, Components: []string{"codeintel"}, HaveVersion: true, CatalogVersion: 1}})
	if err != nil || strings.Join(c.IDs, ",") != "codeintel" {
		t.Fatalf("registry: %v %v", c, err)
	}
	for name, v := range map[string]*RegistryValues{
		"unknown value":    {HaveComponents: true, HaveVersion: true, CatalogVersion: 1, Unknown: []string{"Distro"}},
		"no components":    {HaveVersion: true, CatalogVersion: 1},
		"no version":       {HaveComponents: true},
		"missing entirely": nil,
	} {
		if _, _, err := parseProbe(Probe{Format: "registry", Registry: v}); err == nil {
			t.Errorf("registry %s parsed", name)
		}
	}
}

func TestFromDistroRenderRoundTrip(t *testing.T) {
	for _, d := range components.Distros() {
		c, err := FromDistro(d)
		if err != nil {
			t.Fatal(err)
		}
		back, notes, err := ParseTOML(RenderTOML(c))
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		if len(notes) != 0 {
			t.Errorf("%s: notes %v", d, notes)
		}
		if strings.Join(back.IDs, ",") != strings.Join(c.IDs, ",") || back.CatalogVersion != components.CatalogVersion {
			t.Errorf("%s: round trip changed the set", d)
		}
		if extra := back.Exceeds(components.InDistro(d)); len(extra) != 0 {
			t.Errorf("%s: its own ceiling is exceeded by %v", d, extra)
		}
	}
	if _, err := FromDistro("tiny"); err == nil {
		t.Error("unknown distro resolved")
	}
	minimal, _ := FromDistro(components.DistroMinimal)
	if extra := minimal.Exceeds(components.InDistro(components.DistroFull)); len(extra) == 0 {
		t.Error("a minimal ceiling does not refuse a transition to full")
	}
}

func TestCheckKeySecurity(t *testing.T) {
	admin, system, users := sidAdministrators, sidSystem, "S-1-5-32-545"
	cases := []struct {
		name  string
		owner string
		dacl  bool
		aces  []ACE
		want  string
	}{
		{"admins and system only", admin, true, []ACE{{Mask: 0xF003F, SID: admin}, {Mask: 0xF003F, SID: system}, {Mask: 0x20019, SID: users}}, ""},
		{"users can read only", system, true, []ACE{{Mask: 0x20019, SID: users}}, ""},
		{"users can set a value", admin, true, []ACE{{Mask: 0x2, SID: users}}, "registry_writable_by_" + users},
		{"generic write", admin, true, []ACE{{Mask: 0x40000000, SID: users}}, "registry_writable_by_" + users},
		{"inherit-only ace is not effective here", admin, true, []ACE{{Flags: aceFlagInheritOnly, Mask: 0x2, SID: "S-1-3-0"}}, ""},
		{"deny ace is not a grant", admin, true, []ACE{{Type: 1, Mask: 0x2, SID: users}}, ""},
		{"untrusted owner", users, true, nil, "registry_owner_not_admin_or_system"},
		{"null dacl", admin, false, nil, "registry_null_dacl"},
		{"trusted installer", sidTrustedInstaller, true, []ACE{{Mask: 0xF003F, SID: sidTrustedInstaller}}, ""},
		// MODPKG-9: an allowed ACE in a layout whose SID is not read cannot
		// be verified, so a write right in it fails closed.
		{"callback allowed ace with write", admin, true, []ACE{{Type: 0x9, Mask: 0x2}}, "registry_unverifiable_allow_ace_type_9"},
		{"object allowed ace with generic all", admin, true, []ACE{{Type: 0x5, Mask: 0x10000000}}, "registry_unverifiable_allow_ace_type_5"},
		{"callback object allowed ace with write dac", admin, true, []ACE{{Type: 0xB, Mask: 0x40000}}, "registry_unverifiable_allow_ace_type_11"},
		{"callback allowed ace, read only", admin, true, []ACE{{Type: 0x9, Mask: 0x20019}}, ""},
		{"inherit-only callback ace is not effective here", admin, true, []ACE{{Type: 0x9, Flags: aceFlagInheritOnly, Mask: 0x2}}, ""},
		{"callback deny ace is not a grant", admin, true, []ACE{{Type: 0xA, Mask: 0x2}}, ""},
	}
	for _, tc := range cases {
		if got := CheckKeySecurity(tc.owner, tc.dacl, tc.aces); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
