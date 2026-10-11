package invariant

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// TestComponentsPackageFence generalises obs_boundary_test.go to every
// component that targets L1 (plan 3.2 item 2): a package a component owns
// may be imported only by
//
//   - the component's own packages,
//   - a component that requires (or requires_compiled) it, directly or
//     transitively (removing the owner removes the dependant with it),
//   - the binary's cmd wiring package (P0: the whole cmd/<binary> dir;
//     phase P2 narrows this to the component's *_on.go wiring files),
//   - the adapter fan-in internal/adapter/defaults (P5 replaces it with
//     per-adapter init registration).
//
// An edge carried only by FEATURE-OWNED files is not debt either (plan
// 5.1: "whole feature-owned files in store, proxy, dashboard, metrics and
// diag carry the component's tag"): when the owner has an omit tag in the
// tree and the importer no longer imports the package in a build with
// that tag, every file making the import is tagged as the component's
// own, so the edge leaves with the component (lane MP-L1B).
//
// Anything else is BOUNDARY DEBT: core reaching into a feature, the
// reason the plan runs phase PB before any L1 claim (rev F1). Today's debt
// is recorded in componentsFenceBaseline (components_fence_baseline_test.go)
// as a ratchet: a NEW violation fails (a lane may not add debt), and a
// baseline entry that no longer occurs fails too (delete it, the debt was
// paid). Lanes MP-BND, MP-L1A and MP-L1B shrink the baseline to zero for
// each component before it may claim L1.
//
// Components whose target stops at L2 or L3 (analytics, guard, org.client,
// projects, the integrations that never leave the binary) are exempt:
// importing them is not debt, because nothing ever compiles them out.
// It runs on the go list -deps closure, so it follows build tags and GOOS
// exactly as the release build does.
func TestComponentsPackageFence(t *testing.T) {
	tagged := buildTagsInTree(t, repoRoot(t))
	var got []string
	for _, b := range componentModuleBinaries {
		ix := newOwnerIndex(b)
		reqs := requiresClosure(b)
		seen := map[string]bool{}
		for _, goos := range binaryGOOS(b) {
			g := goListDeps(t, b.CmdPath(), goos, "")
			for full, p := range g.pkgs {
				if !ownPkg(full) {
					continue
				}
				importer := relPkg(full)
				if strings.HasPrefix(importer, "cmd/") || importer == "internal/adapter/defaults" {
					continue
				}
				from := ix.ownerOf(b, importer)
				for _, imp := range p.imports {
					if !ownPkg(imp) {
						continue
					}
					target := relPkg(imp)
					to, owned := ix.owner[target]
					if !owned || to == from {
						continue
					}
					c := ix.byID[to]
					if !c.Levels.Targets(components.L1) {
						continue
					}
					if reqs[from][to] {
						continue
					}
					if featureFileEdge(t, b, goos, importer, target, c.BuildTag(), tagged) {
						continue
					}
					key := fmt.Sprintf("%s %s -> %s (%s)", b, importer, target, to)
					if !seen[key] {
						seen[key] = true
						got = append(got, key)
					}
				}
			}
		}
	}
	sort.Strings(got)
	base := map[string]bool{}
	for _, k := range componentsFenceBaseline {
		base[k] = true
	}
	now := map[string]bool{}
	for _, k := range got {
		now[k] = true
		if !base[k] {
			t.Errorf("new boundary debt (an L1-target component's package imported from outside it): %s", k)
		}
	}
	if testing.Short() {
		// -short analyses linux only, so windows/darwin-only baseline
		// entries cannot be judged stale.
		return
	}
	for _, k := range componentsFenceBaseline {
		if !now[k] {
			t.Errorf("stale baseline entry (the debt was paid; delete it from componentsFenceBaseline): %s", k)
		}
	}
	if os.Getenv("SBO_COMPONENTS_PRINT_FENCE") != "" {
		for _, k := range got {
			fmt.Printf("\t%q,\n", k)
		}
	}
}

// requiresClosure maps every component to the set it requires or
// requires_compiled, transitively.
func requiresClosure(b components.Binary) map[string]map[string]bool {
	by := map[string]components.Component{}
	for _, c := range components.ForBinary(b) {
		by[c.ID] = c
	}
	out := map[string]map[string]bool{}
	var walk func(id string, acc map[string]bool)
	walk = func(id string, acc map[string]bool) {
		c := by[id]
		for _, r := range append(append(append([]string(nil), c.Requires...), c.RequiresCompiled...), c.Bundles...) {
			if !acc[r] {
				acc[r] = true
				walk(r, acc)
			}
		}
	}
	for id := range by {
		acc := map[string]bool{}
		walk(id, acc)
		out[id] = acc
	}
	return out
}

func dirExists(root, rel string) bool {
	st, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && st.IsDir()
}

// featureFileEdge reports whether importer -> target disappears in a build
// with tag (the target component's omit tag), meaning only files tagged as
// that component's own make the import. An importer that is itself absent
// in that build also counts. A tag no file in the tree names never
// qualifies: there is no feature file yet, so the edge is debt.
func featureFileEdge(t *testing.T, b components.Binary, goos, importer, target, tag string, tagged map[string]bool) bool {
	t.Helper()
	if !tagged[tag] {
		return false
	}
	g := goListDeps(t, b.CmdPath(), goos, tag)
	p, ok := g.pkgs[componentsModule+"/"+importer]
	if !ok {
		return true
	}
	for _, imp := range p.imports {
		if imp == componentsModule+"/"+target {
			return false
		}
	}
	return true
}
