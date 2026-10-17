//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/intervention"
	"github.com/marmutapp/superbased-observer/internal/store"
)

var budgetLaunchInterventionCandidates = nodeInterventionCandidates

// budgetLaunchInventorySettle bounds how long a launch waits for the daemon's
// process controller to take inventory of an install it has not seen yet. The
// daemon rebuilds its install manifest every 30 seconds
// (nodeintervention.go), so a tool installed or updated moments before its
// launch (the dashboard's guided install, then Start) used to be refused for
// up to that long: on demo node 1 aider was installed at 01:08:06Z and its
// launch refused at 01:08:13Z (T-693). The wait ends as soon as a report with
// the launcher's own inventory fingerprint lands; it never relaxes the match.
var budgetLaunchInventorySettle = 35 * time.Second

// budgetLaunchInventoryPoll is how often the wait re-reads the report.
var budgetLaunchInventoryPoll = time.Second

// budgetLaunchSettleNotice tells the developer why the launch is pausing.
var budgetLaunchSettleNotice = func(tool string, wait time.Duration) {
	fmt.Fprintf(os.Stderr, "observer %s: waiting up to %s for the Observer daemon to take inventory of this install (installed or updated since its last pass)...\n", tool, wait.Round(time.Second))
}

// nodeProcessCutoffCoversLaunch admits a direct vendor process only when the
// live daemon attests process cutoff over the exact current install inventory
// and the launch resolves to that inventory's declared CLI surface. This is
// observed-process coverage; it does not claim request admission.
//
// COVERAGE IS PER SURFACE, NOT NODE-WIDE. The report's process_cutoff cell
// drops to "degraded" for a node-wide reason — one uninspectable same-UID
// process anywhere on the machine leaves the inventory incomplete — and gating
// on it made direct-launch recovery unreachable on any real workstation: every
// launch of every tool was refused because something unrelated could not be
// read. The question this function asks is narrower and answerable: is THIS
// surface bound, ready and controlled under the fingerprint the daemon is
// currently reconciling? ControlledSurfaces is exactly that per-surface
// witness, and the fingerprint check keeps it tied to this inventory.
func nodeProcessCutoffCoversLaunch(ctx context.Context, cfg config.Config, st *store.Store, tool string, evidence budgetLaunchEvidence) bool {
	covered, _ := nodeProcessCutoffCoverageSettling(ctx, cfg, st, tool, evidence, 0)
	return covered
}

// nodeProcessCutoffCoverageNow is the picker preflight's form: it reports
// the current answer and never waits.
func nodeProcessCutoffCoverageNow(ctx context.Context, cfg config.Config, st *store.Store, tool string, evidence budgetLaunchEvidence) (bool, string) {
	return nodeProcessCutoffCoverageSettling(ctx, cfg, st, tool, evidence, 0)
}

// nodeProcessCutoffCoverage is the launch gate's form: it waits up to
// budgetLaunchInventorySettle for the daemon's inventory to catch up with an
// install it has not seen yet.
func nodeProcessCutoffCoverage(ctx context.Context, cfg config.Config, st *store.Store, tool string, evidence budgetLaunchEvidence) (bool, string) {
	return nodeProcessCutoffCoverageSettling(ctx, cfg, st, tool, evidence, budgetLaunchInventorySettle)
}

// nodeProcessCutoffCoverageSettling is nodeProcessCutoffCoversLaunch with the
// reason a launch is NOT covered, so a refusal can say exactly which check
// failed (T-693) instead of only that none passed. The reason is a fixed
// phrase per check, never a path or an argument. settle is how long to wait
// for the daemon's inventory to match this launcher's; the picker's preflight
// passes zero and never waits.
func nodeProcessCutoffCoverageSettling(ctx context.Context, cfg config.Config, st *store.Store, tool string, evidence budgetLaunchEvidence, settle time.Duration) (bool, string) {
	if ctx == nil || st == nil || evidence.Executable == "" || evidence.Route == budgetLaunchRouteMaintenance {
		return false, "this launch named no installed executable"
	}
	surfaceID := tool + "/cli"
	candidates := budgetLaunchInterventionCandidates()
	manifest, err := intervention.BuildInstallManifest(ctx, candidates)
	if err != nil {
		return false, "this launcher could not take inventory of the installed tools"
	}
	want := intervention.InstallManifestFingerprint(manifest)
	// State "partial" is the live-reconciliation claim, and it is what holds
	// the report to the fast freshness bound (nodeInterventionStatusMaxAge).
	read := func() (nodeInterventionStatus, string) {
		status, err := readNodeInterventionStatus(ctx, st, cfg.Observer.DBPath)
		if err != nil {
			return status, "the daemon's process-control report could not be used (" + err.Error() + "); is `observer start` running?"
		}
		if status.State != "partial" || status.ManifestFingerprint == "" {
			return status, "the daemon's process controller is not reconciling (state " + status.State + ")"
		}
		return status, ""
	}
	status, why := read()
	if why != "" {
		return false, why
	}
	if status.ManifestFingerprint != want && settle > 0 {
		budgetLaunchSettleNotice(tool, settle)
		deadline := time.Now().Add(settle)
		for status.ManifestFingerprint != want && time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return false, "the launch was cancelled while waiting for the daemon's inventory"
			case <-time.After(budgetLaunchInventoryPoll):
			}
			if status, why = read(); why != "" {
				return false, why
			}
		}
	}
	if status.ManifestFingerprint != want {
		return false, "the installed tools this launcher sees differ from the daemon's last inventory (an install or update it has not taken in yet, or a different PATH)"
	}
	if !sortedStringContains(status.ControlledSurfaces, surfaceID) {
		return false, "the daemon does not list `" + surfaceID + "` under controlled surfaces"
	}
	if !resolvedLaunchCandidate(candidates, tool, surfaceID, evidence.Executable) {
		return false, "this launch does not start the installed binary the daemon bound"
	}
	surface, ok := manifest.Surface(surfaceID)
	if !ok || surface.Spec.Tool != tool || surface.State != intervention.BindingReady ||
		!surface.Spec.NativeUsage.Available() || !surfaceHasCurrentTarget(surface) {
		return false, "`" + surfaceID + "` is not bound and ready in this launcher's inventory"
	}
	// The child inherits this process's environment, so a vendor override set
	// here would run a binary the controller never bound.
	for _, name := range surface.Spec.Binding.LaunchOverrideEnv {
		if value, set := os.LookupEnv(name); set && strings.TrimSpace(value) != "" {
			return false, "this launch sets " + name + ", which makes the vendor launcher run a binary outside the layout the controller binds"
		}
	}
	leading, present := "", false
	if len(evidence.Arguments) > 0 {
		leading, present = evidence.Arguments[0], true
	}
	if intervention.ClassifyInvocation(surface.Spec.Binding.Invocation, leading, present) != intervention.InvocationCLI {
		return false, "this invocation's first argument is a declared non-billable form, which the controller does not stop"
	}
	return true, ""
}

func resolvedLaunchCandidate(candidates []intervention.InstalledCandidate, tool, surfaceID, executable string) bool {
	if !filepath.IsAbs(executable) {
		return false
	}
	launched, err := os.Stat(executable)
	if err != nil {
		return false
	}
	for _, candidate := range candidates {
		if candidate.Tool != tool || (candidate.SurfaceID != "" && candidate.SurfaceID != surfaceID) {
			continue
		}
		installed, err := os.Stat(candidate.LauncherPath)
		if err == nil && os.SameFile(launched, installed) {
			return true
		}
	}
	return false
}

func surfaceHasCurrentTarget(surface intervention.SurfaceManifest) bool {
	for _, target := range surface.Targets {
		if target.BoundProcess == nil {
			return true
		}
	}
	return false
}

func sortedStringContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
		if candidate > value {
			return false
		}
	}
	return false
}
