package costtext

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins costtext to a closed stdlib text/JSON allow-list
// (CLAUDE.md #1): no I/O beyond a caller-supplied io.Writer, no internal package.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		AllowOnly: true,
		Allowed: []string{
			"bytes",
			"encoding/json",
			"io",
			"regexp",
			"strconv",
			"strings",
		},
		Reason: "costtext stays a pure text package",
	})
}
