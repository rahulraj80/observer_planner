//go:build !sbo_omit_adapter_claude_code

package main

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/claudecode"
)

// adapter.claude-code's watcher wiring (see adapterWirers).
func init() {
	adapterWirers = append(adapterWirers, func(a adapter.Adapter, env adapterWireEnv) {
		st := env.st
		// Claude-code: stamp per-turn effort.level from the hook-
		// captured claudecode_effort sidecar onto tool_use ToolEvents
		// at parse time. The Claude Code JSONL itself never carries
		// the user's effort dropdown selection; PreToolUse /
		// PostToolUse hooks do (`effort.level`), and observer's hook
		// handler upserts them into the sidecar. Joining at parse
		// time (rather than on every dashboard read) keeps the hot
		// query path unchanged.
		if cc, ok := a.(*claudecode.Adapter); ok {
			cc.WithEffortLookup(func(ctx context.Context, sessionID string) (map[string]string, error) {
				return st.LoadClaudecodeEffortMap(ctx, sessionID)
			})
		}
	})
}
