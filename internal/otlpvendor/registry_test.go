package otlpvendor

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestTableMatchesRegistry pins the two owners against each other: the
// mapping table here and the registry's OTLP column (internal/integration).
// Every mapping row is a registry tool declaring Mapped, and every Mapped
// registry tool has exactly one mapping row. A team-grain row must be a
// vendor-cloud export, and a user-grain one a local export.
func TestTableMatchesRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range Vendors() {
		if seen[v.Tool] {
			t.Errorf("two mapping rows for %s", v.Tool)
		}
		seen[v.Tool] = true
		c, ok := integration.For(v.Tool)
		if !ok {
			t.Errorf("mapping row %s is not a registry tool", v.Tool)
			continue
		}
		if !c.OTLP.Mapped {
			t.Errorf("%s: mapped here but the registry OTLP column says not mapped", v.Tool)
		}
		wantOrigin := integration.OTLPLocal
		if v.Grain == GrainTeam {
			wantOrigin = integration.OTLPVendorCloud
		}
		if c.OTLP.Origin != wantOrigin {
			t.Errorf("%s: grain %s but registry origin %q", v.Tool, v.Grain, c.OTLP.Origin)
		}
		if v.SourceTag == "" || v.Evidence == "" || len(v.Rules) == 0 {
			t.Errorf("%s: incomplete row (source tag, evidence, rules)", v.Tool)
		}
	}
	for _, tool := range integration.OTLPExportTools() {
		c, _ := integration.For(tool)
		if c.OTLP.Mapped && !seen[tool] {
			t.Errorf("registry says %s is mapped but no mapping row exists", tool)
		}
	}
}

// TestActionTypesAreKnown keeps every raw-tool-name mapping inside the
// closed action vocabulary.
func TestActionTypesAreKnown(t *testing.T) {
	known := map[string]bool{
		models.ActionReadFile: true, models.ActionWriteFile: true, models.ActionEditFile: true,
		models.ActionRunCommand: true, models.ActionSearchText: true, models.ActionSearchFiles: true,
		models.ActionWebSearch: true, models.ActionWebFetch: true, models.ActionMCPCall: true,
	}
	for _, v := range Vendors() {
		for raw, a := range v.ActionTypes {
			if !known[a] {
				t.Errorf("%s: %q maps to unknown action type %q", v.Tool, raw, a)
			}
		}
	}
}

// TestServiceNamesAreUnique: one service.name claims one vendor.
func TestServiceNamesAreUnique(t *testing.T) {
	owner := map[string]string{}
	for _, v := range Vendors() {
		for _, n := range v.ServiceNames {
			if prev, dup := owner[n]; dup {
				t.Errorf("service.name %q claimed by %s and %s", n, prev, v.Tool)
			}
			owner[n] = v.Tool
		}
	}
}
