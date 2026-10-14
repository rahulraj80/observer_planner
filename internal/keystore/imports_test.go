package keystore

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins keystore: stdlib plus the keychain library
// github.com/zalando/go-keyring only, and no internal package.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"net",
			"github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{
			"github.com/marmutapp/superbased-observer/",
		},
		Custom: func(_, p string) string {
			if strings.Contains(p, ".") && p != "github.com/zalando/go-keyring" {
				return "third-party import; keystore allows only go-keyring"
			}
			return ""
		},
		Reason: "keystore purity",
	})
}
