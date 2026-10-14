package cursor

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
// The classifier is cursorTranscriptActionType (adapter.go), a `switch strings.ToLower(name)`.
func classifyForTooltax(native string) string {
	return cursorTranscriptActionType(native)
}

// tooltaxTools lists the tool ids this adapter's classifier feeds.
var tooltaxTools = []string{models.ToolCursor}

// unclassifiedDomain freezes case labels that resolve to ActionUnknown, each
// labelled "deliberate: <why>" or "dead: <why>" (see conformtest.Domain).
var unclassifiedDomain = map[string]string{
	// DELIBERATE: `Await` is a control-flow primitive the agent uses to
	// wait on a long-running call. It carries no file/command target, so
	// the adapter keeps it unclassified rather than mis-bucketing it; the
	// identity survives on RawToolName. tooltax mirrors it as an explicit
	// ActionUnknown row.
	"await": "deliberate: control-flow primitive with no target",
}

// missingTooltaxRows freezes case labels the classifier maps but tooltax has
// no tool-specific row for yet (see conformtest.Domain).
var missingTooltaxRows = map[string]string{}

// TestPrivateMappingAgreesWithTooltax is the TABLE to CLASSIFIER direction.
func TestPrivateMappingAgreesWithTooltax(t *testing.T) {
	conformtest.AgreesWithTable(t, tooltaxTools, classifyForTooltax)
}

// TestClassifierDomainIsSubsetOfTooltax is the CLASSIFIER to TABLE direction
// over the case labels of cursorTranscriptActionType, extracted from source.
func TestClassifierDomainIsSubsetOfTooltax(t *testing.T) {
	conformtest.DomainIsSubset(t, conformtest.Domain{
		Dir:          ".",
		Func:         "cursorTranscriptActionType",
		Tools:        tooltaxTools,
		Classify:     classifyForTooltax,
		Unclassified: unclassifiedDomain,
		Missing:      missingTooltaxRows,
	})
}
