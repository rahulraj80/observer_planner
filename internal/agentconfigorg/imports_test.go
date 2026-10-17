package agentconfigorg

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the catalogue as pure data + lookups: vendor
// file I/O belongs to the boundary that calls internal/cfgfmt.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"os",
			"os/exec",
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "keep internal/agentconfigorg pure",
	})
}
