//go:build sbo_distro_managed

package main

import "github.com/marmutapp/superbased-observer/internal/components"

// components_distro_managed.go stamps a `managed` distro build (phase P3, lane
// MP-REL): scripts/build-distro.sh passes sbo_distro_managed with the distro's
// omit tags (sbo-components tags --distro managed). The stamp makes an
// unconfigured node start from the managed default set and keeps every
// component the distro excludes inert and unselectable, including one whose
// code this release still carries (reported as distro_out). A plain build
// carries no stamp and is the full product, unchanged.
func init() { components.SetBuildDistro(components.Distro("managed")) }
