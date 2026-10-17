package integration

import (
	"sort"

	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// EnterpriseAnswer is a registry row's answer to "what can the vendor's
// enterprise plane give us" (enterprise vendor API plan section 4.2, invariant
// I2). It is either None, or Surfaces and/or InheritsFrom:
//   - Surfaces: the vendor surface catalog rows that cover this tool (ids
//     resolve in internal/vendorsurface/catalog);
//   - InheritsFrom: the parent row whose surfaces also cover this child
//     product (kiro-crew by kiro-cli, grokbot by cursor: Appendix F marks
//     their classes NA); the union is resolved in one place,
//     internal/vendorcoverage;
//   - None: nothing exists, with the date checked and the sources searched.
//
// The relation is the ONLY authored tool to surface relation; every reverse
// index is derived by internal/vendorcoverage, and no other package may hold
// its own copy (TestEnterpriseRelationOnlyInRegistry).
type EnterpriseAnswer struct {
	Surfaces     []vendorsurface.SurfaceID
	InheritsFrom string
	None         *EnterpriseNone
}

// EnterpriseNone records an honest "no vendor enterprise surface" answer.
type EnterpriseNone struct {
	Checked         string
	SourcesSearched []string
}

// Answered reports whether the answer is well formed: None alone, or at
// least one of Surfaces and InheritsFrom.
func (a EnterpriseAnswer) Answered() bool {
	some := len(a.Surfaces) > 0 || a.InheritsFrom != ""
	return some != (a.None != nil)
}

// init folds the Enterprise column into the registry rows (the otlpExports
// precedent). A key with no registry row is a table error caught by
// TestEnterpriseAnswersAreRegistryRows.
func init() {
	for tool, a := range enterpriseAnswers {
		if c, ok := registry[tool]; ok {
			c.Enterprise = a
			registry[tool] = c
		}
	}
}

// EnterpriseTools returns, sorted, every tool with an authored Enterprise
// answer.
func EnterpriseTools() []string {
	out := make([]string, 0, len(enterpriseAnswers))
	for t := range enterpriseAnswers {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
