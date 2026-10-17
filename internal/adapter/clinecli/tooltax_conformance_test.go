package clinecli

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/tooltax/conformtest"
)

// classifyForTooltax runs the adapter's REAL private classifier over one
// native tool name. It is the single place the WP-T3 conformance pins below
// reach into the shipping code (the shared algorithm lives in
// internal/tooltax/conformtest).
//
// The classifier is normalizeToolName (normalize.go), an exact-name switch behind an `mcp_` prefix rule (the prefix rule is not a finite domain and is therefore outside this pin: tooltax's `mcp__*` glob owns that class).
func classifyForTooltax(native string) string {
	return normalizeToolName(native)
}

// tooltaxTools lists the tool ids this adapter's classifier feeds.
var tooltaxTools = []string{models.ToolClineCLI}

// unclassifiedDomain freezes case labels that resolve to ActionUnknown, each
// labelled "deliberate: <why>" or "dead: <why>" (see conformtest.Domain).
var unclassifiedDomain = map[string]string{}

// missingTooltaxRows freezes case labels the classifier maps but tooltax has
// no tool-specific row for yet (see conformtest.Domain).
var missingTooltaxRows = map[string]string{}

// TestPrivateMappingAgreesWithTooltax is the TABLE to CLASSIFIER direction.
func TestPrivateMappingAgreesWithTooltax(t *testing.T) {
	conformtest.AgreesWithTable(t, tooltaxTools, classifyForTooltax)
}

// TestClassifierDomainIsSubsetOfTooltax is the CLASSIFIER to TABLE direction
// over the case labels of normalizeToolName, extracted from source.
func TestClassifierDomainIsSubsetOfTooltax(t *testing.T) {
	conformtest.DomainIsSubset(t, conformtest.Domain{
		Dir:          ".",
		Func:         "normalizeToolName",
		Tools:        tooltaxTools,
		Classify:     classifyForTooltax,
		Unclassified: unclassifiedDomain,
		Missing:      missingTooltaxRows,
	})
}
