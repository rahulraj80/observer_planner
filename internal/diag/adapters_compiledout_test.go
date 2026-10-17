package diag

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestCheckAdapterSaysNotInThisBuildForACompiledOutAdapter pins T-726: a
// registry tool whose adapter the binary was built without answers "not in
// this build" and names the builds that include it, never the lifecycle
// fallback's "active - no adapter". The omission record is swapped through
// the seam so the full test build exercises the omit branch.
func TestCheckAdapterSaysNotInThisBuildForACompiledOutAdapter(t *testing.T) {
	const tool = "t726-compiled-out"
	orig := omittedComponents
	t.Cleanup(func() { omittedComponents = orig })
	omittedComponents = func() []string { return []string{"adapter." + tool} }

	c, ok := CheckAdapter(tool, config.Config{})
	if !ok {
		t.Fatal("CheckAdapter ok = false for a compiled-out adapter")
	}
	if !strings.Contains(c.Message, "not in this build") {
		t.Errorf("message = %q, want it to say not in this build", c.Message)
	}
	if strings.Contains(c.Message, "no adapter") {
		t.Errorf("message = %q still carries the lifecycle fallback wording", c.Message)
	}
	if c.Status != StatusOK {
		t.Errorf("status = %v, want OK (an edition choice is not a broken capture path)", c.Status)
	}
	joined := strings.Join(c.Details, "\n")
	if !strings.Contains(joined, "adapter."+tool) {
		t.Errorf("details do not name the component: %s", joined)
	}

	// An adapter the binary does carry is untouched by the seam.
	omittedComponents = func() []string { return nil }
	if _, ok := CheckAdapter(tool, config.Config{}); ok {
		t.Error("an unknown, not-omitted id must still fall through (ok = false)")
	}
}
