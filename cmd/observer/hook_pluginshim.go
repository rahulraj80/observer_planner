package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/guardposture"
	"github.com/marmutapp/superbased-observer/internal/hook"
	"github.com/marmutapp/superbased-observer/internal/hook/pluginshim"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// Plugin-host receivers (hook-parity Lane D). The pluginshim TS shim and the
// Hermes Python plugin call `observer hook <tool> <event>` with a normalized
// envelope; the binding's dialect picks the handler. Keyed on the DIALECT
// (wire shape), never a tool.

// bindingDialectReceivers serves bindings whose dialect has its own handler
// on the binding-driven path (a tool with bindings and no hookReceivers
// entry, or a receiver that delegates by dialect, like Hermes).
var bindingDialectReceivers = map[string]func(ctx context.Context, tool, event, configPath string, b integration.HookBinding){
	hook.PluginDialectToolGate: handlePluginToolGateHook,
	hook.PromptDialectPluginShim: func(_ context.Context, tool, event, configPath string, b integration.HookBinding) {
		handlePromptSubmitOnlyHook(tool, tool, b.Dialect, b.NativeEvent, event, configPath)
	},
}

// dispatchBindingDialect runs the dialect handler for (tool, event) when its
// binding has one. ok=false leaves the caller's existing path in place.
func dispatchBindingDialect(ctx context.Context, tool, event, configPath string) bool {
	b, ok := resolveHookBinding(tool, event)
	if !ok {
		return false
	}
	fn, ok := bindingDialectReceivers[b.Dialect]
	if !ok {
		return false
	}
	fn(ctx, tool, event, configPath, b)
	return true
}

// pluginToolGateCanAsk reports whether the plugin maps an ask onto the
// host's own approval prompt: the binding must document ask AND the plugin
// must implement it (the shim's host row, or the Hermes plugin, whose
// pre_tool_call "approve" raises Hermes' approval gate). Dispatches on the
// mechanism, never the tool.
func pluginToolGateCanAsk(tool string, b integration.HookBinding) bool {
	if !b.HasDecision(integration.HookDecisionAsk) {
		return false
	}
	if h, ok := pluginshim.HostFor(tool); ok {
		return h.Ask
	}
	c, ok := integration.For(tool)
	return ok && c.Hook.Mechanism == integration.HookHermesPlugin
}

// handlePluginToolGateHook is the plugin dialect's tool-gate receiver: the
// same seam order as handleClaudeCodePreTool (config, truncation, parse,
// guard), every failure through the section 5.7 plan, and exactly one
// plugin reply.
func handlePluginToolGateHook(_ context.Context, tool, event, configPath string, b integration.HookBinding) {
	label := tool + ":" + event
	body, truncated := readHookBodyDetectTruncation(os.Stdin, promptSubmitBodyLimit)
	body = bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})
	fmt.Fprintf(os.Stderr, "observer-hook: event=%s received bytes=%d truncated=%v at=%s\n",
		label, len(body), truncated, time.Now().UTC().Format(time.RFC3339))

	cfg, cfgErr := config.Load(config.LoadOptions{GlobalPath: configPath})
	if cfgErr != nil && hookFailClosed(guardposture.KindConfig, body) {
		return
	}
	// A truncated or unparseable body cannot be evaluated: blocked under
	// enforcement (the plan), allowed otherwise.
	if (truncated || hookPayloadUnparseable(body)) && hookFailClosed(guardposture.KindUnparseable, body) {
		return
	}
	enforced := hookPlanFailsClosed()
	if cfgErr == nil && cfg.Guard.Enabled && cfg.Guard.Mode != "off" && !truncated {
		g := buildHookGuard(cfg, os.Stderr)
		if g == nil {
			if hookFailClosed(guardposture.KindGuardInit, body) {
				return
			}
		} else {
			blocked, recordAfterReply := hook.HandlePluginToolGate(
				tool, event, body, hook.PluginToolGateCaps(pluginToolGateCanAsk(tool, b)), enforced,
				g, makeGuardPersist(cfg, g, label, os.Stderr), hookStdout, os.Stderr,
			)
			markHookReplied()
			if !blocked && recordAfterReply != nil {
				recordAfterReply()
			}
			return
		}
	}
	hook.WritePluginReply(hookStdout, hook.PluginReply{Decision: "allow", Enforced: hook.EnforcedHint(enforced)})
	markHookReplied()
}
