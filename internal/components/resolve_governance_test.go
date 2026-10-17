package components

import (
	"strings"
	"testing"
)

// TestResolveGovernanceTable is one row per managed-policy row of
// resolveRules and per line of the plan 6.2.1 normative conflict table that
// the resolver (rather than the publish-time lint) decides.
func TestResolveGovernanceTable(t *testing.T) {
	type want struct {
		id      string
		state   State
		reason  Reason
		because string
	}
	policy := func(p PolicySets) *Governance { return &Governance{Policy: &p} }
	cases := []struct {
		name    string
		in      ResolveInput
		want    []want
		wantErr string
		noErr   bool
	}{
		{
			name: "required by policy but not compiled is required_missing (row 2)",
			in: ResolveInput{
				Binary: BinaryNode, Compiled: InDistro(DistroMinimal),
				Governance: policy(PolicySets{Require: []string{"terminals"}}),
			},
			want:    []want{{"terminals", StateRequiredMissing, ReasonPolicyRequired, ""}},
			wantErr: "terminals: required by the organization's component policy but not in this build",
		},
		{
			name: "a missing requirement of a required component is required_missing too",
			in: ResolveInput{
				Binary: BinaryNode, Compiled: without(InDistro(DistroFull), "terminals"),
				Governance: policy(PolicySets{Require: []string{"benchmark"}}),
			},
			want: []want{
				{"terminals", StateRequiredMissing, ReasonPolicyRequired, "benchmark"},
				{"benchmark", StateBlocked, ReasonRequiresNotCompiled, "terminals"},
			},
			wantErr: "benchmark: required by the organization's component policy but blocked (requires_not_compiled)",
		},
		{
			name: "outside the local ceiling is blocked",
			in: ResolveInput{
				Binary: BinaryNode, Selected: []string{"codeintel", "remote"},
				Governance: &Governance{CeilingPinned: true, Ceiling: []string{"codeintel"}},
			},
			want: []want{
				{"codeintel", StateActive, ReasonSelected, ""},
				{"remote", StateBlocked, ReasonAboveCeiling, ""},
				{"core", StateActive, ReasonCore, ""},
			},
			wantErr: "remote: outside this machine's local component ceiling",
		},
		{
			name: "an empty pinned ceiling leaves core only",
			in: ResolveInput{
				Binary: BinaryNode, Selected: []string{"codeintel"},
				Governance: &Governance{CeilingPinned: true},
			},
			want: []want{
				{"core", StateActive, ReasonCore, ""},
				{"codeintel", StateBlocked, ReasonAboveCeiling, ""},
			},
		},
		{
			name: "the ceiling beats a policy requirement and says so",
			in: ResolveInput{
				Binary:     BinaryNode,
				Governance: &Governance{CeilingPinned: true, Policy: &PolicySets{Require: []string{"remote"}}},
			},
			want:    []want{{"remote", StateBlocked, ReasonAboveCeiling, ""}},
			wantErr: "remote: required by the organization's component policy but blocked (above_ceiling)",
		},
		{
			name: "denied by policy is blocked (row 4)",
			in: ResolveInput{
				Binary: BinaryNode, Selected: []string{"remote"},
				Governance: policy(PolicySets{Allow: []string{"codeintel"}, Deny: []string{"remote"}}),
			},
			want:    []want{{"remote", StateBlocked, ReasonPolicyDenied, ""}},
			wantErr: "remote: denied by the organization's component policy",
		},
		{
			name: "required by policy is active without a local selection (row 5)",
			in: ResolveInput{
				Binary:     BinaryNode,
				Governance: policy(PolicySets{Require: []string{"proxy.routing"}}),
			},
			want: []want{
				{"proxy.routing", StateActive, ReasonPolicyRequired, ""},
				{"capture.proxy", StateActive, ReasonPolicyRequired, "proxy.routing"},
			},
			noErr: true,
		},
		{
			name: "allowed and selected is active (row 6)",
			in: ResolveInput{
				Binary: BinaryNode, Selected: []string{"codeintel"},
				Governance: policy(PolicySets{Allow: []string{"codeintel"}}),
			},
			want:  []want{{"codeintel", StateActive, ReasonSelected, ""}},
			noErr: true,
		},
		{
			name: "allowed and not selected is available (row 7)",
			in: ResolveInput{
				Binary:     BinaryNode,
				Governance: policy(PolicySets{Allow: []string{"codeintel"}}),
			},
			want:  []want{{"codeintel", StateAvailable, ReasonNotSelected, ""}},
			noErr: true,
		},
		{
			name: "not named by policy is inert, quietly (row 8, Q5)",
			in: ResolveInput{
				Binary: BinaryNode, Selected: []string{"codeintel", "remote"},
				Governance: policy(PolicySets{Allow: []string{"codeintel"}}),
			},
			want: []want{
				{"remote", StateBlocked, ReasonPolicyUnlisted, ""},
				{"codeintel", StateActive, ReasonSelected, ""},
			},
			noErr: true,
		},
		{
			name: "allowing a component allows what it requires",
			in: ResolveInput{
				Binary: BinaryNode, Selected: []string{"benchmark"},
				Governance: policy(PolicySets{Allow: []string{"benchmark"}}),
			},
			want: []want{
				{"benchmark", StateActive, ReasonSelected, ""},
				{"terminals", StateActive, ReasonRequiredBy, "benchmark"},
			},
			noErr: true,
		},
		{
			name: "an explicitly denied requirement blocks the allowed dependant",
			in: ResolveInput{
				Binary: BinaryNode, Selected: []string{"benchmark"},
				Governance: policy(PolicySets{Allow: []string{"benchmark"}, Deny: []string{"terminals"}}),
			},
			want: []want{
				{"terminals", StateBlocked, ReasonPolicyDenied, ""},
				{"benchmark", StateBlocked, ReasonDeniedDependency, "terminals"},
			},
		},
		{
			name: "a grant overrides a grantable deny entry",
			in: ResolveInput{Binary: BinaryNode, Governance: &Governance{
				Policy:  &PolicySets{Deny: []string{"remote"}, Grantable: []string{"remote"}},
				Granted: []string{"remote"},
			}},
			want:  []want{{"remote", StateActive, ReasonGranted, ""}},
			noErr: true,
		},
		{
			name: "a grant never overrides a non-grantable deny entry",
			in: ResolveInput{Binary: BinaryNode, Governance: &Governance{
				Policy:  &PolicySets{Deny: []string{"remote"}},
				Granted: []string{"remote"},
			}},
			want: []want{{"remote", StateBlocked, ReasonPolicyDenied, ""}},
		},
		{
			name: "a grant activates an unlisted component",
			in: ResolveInput{Binary: BinaryNode, Governance: &Governance{
				Policy: &PolicySets{}, Granted: []string{"codeintel"},
			}},
			want:  []want{{"codeintel", StateActive, ReasonGranted, ""}},
			noErr: true,
		},
		{
			name: "a grant never overrides the local ceiling",
			in: ResolveInput{Binary: BinaryNode, Governance: &Governance{
				CeilingPinned: true, Policy: &PolicySets{}, Granted: []string{"codeintel"},
			}},
			want: []want{{"codeintel", StateBlocked, ReasonAboveCeiling, ""}},
		},
		{
			name: "a grant never overrides a missing build",
			in: ResolveInput{Binary: BinaryNode, Compiled: InDistro(DistroMinimal), Governance: &Governance{
				Policy: &PolicySets{}, Granted: []string{"terminals"},
			}},
			want: []want{{"terminals", StateAbsent, ReasonNotCompiled, ""}},
		},
		{
			name: "core stays active under an empty policy",
			in:   ResolveInput{Binary: BinaryNode, Governance: policy(PolicySets{})},
			want: []want{{"core", StateActive, ReasonCore, ""}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Resolve(tc.in)
			for _, w := range tc.want {
				d, ok := res.Decision(w.id)
				if !ok {
					t.Fatalf("%s: no decision", w.id)
				}
				if d.State != w.state || d.Reason != w.reason || d.Because != w.because {
					t.Errorf("%s: got %s/%s/%q, want %s/%s/%q", w.id, d.State, d.Reason, d.Because, w.state, w.reason, w.because)
				}
			}
			errs := strings.Join(res.Errors, "\n")
			if tc.noErr && errs != "" {
				t.Errorf("unexpected errors:\n%s", errs)
			}
			if tc.wantErr != "" && !strings.Contains(errs, tc.wantErr) {
				t.Errorf("want an error containing %q, got:\n%s", tc.wantErr, errs)
			}
		})
	}
}

// TestResolveNilGovernanceIsUnchanged pins CLAUDE.md #6: a nil Governance
// (every caller before MP-GOVF) and an empty one produce exactly the
// ungoverned decisions, for every distro default.
func TestResolveNilGovernanceIsUnchanged(t *testing.T) {
	for _, d := range Distros() {
		base := ResolveInput{Binary: BinaryNode, Compiled: InDistro(d), Selected: DefaultActive(d)}
		want := Resolve(base)
		withEmpty := base
		withEmpty.Governance = &Governance{}
		got := Resolve(withEmpty)
		if len(got.Decisions) != len(want.Decisions) {
			t.Fatalf("%s: %d decisions, want %d", d, len(got.Decisions), len(want.Decisions))
		}
		for i := range want.Decisions {
			if got.Decisions[i] != want.Decisions[i] {
				t.Errorf("%s: %+v, want %+v", d, got.Decisions[i], want.Decisions[i])
			}
		}
		if strings.Join(got.Errors, "\n") != strings.Join(want.Errors, "\n") {
			t.Errorf("%s: errors differ", d)
		}
	}
}

// TestResolveGovernanceEveryStateHasARow fails when a State has no row in
// either resolver table, so a new state cannot land without a test.
func TestResolveGovernanceEveryStateHasARow(t *testing.T) {
	seen := map[State]bool{}
	for _, r := range resolveRules {
		seen[r.state] = true
	}
	for _, s := range []State{StateActive, StateAvailable, StateAbsent, StateBlocked, StateRequiredMissing} {
		if !seen[s] {
			t.Errorf("state %s has no rule", s)
		}
	}
}
