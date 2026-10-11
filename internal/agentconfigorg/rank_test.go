package agentconfigorg

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
)

// TestComposeTeamRank pins D-123 with the stored integer priority: the team
// with the higher unique priority wins; equal priorities (including two
// unset ones) leave the conflict; ordered-danger keys stay strictest-wins
// across every team whatever the priorities say.
func TestComposeTeamRank(t *testing.T) {
	src := func(name, team, setting string, v any) Source {
		return Source{Name: name, Layer: agentconfig.LayerO2, Team: team, Spec: Spec{Settings: []Entry{{Setting: setting, Value: v, Mode: "enforced"}}}}
	}
	a, b, c := src("A", "ta", "codex/model", "gpt-5"), src("B", "tb", "codex/model", "o3"), src("C", "tc", "codex/model", "o4")
	cases := []struct {
		name string
		srcs []Source
		rank map[string]int
		want string // "" = conflict skipped
	}{
		{"higher unique priority wins", []Source{a, b}, map[string]int{"ta": 1, "tb": 5}, `"o3"`},
		{"equal priority conflicts", []Source{a, b}, map[string]int{"ta": 5, "tb": 5}, ""},
		{"unset priorities conflict", []Source{a, b}, map[string]int{}, ""},
		{"top beats a tie below it", []Source{a, b, c}, map[string]int{"ta": 2, "tb": 2, "tc": 9}, `"o4"`},
		{"tie at the top conflicts", []Source{a, b, c}, map[string]int{"ta": 9, "tb": 9, "tc": 1}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Compose(ComposeInput{Sources: tc.srcs, TeamRank: tc.rank})
			if err != nil {
				t.Fatal(err)
			}
			d, ok := directive(got.Body, "codex/model", "", "enforced")
			if tc.want == "" {
				if ok || len(got.Skipped) != 1 {
					t.Fatalf("expected a conflict, got %+v", got)
				}
				return
			}
			if !ok || string(d.Value) != tc.want {
				t.Fatalf("got %+v (ok %v), want %s", d, ok, tc.want)
			}
		})
	}
	// Ordered-danger common key: strictest wins regardless of priority.
	x := Source{Name: "X", Layer: agentconfig.LayerO2, Team: "tx", Spec: Spec{Settings: []Entry{{Common: "approval_mode", Value: "full_auto", Mode: "enforced"}}}}
	y := Source{Name: "Y", Layer: agentconfig.LayerO2, Team: "ty", Spec: Spec{Settings: []Entry{{Common: "approval_mode", Value: "ask_edits", Mode: "enforced"}}}}
	got, err := Compose(ComposeInput{Sources: []Source{x, y}, TeamRank: map[string]int{"tx": 9, "ty": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := directive(got.Body, "claude-code/permissions.defaultMode", "", "enforced"); !ok || string(d.Value) != `"default"` {
		t.Fatalf("ordered-danger key not strictest-wins: %+v", got)
	}
}

func TestLintTeamConflictsRanked(t *testing.T) {
	docs := []TeamDoc{
		{Name: "A", Teams: []string{"ta"}, Spec: Spec{Settings: []Entry{{Setting: "codex/model", Value: "gpt-5", Mode: "enforced"}}}},
		{Name: "B", Teams: []string{"tb"}, Spec: Spec{Settings: []Entry{{Setting: "codex/model", Value: "o3", Mode: "enforced"}}}},
		{Name: "B2", Teams: []string{"tc"}, Spec: Spec{Settings: []Entry{{Setting: "codex/model", Value: "gpt-5", Mode: "enforced"}}}},
		{Name: "D", Teams: []string{"td"}, Spec: Spec{Settings: []Entry{{Common: "approval_mode", Value: "full_auto", Mode: "enforced"}}}},
		{Name: "E", Teams: []string{"te"}, Spec: Spec{Settings: []Entry{{Common: "approval_mode", Value: "ask_edits", Mode: "enforced"}}}},
	}
	members := map[string][]string{
		"u-both":   {"ta", "tb"}, // different values, equal (unset) priority: conflict
		"u-same":   {"ta", "tc"}, // same value: no conflict
		"u-danger": {"td", "te"}, // ordered-danger: never a conflict
		"u-one":    {"tb"},
	}
	got := LintTeamConflictsRanked(docs, map[string]int{}, members)
	if len(got) != 1 || got[0].Member != "u-both" || got[0].Target != "setting:codex/model" {
		t.Fatalf("conflicts = %+v", got)
	}
	if got := LintTeamConflictsRanked(docs, map[string]int{"tb": 3}, members); len(got) != 0 {
		t.Fatalf("a unique higher priority should clear the conflict: %+v", got)
	}
	if got := LintTeamConflictsRanked(docs, map[string]int{"ta": 3, "tb": 3}, members); len(got) != 1 {
		t.Fatalf("equal priority must still conflict: %+v", got)
	}
}
