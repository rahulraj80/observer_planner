//go:build sbo_omit_org_update

package main

import "github.com/marmutapp/superbased-observer/internal/components"

// components_build_noorgupdate.go reports self-update compiled out: a
// `-tags sbo_omit_org_update` build links update_omitted.go (cmd/observer)
// and internal/orgclient/update_omitted.go instead of the update rail, so
// internal/update leaves the binary (lane MP-GOV4, T-711).
func init() { components.MarkOmitted("org.update") }
