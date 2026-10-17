package nodecomponents

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/policyfam/nodefeatures"
	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

// TestCrossRailRows is one case per crossRailRows row (T-682) plus the
// clean case and the no-features case.
func TestCrossRailRows(t *testing.T) {
	compile := func(body string) PolicySpec {
		t.Helper()
		spec, _, err := CompileBody([]byte(body), 1<<20)
		if err != nil {
			t.Fatalf("compile %s: %v", body, err)
		}
		return spec
	}
	features := func(body string) *nodefeatures.PolicySpec {
		t.Helper()
		spec, _, err := nodefeatures.CompileBody([]byte(body), 1<<20)
		if err != nil {
			t.Fatalf("compile features %s: %v", body, err)
		}
		return &spec
	}
	for _, tc := range []struct {
		name      string
		policy    string
		features  string
		component string
		rule      string
	}{
		{
			name: "required component, feature disabled", policy: `{"require":["terminals"],"unlisted":"inert"}`,
			features: `{"terminals":{"enabled":false}}`, component: "terminals",
			rule: "required component, feature disabled by node.features",
		},
		{
			name: "feature enabled, component blocked", policy: `{"allow":["codeintel"],"unlisted":"inert"}`,
			features: `{"remote":{"enabled":true}}`, component: "remote",
			rule: "feature enabled by node.features, component blocked",
		},
		{
			name: "adapter allowed, launch disallowed", policy: `{"allow":["adapter.codex"],"unlisted":"inert"}`,
			features: `{"tools":{"disallow":["codex"]}}`, component: "adapter.codex",
			rule: "adapter allowed, launch disallowed by tools.disallow",
		},
		{
			name: "consistent rails: no finding", policy: `{"require":["terminals"],"unlisted":"inert"}`,
			features: `{"terminals":{"enabled":true}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CrossRail(compile(tc.policy), features(tc.features), nil)
			if tc.component == "" {
				if len(got) != 0 {
					t.Fatalf("findings = %+v, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0].Component != tc.component || got[0].Rule != tc.rule || got[0].Severity != "warning" || got[0].Message == "" {
				t.Fatalf("findings = %+v", got)
			}
			if HasError(got) {
				t.Fatalf("a warning row reported an error: %+v", got)
			}
		})
	}
	if got := CrossRail(compile(`{"require":["terminals"],"unlisted":"inert"}`), nil, nil); got != nil {
		t.Fatalf("no node.features policy: %+v", got)
	}
}

// TestCrossRailFeatureLockError is the error row (T-707): a node.governance
// feature.lock forcing a feature ON whose component the policy denies or
// leaves unlisted is an error; forcing it OFF, allowing the component, or a
// core feature is not.
func TestCrossRailFeatureLockError(t *testing.T) {
	compile := func(body string) PolicySpec {
		t.Helper()
		spec, _, err := CompileBody([]byte(body), 1<<20)
		if err != nil {
			t.Fatalf("compile %s: %v", body, err)
		}
		return spec
	}
	const rule = "feature.lock forces a feature on, component blocked"
	for _, tc := range []struct {
		name      string
		policy    string
		locks     map[string]bool
		component string
	}{
		{name: "locked on, denied", policy: `{"allow":["terminals"],"deny":[{"selector":"codeintel"}],"unlisted":"inert"}`, locks: map[string]bool{"codeintel": true}, component: "codeintel"},
		{name: "locked on, unlisted", policy: `{"allow":["terminals"],"unlisted":"inert"}`, locks: map[string]bool{"remote_access": true}, component: "remote"},
		{name: "locked on, allowed", policy: `{"allow":["capture.browser"],"unlisted":"inert"}`, locks: map[string]bool{"browser_capture": true}},
		{name: "locked on, required", policy: `{"require":["proxy.compression"],"unlisted":"inert"}`, locks: map[string]bool{"compression_conversation": true}},
		{name: "locked off, denied", policy: `{"deny":[{"selector":"codeintel"}],"unlisted":"inert"}`, locks: map[string]bool{"codeintel": false}},
		{name: "core feature", policy: `{"unlisted":"inert"}`, locks: map[string]bool{"secrets": true}},
		{name: "unknown feature id", policy: `{"unlisted":"inert"}`, locks: map[string]bool{"not_a_feature": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CrossRail(compile(tc.policy), nil, tc.locks)
			if tc.component == "" {
				if len(got) != 0 || HasError(got) {
					t.Fatalf("findings = %+v, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0].Component != tc.component || got[0].Rule != rule || got[0].Severity != SeverityError || got[0].Message == "" {
				t.Fatalf("findings = %+v", got)
			}
			if !HasError(got) {
				t.Fatal("HasError = false")
			}
		})
	}
}

// TestLockComponentsCoverTheFeatureLockVocabulary pins lockComponents to
// nodegov.Features: a new feature.lock id must be mapped (to a component, or
// to "" for core) before the lint can judge it, and every mapped component
// is a node catalog row.
func TestLockComponentsCoverTheFeatureLockVocabulary(t *testing.T) {
	for _, f := range nodegov.Features {
		comp, ok := LockComponent(f.ID)
		if !ok {
			t.Errorf("feature.lock id %q has no lockComponents row", f.ID)
			continue
		}
		if comp == "" {
			continue
		}
		row, known := components.Get(comp)
		if !known || row.Binary != components.BinaryNode {
			t.Errorf("feature.lock id %q maps to %q, which is not a node component", f.ID, comp)
		}
	}
	if len(lockComponents) != len(nodegov.Features) {
		t.Errorf("lockComponents has %d rows, nodegov.Features %d: remove the stale rows", len(lockComponents), len(nodegov.Features))
	}
}
