//go:build sbo_omit_codeintel

package main

import (
	"context"
	"database/sql"
	"io"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/codeintelapi"
	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// codeintel_omit.go stands in for codeintel.go, codeintel_surfaces.go and
// codeintel_provider.go in a build without the codeintel component (-tags
// sbo_omit_codeintel, lane MP-L1H, T-715): the engine, its parsers and the
// wazero tree-sitter runtime are not linked. `observer codeintel` and
// `observer index` answer "not in this build" (exit 3); the MCP symbol tools
// and the proxy's enrichment see the empty provider, and the retention prune
// of an older index still runs (internal/store/codeintelprune.go).
func init() { components.MarkOmitted("codeintel") }

// newCodeIntelCmd registers `observer codeintel` as a compiled-out verb.
func newCodeIntelCmd() *cobra.Command {
	return compiledOutVerb("codeintel", "Code intelligence (not in this build)", "codeintel")
}

// newIndexCmd registers `observer index` as a compiled-out verb.
func newIndexCmd() *cobra.Command {
	return compiledOutVerb("index", "Code intelligence index (not in this build)", "codeintel")
}

// runCodeIntelOnStart indexes nothing.
func runCodeIntelOnStart(context.Context, string, io.Writer) {}

// buildCodeIntelProvider returns the empty provider.
func buildCodeIntelProvider(cfg config.Config, _ *sql.DB, _ *slog.Logger) codeintelapi.Provider {
	if !cfg.CodeIntel.Enabled {
		return codeintelapi.Disabled()
	}
	return codeintelapi.Unavailable()
}

// codeIntelParserReplayable: no parser is replayable without the engine, so
// an archive rehydrate reports a re-index as required.
func codeIntelParserReplayable(string) bool { return false }

// aggressiveExtsFor collapses nothing: body-collapse needs the index's exact
// spans, which this build cannot produce.
func aggressiveExtsFor([]string) map[string]bool { return map[string]bool{} }
