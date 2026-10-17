//go:build sbo_omit_processobs

package main

import "github.com/marmutapp/superbased-observer/internal/components"

// components_build_noprocessobs.go reports process observation compiled
// out: a `-tags sbo_omit_processobs` build links processobs_omit.go instead
// of the capture backends, so `observer components` and GET /api/components
// say so.
func init() { components.MarkOmitted("processobs") }
