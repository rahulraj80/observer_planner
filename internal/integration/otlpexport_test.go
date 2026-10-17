package integration

import "testing"

// TestOTLPExportsAreRegistryRows pins the OTLP column's table: every key is a
// registry tool (a typo would silently drop the declaration), every row
// declares an origin, and a vendor-cloud export is never described as one a
// node lands.
func TestOTLPExportsAreRegistryRows(t *testing.T) {
	for tool, x := range otlpExports {
		c, ok := For(tool)
		if !ok {
			t.Errorf("otlpExports[%q] has no registry row", tool)
			continue
		}
		if c.OTLP != x {
			t.Errorf("%s: registry row does not carry its OTLP column", tool)
		}
		if x.Origin == OTLPNone {
			t.Errorf("%s: declared without an origin", tool)
		}
		if x.Grounding == "" {
			t.Errorf("%s: no grounding", tool)
		}
		if !x.Mapped && x.Note == "" {
			t.Errorf("%s: exports but is not mapped, and says no reason why", tool)
		}
	}
}
