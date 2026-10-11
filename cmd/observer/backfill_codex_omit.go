//go:build sbo_omit_adapter_codex || sbo_omit_adapter_open_interpreter

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/scrub"
)

// backfillCodexForkDedup refuses: the codex adapter is compiled out.
func backfillCodexForkDedup(context.Context, *sql.DB, bool, int, io.Writer) (CodexForkDedupBackfill, error) {
	return CodexForkDedupBackfill{}, adapterPassNotInBuild("--codex-fork-dedup", models.ToolCodex)
}

// deriveCodexToolInputs refuses: the codex adapter is compiled out.
func deriveCodexToolInputs(context.Context, *scrub.Scrubber, string, string) (map[string]string, error) {
	return nil, errCodexAdapterNotInBuild
}

// scrubbedChanges has no renderer without the adapter; never reached, since
// deriveCodexToolInputs refuses first.
func scrubbedChanges(*scrub.Scrubber, json.RawMessage) string { return "" }
