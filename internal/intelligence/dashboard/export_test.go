//go:build !sbo_omit_ui_export_xlsx

package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// TestAPIExportXLSX_Sheets seeds minimal data and verifies the export
// endpoint returns a non-empty xlsx with the expected sheet names.
// Catches regressions where a sheet writer panics or excelize fails to
// render the workbook.
func TestAPIExportXLSX_Sheets(t *testing.T) {
	s, _ := newTestServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/export.xlsx?days=30", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("/api/export.xlsx: %d  body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "spreadsheetml") {
		t.Errorf("Content-Type: got %q want xlsx", got)
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition: got %q", cd)
	}
	body := rr.Body.Bytes()
	if len(body) < 1024 {
		t.Fatalf("xlsx body suspiciously small: %d bytes", len(body))
	}
	// xlsx is a zip - check the magic.
	if body[0] != 'P' || body[1] != 'K' {
		t.Errorf("body doesn't start with PK zip magic: %q", body[:8])
	}
	// Open the workbook from the response bytes and assert the expected
	// sheet names exist.
	f, err := excelize.OpenReader(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("excelize.OpenReader: %v", err)
	}
	defer f.Close()
	want := map[string]bool{
		"Overview": false, "Cost": false, "Sessions": false,
		"Actions": false, "Tools": false, "Compression": false,
		"Discovery - stale rereads": false, "Discovery - repeated commands": false,
		"Patterns": false,
	}
	for _, name := range f.GetSheetList() {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("missing sheet: %q", k)
		}
	}
}
