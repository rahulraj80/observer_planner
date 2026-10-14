package main

import (
	"context"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/componentsvc"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// fakeChooserService serves a fixed snapshot and records the change the
// chooser previews and applies, so a distro or a managed node can be
// exercised without stamping the test binary.
type fakeChooserService struct {
	snap    componentstate.Snapshot
	applied *componentsvc.Change
	preview componentsvc.Change
}

func (f *fakeChooserService) State(context.Context) (componentstate.Snapshot, error) {
	return f.snap, nil
}

func (f *fakeChooserService) Preview(_ context.Context, ch componentsvc.Change) (componentsvc.Preview, error) {
	f.preview = ch
	return componentsvc.Preview{Fingerprint: "fp"}, nil
}

func (f *fakeChooserService) Apply(_ context.Context, ch componentsvc.Change, _ componentsvc.ApplyOptions) (componentsvc.ApplyResult, error) {
	c := ch
	f.applied = &c
	return componentsvc.ApplyResult{Wrote: true}, nil
}

func distroSnapshot(d components.Distro, g componentstate.Governed) componentstate.Snapshot {
	return componentstate.ResolveGoverned(config.ComponentsConfig{}, components.InDistro(d), g)
}

// TestChooserStartsFromTheBinarysOwnDistro pins T-731 and T-733 (D-551),
// one row per distro: the chooser writes the binary's own distro (never a
// hard-coded developer), accepting every default writes exactly that
// distro's defaults (no add, no remove), and a detected tool the base
// leaves off (minimal) is turned on by step 1.
func TestChooserStartsFromTheBinarysOwnDistro(t *testing.T) {
	cases := []struct {
		distro   components.Distro
		detected []string
		wantAdd  []string
	}{
		{components.DistroMinimal, nil, nil},
		{components.DistroMinimal, []string{"cursor"}, []string{"adapter.cursor"}},
		{components.DistroDeveloper, nil, nil},
		{components.DistroManaged, nil, nil},
		{components.DistroFull, nil, nil},
	}
	for _, tc := range cases {
		t.Run(string(tc.distro)+"/"+strings.Join(tc.detected, ","), func(t *testing.T) {
			requireSimulatedDistro(t, tc.distro)
			f := &fakeChooserService{snap: distroSnapshot(tc.distro, componentstate.Governed{})}
			// Every question at its default, then confirm.
			runChooser(t, f, strings.Repeat("\n", 40), tc.detected)
			if f.applied == nil {
				t.Fatal("nothing applied")
			}
			ch := *f.applied
			if ch.SetBase == nil || *ch.SetBase != string(tc.distro) {
				t.Fatalf("base = %v, want %q", ch.SetBase, tc.distro)
			}
			if strings.Join(ch.Add, ",") != strings.Join(tc.wantAdd, ",") {
				t.Fatalf("add = %v, want %v (defaults must add nothing the base already has)", ch.Add, tc.wantAdd)
			}
			on := idSetOf(componentstate.BaseSet(tc.distro))
			for _, id := range ch.Add {
				if on[id] {
					t.Fatalf("add lists %q, which the %s base already turns on", id, tc.distro)
				}
			}
			if tc.detected == nil && len(ch.Remove) > 0 {
				t.Fatalf("accepting the defaults removed %v", ch.Remove)
			}
		})
	}
}

// TestChooserGroupYesAddsOnlyWhatTheBaseLeavesOff pins T-733: a "yes" to a
// group the base already turns on adds nothing (it used to add every
// feasible id of the group: proxy.routing, remote, terminals.sandbox, ...),
// and a "yes" to a group the base leaves off adds its ids.
func TestChooserGroupYesAddsOnlyWhatTheBaseLeavesOff(t *testing.T) {
	requireSimulatedDistro(t, components.DistroDeveloper)
	f := &fakeChooserService{snap: distroSnapshot(components.DistroDeveloper, componentstate.Governed{})}
	// step 1 (nothing detected) is answered y too, which only removes
	// adapters; every group is answered y, then confirm.
	runChooser(t, f, strings.Repeat("y\n", 20), nil)
	if f.applied == nil {
		t.Fatal("nothing applied")
	}
	on := idSetOf(componentstate.BaseSet(components.DistroDeveloper))
	for _, id := range f.applied.Add {
		if on[id] {
			t.Fatalf("add lists %q, already on in developer: %v", id, f.applied.Add)
		}
	}
	for _, id := range []string{"proxy.routing", "terminals.sandbox", "remote"} {
		for _, a := range f.applied.Add {
			if a == id && !groupAllOff(id, on) {
				t.Fatalf("a yes to a default-on group added %q", id)
			}
		}
	}
}

// groupAllOff reports whether id's chooser group has nothing on in the base.
func groupAllOff(id string, on map[string]bool) bool {
	for _, g := range chooserGroups {
		for _, x := range g.IDs {
			if x != id {
				continue
			}
			for _, y := range g.IDs {
				if on[y] {
					return false
				}
			}
			return true
		}
	}
	return true
}

// TestChooserOnAManagedNodeOffersOnlyWidening pins T-737 (D-514, D-548): on
// an org-managed node the chooser writes no distro, never asks a question
// whose answer would narrow the node (the summary used to refuse every "no"
// after the fact), and says why before step 3.
func TestChooserOnAManagedNodeOffersOnlyWidening(t *testing.T) {
	f := &fakeChooserService{snap: distroSnapshot(components.DistroManaged,
		componentstate.Governed{OrgManaged: true, OrgName: "acme.example"})}
	out := runChooser(t, f, strings.Repeat("n\n", 20)+strings.Repeat("\n", 5), []string{"cursor"})
	if f.preview.SetBase != nil {
		t.Fatalf("a managed node wrote a distro %q", *f.preview.SetBase)
	}
	if len(f.preview.Remove) > 0 {
		t.Fatalf("a managed node was offered a narrowing: remove %v", f.preview.Remove)
	}
	summary := strings.Index(out, "3. Summary")
	reason := strings.Index(out, "managed by acme.example")
	if reason < 0 || (summary >= 0 && reason > summary) {
		t.Fatalf("the managed reason must come before the summary:\n%s", out)
	}
	if strings.Contains(out, "refused:") {
		t.Fatalf("the summary still refuses choices the chooser offered:\n%s", out)
	}
}

// idSetOf is a set of ids (local, so the test runs against an older chooser
// for the red half).
func idSetOf(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}
