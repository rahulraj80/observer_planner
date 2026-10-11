//go:build sbo_omit_ingest_otlp

package main

import (
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/diag"
	"github.com/marmutapp/superbased-observer/internal/proxyroute"
)

// otlp_proxyroute_omit.go is the off side of otlp_proxyroute.go: without
// OTLP ingest no export can double a proxied call, so the doctor row says
// so instead of consulting the vendor table.
func otlpTokenOwnerCheck(_ config.Config, _ string, _ []proxyroute.RouteStatus) diag.Check {
	return diag.Check{Name: "otlp.token_owner", Status: diag.StatusOK, Message: "OTLP ingest (ingest.otlp) is not in this build: no export can double a proxied call"}
}
