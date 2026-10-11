//go:build sbo_omit_adapter_cursor

package main

import (
	"context"
	"database/sql"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// cursorHookLogRoots has no roots without the adapter.
func cursorHookLogRoots() []string { return nil }

// backfillCursorHookUsage refuses: the cursor adapter is compiled out.
func backfillCursorHookUsage(context.Context, *sql.DB, []string, int) (CursorHookUsageBackfill, error) {
	return CursorHookUsageBackfill{}, adapterPassNotInBuild("--cursor-hook-usage", models.ToolCursor)
}

// backfillCursorUserPrompts refuses: the cursor adapter is compiled out.
func backfillCursorUserPrompts(context.Context, *sql.DB, string, int) (CursorUserPromptsBackfill, error) {
	return CursorUserPromptsBackfill{}, adapterPassNotInBuild("--cursor-user-prompts", models.ToolCursor)
}

// backfillCursorSubagents refuses: the cursor adapter is compiled out.
func backfillCursorSubagents(context.Context, *sql.DB, string, int) (CursorSubagentsBackfill, error) {
	return CursorSubagentsBackfill{}, adapterPassNotInBuild("--cursor-subagents", models.ToolCursor)
}

// backfillCursorTranscriptActions refuses: the cursor adapter is compiled
// out (--message-id skips it).
func backfillCursorTranscriptActions(context.Context, *sql.DB, string, int) (MessageIDBackfill, error) {
	return MessageIDBackfill{}, adapterPassNotInBuild("--message-id (cursor transcripts)", models.ToolCursor)
}
