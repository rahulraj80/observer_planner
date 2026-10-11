package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHealthDoctorCarriesRemediations pins T-421 item 3 on the wire: a check
// with a structured remediation reaches the Health page with the command the
// CLI text quotes and the in-app control (label + link). A fresh sandboxed
// home has no hook_checksums.json, so hooks.checksums tells the operator to
// run `observer init` and the dashboard links Connected tools.
func TestHealthDoctorCarriesRemediations(t *testing.T) {
	server, _ := wizardTestServer(t)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/health/doctor", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Checks []struct {
			Name         string `json:"name"`
			Message      string `json:"message"`
			Remediations []struct {
				Action  string `json:"action"`
				Command string `json:"command"`
				Label   string `json:"label"`
				Href    string `json:"href"`
			} `json:"remediations"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	for _, c := range got.Checks {
		if c.Name != "hooks.checksums" {
			continue
		}
		if len(c.Remediations) != 1 {
			t.Fatalf("hooks.checksums remediations = %+v", c.Remediations)
		}
		r := c.Remediations[0]
		if r.Action != "init" || r.Command != "observer init" || r.Label == "" || r.Href != "/settings?section=tools" {
			t.Errorf("remediation = %+v", r)
		}
		if !strings.Contains(c.Message, "`"+r.Command+"`") {
			t.Errorf("the CLI text %q does not quote the remediation's command %q", c.Message, r.Command)
		}
		return
	}
	t.Fatal("no hooks.checksums check in the report")
}
