package sidecar

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageIsStdlibOnly pins the sidecar reader stdlib-only (6.3): config.Load
// reads it on every hook call, so no store, HTTP, fsnotify or internal/config
// (also a cycle) may sit on that critical path.
func TestPackageIsStdlibOnly(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Custom: func(_, p string) string {
			if strings.Contains(p, ".") {
				return "internal/govern/sidecar must stay stdlib-only"
			}
			return ""
		},
	})
}
