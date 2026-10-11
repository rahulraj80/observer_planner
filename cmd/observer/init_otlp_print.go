package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/clientwire"
)

// init_otlp_print.go is core: `observer uninstall` prints exporter-wiring
// removals in every build, while the writer behind `observer init
// --otlp-export` (init_otlp.go) belongs to ingest.otlp.

// printTelemetryResult prints one tool's exporter-wiring outcome. It never
// prints a value from the config file (only Observer's own summary lines).
func printTelemetryResult(out io.Writer, res clientwire.TelemetryResult) {
	verb := ""
	if res.DryRun {
		verb = " (dry run)"
	}
	switch {
	case res.Error != nil:
		fmt.Fprintf(out, "%-12s otlp ✗ %v\n", res.Tool, res.Error)
	case res.Conflict != "":
		fmt.Fprintf(out, "%-12s otlp ✗ %s: %s\n", res.Tool, res.ConfigPath, res.Conflict)
	case !res.Changed:
		fmt.Fprintf(out, "%-12s otlp ✓ %s: %s\n", res.Tool, res.ConfigPath, strings.Join(res.Summary, "; "))
	default:
		fmt.Fprintf(out, "%-12s otlp ✓%s %s: %s\n", res.Tool, verb, res.ConfigPath, strings.Join(res.Summary, "; "))
	}
}
