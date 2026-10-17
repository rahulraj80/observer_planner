package main

import (
	"context"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/componentsvc"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/diag"
)

func checkByName(cs []diag.Check, name string) (diag.Check, bool) {
	for _, c := range cs {
		if c.Name == name {
			return c, true
		}
	}
	return diag.Check{}, false
}

// TestComponentsDoctorChecks walks the section's rows: a legacy install gets
// one OK selection line and no leftover probe; a configured selection
// reports its problems; the leftover probe turns Observer entries an
// inactive component left in other tools' config into a WARN naming the
// teardown command.
func TestComponentsDoctorChecks(t *testing.T) {
	ctx := context.Background()
	t.Run("legacy", func(t *testing.T) {
		called := false
		cs := componentsDoctorChecks(ctx, componentstate.Legacy(), func(context.Context, []string) ([]componentsvc.StepOutcome, error) {
			called = true
			return nil, nil
		})
		sel, ok := checkByName(cs, "components.selection")
		// T-732 / T-739: the line names what is active (every component, or
		// the build's defaults with the count), not "as before".
		if !ok || sel.Status != diag.StatusOK || !(strings.Contains(sel.Message, "every component this build contains is active") ||
			strings.Contains(sel.Message, "this build's defaults are active")) {
			t.Fatalf("selection = %+v", sel)
		}
		if _, ok := checkByName(cs, "components.selection-issues"); ok {
			t.Fatal("a legacy install has no selection issues")
		}
		// A build that compiles a component out has it inactive even with
		// no selection, so only the full build has nothing to probe.
		if called && len(components.Omitted()) == 0 {
			t.Fatal("nothing is inactive, so no leftover probe runs")
		}
		for _, n := range []string{"components.build", "components.policy", "components.signature"} {
			if _, ok := checkByName(cs, n); !ok {
				t.Errorf("%s missing", n)
			}
		}
	})
	t.Run("issues and leftovers", func(t *testing.T) {
		snap := componentstate.Resolve(config.ComponentsConfig{Remove: []string{"integration.hooks", "no.such.component"}}, nil)
		var probed []string
		cs := componentsDoctorChecks(ctx, snap, func(_ context.Context, ids []string) ([]componentsvc.StepOutcome, error) {
			probed = ids
			return []componentsvc.StepOutcome{{
				Component: "integration.hooks", Kind: componentsvc.StepToolWiring, Target: "claude-code",
				Changed: true, Detail: []string{"PreToolUse hook"},
			}}, nil
		})
		iss, ok := checkByName(cs, "components.selection-issues")
		if !ok || iss.Status != diag.StatusWarn || !strings.Contains(strings.Join(iss.Details, " "), "no.such.component") {
			t.Fatalf("issues = %+v", iss)
		}
		if len(probed) == 0 {
			t.Fatal("the leftover probe did not run for an inactive component with teardown")
		}
		left, ok := checkByName(cs, "components.leftovers")
		if !ok || left.Status != diag.StatusWarn || !strings.Contains(left.Message, "observer components teardown integration.hooks") {
			t.Fatalf("leftovers = %+v", left)
		}
	})
	t.Run("clean leftovers", func(t *testing.T) {
		snap := componentstate.Resolve(config.ComponentsConfig{Remove: []string{"integration.hooks"}}, nil)
		cs := componentsDoctorChecks(ctx, snap, func(context.Context, []string) ([]componentsvc.StepOutcome, error) {
			return []componentsvc.StepOutcome{{Component: "integration.hooks", Target: "claude-code"}}, nil
		})
		if left, _ := checkByName(cs, "components.leftovers"); left.Status != diag.StatusOK {
			t.Fatalf("leftovers = %+v", left)
		}
	})
}
