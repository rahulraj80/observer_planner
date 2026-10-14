package cloudclient

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the dependency boundary (plan 6 CI-P2): stdlib
// plus cloudpop, cloudcred, cloudcontract and cloudevidence only.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/config",
			"github.com/marmutapp/superbased-observer/internal/db",
		},
		Custom: func(_, p string) string {
			if strings.HasPrefix(p, "github.com/marmutapp/superbased-observer/") && !(p == "github.com/marmutapp/superbased-observer/internal/cloudpop" || p == "github.com/marmutapp/superbased-observer/internal/cloudcred" || p == "github.com/marmutapp/superbased-observer/internal/cloudcontract" || p == "github.com/marmutapp/superbased-observer/internal/cloudevidence") {
				return "cloudclient allows only cloudpop/cloudcred/cloudcontract/cloudevidence"
			}
			return ""
		},
		Reason: "cloudclient dependency boundary",
	})
}
