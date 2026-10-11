//go:build !sbo_omit_adapter_cursor

package main

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/cursor"
)

// adapter.cursor's watcher wiring (see adapterWirers).
func init() {
	adapterWirers = append(adapterWirers, func(a adapter.Adapter, env adapterWireEnv) {
		st := env.st
		// Cursor: defer transcript-derived events when the live hook
		// has already captured the session (any row tagged
		// SourceFile="cursor:hook" implies beforeSubmitPrompt fired,
		// stop will follow with token + tool_use replay). The watcher
		// path is then a fallback for cold-start ingestion / pre-
		// install historical transcripts only.
		if ca, ok := a.(*cursor.Adapter); ok {
			ca.WithSessionHookChecker(func(ctx context.Context, sessionID string) (bool, error) {
				return st.SessionHasSourceFileRows(ctx, sessionID, "cursor:hook")
			})
			wireCursorReplaySeams(ca, st)
		}
	})
}
