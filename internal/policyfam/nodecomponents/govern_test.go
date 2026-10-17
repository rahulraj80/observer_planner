package nodecomponents

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// TestSelectSourceTable is one row per row of sourceRules (plan 6.2 rows 9
// to 11, 5.6.1 legacy).
func TestSelectSourceTable(t *testing.T) {
	live := Facts{Managed: true, Live: true, Authorized: true}
	with := func(f Facts, mod func(*Facts)) Facts { mod(&f); return f }
	cases := []struct {
		name string
		f    Facts
		want Source
	}{
		{"individual node", Facts{HavePolicy: true, Live: true, Authorized: true}, SourceLocal},
		{"offboarded or expired managed grant goes dormant (row 11)", with(live, func(f *Facts) { f.Live = false; f.HavePolicy = true }), SourceDormant},
		{"live grant without components.govern", with(live, func(f *Facts) { f.Authorized = false; f.HavePolicy = true }), SourceUnauthorized},
		{"current verified policy", with(live, func(f *Facts) { f.HavePolicy = true; f.HaveLastKnownGood = true; f.HaveInitial = true }), SourcePolicy},
		{"expired or unverifiable policy keeps the last known good (row 10)", with(live, func(f *Facts) { f.HaveLastKnownGood = true; f.HaveInitial = true }), SourceLastKnownGood},
		{"no policy ever: the grant's initial set (row 9)", with(live, func(f *Facts) { f.HaveInitial = true }), SourceInitialGrant},
		{"legacy enrolment, no policy: pinned", live, SourceLegacyPinned},
	}
	covered := map[Source]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := SelectSource(tc.f)
			if got != tc.want {
				t.Errorf("source = %s, want %s", got, tc.want)
			}
		})
		covered[tc.want] = true
	}
	for _, r := range sourceRules {
		if !covered[r.source] {
			t.Errorf("source rule %q has no test row", r.name)
		}
	}
}

func decision(t *testing.T, out Outcome, compiled, selected []string, id string) components.Decision {
	t.Helper()
	g := out.Governance
	res := components.Resolve(components.ResolveInput{Binary: components.BinaryNode, Compiled: compiled, Selected: selected, Governance: &g})
	d, ok := res.Decision(id)
	if !ok {
		t.Fatalf("no decision for %s", id)
	}
	return d
}

// TestGovernEndToEnd runs Govern's output through components.Resolve, the
// one resolver, for each source.
func TestGovernEndToEnd(t *testing.T) {
	all := components.InDistro(components.DistroFull)
	policy := mustSpec(t, `{"allow":["codeintel"],"deny":[{"selector":"remote","grantable":true}]}`)
	older := mustSpec(t, `{"allow":["remote"]}`)
	managed := Inputs{Managed: true, Live: true, Authorized: true, Compiled: all}

	t.Run("individual: local selection, ceiling still applies", func(t *testing.T) {
		in := Inputs{Current: &policy, CeilingPinned: true, Ceiling: []string{"codeintel"}}
		out := Govern(in)
		if out.Source != SourceLocal || out.Governance.Policy != nil {
			t.Fatalf("got %s policy=%v", out.Source, out.Governance.Policy)
		}
		if d := decision(t, out, all, []string{"remote"}, "remote"); d.Reason != components.ReasonAboveCeiling {
			t.Errorf("remote: %+v", d)
		}
	})
	t.Run("policy: unlisted inert, deny blocks, grant overrides a grantable deny", func(t *testing.T) {
		in := managed
		in.Current, in.LastKnownGood = &policy, &older
		out := Govern(in)
		if out.Source != SourcePolicy || out.Spec != &policy {
			t.Fatalf("source %s", out.Source)
		}
		if d := decision(t, out, all, []string{"terminals"}, "terminals"); d.Reason != components.ReasonPolicyUnlisted {
			t.Errorf("terminals: %+v", d)
		}
		if d := decision(t, out, all, []string{"remote"}, "remote"); d.Reason != components.ReasonPolicyDenied {
			t.Errorf("remote: %+v", d)
		}
		in.Granted = []string{"remote"}
		if d := decision(t, Govern(in), all, nil, "remote"); d.Reason != components.ReasonGranted {
			t.Errorf("granted remote: %+v", d)
		}
	})
	t.Run("last known good stays in force", func(t *testing.T) {
		in := managed
		in.LastKnownGood = &older
		out := Govern(in)
		if out.Source != SourceLastKnownGood || out.Governance.Policy.Source != string(SourceLastKnownGood) {
			t.Fatalf("source %s", out.Source)
		}
		if d := decision(t, out, all, []string{"remote"}, "remote"); d.State != components.StateActive {
			t.Errorf("remote: %+v", d)
		}
	})
	t.Run("initial grant set, no fail-open window", func(t *testing.T) {
		in := managed
		in.Initial = []string{"codeintel", "adapter.not-in-this-catalog"}
		out := Govern(in)
		if out.Source != SourceInitialGrant {
			t.Fatalf("source %s", out.Source)
		}
		if d := decision(t, out, all, []string{"codeintel", "remote"}, "codeintel"); d.State != components.StateActive {
			t.Errorf("codeintel: %+v", d)
		}
		if d := decision(t, out, all, []string{"codeintel", "remote"}, "remote"); d.Reason != components.ReasonPolicyUnlisted {
			t.Errorf("remote: %+v", d)
		}
	})
	t.Run("an empty initial set is core only", func(t *testing.T) {
		in := managed
		in.Initial = []string{}
		out := Govern(in)
		if d := decision(t, out, all, []string{"codeintel"}, "codeintel"); d.Reason != components.ReasonPolicyUnlisted {
			t.Errorf("codeintel: %+v", d)
		}
		if d := decision(t, out, all, nil, "core"); d.State != components.StateActive {
			t.Errorf("core: %+v", d)
		}
	})
	t.Run("legacy managed node: pinned to the recorded compiled set, no widening", func(t *testing.T) {
		in := managed
		in.Pinned = without(all, "codeintel") // the binary it ran before an update
		out := Govern(in)
		if out.Source != SourceLegacyPinned {
			t.Fatalf("source %s", out.Source)
		}
		if d := decision(t, out, all, []string{"codeintel"}, "codeintel"); d.Reason != components.ReasonPolicyUnlisted {
			t.Errorf("codeintel widened: %+v", d)
		}
		if d := decision(t, out, all, []string{"remote"}, "remote"); d.State != components.StateActive {
			t.Errorf("remote narrowed: %+v", d)
		}
	})
	t.Run("legacy managed node with no recorded set: nothing changes", func(t *testing.T) {
		out := Govern(managed)
		for _, d := range components.DefaultActive(components.DistroFull) {
			got := decision(t, out, all, components.DefaultActive(components.DistroFull), d)
			want := decision(t, Outcome{}, all, components.DefaultActive(components.DistroFull), d)
			if got.State != want.State {
				t.Errorf("%s: %s, ungoverned %s", d, got.State, want.State)
			}
		}
	})
	t.Run("dormant: the local selection, grants ignored", func(t *testing.T) {
		in := managed
		in.Live = false
		in.Current = &policy
		in.Granted = []string{"remote"}
		out := Govern(in)
		if out.Source != SourceDormant || out.Governance.Policy != nil || out.Governance.Granted != nil {
			t.Fatalf("got %+v", out)
		}
	})
}

// TestGovernPinnedSetsNeitherWidenNorNarrow is ledger MODPKG-7's design gap
// (T-677): the pinned, unauthorized and initial-grant sources are the org's
// set, so they constrain like a selection the operator cannot edit. A local
// selection that leaves a pinned component out must not make it inactive
// (no narrowing), and one that names a component outside the set must not
// activate it (no widening). Before the fix these sources were expressed as
// PolicySets.Allow, which only lifts the unlisted block, so a local removal
// still narrowed the node.
func TestGovernPinnedSetsNeitherWidenNorNarrow(t *testing.T) {
	all := components.InDistro(components.DistroFull)
	managed := Inputs{Managed: true, Live: true, Authorized: true, Compiled: all}
	cases := []struct {
		name string
		in   func() Inputs
		in1  string // in the org's set: must stay active whatever the local selection
		out1 string // outside the set: must stay inactive even when selected
	}{
		{"initial grant set", func() Inputs { in := managed; in.Initial = []string{"codeintel"}; return in }, "codeintel", "remote"},
		{"legacy pinned set", func() Inputs { in := managed; in.Pinned = without(all, "codeintel"); return in }, "remote", "codeintel"},
		{"unauthorized grant keeps the pinned set", func() Inputs {
			in := managed
			in.Authorized = false
			in.Pinned = without(all, "codeintel")
			return in
		}, "remote", "codeintel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Govern(tc.in())
			// The local selection removes everything but core.
			if d := decision(t, out, all, []string{"core"}, tc.in1); d.State != components.StateActive {
				t.Errorf("%s narrowed by the local selection: %+v", tc.in1, d)
			}
			// The local selection names a component outside the set.
			if d := decision(t, out, all, []string{"core", tc.out1}, tc.out1); d.State == components.StateActive {
				t.Errorf("%s widened by the local selection: %+v", tc.out1, d)
			}
		})
	}
}

func without(ids []string, drop string) []string {
	var out []string
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

func TestLegacyDefaults(t *testing.T) {
	d := LegacyDefaults()
	if d.Distro != components.DistroFull || d.DistroEpoch != 0 || !d.DigestFromBuild {
		t.Fatalf("legacy defaults %+v", d)
	}
}

func TestLegacyFullFallbackAllowed(t *testing.T) {
	full := components.InDistro(components.DistroFull)
	cases := []struct {
		name    string
		running components.Distro
		pinned  bool
		ceiling []string
		want    bool
	}{
		{"full, no ceiling", components.DistroFull, false, nil, true},
		{"full, ceiling permits full", components.DistroFull, true, full, true},
		{"full, narrower ceiling", components.DistroFull, true, []string{"codeintel"}, false},
		{"developer never takes the v1 full artifact", components.DistroDeveloper, false, nil, false},
		{"minimal never takes the v1 full artifact", components.DistroMinimal, false, nil, false},
	}
	for _, tc := range cases {
		if got := LegacyFullFallbackAllowed(tc.running, tc.pinned, tc.ceiling); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
