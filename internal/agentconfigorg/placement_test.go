package agentconfigorg

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// firstRow returns the first catalogue row matching pred; ok is false when
// the catalogue has none (the case is then skipped: the rule still holds
// through TestPlacementAgreesWithLint).
func firstRow(pred func(agentconfig.SettingRow) bool) (agentconfig.SettingRow, bool) {
	for _, r := range agentconfig.Catalog() {
		if pred(r) {
			return r, true
		}
	}
	return agentconfig.SettingRow{}, false
}

func TestPlacementFor(t *testing.T) {
	cases := []struct {
		name    string
		pick    func(agentconfig.SettingRow) bool
		user    bool
		refusal string
	}{
		{"plain user setting", func(r agentconfig.SettingRow) bool { return r.ID == "claude-code/model" }, true, ""},
		{"secret row", func(r agentconfig.SettingRow) bool { return r.Secret }, false, "secret_setting"},
		{"tool-owned row", func(r agentconfig.SettingRow) bool { return !r.Secret && r.Owner != agentconfig.OwnerUser }, false, "not_writable"},
		{"Observer span", func(r agentconfig.SettingRow) bool {
			return !r.Secret && r.Owner == agentconfig.OwnerUser && r.Writable && len(agentconfig.SpansFor(r.ID)) > 0
		}, false, "observer_span"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, ok := firstRow(tc.pick)
			if !ok {
				t.Skip("no catalogue row of this shape")
			}
			p := PlacementFor(r.ID)
			if p.User != tc.user || p.Refusal != tc.refusal {
				t.Fatalf("%s: PlacementFor = %+v, want user=%v refusal=%q", r.ID, p, tc.user, tc.refusal)
			}
		})
	}
	if p := PlacementFor("no-such-tool/x"); p.Any() || p.Refusal != "unknown_setting" {
		t.Fatalf("unknown setting: %+v", p)
	}
}

// TestPlacementAgreesWithLint holds PlacementFor to the publish lint for
// every catalogue row: a refused placement shows up as that refusal code in
// Lint, an open one raises none of the per-setting refusal codes.
func TestPlacementAgreesWithLint(t *testing.T) {
	settingCodes := map[string]bool{
		"secret_setting": true, "not_writable": true, "observer_span": true, "no_user_file": true,
		"no_project_file": true, "no_local_file": true,
	}
	type probe struct {
		name  string
		entry func(id string) Entry
		open  func(Placement) bool
		code  func(Placement) string
	}
	probes := []probe{
		{
			name:  "user",
			entry: func(id string) Entry { return Entry{Setting: id, Mode: orgcontract.AgentConfigModeEnforced} },
			open:  func(p Placement) bool { return p.User },
			code:  func(p Placement) string { return p.Refusal },
		},
		{
			name: "project local",
			entry: func(id string) Entry {
				return Entry{Setting: id, Mode: orgcontract.AgentConfigModeEnforced, Project: projHash, Placement: orgcontract.AgentConfigPlacementLocalOnly}
			},
			open: func(p Placement) bool { return p.ProjectLocal },
		},
		{
			name: "project shared",
			entry: func(id string) Entry {
				return Entry{Setting: id, Mode: orgcontract.AgentConfigModeEnforced, Project: projHash, Placement: orgcontract.AgentConfigPlacementShared}
			},
			open: func(p Placement) bool { return p.ProjectShared },
		},
	}
	for _, r := range agentconfig.Catalog() {
		p := PlacementFor(r.ID)
		for _, pr := range probes {
			got := map[string]bool{}
			for _, is := range Lint(Spec{Settings: []Entry{pr.entry(r.ID)}}, nil) {
				if settingCodes[is.Code] && is.Severity == SevError {
					got[is.Code] = true
				}
			}
			if pr.open(p) && len(got) > 0 {
				t.Errorf("%s %s: placement open but lint refuses %v", r.ID, pr.name, got)
			}
			if !pr.open(p) && len(got) == 0 {
				t.Errorf("%s %s: placement closed but lint raises no refusal", r.ID, pr.name)
			}
			if pr.code != nil && !pr.open(p) && !got[pr.code(p)] {
				t.Errorf("%s %s: refusal %q not raised by lint (got %v)", r.ID, pr.name, pr.code(p), got)
			}
		}
	}
}
