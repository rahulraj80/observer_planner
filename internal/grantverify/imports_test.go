package grantverify

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPureImports pins the package pure: the I/O (rows, key material) stays
// with the callers.
func TestPureImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Custom: func(_, p string) string {
			for _, bad := range []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "/internal/store", "/internal/orgclient"} {
				if p == bad || strings.HasSuffix(p, bad) {
					return "forbidden"
				}
			}
			return ""
		},
		Reason: "grantverify is pure",
	})
}
