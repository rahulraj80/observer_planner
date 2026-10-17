//go:build sbo_omit_proxy_routing && sbo_omit_terminals && sbo_omit_ingest_otlp

package dashboard

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/componentstate"
)

// TestFeatureRoutesRefuseWhenCompiledOut: the routing-apply, tools-launch
// and telemetry-setup routes answer 404 component_not_installed naming
// their component when that component is compiled out.
func TestFeatureRoutesRefuseWhenCompiledOut(t *testing.T) {
	_, h := newManageServer(t)
	for _, rt := range []struct{ method, path, comp string }{
		{http.MethodPost, "/api/routing/apply", "proxy.routing"},
		{http.MethodPost, "/api/routing/apply/revert", "proxy.routing"},
		{http.MethodGet, "/api/routing/apply/ledger", "proxy.routing"},
		{http.MethodPost, "/api/tools/launch", "terminals"},
		{http.MethodPost, "/api/setup/telemetry", "ingest.otlp"},
	} {
		rec := sshProfilesReq(t, h, rt.method, rt.path, "", true)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404: %s", rt.method, rt.path, rec.Code, rec.Body)
		}
		var body componentstate.NotActiveBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Component != rt.comp || body.Error != componentstate.NotActiveError {
			t.Errorf("%s body = %+v (%v)", rt.path, body, err)
		}
	}
}
