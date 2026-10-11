//go:build sbo_omit_ui_export_xlsx

package dashboard

import "net/http"

// export_omit.go is the off side of export.go: in a `-tags
// sbo_omit_ui_export_xlsx` build (component ui.export-xlsx compiled out)
// the route stays registered and answers 404 component_not_installed,
// reason not_compiled, and this package does not import the excelize
// module (modular packaging plan 5.1, lane MP-L1D).

// handleExportXLSX refuses: ui.export-xlsx is not in this build.
func (s *Server) handleExportXLSX(w http.ResponseWriter, _ *http.Request) {
	writeCompiledOut(w, "ui.export-xlsx")
}
