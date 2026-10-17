package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/proxyroute"
)

// TestBudgetLaunchAdviceTableCoversEveryRefusalCause pins T-685: every cause
// the launch boundary can refuse on carries a reason AND a remedy, so the
// refusal is never a dead end.
func TestBudgetLaunchAdviceTableCoversEveryRefusalCause(t *testing.T) {
	for _, cause := range []budgetLaunchCause{
		budgetLaunchCauseGuardNotReady,
		budgetLaunchCauseNoRoute,
		budgetLaunchCauseRouteUnprovable,
		budgetLaunchCauseRouteUnverified,
	} {
		advice, ok := budgetLaunchAdviceFor(cause)
		if !ok {
			t.Fatalf("cause %d has no advice row", cause)
		}
		if strings.TrimSpace(advice.reason) == "" || advice.remedy == nil ||
			strings.TrimSpace(advice.remedy(integration.Capability{}, "the cutoff clause")) == "" {
			t.Fatalf("cause %d advice is incomplete: %+v", cause, advice)
		}
	}
	if _, ok := budgetLaunchAdviceFor(budgetLaunchCovered); ok {
		t.Fatal("a covered launch must have nothing to advise")
	}
}

// TestBudgetLaunchCoverageCauseByCapabilityShape walks one tool per registry
// shape through the cause classifier: no proxy route, an unprovable route, a
// provable route the invocation did not carry, and the same route carried.
func TestBudgetLaunchCoverageCauseByCapabilityShape(t *testing.T) {
	const proxyURL = "http://127.0.0.1:8820"
	never := func() bool { return false }
	unknown := budgetLaunchEvidence{Route: budgetLaunchRouteUnknown}
	cases := []struct {
		name     string
		tool     string
		evidence budgetLaunchEvidence
		covers   func() bool
		want     budgetLaunchCause
	}{
		{"no proxy route", "muse", unknown, never, budgetLaunchCauseNoRoute},
		{"route never provable", "aider", unknown, never, budgetLaunchCauseRouteUnprovable},
		{"provable route not carried", "opencode", unknown, never, budgetLaunchCauseRouteUnverified},
		{"provable route carried", "opencode", budgetLaunchEvidence{Route: budgetLaunchRouteObserverProxy, ProxyURL: proxyURL}, never, budgetLaunchCovered},
		{"process cutoff covers", "muse", unknown, func() bool { return true }, budgetLaunchCovered},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := budgetLaunchCoverageCause(tc.covers, tc.tool, tc.evidence, proxyURL); got != tc.want {
				t.Fatalf("cause = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestBudgetLaunchRefusalSaysWhatToDo pins the rendered refusal: the old
// reason survives verbatim (scripts and docs quote it), the sentinel survives,
// and a "What to do:" sentence follows, using the tool's own launcher verb
// when the registry declares one.
func TestBudgetLaunchRefusalSaysWhatToDo(t *testing.T) {
	cases := []struct {
		tool  string
		cause budgetLaunchCause
		want  []string
	}{
		{"muse", budgetLaunchCauseNoRoute, []string{"no verified Observer budget-admission route", "What to do:", "soft", "org admin"}},
		{"aider", budgetLaunchCauseRouteUnprovable, []string{"direct or cannot be verified", "What to do:", "soft"}},
		{"opencode", budgetLaunchCauseRouteUnverified, []string{"direct or cannot be verified", "What to do:", "`observer opencode`"}},
		{"opencode", budgetLaunchCauseGuardNotReady, []string{"not in enforce mode", "What to do:", `mode = "enforce"`}},
	}
	for _, tc := range cases {
		err := budgetLaunchRefusal(tc.tool, tc.cause)
		if !errors.Is(err, errBudgetLaunchUncontrolled) {
			t.Fatalf("%s: refusal lost sentinel: %v", tc.tool, err)
		}
		for _, want := range tc.want {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s cause %d: refusal %q is missing %q", tc.tool, tc.cause, err.Error(), want)
			}
		}
		if strings.ContainsAny(err.Error(), string([]rune{0x2013, 0x2014})) {
			t.Fatalf("refusal carries a dash: %q", err.Error())
		}
	}
}

// TestBudgetLaunchPreflightPredictsOnlyRouteIndependentRefusals runs the
// picker's dry run against a managed hard budget fixture: a tool with no
// proxy route is predicted refused with a remedy, a provable-route tool is
// left to the launcher (its route is decided at launch), and an observing
// Guard predicts the Guard refusal for any tool. A node with no managed
// enrolment predicts nothing.
func TestBudgetLaunchPreflightPredictsOnlyRouteIndependentRefusals(t *testing.T) {
	ctx := context.Background()
	cfgPath, _ := writeManagedBudgetLaunchFixture(t, true, "enforce")
	got, ok := budgetLaunchPreflight(ctx, cfgPath, "muse", "/nonexistent/muse")
	if !ok || got == nil || !strings.Contains(got.Reason, "no verified Observer budget-admission route") ||
		!strings.HasPrefix(got.Remedy, "What to do: ") {
		t.Fatalf("muse preflight = %+v ok=%v, want a no-route refusal with a remedy", got, ok)
	}
	if got, ok := budgetLaunchPreflight(ctx, cfgPath, "opencode", "/nonexistent/opencode"); ok || got != nil {
		t.Fatalf("opencode preflight = %+v, want nothing (route decided at launch)", got)
	}
	observing, _ := writeManagedBudgetLaunchFixture(t, true, "observe")
	if got, ok := budgetLaunchPreflight(ctx, observing, "opencode", "/nonexistent/opencode"); !ok || !strings.Contains(got.Reason, "not in enforce mode") {
		t.Fatalf("observing-guard preflight = %+v ok=%v, want the Guard refusal", got, ok)
	}
	// An unenrolled node: its own temp config and DB, never the real home.
	dir := t.TempDir()
	unmanaged := filepath.Join(dir, "config.toml")
	body := fmt.Sprintf("[observer]\ndb_path = %q\n[guard]\nenabled = true\nmode = \"enforce\"\n", filepath.Join(dir, "observer.db"))
	if err := os.WriteFile(unmanaged, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := budgetLaunchPreflight(ctx, unmanaged, "muse", "/nonexistent/muse"); ok || got != nil {
		t.Fatalf("unmanaged preflight = %+v, want nothing", got)
	}
}

// TestRouteSetupPreflightOnlyForMissingConfigLaneArtifact pins the picker's
// route-setup line: only a config-lane tool (registry ProxyProbe) whose own
// config file is absent gets it.
func TestRouteSetupPreflightOnlyForMissingConfigLaneArtifact(t *testing.T) {
	orig := routeSetupInspect
	t.Cleanup(func() { routeSetupInspect = orig })
	set := func(present bool, state proxyroute.RouteState) {
		routeSetupInspect = func() []proxyroute.RouteStatus {
			return []proxyroute.RouteStatus{{Tool: "crush", State: state, ArtifactPresent: present}}
		}
	}
	set(false, proxyroute.RouteAbsent)
	if got := routeSetupPreflight("crush"); got != routeSetupMissingConfig {
		t.Fatalf("missing crush.json: got %q", got)
	}
	set(true, proxyroute.RouteAbsent)
	if got := routeSetupPreflight("crush"); got != "" {
		t.Fatalf("present crush.json without a route: got %q, want nothing", got)
	}
	set(true, proxyroute.RouteOurs)
	if got := routeSetupPreflight("crush"); got != "" {
		t.Fatalf("routed crush: got %q", got)
	}
	set(false, proxyroute.RouteAbsent)
	if got := routeSetupPreflight("opencode"); got != "" {
		t.Fatalf("launcher-routed tool: got %q", got)
	}
}

// TestCrushNoConfigNoticeIsActionable pins the launcher line T-685 replaced:
// it names the file and the next step, and no longer points at an init flag
// that does nothing for a missing crush.json.
func TestCrushNoConfigNoticeIsActionable(t *testing.T) {
	got := crushNoConfigNotice("/home/dev/.config/crush/crush.json")
	for _, want := range []string{"/home/dev/.config/crush/crush.json", "providers.openai", "observer crush", "never reads or writes the key"} {
		if !strings.Contains(got, want) {
			t.Fatalf("notice %q is missing %q", got, want)
		}
	}
	if strings.Contains(got, "--skip-hooks") {
		t.Fatalf("notice still points at the old flag: %q", got)
	}
}

// TestBudgetLaunchCutoffGapByDeclaration pins T-693: a no-route refusal says
// exactly why the node's process cutoff does not cover the tool, read from
// the registry's intervention declaration, one row per declaration shape.
func TestBudgetLaunchCutoffGapByDeclaration(t *testing.T) {
	cases := []struct {
		tool string
		want budgetLaunchCutoffGap
		text []string
	}{
		// Bindable with a grounded usage source: covered once the daemon
		// reports the surface controlled, so the refusal names that check.
		{"junie", budgetLaunchGapNotActive, []string{"does not cover this launch right now", "`junie/cli` under controlled surfaces", "observer guard status"}},
		{"muse", budgetLaunchGapNotActive, []string{"`muse/cli` under controlled surfaces"}},
		// T-706: the standalone pool CLI's trajectory store is grounded on
		// Linux (~/.local/state/poolside), so poolside/cli now has a usage
		// source and is covered once the daemon lists it.
		{"poolside", budgetLaunchGapNotActive, []string{"`poolside/cli` under controlled surfaces"}},
		// Bindable but no grounded usage source: the cutoff could never be
		// tied to the cap, and the registry's note says why.
		{"freebuff", budgetLaunchGapNoUsageSource, []string{"`freebuff/cli`"}},
		// No installed process form can be bound.
		{"hermes", budgetLaunchGapNoBindableForm, []string{"no installed process form of `hermes/cli`", "Bourne launcher"}},
		// The five tools refused on node 1 (D-445, D-453) and the other rows
		// found there in the same state now declare a bindable layout.
		{"crush", budgetLaunchGapNotActive, []string{"`crush/cli` under controlled surfaces"}},
		{"cline-cli", budgetLaunchGapNotActive, []string{"`cline-cli/cli`"}},
		{"aider", budgetLaunchGapNotActive, []string{"`aider/cli`"}},
		{"grok", budgetLaunchGapNotActive, []string{"`grok/cli`"}},
		{"prime-agent", budgetLaunchGapNotActive, []string{"`prime-agent/cli`"}},
		// No dedicated process at all.
		{"claude-web", budgetLaunchGapNoProcessSurface, []string{"no process of this tool"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			if got, _ := budgetLaunchCutoffGapFor(tc.tool); got != tc.want {
				t.Fatalf("gap = %d, want %d", got, tc.want)
			}
			clause := budgetLaunchCutoffGapClause(tc.tool, "")
			for _, want := range tc.text {
				if !strings.Contains(clause, want) {
					t.Errorf("clause %q is missing %q", clause, want)
				}
			}
			if strings.ContainsAny(clause, string([]rune{0x2013, 0x2014})) {
				t.Fatalf("clause carries a dash: %q", clause)
			}
		})
	}
	for _, row := range budgetLaunchCutoffGapTable {
		if strings.TrimSpace(row.clause(integration.InterventionSurfaceSpec{ID: "x/cli"})) == "" {
			t.Fatalf("gap %d renders nothing", row.gap)
		}
	}
}

// TestBudgetLaunchNoRouteRefusalNamesTheCutoffGap pins the rendered refusal
// for the two tools the operator reported (D-452): the old reason survives
// and the remedy states the tool's own cutoff gap before the soft fallback.
func TestBudgetLaunchNoRouteRefusalNamesTheCutoffGap(t *testing.T) {
	for tool, want := range map[string]string{
		"junie":    "`junie/cli` under controlled surfaces",
		"poolside": "`poolside/cli` under controlled surfaces",
	} {
		msg := budgetLaunchRefusal(tool, budgetLaunchCauseNoRoute).Error()
		for _, part := range []string{"no verified Observer budget-admission route", "process cutoff", want, "soft"} {
			if !strings.Contains(msg, part) {
				t.Errorf("%s refusal %q is missing %q", tool, msg, part)
			}
		}
	}
}
