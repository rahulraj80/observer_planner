//go:build !sbo_omit_org_update

package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/managedconfig"
	"github.com/marmutapp/superbased-observer/internal/store"
	"github.com/marmutapp/superbased-observer/internal/update"
)

// update_distro_test.go covers the cmd side of the distro-aware update rail
// (modular packaging plan 5.6 / 5.6.1 / 6.2.2, lane MP-REL2, T-687): the pure
// distro context, the apply-side re-check of rule 12, the ceiling gate, the
// pre-swap components digest probe, and what a successful apply records.

func validCeiling(ids ...string) managedconfig.Result {
	return managedconfig.Result{
		Status: managedconfig.StatusValid, UpdatesAllowed: true,
		Ceiling: &managedconfig.Ceiling{IDs: ids, CatalogVersion: components.CatalogVersion},
	}
}

func TestResolveUpdateDistroContext(t *testing.T) {
	minimalIDs := components.InDistro(components.DistroMinimal)
	cases := []struct {
		name                         string
		in                           updateDistroInputs
		wantCurrent, wantTarget      string
		wantAllowed, wantLegacy, upd bool
	}{
		{
			name: "unstamped build, no ceiling", in: updateDistroInputs{Ceiling: managedconfig.Result{Status: managedconfig.StatusNone, UpdatesAllowed: true}},
			wantCurrent: "full", wantTarget: "full", wantLegacy: true, upd: true,
		},
		{
			name: "minimal build never takes v1 full", in: updateDistroInputs{Running: components.DistroMinimal, Ceiling: managedconfig.Result{Status: managedconfig.StatusNone, UpdatesAllowed: true}},
			wantCurrent: "minimal", wantTarget: "minimal", upd: true,
		},
		{
			name: "policy names managed, no ceiling", in: updateDistroInputs{PolicyDistro: "managed", Ceiling: managedconfig.Result{Status: managedconfig.StatusNone, UpdatesAllowed: true}},
			wantCurrent: "full", wantTarget: "managed", wantAllowed: true, wantLegacy: true, upd: true,
		},
		{
			name: "widening past the ceiling is not allowed", in: updateDistroInputs{Running: components.DistroMinimal, PolicyDistro: "full", Ceiling: validCeiling(minimalIDs...)},
			wantCurrent: "minimal", wantTarget: "full", wantAllowed: false, upd: true,
		},
		{
			name: "narrowing within the ceiling is allowed", in: updateDistroInputs{PolicyDistro: "minimal", Ceiling: validCeiling(minimalIDs...)},
			wantCurrent: "full", wantTarget: "minimal", wantAllowed: true, upd: true,
		},
		{
			name: "invalid ceiling refuses updates", in: updateDistroInputs{Ceiling: managedconfig.Result{
				Status:  managedconfig.StatusInvalidCoreOnly,
				Ceiling: &managedconfig.Ceiling{IDs: []string{}},
			}},
			wantCurrent: "full", wantTarget: "full", upd: false,
		},
		{
			name: "unknown policy distro is ignored", in: updateDistroInputs{PolicyDistro: "custom", Ceiling: managedconfig.Result{Status: managedconfig.StatusNone, UpdatesAllowed: true}},
			wantCurrent: "full", wantTarget: "full", wantLegacy: true, upd: true,
		},
	}
	for _, c := range cases {
		dc := resolveUpdateDistroContext(c.in)
		if !dc.Aware || dc.Current != c.wantCurrent || dc.Target != c.wantTarget || dc.TargetAllowed != c.wantAllowed ||
			dc.LegacyFullAllowed != c.wantLegacy || dc.UpdatesAllowed != c.upd {
			t.Errorf("%s: %+v", c.name, dc)
		}
	}
}

func TestApplyDistroRefusal(t *testing.T) {
	minArt := update.Artifact{Distro: "minimal"}
	man := update.Manifest{DistroEpoch: 3}
	ok := update.DistroContext{Current: "full", Target: "minimal", TargetAllowed: true, Epoch: 2}
	if r := applyDistroRefusal(ok, true, minArt, man); r != "" {
		t.Fatalf("allowed transition refused: %q", r)
	}
	if r := applyDistroRefusal(ok, false, update.Artifact{}, man); r != "" {
		t.Fatalf("a same-distro apply is never refused here: %q", r)
	}
	stale := ok
	stale.Epoch = 3
	if r := applyDistroRefusal(stale, true, minArt, man); r != update.ReasonDistroEpochStale {
		t.Fatalf("stale epoch: %q", r)
	}
	moved := ok
	moved.Target = "managed"
	if r := applyDistroRefusal(moved, true, minArt, man); r != update.ReasonDistroNotAllowed {
		t.Fatalf("policy moved since the fetch: %q", r)
	}
	notAllowed := ok
	notAllowed.TargetAllowed = false
	if r := applyDistroRefusal(notAllowed, true, minArt, man); r != update.ReasonDistroNotAllowed {
		t.Fatalf("not allowed: %q", r)
	}
}

func TestRecordAppliedDistro(t *testing.T) {
	row := store.UpdateStateRow{CurrentDistro: "full", DistroEpochs: map[string]int64{"stable": 2}}
	opts := applyOptions{
		Manifest: update.Manifest{Channel: update.ChannelStable}, Artifact: update.Artifact{Distro: "minimal", ComponentsDigest: "abc"},
		DistroTransition: true, DistroEpoch: 3,
	}
	recordAppliedDistro(&row, opts)
	if row.CurrentDistro != "minimal" || row.ComponentsDigest != "abc" || row.DistroEpochs["stable"] != 3 {
		t.Fatalf("transition: %+v", row)
	}
	same := applyOptions{Manifest: update.Manifest{Channel: update.ChannelStable}, Artifact: update.Artifact{}, DistroEpoch: 9}
	recordAppliedDistro(&row, same)
	if row.CurrentDistro != "full" || row.DistroEpochs["stable"] != 3 {
		t.Fatalf("a same-distro v1 apply must not move the epoch: %+v", row)
	}
}

// TestCeilingInvalidBlocksTheApplyWithoutDownloading: the ceiling gate runs
// before a byte is fetched, from a cached manifest too.
func TestCeilingInvalidBlocksTheApplyWithoutDownloading(t *testing.T) {
	f := newApplyFixture(t)
	f.opts.CeilingInvalid = true
	downloads := 0
	f.deps.Download = func(context.Context, string, string, io.Writer, int64) error {
		downloads++
		return nil
	}
	out, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	if out.State != update.StateBlocked || out.Reason != update.ReasonCeilingInvalid || downloads != 0 {
		t.Fatalf("outcome %+v, downloads %d", out, downloads)
	}
	if strings.Contains(f.installedBinary(), "NEW") {
		t.Fatal("the binary was replaced")
	}
}

// TestComponentsDigestProbeGuardsTheSwap: a v2 artifact whose staged binary
// reports another compiled set is refused before the swap; the matching one
// applies and records its distro.
func TestComponentsDigestProbeGuardsTheSwap(t *testing.T) {
	want := strings.Repeat("a", 64)
	f := newApplyFixture(t)
	f.opts.Artifact.Distro, f.opts.Artifact.ComponentsDigest = "minimal", want
	f.opts.Manifest.Artifacts[0] = f.opts.Artifact
	f.deps.ProbeComponents = func(context.Context, string) (string, error) { return strings.Repeat("b", 64), nil }
	out, err := f.run()
	if err == nil && out.State != update.StateFailed {
		t.Fatalf("digest mismatch applied: %+v", out)
	}
	if out.ErrorClass != update.ErrorProbe || strings.Contains(f.installedBinary(), "NEW") {
		t.Fatalf("outcome %+v; the running binary must be untouched", out)
	}

	g := newApplyFixture(t)
	g.opts.Artifact.Distro, g.opts.Artifact.ComponentsDigest = "minimal", want
	g.opts.Manifest.Artifacts[0] = g.opts.Artifact
	g.opts.DistroTransition, g.opts.DistroEpoch = true, 4
	g.deps.ProbeComponents = func(context.Context, string) (string, error) { return want, nil }
	out, err = g.run()
	if err != nil || !out.Applied {
		t.Fatalf("matching digest: %+v %v", out, err)
	}
	row, _ := g.st.LoadUpdateState(context.Background())
	if row.CurrentDistro != "minimal" || row.ComponentsDigest != want || row.DistroEpochs["stable"] != 4 {
		t.Fatalf("recorded distro state: %+v", row)
	}
}
