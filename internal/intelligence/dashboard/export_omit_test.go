//go:build sbo_omit_ui_export_xlsx

package dashboard

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/componentstate"
)

// TestExportXLSXRefusesWhenCompiledOut: /api/export.xlsx answers 404
// component_not_installed naming ui.export-xlsx in a build without it.
func TestExportXLSXRefusesWhenCompiledOut(t *testing.T) {
	_, h := newManageServer(t)
	rec := sshProfilesReq(t, h, http.MethodGet, "/api/export.xlsx?days=30", "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/export.xlsx = %d, want 404: %s", rec.Code, rec.Body)
	}
	var body componentstate.NotActiveBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Component != "ui.export-xlsx" || body.Error != componentstate.NotActiveError {
		t.Errorf("body = %+v (%v)", body, err)
	}
}
