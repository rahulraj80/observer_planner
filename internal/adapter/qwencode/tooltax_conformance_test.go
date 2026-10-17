package qwencode

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
// The classifier is mapToolName (records.go), a normalized-key switch.
func classifyForTooltax(native string) string {
	return mapToolName(native)
}

// tooltaxTools lists the tool ids this adapter's classifier feeds.
var tooltaxTools = []string{models.ToolQwenCode}

// unclassifiedDomain freezes case labels that resolve to ActionUnknown, each
// labelled "deliberate: <why>" or "dead: <why>" (see conformtest.Domain).
var unclassifiedDomain = map[string]string{}

// missingTooltaxRows freezes case labels the classifier maps but tooltax has
// no tool-specific row for yet (see conformtest.Domain).
var missingTooltaxRows = map[string]string{
	// EMPTY, and it should stay that way. It held qwen-code's `memorize`
	// and `savememory` between the round that FOUND them (the
	// classifier-domain direction below) and the round that added the
	// matching internal/tooltax rows; the staleness pin at the bottom of
	// TestClassifierDomainIsSubsetOfTooltax went red the moment the rows
	// landed, which is what forced this freeze to be deleted rather than
	// quietly outliving the gap.
}

// TestPrivateMappingAgreesWithTooltax is the TABLE to CLASSIFIER direction.
func TestPrivateMappingAgreesWithTooltax(t *testing.T) {
	conformtest.AgreesWithTable(t, tooltaxTools, classifyForTooltax)
}

// TestClassifierDomainIsSubsetOfTooltax is the CLASSIFIER to TABLE direction
// over the case labels of mapToolName, extracted from source.
func TestClassifierDomainIsSubsetOfTooltax(t *testing.T) {
	conformtest.DomainIsSubset(t, conformtest.Domain{
		Dir:          ".",
		Func:         "mapToolName",
		Tools:        tooltaxTools,
		Classify:     classifyForTooltax,
		Unclassified: unclassifiedDomain,
		Missing:      missingTooltaxRows,
	})
}
