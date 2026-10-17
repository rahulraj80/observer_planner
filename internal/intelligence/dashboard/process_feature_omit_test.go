//go:build sbo_omit_processobs

package dashboard

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
)

// allowAllProcessCapture has nothing to set when process observation is
// compiled out.
func allowAllProcessCapture(*Server) {}

// TestProcessSetupRoutesRefuseWhenCompiledOut: with -tags
// sbo_omit_processobs the three process setup routes stay registered (the
// route tables are identical in every build) and answer 404
// component_not_installed, reason not_compiled, naming processobs.
func TestProcessSetupRoutesRefuseWhenCompiledOut(t *testing.T) {
	_, h := newManageServer(t)
	for _, rt := range []struct{ method, path string }{
		{http.MethodPost, "/api/process/enable-capture"},
		{http.MethodGet, "/api/process/etw/status"},
		{http.MethodPost, "/api/process/etw/register"},
	} {
		rec := sshProfilesReq(t, h, rt.method, rt.path, "", true)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404: %s", rt.method, rt.path, rec.Code, rec.Body)
		}
		var body componentstate.NotActiveBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode: %v", rt.path, err)
		}
		if body.Error != componentstate.NotActiveError || body.Component != "processobs" ||
			body.State != components.StateAbsent || body.Reason != components.ReasonNotCompiled {
			t.Errorf("%s body = %+v", rt.path, body)
		}
	}
}
