//go:build !sbo_omit_codeintel

package main

import (
	"database/sql"
	"log/slog"

	"github.com/marmutapp/superbased-observer/internal/codeintel"
	"github.com/marmutapp/superbased-observer/internal/codeintel/parse"
	"github.com/marmutapp/superbased-observer/internal/codeintelapi"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// codeIntelParserReplayable is the archive rehydrate's parser check
// (codeintel.ParserReplayable), named here so archive_cmd.go does not import
// the engine (lane MP-L1H, T-715).
var codeIntelParserReplayable = codeintel.ParserReplayable

// buildCodeIntelProvider returns the code-intelligence provider for the
// proxy + MCP layers. The NATIVE in-process engine is the only
// implementation (docs/codeintel/architecture.md). The engine is returned
// even when its index is still empty: it self-heals as `observer start`
// indexes projects, flipping Available() true mid-run without a restart.
// Never returns nil - an unavailable provider degrades enrichment
// gracefully. With [codeintel].enabled = false (the default since D-307)
// it returns codeintel.Disabled(): every consumer (marker enrichment,
// body-collapse, the MCP symbol tools) sees an unavailable index and does
// nothing, and the MCP tools can say the module is off by choice.
func buildCodeIntelProvider(cfg config.Config, database *sql.DB, logger *slog.Logger) codeintelapi.Provider {
	if !cfg.CodeIntel.Enabled {
		return codeintel.Disabled()
	}
	if database == nil {
		return codeintel.Unavailable()
	}
	native := codeintel.NewEngine(store.New(database))
	if native.Available() {
		logger.Info("codeintel: native engine attached (structural enrichment, traversal, collapse)")
	} else {
		logger.Info("codeintel: native engine attached; index empty - enrichment activates as projects are indexed")
	}
	return native
}

// aggressiveExtsFor expands the [codeintel.compression].aggressive_languages
// list into the concrete set of file extensions the proxy's collapse hook
// checks. Unknown language names contribute nothing (no extensions).
func aggressiveExtsFor(langs []string) map[string]bool {
	out := map[string]bool{}
	for _, l := range langs {
		for _, ext := range parse.Extensions(codeintel.Language(l)) {
			out[ext] = true
		}
	}
	return out
}
