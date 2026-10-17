package cloudevidence

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins cloudevidence (plan 6 CI-P1): stdlib plus
// cloudcontract, scrub and dataauthority only, and no file/network/db/process
// surface.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"net",
			"os",
			"io/fs",
			"os/exec",
			"github.com/fsnotify/fsnotify",
		},
		Custom: func(_, p string) string {
			if strings.HasPrefix(p, "github.com/marmutapp/superbased-observer/") && !(p == "github.com/marmutapp/superbased-observer/internal/cloudcontract" || p == "github.com/marmutapp/superbased-observer/internal/scrub" || p == "github.com/marmutapp/superbased-observer/internal/dataauthority") {
				return "cloudevidence may import only cloudcontract, scrub, dataauthority"
			}
			return ""
		},
		Reason: "cloudevidence must stay pure",
	})
}
