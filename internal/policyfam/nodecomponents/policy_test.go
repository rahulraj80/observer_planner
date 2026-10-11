package nodecomponents

import (
	"slices"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

func mustSpec(t *testing.T, raw string) PolicySpec {
	t.Helper()
	spec, _, err := CompileBody([]byte(raw), testCap)
	if err != nil {
		t.Fatalf("compile %s: %v", raw, err)
	}
	return spec
}

// TestSetsSpecificity is one row per selector precedence rule (plan 6.2:
// most specific match wins, deny beats allow; plan 6.2.1 conflict table:
// equal specificity, deny wins and the composer warns).
func TestSetsSpecificity(t *testing.T) {
	type in struct{ id, list string }
	cases := []struct {
		name string
		raw  string
		want []in
		warn string
	}{
		{
			name: "an exact allow beats a prefix deny",
			raw:  `{"allow":["adapter.codex"],"deny":[{"selector":"adapter.*"}]}`,
			want: []in{{"adapter.codex", "allow"}, {"adapter.cline", "deny"}},
		},
		{
			name: "an exact deny beats a class allow",
			raw:  `{"allow":["class:adapter"],"deny":[{"selector":"adapter.zed"}]}`,
			want: []in{{"adapter.zed", "deny"}, {"adapter.codex", "allow"}},
		},
		{
			name: "equal specificity: deny wins and the composer warns",
			raw:  `{"allow":["class:adapter"],"deny":[{"selector":"stability:stable"}]}`,
			warn: "equally specific; deny wins",
		},
		{
			name: "a longer prefix is more specific",
			raw:  `{"allow":["adapter.kilo-code"],"deny":[{"selector":"adapter.*"}]}`,
			want: []in{{"adapter.kilo-code", "allow"}},
		},
		{
			name: "require is reported as require",
			raw:  `{"require":["proxy.routing"]}`,
			want: []in{{"proxy.routing", "require"}, {"capture.proxy", ""}},
		},
		{
			name: "a grantable deny is grantable",
			raw:  `{"deny":[{"selector":"remote","grantable":true}]}`,
			want: []in{{"remote", "grantable"}},
		},
		{
			name: "a non-grantable deny at the winning specificity keeps it non-grantable",
			raw:  `{"deny":[{"selector":"class:feature","grantable":true},{"selector":"stability:stable"}]}`,
		},
		{
			name: "unnamed ids are in no list",
			raw:  `{"allow":["codeintel"]}`,
			want: []in{{"remote", ""}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := mustSpec(t, tc.raw)
			sets := spec.Sets("policy")
			list := func(id string) string {
				switch {
				case slices.Contains(sets.Allow, id):
					return "allow"
				case slices.Contains(sets.Require, id):
					return "require"
				case slices.Contains(sets.Grantable, id):
					return "grantable"
				case slices.Contains(sets.Deny, id):
					return "deny"
				}
				return ""
			}
			for _, w := range tc.want {
				if got := list(w.id); got != w.list {
					t.Errorf("%s: in %q, want %q", w.id, got, w.list)
				}
			}
			if tc.warn != "" && !strings.Contains(strings.Join(spec.Warnings, "\n"), tc.warn) {
				t.Errorf("warnings %v, want one containing %q", spec.Warnings, tc.warn)
			}
			// Each id is in at most one of Allow, Require, Deny, and
			// Grantable is a subset of Deny.
			seen := map[string]int{}
			for _, l := range [][]string{sets.Allow, sets.Require, sets.Deny} {
				for _, id := range l {
					seen[id]++
				}
			}
			for id, n := range seen {
				if n > 1 {
					t.Errorf("%s appears in %d lists", id, n)
				}
			}
			for _, id := range sets.Grantable {
				if !slices.Contains(sets.Deny, id) {
					t.Errorf("grantable %s not in deny", id)
				}
			}
		})
	}
}

// TestSetsMixedGrantableAtEqualSpecificity: two deny entries at the winning
// specificity, one not grantable, make the id non-grantable.
func TestSetsMixedGrantableAtEqualSpecificity(t *testing.T) {
	spec := mustSpec(t, `{"deny":[{"selector":"class:feature","grantable":true},{"selector":"stability:stable"}]}`)
	sets := spec.Sets("policy")
	for _, id := range sets.Grantable {
		c, _ := components.Get(id)
		if c.Class == components.ClassFeature && c.Stability == components.StabilityStable {
			t.Errorf("%s is grantable although a non-grantable deny matches it equally", id)
		}
	}
}
