package emit

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins emit as the NEUTRAL sink contract (D-436):
// no internal/obs, no OpenTelemetry SDK and no internal/exporter, which belong
// to the export.otel component (emit/otelsink).
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"github.com/marmutapp/superbased-observer/internal/obs",
			"github.com/marmutapp/superbased-observer/internal/exporter",
			"go.opentelemetry.io",
		},
		Reason: "emit is the neutral sink contract; the OTel sink lives in emit/otelsink",
	})
}
