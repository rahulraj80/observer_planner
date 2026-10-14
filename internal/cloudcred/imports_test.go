package cloudcred

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins cloudcred (plan 6 CI-P2): stdlib plus the keychain
// library github.com/zalando/go-keyring, and exactly one internal package:
// internal/keystore, the shared hardened credential backend it delegates to.
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
		Except: []string{"github.com/marmutapp/superbased-observer/internal/keystore"},
		Custom: func(_, p string) string {
			if strings.Contains(p, ".") && p != "github.com/zalando/go-keyring" &&
				p != "github.com/marmutapp/superbased-observer/internal/keystore" {
				return "third-party import; cloudcred allows only go-keyring"
			}
			return ""
		},
		Reason: "cloudcred purity",
	})
}
