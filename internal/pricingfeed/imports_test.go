package pricingfeed

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/pricingfeed as the PURE shared contract
// (CLAUDE.md section 1, the plan's Wave 0 gate): the cache, bookkeeping and the
// opt-in fetch lane live behind store and egress seams.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"os/exec",
		},
		Reason: "internal/pricingfeed must stay pure",
	})
}
