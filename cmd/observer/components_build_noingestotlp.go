//go:build sbo_omit_ingest_otlp

package main

import "github.com/marmutapp/superbased-observer/internal/components"

// components_build_noingestotlp.go reports vendor OTLP ingest compiled out:
// a `-tags sbo_omit_ingest_otlp` build links init_otlp_omit.go,
// otlp_proxyroute_omit.go and otlp_route_marker_omit.go instead of the
// vendor wiring, so internal/otlpvendor and internal/telemetrywire leave the
// binary (D-436). Without this file such a build still reported ingest.otlp
// as compiled, which the per-distro proof gate caught (lane MP-REL).
func init() { components.MarkOmitted("ingest.otlp") }
