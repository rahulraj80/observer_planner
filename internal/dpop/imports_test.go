package dpop

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImportsBounded pins doc3 2.1: internal/dpop is pure, never imports
// internal/agentid (the cycle guard), and its only module import is
// internal/dpop/jose.
func TestPackageImportsBounded(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/agentid",
		},
		Custom: func(_, p string) string {
			if strings.HasPrefix(p, "github.com/marmutapp/superbased-observer/") && p != "github.com/marmutapp/superbased-observer/internal/dpop/jose" {
				return "dpop may import only internal/dpop/jose"
			}
			return ""
		},
		Reason: "doc3 2.1",
	})
}
