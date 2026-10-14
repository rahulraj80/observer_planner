package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// chooserPrompt matches one y/n chooser question as printed (the prompter
// does not echo the answer, so the questions appear in order).
var chooserPrompt = regexp.MustCompile(`  (.+?) \[(?:Y/n|y/N), b = back\] `)

// chooserRow matches one step 2 checklist row as printed (D-571): its
// number, whether it is ticked, and its text.
var chooserRow = regexp.MustCompile(`(?m)^  (\d+)\) \[(x| )\] (.+)$`)

// rowState reports whether the first checklist row starting with label was
// printed ticked, and its number (0 when no row starts with label).
func rowState(out, label string) (on bool, num string) {
	for _, m := range chooserRow.FindAllStringSubmatch(out, -1) {
		if strings.HasPrefix(m[3], label) {
			return m[2] == "x", m[1]
		}
	}
	return false, ""
}

// toggleInput answers a fresh run of the same chooser: every y/n question
// before the checklist at its default, the row starting with label toggled,
// the checklist accepted, then the confirm at its default (yes).
func toggleInput(out, label string) string {
	_, num := rowState(out, label)
	first := strings.Index(out, "  1) [")
	before := 0
	if first > 0 {
		before = len(chooserPrompt.FindAllString(out[:first], -1))
	}
	return strings.Repeat("\n", before) + num + "\n\n\n"
}

func addsSnapshot(d components.Distro, sec config.ComponentsConfig) componentstate.Snapshot {
	return componentstate.ResolveGoverned(sec, components.InDistro(d), componentstate.Governed{})
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// TestChooserRunsOverInstallerWrittenAdds pins T-745 (node 3, rc.2): the
// installer's service offer writes `[components] add =
// ["integration.service"]` before the first `observer init`, and any block
// used to skip the chooser, so the documented install -> y -> init flow never
// showed it and recorded no distro. An add-only block now runs the chooser
// with those adds preselected and records the base; a "no" still turns the
// service off. A block that names a distro or a remove stays a recorded
// choice.
func TestChooserRunsOverInstallerWrittenAdds(t *testing.T) {
	requireSimulatedDistro(t, components.DistroDeveloper, components.DistroManaged)
	addOnly := config.ComponentsConfig{Add: []string{"integration.service"}}

	f := &fakeChooserService{snap: addsSnapshot(components.DistroDeveloper, addOnly)}
	out := runChooser(t, f, strings.Repeat("\n", 40), nil)
	if !strings.Contains(out, "already turned on: integration.service") {
		t.Fatalf("the chooser did not run over an installer-written add (or did not say it kept it):\n%s", out)
	}
	if f.applied == nil || f.applied.SetBase == nil || *f.applied.SetBase != string(components.DistroDeveloper) {
		t.Fatalf("the edition base was not recorded: %+v", f.applied)
	}
	if containsID(f.applied.Remove, "integration.service") || containsID(f.applied.Add, "integration.service") {
		t.Fatalf("accepting the defaults touched the preselected service: add %v remove %v", f.applied.Add, f.applied.Remove)
	}
	on, num := rowState(out, "Start at login")
	if num == "" {
		t.Fatalf("no service row in the checklist:\n%s", out)
	}
	if !on {
		t.Fatalf("the preselected service is not shown as on ([x]):\n%s", out)
	}

	// Unticking the service turns it off.
	f2 := &fakeChooserService{snap: addsSnapshot(components.DistroDeveloper, addOnly)}
	runChooser(t, f2, toggleInput(out, "Start at login"), nil)
	if f2.applied == nil || !containsID(f2.applied.Remove, "integration.service") {
		t.Fatalf("answering no to the preselected service did not turn it off: %+v", f2.applied)
	}

	// A recorded choice is never asked again.
	for name, sec := range map[string]config.ComponentsConfig{
		"distro": {Distro: "developer", Add: []string{"integration.service"}},
		"remove": {Add: []string{"integration.service"}, Remove: []string{"codeintel"}},
	} {
		f3 := &fakeChooserService{snap: addsSnapshot(components.DistroDeveloper, sec)}
		if got := runChooser(t, f3, "", nil); got != "" || f3.applied != nil {
			t.Fatalf("%s: the chooser ran over a recorded choice:\n%s", name, got)
		}
	}
	// A managed node has no base to record: an add-only block stays as it is.
	g := componentstate.Governed{OrgManaged: true, OrgName: "acme.example"}
	f4 := &fakeChooserService{snap: componentstate.ResolveGoverned(addOnly, components.InDistro(components.DistroManaged), g)}
	if got := runChooser(t, f4, "", nil); got != "" {
		t.Fatalf("the chooser ran on a managed node with an add-only block:\n%s", got)
	}
}

// TestChooserAsksAboutBrowserCapture pins D-561 (node 5, rc.2 npm full
// build): the chooser asked nothing about browser capture, so capture.browser
// stayed on unless the operator ran `observer components remove
// capture.browser`. It is now a step-2 row carrying the browser-chat adapters
// the catalog bundles with it: asked where the build carries it (default
// from the base), a "no" turns the listener and its site adapters off, step
// 1's "capture only the detected tools" no longer removes those adapters
// behind the row's back, and a build without it shows the row unavailable.
func TestChooserAsksAboutBrowserCapture(t *testing.T) {
	requireSimulatedDistro(t, components.DistroFull, components.DistroManaged, components.DistroMinimal)
	ids := withBundles("capture.browser")
	if len(ids) < 2 {
		t.Fatalf("capture.browser bundles no site adapters in the catalog: %v", ids)
	}

	f := &fakeChooserService{snap: distroSnapshot(components.DistroFull, componentstate.Governed{})}
	out := runChooser(t, f, strings.Repeat("y\n", 40), nil)
	on, num := rowState(out, "Browser chat capture")
	if num == "" {
		t.Fatalf("no browser capture row on a full build:\n%s", out)
	}
	if !on {
		t.Fatalf("browser capture is on in full but not shown as on ([x]):\n%s", out)
	}
	// Step 1 "yes" (capture only detected tools) with nothing detected must
	// not remove the browser-chat adapters: their row said yes.
	if f.applied == nil {
		t.Fatal("nothing applied")
	}
	for _, id := range ids {
		if containsID(f.applied.Remove, id) {
			t.Fatalf("a yes to browser capture still removed %s (step 1 overrode the row): %v", id, f.applied.Remove)
		}
	}

	f2 := &fakeChooserService{snap: distroSnapshot(components.DistroFull, componentstate.Governed{})}
	runChooser(t, f2, toggleInput(out, "Browser chat capture"), nil)
	if f2.applied == nil {
		t.Fatal("nothing applied")
	}
	for _, id := range ids {
		if !containsID(f2.applied.Remove, id) {
			t.Fatalf("no to browser capture left %s on: remove %v", id, f2.applied.Remove)
		}
	}

	// Managed leaves it off: offered as a widening (default no).
	f3 := &fakeChooserService{snap: distroSnapshot(components.DistroManaged, componentstate.Governed{})}
	out3 := runChooser(t, f3, strings.Repeat("\n", 40), nil)
	if on3, num3 := rowState(out3, "Browser chat capture"); num3 == "" || on3 {
		t.Fatalf("managed base: browser capture not offered as off:\n%s", out3)
	}
	if f3.applied != nil && containsID(f3.applied.Add, "capture.browser") {
		t.Fatalf("accepting the managed defaults turned browser capture on: %v", f3.applied.Add)
	}

	// Minimal compiles it out: shown unavailable with the reason, not asked.
	f4 := &fakeChooserService{snap: distroSnapshot(components.DistroMinimal, componentstate.Governed{})}
	out4 := runChooser(t, f4, strings.Repeat("\n", 40), nil)
	if !strings.Contains(out4, "Browser chat capture: unavailable here (") {
		t.Fatalf("minimal: browser capture not shown unavailable with its reason:\n%s", out4)
	}
}
