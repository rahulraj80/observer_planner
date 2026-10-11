package main

import (
	"debug/buildinfo"
	"os"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/components/sbom"
)

// TestExpectedSetsSplitL1FromL2 pins how the proof gate reads the catalog:
// every excluded component that claims L1 must be absent, every other one is
// L2 only, and full excludes nothing.
func TestExpectedSetsSplitL1FromL2(t *testing.T) {
	absent, l2 := expectedSets(components.DistroFull)
	if len(absent) != 0 || len(l2) != 0 {
		t.Fatalf("full excludes nothing, got absent=%v l2=%v", absent, l2)
	}
	absent, l2 = expectedSets(components.DistroMinimal)
	for _, id := range absent {
		c, _ := components.Get(id)
		if !c.Levels.Has(components.L1) {
			t.Errorf("%s listed as absent but does not claim L1", id)
		}
	}
	for _, id := range l2 {
		c, _ := components.Get(id)
		if c.Levels.Has(components.L1) {
			t.Errorf("%s listed as L2 only but claims L1", id)
		}
	}
	// capture.proxy compiles out since MP-L1L (D-519), so minimal lists it as
	// absent, like obs; capture.browser is the one component kept L2 by
	// ruling (D-517).
	if !contains(absent, "obs") || !contains(absent, "capture.proxy") {
		t.Errorf("minimal: obs and capture.proxy must be absent (L1); absent=%v l2=%v", absent, l2)
	}
	if got := len(absent) + len(l2); got != len(components.ForBinary(components.BinaryNode))-len(components.InDistro(components.DistroMinimal)) {
		t.Errorf("absent+l2 = %d, want every node component minimal excludes", got)
	}
}

// TestProveFailsOnEachBrokenFact runs the gate on this test binary with a
// faked go list and report, so each failure row is exercised without a
// distro build.
func TestProveFailsOnEachBrokenFact(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bi, err := buildinfo.ReadFile(exe)
	if err != nil {
		t.Skipf("no build info in the test binary: %v", err)
	}
	linked := map[string]bool{}
	for _, m := range sbom.Modules(bi) {
		linked[m] = true
	}
	prevList, prevRun := goListDeps, runReport
	t.Cleanup(func() { goListDeps, runReport = prevList, prevRun })

	// A closure that still holds obs's package, and one module the binary
	// does not link.
	goListDeps = func(root, cmdPath, tags, goos, goarch string) (proofDeps, error) {
		d := proofDeps{pkgs: map[string]bool{"internal/obs": true}, modules: map[string]bool{"example.com/extra@v1.0.0": true}}
		for m := range linked {
			d.modules[m] = true
		}
		return d, nil
	}
	runReport = func(string) (components.Report, error) {
		return components.Report{Distro: components.DistroFull, Omitted: []string{}, DistroOut: []string{}}, nil
	}
	rec, err := prove(".", components.DistroMinimal, exe, true)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Pass {
		t.Fatal("a minimal proof of an untagged test binary passed")
	}
	all := strings.Join(rec.Failures, "\n")
	for _, want := range []string{
		"build tags",                     // the test binary has no distro tags
		"internal/obs is in the closure", // obs claims L1 and minimal excludes it
		"example.com/extra@v1.0.0",       // the closure reaches a module the binary lacks
		"reports distro \"full\"",        // the binary's own report disagrees
		"reports omitted",
		"reports distro_out",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("failures lack %q:\n%s", want, all)
		}
	}
}

// TestImageTargetCoreExcludesTheOmittedModules pins the P6b core-image
// expectation: the core image's tags are ImageTags(core), every row its
// column lists out that claims L1 must be absent, and the full image
// excludes nothing.
func TestImageTargetCoreExcludesTheOmittedModules(t *testing.T) {
	full := imageTarget(components.ImageFull)
	if len(full.wantTags) != 0 || len(full.absentL1) != 0 {
		t.Fatalf("full image: tags %v absent %v, want none", full.wantTags, full.absentL1)
	}
	core := imageTarget(components.ImageCore)
	if core.cmdPath != "./cmd/observer-org" || core.binary != components.BinaryOrg {
		t.Fatalf("core image target = %+v", core)
	}
	if strings.Join(core.wantTags, ",") != strings.Join(components.ImageTags(components.ImageCore), ",") {
		t.Errorf("core tags %v, want ImageTags(core)", core.wantTags)
	}
	for _, id := range core.absentL1 {
		c, _ := components.Get(id)
		if c.Images[components.ImageCore] != components.ImageOut || !c.Levels.Has(components.L1) {
			t.Errorf("%s listed absent in core but its cell is %v / levels %v", id, c.Images[components.ImageCore], c.Levels.Implemented)
		}
	}
	if len(core.absentL1) == 0 {
		t.Error("the core image compiles nothing out; MP-ORG's six L1 modules should be here")
	}
	if _, ok := targetFromFlags("minimal", "core"); ok {
		t.Error("--distro and --image together must be refused")
	}
	if _, ok := targetFromFlags("", "nope"); ok {
		t.Error("an unknown image must be refused")
	}
}
