package nodecomponents

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// report builds a node report the way a node would send it: every node row
// not in running is inactive (absent when listed in notBuilt, available
// otherwise), core always runs, and the active digest is computed over the
// running set unless badDigest asks for a mismatch.
func report(source string, running []string, notBuilt []string, badDigest bool) NodeReport {
	run := toSet(running)
	absent := toSet(notBuilt)
	var active []string
	r := NodeReport{Source: source, CatalogVersion: components.CatalogVersion}
	for _, row := range components.ForBinary(components.BinaryNode) {
		switch {
		case row.Class == components.ClassCore || run[row.ID]:
			active = append(active, row.ID)
		case absent[row.ID]:
			r.Inactive = append(r.Inactive, InactiveEntry{ID: row.ID, State: "absent", Reason: "not_compiled"})
		default:
			r.Inactive = append(r.Inactive, InactiveEntry{ID: row.ID, State: "available", Reason: "not_selected"})
		}
	}
	r.ActiveDigest = components.Digest(active)
	if badDigest {
		r.ActiveDigest = components.Digest([]string{"core"})
	}
	return r
}

func spec(t *testing.T, body string) PolicySpec {
	t.Helper()
	s, _, err := CompileBody([]byte(body), 1<<16)
	if err != nil {
		t.Fatalf("compile %s: %v", body, err)
	}
	return s
}

func lossMap(i Impact) map[string]LossReason {
	out := map[string]LossReason{}
	for _, c := range i.Components {
		out[c.Component] = c.Reason
	}
	return out
}

// TestImpactOf is the would-lose table (T-777): one row per way a running
// component stops, plus the nodes the draft does not reach and the ones whose
// running set cannot be verified.
func TestImpactOf(t *testing.T) {
	published := `{"require":["terminals"],"allow":["codeintel"],"unlisted":"inert"}`
	cases := []struct {
		name      string
		draft     string
		published string
		nodes     []NodeReport
		want      map[string]LossReason
		losing    int
		mayLose   int
		governed  int
		unchecked int
		notGov    int
		blank     bool
	}{
		{
			name:   "pinned node: unnamed component stops, allowed one may stop",
			draft:  `{"allow":["terminals"],"unlisted":"inert"}`,
			nodes:  []NodeReport{report("legacy_pinned", []string{"terminals", "codeintel"}, nil, false)},
			want:   map[string]LossReason{"codeintel": LossUnlisted, "terminals": LossSelectionOnly},
			losing: 1, governed: 1,
		},
		{
			name:      "policy node: same rules as today lose nothing",
			draft:     `{"require":["terminals"],"allow":["codeintel"],"unlisted":"inert"}`,
			published: published,
			nodes:     []NodeReport{report("policy", []string{"terminals", "codeintel"}, nil, false)},
			want:      map[string]LossReason{},
			governed:  1,
		},
		{
			name:      "policy node: required today, only allowed now",
			draft:     `{"allow":["terminals","codeintel"],"unlisted":"inert"}`,
			published: published,
			nodes:     []NodeReport{report("policy", []string{"terminals", "codeintel"}, nil, false)},
			want:      map[string]LossReason{"terminals": LossSelectionOnly},
			mayLose:   1, governed: 1,
		},
		{
			name:      "grantable deny",
			draft:     `{"require":["terminals"],"deny":[{"selector":"codeintel","grantable":true}],"unlisted":"inert"}`,
			published: published,
			nodes:     []NodeReport{report("policy", []string{"terminals", "codeintel"}, nil, false)},
			want:      map[string]LossReason{"codeintel": LossDeniedGrantable},
			losing:    1, governed: 1,
		},
		{
			name:      "deny stops the dependant too",
			draft:     `{"allow":["terminals.ssh","codeintel"],"deny":[{"selector":"terminals"}],"unlisted":"inert"}`,
			published: published,
			nodes:     []NodeReport{report("policy", []string{"terminals", "terminals.ssh", "codeintel"}, nil, false)},
			want:      map[string]LossReason{"terminals": LossDenied, "terminals.ssh": LossNeedsBlocked},
			losing:    1, governed: 1,
		},
		{
			name:  "individual, dormant and unauthorized nodes are not reached",
			draft: `{"deny":[{"selector":"codeintel"}],"unlisted":"inert"}`,
			nodes: []NodeReport{
				report("local", []string{"codeintel"}, nil, false),
				report("dormant", []string{"codeintel"}, nil, false),
				report("unauthorized", []string{"codeintel"}, nil, false),
			},
			want:   map[string]LossReason{},
			notGov: 3, blank: true,
		},
		{
			name:     "unverifiable running set is unchecked, never fine",
			draft:    `{"allow":["terminals"],"unlisted":"inert"}`,
			nodes:    []NodeReport{report("initial_grant", []string{"codeintel"}, nil, true)},
			want:     map[string]LossReason{},
			governed: 1, unchecked: 1,
		},
		{
			name:    "a component not in the build is never a loss",
			draft:   `{"allow":["terminals"],"unlisted":"inert"}`,
			nodes:   []NodeReport{report("legacy_pinned", []string{"terminals"}, []string{"codeintel"}, false)},
			want:    map[string]LossReason{"terminals": LossSelectionOnly},
			mayLose: 1, governed: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pub *PolicySpec
			if tc.published != "" {
				p := spec(t, tc.published)
				pub = &p
			}
			got := ImpactOf(spec(t, tc.draft), pub, tc.nodes)
			lm := lossMap(got)
			if len(lm) != len(tc.want) {
				t.Fatalf("losses = %+v, want %v", got.Components, tc.want)
			}
			for id, r := range tc.want {
				if lm[id] != r {
					t.Fatalf("loss[%s] = %q, want %q (all %+v)", id, lm[id], r, got.Components)
				}
			}
			if got.NodesLosing != tc.losing || got.NodesMayLose != tc.mayLose || got.Governed != tc.governed ||
				got.Unchecked != tc.unchecked || got.NotGoverned != tc.notGov || got.Blank != tc.blank {
				t.Fatalf("impact = %+v", got)
			}
			if got.Affected() != tc.losing+tc.mayLose {
				t.Fatalf("affected = %d", got.Affected())
			}
		})
	}
}

// TestComponentRules pins the per-component reading of a policy: named rules
// win, an unnamed component an allowed one needs is a dependency, everything
// else is off; core is never listed.
func TestComponentRules(t *testing.T) {
	r := ComponentRules(spec(t, `{"require":["codeintel"],"allow":["terminals.ssh"],"deny":[{"selector":"remote","grantable":true},{"selector":"demo"}],"unlisted":"inert"}`))
	want := map[string]Rule{
		"codeintel":     RuleRequire,
		"terminals.ssh": RuleAllow,
		"terminals":     RuleDependency,
		"remote":        RuleDenyGrantable,
		"demo":          RuleDeny,
		"guard":         RuleUnlisted,
	}
	for id, rule := range want {
		if r[id] != rule {
			t.Fatalf("rule[%s] = %q, want %q", id, r[id], rule)
		}
	}
	if _, ok := r["core"]; ok {
		t.Fatal("core must not be listed")
	}
	if !IsBlank(spec(t, `{"deny":[{"selector":"demo"}],"unlisted":"inert"}`)) || IsBlank(spec(t, `{"allow":["demo"],"unlisted":"inert"}`)) {
		t.Fatal("IsBlank")
	}
}

// TestFleetOf counts what managed nodes run, have off, lack, or cannot be
// checked, and breaks every reporting node down by governance source.
func TestFleetOf(t *testing.T) {
	f := FleetOf([]NodeReport{
		report("legacy_pinned", []string{"terminals", "codeintel"}, nil, false),
		report("policy", []string{"terminals"}, []string{"codeintel"}, false),
		report("initial_grant", []string{"terminals"}, nil, true),
		report("local", []string{"codeintel"}, nil, false),
		{Source: ""},
	})
	if f.Reporting != 5 || f.Managed != 3 || f.Verified != 2 {
		t.Fatalf("fleet = %+v", f)
	}
	if f.BySource["legacy_pinned"] != 1 || f.BySource["local"] != 1 || f.BySource["unknown"] != 1 {
		t.Fatalf("by source = %v", f.BySource)
	}
	row := func(id string) ComponentFleet {
		for _, c := range f.Components {
			if c.ID == id {
				return c
			}
		}
		t.Fatalf("no row %s", id)
		return ComponentFleet{}
	}
	if c := row("terminals"); c.Running != 2 || c.Unknown != 1 || c.Off != 0 {
		t.Fatalf("terminals = %+v", c)
	}
	if c := row("codeintel"); c.Running != 1 || c.NotInBuild != 1 || c.Off != 1 || c.Reasons["not_compiled"] != 1 || c.Reasons["not_selected"] != 1 {
		t.Fatalf("codeintel = %+v", c)
	}
	for _, c := range f.Components {
		if c.ID == "core" {
			t.Fatal("core must not be listed")
		}
	}
}
