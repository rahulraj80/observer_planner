//go:build sbo_omit_ui_export_xlsx

package main

import "github.com/marmutapp/superbased-observer/internal/components"

// components_build_noexportxlsx.go reports spreadsheet export compiled out:
// a `-tags sbo_omit_ui_export_xlsx` build links the dashboard's
// export_omit.go instead of export.go, so the excelize module leaves the
// binary. Without this mark the binary listed ui.export-xlsx as inert
// (distro_out) instead of omitted, and `sbo-components proof` refused the
// minimal edition, the one prebuilt distro that excludes it (found by the
// v1.36.0-rc.1 release build). It lives here, not beside export_omit.go,
// so the mark holds even in a build that compiles the dashboard out too.
func init() { components.MarkOmitted("ui.export-xlsx") }
