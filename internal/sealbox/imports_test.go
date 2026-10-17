package sealbox

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoImpureImports pins the package pure stdlib crypto: no SQL, HTTP,
// fsnotify, logging or third-party crypto (the internal/routing discipline).
func TestNoImpureImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenPrefixes: []string{
			"database/sql",
			"net/http",
			"fsnotify",
			"log/slog",
			"golang.org/x/crypto",
			"github.com/",
		},
		Reason: "sealbox must stay pure stdlib crypto",
	})
}
