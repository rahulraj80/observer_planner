package policyfam

import (
	"strings"
	"testing"
)

// TestCompileFamilyBody_NodeGovernanceIgnoresUnknownSectionIDs pins the agent
// side of the vocabulary-skew fix: a body naming a page this build does not
// have compiles (that one id dropped, reported through IgnoredDirectives)
// instead of failing, so every other directive in it still applies.
func TestCompileFamilyBody_NodeGovernanceIgnoresUnknownSectionIDs(t *testing.T) {
	spec, _, err := CompileFamilyBody(FamilyNodeGovernance,
		[]byte(`{"schema":1,"sections":{"hidden":["cost","page-from-the-future"]}}`), 1<<16)
	if err != nil {
		t.Fatalf("CompileFamilyBody: %v", err)
	}
	if got := strings.Join(IgnoredDirectives(FamilyNodeGovernance, spec), ","); got != "sections.hidden:page-from-the-future" {
		t.Fatalf("IgnoredDirectives = %q", got)
	}
	// Every other family keeps its strict compile and reports nothing.
	adm, _, err := CompileFamilyBody(FamilyAdmissionInput, []byte(`{"mode":"observe"}`), 1<<16)
	if err != nil {
		t.Fatalf("admission: %v", err)
	}
	if got := IgnoredDirectives(FamilyAdmissionInput, adm); got != nil {
		t.Fatalf("admission IgnoredDirectives = %v, want nil", got)
	}
}

func TestIsServeTimeProjection(t *testing.T) {
	full := `{"schema":1,"sections":{"hidden":["agent-config","cost"]}}`
	cases := []struct {
		name, family, served, held string
		want                       bool
	}{
		{"node.governance projection", FamilyNodeGovernance, full, `{"schema":1,"sections":{"hidden":["cost"]}}`, true},
		{
			"node.governance schema projection (T-372)", FamilyNodeGovernance,
			`{"schema":3,"sections":{"hidden":["cost"],"visible":["security"]},"pinned":{"guard.enabled":true,"org_client.push_interval_seconds":300}}`,
			`{"schema":2,"sections":{"hidden":["cost"]},"pinned":{"guard.enabled":true}}`, true,
		},
		{"node.governance different policy", FamilyNodeGovernance, full, `{"schema":1,"sections":{"hidden":["routing"]}}`, false},
		{"node.governance identical body", FamilyNodeGovernance, full, full, false},
		{"undecodable held body", FamilyNodeGovernance, full, `{`, false},
		{"another family never projects", FamilyAdmissionInput, `{"mode":"observe"}`, `{"mode":"enforce"}`, false},
	}
	for _, tc := range cases {
		if got := IsServeTimeProjection(tc.family, []byte(tc.served), []byte(tc.held), 1<<16); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
