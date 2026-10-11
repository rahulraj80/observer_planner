package integration

import (
	"sort"
	"testing"
)

// TestEnterpriseAnswersAreRegistryRows: every key of the Enterprise table is a
// registry row, every answer has exactly one shape, surface lists are sorted
// and duplicate-free, and inheritance points at a registry row that has
// surfaces of its own (depth one; vendorcoverage resolves it).
func TestEnterpriseAnswersAreRegistryRows(t *testing.T) {
	for tool, a := range enterpriseAnswers {
		if _, ok := registry[tool]; !ok {
			t.Errorf("enterpriseAnswers[%q] has no registry row", tool)
		}
		if !a.Answered() {
			t.Errorf("enterpriseAnswers[%q] is not None alone or surfaces and/or a parent", tool)
		}
		ids := make([]string, len(a.Surfaces))
		for i, s := range a.Surfaces {
			ids[i] = string(s)
		}
		if !sort.StringsAreSorted(ids) {
			t.Errorf("enterpriseAnswers[%q] surfaces are not sorted", tool)
		}
		for i := 1; i < len(ids); i++ {
			if ids[i] == ids[i-1] {
				t.Errorf("enterpriseAnswers[%q] lists %s twice", tool, ids[i])
			}
		}
		if a.InheritsFrom != "" {
			p, ok := enterpriseAnswers[a.InheritsFrom]
			if !ok || len(p.Surfaces) == 0 {
				t.Errorf("enterpriseAnswers[%q] inherits from %q, which has no surfaces", tool, a.InheritsFrom)
			}
		}
	}
}

// TestEnterpriseFoldedIntoRegistry: the init fold reaches For().
func TestEnterpriseFoldedIntoRegistry(t *testing.T) {
	c, ok := For("claude-code")
	if !ok || len(c.Enterprise.Surfaces) == 0 {
		t.Fatal("claude-code carries no Enterprise surfaces after the init fold")
	}
	if len(EnterpriseTools()) != len(enterpriseAnswers) {
		t.Fatal("EnterpriseTools does not list every answer")
	}
}
