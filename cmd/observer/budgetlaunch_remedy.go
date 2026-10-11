package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// budgetLaunchCause classifies WHY a managed hard budget refuses a launch on
// coverage (T-685). The refusal used to name only the cause, which left the
// developer at a dead end; each cause now carries the reason the launcher has
// always printed AND what the developer can do about it, from one table.
type budgetLaunchCause uint8

const (
	// budgetLaunchCovered: an enforcement point exists for this invocation.
	budgetLaunchCovered budgetLaunchCause = iota
	// budgetLaunchCauseGuardNotReady: the node's Guard is off or observing,
	// so nothing on this node can enforce the org's hard cap.
	budgetLaunchCauseGuardNotReady
	// budgetLaunchCauseNoRoute: the registry declares no Observer proxy route
	// for this tool and no process cutoff covers it.
	budgetLaunchCauseNoRoute
	// budgetLaunchCauseRouteUnprovable: the tool has a proxy route, but the
	// registry says applying it never proves the invocation's final backend
	// (the tool's own settings can send requests around the proxy).
	budgetLaunchCauseRouteUnprovable
	// budgetLaunchCauseRouteUnverified: the route is provable, but this
	// invocation did not carry it (a direct launch, an operator selector
	// flag, or a route the launcher could not put in place).
	budgetLaunchCauseRouteUnverified
)

// budgetLaunchAdvice is one cause's reason (the clause after "because") and
// remedy (the next step). Remedy is rendered by a func so a row can use the
// capability's own launcher verb; it never switches on a tool name.
type budgetLaunchAdvice struct {
	cause  budgetLaunchCause
	reason string
	// remedy receives the capability and the rendered process-cutoff clause
	// (budgetLaunchCutoffGapClause): every coverage refusal says why the
	// node's process cutoff did not cover the launch, because that cutoff is
	// the path that admits a tool whose model route is absent, direct or
	// unprovable.
	remedy func(c integration.Capability, cutoff string) string
}

// budgetLaunchSoftRemedy is the one remedy a developer cannot apply alone: the
// budget's enforcement is an org setting.
const budgetLaunchSoftRemedy = "ask your org admin to set this budget's enforcement to soft (org dashboard, Budgets) for the time you need this tool"

// budgetLaunchAdviceTable is walked top-down by cause (CLAUDE.md #5).
var budgetLaunchAdviceTable = []budgetLaunchAdvice{
	{
		cause:  budgetLaunchCauseGuardNotReady,
		reason: "the configured Guard is disabled or is not in enforce mode",
		remedy: func(integration.Capability, string) string {
			return "turn the Guard on in enforce mode ([guard] enabled = true and mode = \"enforce\" in ~/.observer/config.toml), or ask your org admin"
		},
	},
	{
		cause:  budgetLaunchCauseNoRoute,
		reason: "this tool has no verified Observer budget-admission route",
		remedy: func(_ integration.Capability, cutoff string) string {
			return "this tool's model requests cannot pass through the Observer proxy, so the only way a hard budget can cover it is the node's process cutoff, and " +
				cutoff + "; otherwise " +
				budgetLaunchSoftRemedy + ", or use a tool Observer routes (`observer adapters` lists them in the PROXY column)"
		},
	},
	{
		cause:  budgetLaunchCauseRouteUnprovable,
		reason: "this invocation's final model route is direct or cannot be verified",
		remedy: func(_ integration.Capability, cutoff string) string {
			return "Observer can route this tool, but its own settings can still send a request around the proxy, so no launch of it can be verified; the node's process cutoff is the other way a hard budget can cover it, and " +
				cutoff + "; otherwise " + budgetLaunchSoftRemedy
		},
	},
	{
		cause:  budgetLaunchCauseRouteUnverified,
		reason: "this invocation's final model route is direct or cannot be verified",
		remedy: func(c integration.Capability, cutoff string) string {
			fallback := "; without the route, the node's process cutoff is the other way a hard budget can cover it, and " + cutoff
			if c.Proxy != nil && strings.TrimSpace(c.Proxy.Launcher) != "" {
				return fmt.Sprintf("start it with `%s` (a terminal, or the dashboard New Terminal), which puts the proxy route in place, and leave out provider, base-URL and config flags; if the launcher says the route cannot be set up, follow the setup line it prints, or %s%s",
					strings.TrimSpace(c.Proxy.Launcher), budgetLaunchSoftRemedy, fallback)
			}
			return "run `observer init` to write this tool's proxy route, then start it again; if the route cannot be set up, " + budgetLaunchSoftRemedy + fallback
		},
	},
}

// budgetLaunchCutoffGap names why the node's process cutoff (B-602) does not
// cover a launch of a tool with no proxy route. It is derived ONLY from the
// tool's registry intervention declaration (integration.InterventionFor), so
// the refusal states the same fact `observer guard status` reports, and no
// tool name is ever compared here.
type budgetLaunchCutoffGap uint8

const (
	// budgetLaunchGapNoProcessSurface: the registry declares no dedicated
	// process surface for this tool, so there is no process to stop.
	budgetLaunchGapNoProcessSurface budgetLaunchCutoffGap = iota
	// budgetLaunchGapNoUsageSource: a process could be stopped, but the
	// surface has no grounded local usage source, so the tool's own spend
	// could never count against the cap.
	budgetLaunchGapNoUsageSource
	// budgetLaunchGapNoBindableForm: the surface declares no installed
	// process shape the controller can bind exactly.
	budgetLaunchGapNoBindableForm
	// budgetLaunchGapNotActive: the declaration supports a cutoff, but the
	// running daemon does not report this surface controlled for this launch.
	budgetLaunchGapNotActive
)

// budgetLaunchCutoffGapRow is one gap's explanation. The clause is a function
// of the surface so a row can quote the surface id and the registry's own
// note; it never switches on a tool name.
type budgetLaunchCutoffGapRow struct {
	gap    budgetLaunchCutoffGap
	clause func(surface integration.InterventionSurfaceSpec) string
}

// budgetLaunchCutoffGapTable is walked top-down by gap (CLAUDE.md #5).
var budgetLaunchCutoffGapTable = []budgetLaunchCutoffGapRow{
	{budgetLaunchGapNoProcessSurface, func(integration.InterventionSurfaceSpec) string {
		return "Observer has no process of this tool on this node that a cutoff could stop"
	}},
	{budgetLaunchGapNoUsageSource, func(surface integration.InterventionSurfaceSpec) string {
		return withSurfaceNote("the process cutoff cannot cover it: Observer has no grounded local usage source for `"+surface.ID+
			"`, so this tool's spend could never count against the cap and stopping it would not be tied to the budget", surface)
	}},
	{budgetLaunchGapNoBindableForm, func(surface integration.InterventionSurfaceSpec) string {
		return withSurfaceNote("the process cutoff cannot cover it: Observer declares no installed process form of `"+surface.ID+
			"` it can bind exactly", surface)
	}},
	{budgetLaunchGapNotActive, func(surface integration.InterventionSurfaceSpec) string {
		return withSurfaceNote("the process cutoff does not cover this launch right now: the Observer daemon must be running with the Guard in enforce mode and must list `"+
			surface.ID+"` under controlled surfaces (`observer guard status`, Node control line; if the daemon is running and the surface is missing there, this install's launcher could not be bound), and the launch must start the installed binary the daemon bound", surface)
	}},
}

// withSurfaceNote appends the registry's own explanation for the surface when
// it declares one (an unsupported launcher form, or why usage is missing).
func withSurfaceNote(clause string, surface integration.InterventionSurfaceSpec) string {
	note := strings.TrimSpace(surface.Binding.UnsupportedReason)
	if note == "" {
		note = strings.TrimSpace(surface.Note)
	}
	if note == "" {
		return clause
	}
	return clause + " (" + note + ")"
}

// budgetLaunchCutoffGapFor classifies tool's dedicated process surface. A tool
// with several dedicated surfaces is judged by its first, the one the
// launcher starts; shared-host and remote surfaces never reach here (they are
// covered as unbindable before a refusal is considered).
func budgetLaunchCutoffGapFor(tool string) (budgetLaunchCutoffGap, integration.InterventionSurfaceSpec) {
	surfaces, _ := integration.InterventionFor(tool)
	for _, surface := range surfaces {
		if surface.Class != integration.SurfaceDedicatedProcess {
			continue
		}
		binding := surface.Binding
		switch {
		case !surface.NativeUsage.Available():
			return budgetLaunchGapNoUsageSource, surface
		case surface.Binary == nil || (!binding.AllowNative && len(binding.Interpreters) == 0 && len(binding.Workers) == 0):
			return budgetLaunchGapNoBindableForm, surface
		default:
			return budgetLaunchGapNotActive, surface
		}
	}
	return budgetLaunchGapNoProcessSurface, integration.InterventionSurfaceSpec{}
}

// budgetLaunchCutoffGapClause renders the gap for tool from the table. why is
// the live check that failed for this launch (nodeProcessCutoffCoverage); it
// is quoted only where the declaration allows a cutoff, because for the other
// gaps the declaration is already the whole answer.
func budgetLaunchCutoffGapClause(tool, why string) string {
	gap, surface := budgetLaunchCutoffGapFor(tool)
	clause := budgetLaunchCutoffGapTable[0].clause(surface)
	for _, row := range budgetLaunchCutoffGapTable {
		if row.gap == gap {
			clause = row.clause(surface)
			break
		}
	}
	if why = strings.TrimSpace(why); gap == budgetLaunchGapNotActive && why != "" {
		clause += "; for this launch: " + why
	}
	return clause
}

// budgetLaunchAdviceFor returns the table row for cause. ok is false for
// budgetLaunchCovered (nothing to say) and for an unknown cause.
func budgetLaunchAdviceFor(cause budgetLaunchCause) (budgetLaunchAdvice, bool) {
	for _, row := range budgetLaunchAdviceTable {
		if row.cause == cause {
			return row, true
		}
	}
	return budgetLaunchAdvice{}, false
}

// budgetLaunchCoverageCause answers the coverage question of
// enforceBudgetControlledLaunch as a cause. Every branch dispatches on a
// registry capability SHAPE (proven route, budget-admission channel, declared
// surface class), never on the tool's name.
func budgetLaunchCoverageCause(covers func() bool, tool string, evidence budgetLaunchEvidence, proxyURL string) budgetLaunchCause {
	capability, _ := integration.For(tool)
	if capability.RouteProven() && evidence.Route == budgetLaunchRouteObserverProxy &&
		urlRoutesToProxy(evidence.ProxyURL, proxyURL) {
		return budgetLaunchCovered
	}
	if covers != nil && covers() {
		return budgetLaunchCovered
	}
	if budgetLaunchSurfaceUnbindable(tool, evidence) {
		// The registry says this surface has no process boundary a cutoff
		// could ever bind to. Refusing it at $0 would report a coverage gap
		// the org's posture already reports honestly (proxy_only / partial),
		// while blocking work that is under the cap. The spend gate still
		// applies.
		return budgetLaunchCovered
	}
	switch {
	case capability.BudgetAdmissionChannel() != integration.BudgetAdmissionObserverProxy:
		return budgetLaunchCauseNoRoute
	case !capability.RouteProven():
		return budgetLaunchCauseRouteUnprovable
	default:
		return budgetLaunchCauseRouteUnverified
	}
}

// budgetLaunchPreflight is the New Terminal picker's dry run of the launch
// boundary's coverage question (T-685). It reports a refusal ONLY when the
// answer does not depend on the route a launch would carry: the Guard is not
// ready, the tool has no proxy route, or its route can never be proven, and
// no process cutoff covers the resolved binary. A provable route is decided
// at launch, so it is never predicted here. Any read failure says nothing:
// the launcher itself still decides, and a broken preflight must never block.
func budgetLaunchPreflight(ctx context.Context, configPath, tool, bin string) (*dashapi.PreflightBudgetLaunch, bool) {
	cfg, database, cleanup, err := loadConfigAndDB(ctx, configPath)
	if err != nil {
		return nil, false
	}
	defer cleanup()
	state, err := resolveManagedBudgetLaunchState(ctx, cfg, database)
	if err != nil || !state.Required {
		return nil, false
	}
	capability, _ := integration.For(tool)
	cause := budgetLaunchCauseGuardNotReady
	why := ""
	if state.GuardReady {
		st := store.New(database)
		evidence := budgetLaunchEvidence{Route: budgetLaunchRouteUnknown, Executable: bin}
		cause = budgetLaunchCoverageCause(func() bool {
			var covered bool
			covered, why = nodeProcessCutoffCoverageNow(ctx, cfg, st, tool, evidence)
			return covered
		}, tool, evidence, resolveProxyURL(cfg.Proxy.Port, ""))
		if cause == budgetLaunchCauseRouteUnverified {
			return nil, false
		}
	}
	advice, ok := budgetLaunchAdviceFor(cause)
	if !ok {
		return nil, false
	}
	return &dashapi.PreflightBudgetLaunch{
		Reason: "The org's hard budget will stop this launch: " + advice.reason + ".",
		Remedy: budgetLaunchRemedySentence(advice.remedy(capability, budgetLaunchCutoffGapClause(tool, why))),
	}, true
}

// budgetLaunchRemedySentence capitalises a remedy clause into a sentence.
func budgetLaunchRemedySentence(clause string) string {
	clause = strings.TrimSpace(clause)
	if clause == "" {
		return ""
	}
	return "What to do: " + clause + "."
}
