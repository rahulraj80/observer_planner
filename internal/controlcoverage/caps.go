package controlcoverage

import (
	"github.com/marmutapp/superbased-observer/internal/guard/compile"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// Per-capability coverage view (hook-parity audit item 0.7,
// docs/plans/hook-parity-lane0-brief.md step 6): for every hook capability,
// how far Observer reaches it on this tool today, and the first non-hook
// seam that can stand in for it (audit section 5.6 feature ladders). Read
// from the registry's hook bindings; additive to Row's existing fields.

// CoverageState is how far Observer reaches one capability on one tool.
type CoverageState string

const (
	// CoverageWired: an Observer feature acts on a binding of this
	// capability today.
	CoverageWired CoverageState = "wired"
	// CoverageRegistered: Observer registers the event but no feature acts
	// on it yet.
	CoverageRegistered CoverageState = "registered"
	// CoverageProbeRequired: the vendor documents the event but its wire
	// shape is unverified (`observer doctor --probe-hook` decides).
	CoverageProbeRequired CoverageState = "probe_required"
	// CoverageVendorOnly: the vendor documents the event; Observer does not
	// register it.
	CoverageVendorOnly CoverageState = "vendor_only"
	// CoverageNone: no binding of this capability at all.
	CoverageNone CoverageState = "none"
)

// Alternative (non-hook) seams, audit section 5.6.
const (
	AltSeamNone          = ""
	AltSeamProxy         = "proxy"
	AltSeamNativeCompile = "native_compile"
	AltSeamSandbox       = "sandbox"
	AltSeamWatcher       = "watcher"
	AltSeamCaptureLane   = "capture_lane"
)

// CapCoverage is one capability's coverage on one tool.
type CapCoverage struct {
	State CoverageState
	// AltSeam is the first available non-hook seam for the capability's
	// feature on this tool (AltSeamNone when the ladder has none).
	AltSeam string
}

// capStateRules is the ordered table deciding a capability's state from the
// tool's bindings: walked top-down, first match wins.
var capStateRules = []struct {
	state CoverageState
	match func(c integration.Capability, bs []integration.HookBinding, cp integration.HookCap) bool
}{
	{CoverageWired, func(_ integration.Capability, bs []integration.HookBinding, _ integration.HookCap) bool {
		return anyBinding(bs, func(b integration.HookBinding) bool { return b.Wired })
	}},
	{CoverageRegistered, func(_ integration.Capability, bs []integration.HookBinding, _ integration.HookCap) bool {
		return anyBinding(bs, func(b integration.HookBinding) bool { return b.Registered })
	}},
	{CoverageProbeRequired, func(c integration.Capability, bs []integration.HookBinding, cp integration.HookCap) bool {
		return len(bs) > 0 && cp == integration.HookCapPromptGate && c.PromptLane == integration.PromptLaneProbeRequired
	}},
	{CoverageVendorOnly, func(_ integration.Capability, bs []integration.HookBinding, _ integration.HookCap) bool {
		return len(bs) > 0
	}},
}

func anyBinding(bs []integration.HookBinding, f func(integration.HookBinding) bool) bool {
	for _, b := range bs {
		if f(b) {
			return true
		}
	}
	return false
}

// altSeamAvailable reports whether a non-hook seam exists for the tool.
var altSeamAvailable = map[string]func(c integration.Capability) bool{
	AltSeamProxy:         func(c integration.Capability) bool { return c.Proxy != nil },
	AltSeamNativeCompile: nativeCompileImplemented,
	AltSeamSandbox:       func(c integration.Capability) bool { return c.Handoff.Launchable() },
	// The watcher tails a transcript / DB; the browser-capture rows have
	// none (their turns arrive over native messaging).
	AltSeamWatcher: func(c integration.Capability) bool {
		return c.Hook.Mechanism != integration.HookBrowserExtension
	},
	// The post-hoc prompt capture lane runs on every capture path.
	AltSeamCaptureLane: func(integration.Capability) bool { return true },
}

// capAltSeamLadder is the section 5.6 ladder per capability: the non-hook
// seams, in preference order, that can carry the capability's feature.
var capAltSeamLadder = map[integration.HookCap][]string{
	integration.HookCapToolGate:            {AltSeamNativeCompile, AltSeamSandbox, AltSeamProxy, AltSeamWatcher},
	integration.HookCapToolRewrite:         {AltSeamProxy},
	integration.HookCapPromptGate:          {AltSeamProxy, AltSeamCaptureLane},
	integration.HookCapSessionLifecycle:    {AltSeamWatcher},
	integration.HookCapTurnLifecycle:       {AltSeamWatcher},
	integration.HookCapSubagentLifecycle:   {AltSeamWatcher},
	integration.HookCapCompactLifecycle:    {AltSeamProxy, AltSeamWatcher},
	integration.HookCapToolObserve:         {AltSeamWatcher},
	integration.HookCapToolFailureObserve:  {AltSeamWatcher},
	integration.HookCapModelCallObserve:    {AltSeamProxy, AltSeamWatcher},
	integration.HookCapPermissionObserve:   {AltSeamWatcher},
	integration.HookCapEnvironmentObserve:  {AltSeamWatcher},
	integration.HookCapNotificationObserve: {AltSeamWatcher},
	integration.HookCapTaskObserve:         {AltSeamWatcher},
}

// nativeCompileImplemented reports whether internal/guard/compile writes a
// native deny list for the tool (policy enforced even with the daemon down).
func nativeCompileImplemented(c integration.Capability) bool {
	for _, t := range compile.Targets() {
		if t.Implemented && t.Client == c.Tool {
			return true
		}
	}
	return false
}

// capsFor builds the per-capability coverage map for one registry row:
// every capability of the closed vocabulary gets an entry.
func capsFor(c integration.Capability) map[integration.HookCap]CapCoverage {
	out := make(map[integration.HookCap]CapCoverage, len(integration.HookCaps()))
	for _, cp := range integration.HookCaps() {
		var bs []integration.HookBinding
		for _, b := range c.Hook.Bindings {
			if b.Cap == cp {
				bs = append(bs, b)
			}
		}
		cov := CapCoverage{State: CoverageNone}
		for _, r := range capStateRules {
			if r.match(c, bs, cp) {
				cov.State = r.state
				break
			}
		}
		for _, seam := range capAltSeamLadder[cp] {
			if altSeamAvailable[seam](c) {
				cov.AltSeam = seam
				break
			}
		}
		out[cp] = cov
	}
	return out
}
