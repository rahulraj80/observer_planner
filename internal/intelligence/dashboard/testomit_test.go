package dashboard

import (
	"encoding/json"
	"net/http"
	"testing"

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

// skipWithoutPolicyLever skips a test of the routing shadow card's policy
// lever: with proxy.routing compiled out there is no template map
// (routing_omit.go routingTemplateKnown), so the card offers no policy
// lever at all (the minimal distro).
func skipWithoutPolicyLever(t *testing.T) {
	t.Helper()
	if compiledOutInTest("proxy.routing") {
		t.Skip("proxy.routing is compiled out: the routing shadow card has no policy lever")
	}
}

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
