//go:build !sbo_omit_ingest_otlp

package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/clientwire"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/telemetrywire"
)

// init_otlp.go is `observer init --otlp-export <tool,...>` (T-537, operator
// ruling D-343 "exporter wiring, opt-in per tool"): point each named tool's
// own OpenTelemetry exporter at this node's OTLP receiver. Strictly opt-in:
// never implied by --all, never run by `observer start`, every tool named
// explicitly. --dry-run previews the exact edit; --uninstall removes it (as
// `observer uninstall` also does). The write is clientwire.WireTelemetry,
// the same seam the dashboard's Telemetry export step calls.

// otlpExportTools lists the tools `--otlp-export` accepts, sorted.
func otlpExportTools() []string {
	var out []string
	for _, c := range integration.Capabilities() {
		if clientwire.TelemetrySupported(c.Tool) {
			out = append(out, c.Tool)
		}
	}
	sort.Strings(out)
	return out
}

// runOTLPExportInit applies (or, with uninstall, removes) the exporter
// wiring for each tool. It returns an error when any tool failed, after
// reporting every tool.
func runOTLPExportInit(out io.Writer, configPath string, tools []string, dryRun, uninstall bool) error {
	cfg, err := config.Load(config.LoadOptions{GlobalPath: configPath})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	op := telemetrywire.OpApply
	if uninstall {
		op = telemetrywire.OpRemove
	} else if !cfg.Ingest.OTel.Enabled || strings.TrimSpace(cfg.Ingest.OTel.HTTPAddr) == "" {
		return fmt.Errorf("--otlp-export: this node's OTLP receiver is off; set [ingest.otel] enabled = true (with http_addr) or turn it on in the dashboard Settings first, so the exporter has somewhere to send")
	}
	failed := false
	for _, raw := range tools {
		tool := strings.TrimSpace(raw)
		if tool == "" {
			continue
		}
		if !clientwire.TelemetrySupported(tool) {
			fmt.Fprintf(out, "%-12s otlp ✗ no exporter writer for this tool (supported: %s)\n", tool, strings.Join(otlpExportTools(), ", "))
			failed = true
			continue
		}
		res := clientwire.WireTelemetry(tool, op, clientwire.TelemetryOptions{
			Addr: cfg.Ingest.OTel.HTTPAddr, DryRun: dryRun,
			ReceiverRequiresToken: cfg.Ingest.OTel.AllowNonLoopback,
		})
		printTelemetryResult(out, res)
		if res.Error != nil || res.Conflict != "" {
			failed = true
		}
	}
	if failed {
		return fmt.Errorf("--otlp-export completed with errors; see above")
	}
	return nil
}
