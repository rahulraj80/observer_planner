//go:build linux

package distro

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// binaryReport asks the binary under test for its own component report
// (`observer components list --json`) in a throwaway HOME.
func binaryReport(t *testing.T, bin string) components.Report {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(bin, "components", "list", "--json") //nolint:gosec // G204: the binary under test.
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "TMPDIR=" + home}
	cmd.Dir = home
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s components list --json: %v", bin, err)
	}
	var rep components.Report
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatalf("decode the components report: %v", err)
	}
	return rep
}

// reportCompiled is the set of node components the binary offers.
func reportCompiled(rep components.Report) map[string]bool {
	out := map[string]bool{}
	for _, c := range rep.Components {
		if c.Compiled {
			out[c.ID] = true
		}
	}
	return out
}

// TestDistroDefaultListeners is the per-distro listener smoke (plan 5.2's
// "Listeners by default" column, 5.9 release row; lane MP-REL): a node
// started with no [components] block must listen on exactly the automatic
// listeners of the components its distro activates by default. An
// unstamped (full) build activates every compiled component; a stamped one
// starts from its distro's on and cond cells, and a component the distro
// excludes never listens even when its code is still in the binary.
func TestDistroDefaultListeners(t *testing.T) {
	bin := smokeBinary(t)
	rep := binaryReport(t, bin)
	if !rep.Distro.Valid() {
		t.Skipf("the binary reports distro %q, not a prebuilt one", rep.Distro)
	}
	offered := reportCompiled(rep)
	active := func(id string) bool {
		if !offered[id] {
			return false
		}
		if rep.Distro == components.DistroFull {
			return true
		}
		c, ok := components.Get(id)
		if !ok {
			return false
		}
		cell := c.Distros[rep.Distro]
		return cell == components.DefaultOn || cell == components.DefaultCond
	}
	for _, id := range rep.DistroOut {
		if offered[id] {
			t.Errorf("%s is listed in distro_out but reported as compiled", id)
		}
	}
	cfg, ports := baseConfig(t, "")
	var want []int
	for comp, p := range ports {
		if active(comp) {
			want = append(want, p)
		}
	}
	sort.Ints(want)
	d := startDaemon(t, bin, cfg)
	// Wait until every expected listener is bound (an unstamped full build
	// prints no component summary with no block), or, when none is
	// expected, until the component start report appears; then let every
	// start-time goroutine bind what it would bind and read the sockets.
	waitFor(t, d, 90*time.Second, "the distro's default listeners", func() bool {
		held, err := listeningTCPPorts(d.cmd.Process.Pid)
		if err != nil {
			return false
		}
		for _, p := range want {
			if !held[p] {
				return false
			}
		}
		return len(want) > 0 || strings.Contains(d.output(), "compiled components active")
	})
	time.Sleep(8 * time.Second)
	d.alive(t)
	held, err := listeningTCPPorts(d.cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for p := range held {
		got = append(got, p)
	}
	sort.Ints(got)
	if len(got) != len(want) {
		t.Fatalf("%s distro listens on %v, want exactly %v (catalog automatic listeners of its default set %v); output:\n%s",
			rep.Distro, got, want, ports, d.output())
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s distro listens on %v, want exactly %v; output:\n%s", rep.Distro, got, want, d.output())
		}
	}
	t.Logf("%s distro: %d listeners as the catalog says (%v); distro_out %d: %v", rep.Distro, len(got), got, len(rep.DistroOut), rep.DistroOut)
}
