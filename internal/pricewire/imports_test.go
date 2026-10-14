package pricewire

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestImportsArePure pins the module boundary (CLAUDE.md "core logic lives in
// a pure package"): the projection is a field copy between two vocabularies
// with a closed allow-list of imports.
func TestImportsArePure(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		AllowOnly: true,
		Allowed: []string{
			"time",
			"github.com/marmutapp/superbased-observer/internal/intelligence/cost",
			"github.com/marmutapp/superbased-observer/internal/orgcontract",
			"github.com/marmutapp/superbased-observer/internal/reprice",
		},
		Reason: "pricewire is a pure projection",
	})
}
