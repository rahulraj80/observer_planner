package mcpegress

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the boundary: mcpegress is the egress client (net/http by nature) but
// never a store, never fsnotify, and never a consumer of the packages that consume it.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden:         []string{"database/sql", "github.com/fsnotify/fsnotify"},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/internal/mcpgw", "github.com/marmutapp/superbased-observer/internal/orgserver", "github.com/marmutapp/superbased-observer/internal/aigateway", "github.com/marmutapp/superbased-observer/internal/guard", "github.com/marmutapp/superbased-observer/internal/store"},
		Reason:            "mcpegress must not depend on its consumers",
	})
}
