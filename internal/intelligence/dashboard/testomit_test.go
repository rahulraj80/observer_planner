package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
)

// testomit_test.go is this package's test-side record of the components an
// sbo_omit_* tag removed from the test binary (D-535). This package's
// *_omit.go handler stubs do not call components.MarkOmitted (only the
// binary's main package does), so each testomit_<tag>_test.go file, built
// only under its tag, adds the component id here from init. A full build
// carries no such file, so no full-build test can take an omit branch.
var testOmitted = map[string]bool{}

// compiledOutInTest reports whether an omit tag removed component id from
// this test binary.
func compiledOutInTest(id string) bool { return testOmitted[id] }

// answeredCompiledOut reports whether a response is the compiled-out stub's
// 404 component_not_installed (reason not_compiled, writeCompiledOut) for a
// component this test binary really omits: the build refuses before acting.
func answeredCompiledOut(code int, body []byte) bool {
	if code != http.StatusNotFound {
		return false
	}
	var b componentstate.NotActiveBody
	if err := json.Unmarshal(body, &b); err != nil {
		return false
	}
	return b.Error == componentstate.NotActiveError && b.Reason == components.ReasonNotCompiled && testOmitted[b.Component]
}
