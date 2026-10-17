package cloudcontract

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins cloudcontract to an EXACT CLOSED allow-list (plan 6
// CI-P1): the named stdlib set plus x/text/unicode/norm and internal/dataauthority.
// Adding a dependency here requires an explicit architecture decision.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		AllowOnly: true,
		Allowed: []string{
			"bytes",
			"crypto/sha256",
			"encoding/hex",
			"encoding/json",
			"fmt",
			"html",
			"net/url",
			"strings",
			"time",
			"unicode/utf8",
			"golang.org/x/text/unicode/norm",
			"github.com/marmutapp/superbased-observer/internal/dataauthority",
		},
		Reason: "cloudcontract closed allow-list",
	})
}
