//go:build linux && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// hostedToolBox plays the box's side of the tool check on the harness: the
// probe answers from what the box has, an install puts the tool in place.
type hostedToolBox struct {
	mu        sync.Mutex
	has       []string
	installed bool
	installs  [][]string
	envs      []map[string]string
}

func (tb *hostedToolBox) wrap(hh *hostedHarness) {
	hh.client.execs = func(b *fakeBox, spec sandboxremote.ExecSpec) (sandboxremote.Process, error) {
		tb.mu.Lock()
		defer tb.mu.Unlock()
		switch {
		case len(spec.Argv) >= 3 && spec.Argv[0] == "sh" && spec.Argv[2] == hostedToolProbeScript:
			if tb.installed {
				return newFakeProcess("tool /tmp/observer-home/.local/bin/claude\n", 0), nil
			}
			var out strings.Builder
			for _, h := range tb.has {
				out.WriteString("has " + h + "\n")
			}
			return newFakeProcess(out.String(), 0), nil
		case len(spec.Argv) > 0 && (spec.Argv[0] == "npm" || spec.Argv[0] == "bash"):
			tb.installs = append(tb.installs, spec.Argv)
			tb.envs = append(tb.envs, spec.Env)
			tb.installed = true
			return newFakeProcess("installed\n", 0), nil
		}
		tb.mu.Unlock()
		defer tb.mu.Lock()
		return hh.exec(b, spec)
	}
}

// TestHostedHostInstallsTheToolOverCurl (live run 2026-11-11): a box with
// curl and no npm and no tool gets the vendor's script over curl, is asked
// again, and the agent starts.
func TestHostedHostInstallsTheToolOverCurl(t *testing.T) {
	hh := newHostedHarness(t, sandboxremote.VendorE2B)
	check, err := hostedToolDelivery("claude-code", "", vocab.TierInternet, "")
	if err != nil {
		t.Fatal(err)
	}
	hh.run.plan.Tier, hh.run.plan.ToolCheck = string(vocab.TierInternet), check
	tb := &hostedToolBox{has: []string{"curl", "bash"}}
	tb.wrap(hh)
	code := make(chan int, 1)
	go func() { code <- hh.run.drive(context.Background()) }()
	waitFor(t, func() bool { hh.mu.Lock(); defer hh.mu.Unlock(); return len(hh.ptys) > 0 })
	_, _ = hh.termIn.Write([]byte("exit"))
	select {
	case c := <-code:
		if c != 5 {
			t.Errorf("exit %d, output %s", c, hh.out.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the run did not end")
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()
	if len(tb.installs) != 1 || !slices.Equal(tb.installs[0], hostedPipefail(hostedClaudeScript(t).Argv)) {
		t.Fatalf("installs %q", tb.installs)
	}
	if out := hh.out.String(); !strings.Contains(out, "over curl") || !strings.Contains(out, "claude-code installed at") {
		t.Errorf("output %s", out)
	}
}

// TestHostedHostInstallsWithNPMUnderTheBoxHome (live run 2026-11-11,
// node:22-slim on a SandHold node: npm install -g as the box's non-root
// user failed on the image's root-owned /usr/local): a box with npm gets
// the tool with npm's prefix under the box home, whose bin the check looks
// in, and the agent starts.
func TestHostedHostInstallsWithNPMUnderTheBoxHome(t *testing.T) {
	hh := newHostedHarness(t, sandboxremote.VendorE2B)
	check, err := hostedToolDelivery("claude-code", "node-image", vocab.TierInternet, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(check.Dirs, hostedNPMBinDir) {
		t.Fatalf("the check does not look in %s: %q", hostedNPMBinDir, check.Dirs)
	}
	hh.run.plan.Tier, hh.run.plan.ToolCheck = string(vocab.TierInternet), check
	tb := &hostedToolBox{has: []string{"npm", "bash"}}
	tb.wrap(hh)
	code := make(chan int, 1)
	go func() { code <- hh.run.drive(context.Background()) }()
	waitFor(t, func() bool { hh.mu.Lock(); defer hh.mu.Unlock(); return len(hh.ptys) > 0 })
	_, _ = hh.termIn.Write([]byte("exit"))
	select {
	case <-code:
	case <-time.After(20 * time.Second):
		t.Fatal("the run did not end")
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()
	if len(tb.installs) != 1 || tb.installs[0][0] != "npm" {
		t.Fatalf("installs %q", tb.installs)
	}
	home := hh.run.layout.Home
	if got := tb.envs[0]["NPM_CONFIG_PREFIX"]; got != home+"/.local" || tb.envs[0]["HOME"] != home {
		t.Fatalf("npm env %v, want its prefix under %s", tb.envs[0], home)
	}
}

// TestHostedHostRefusesABoxThatCannotInstallTheTool: a box with neither
// the tool nor npm, curl or wget is refused before the agent starts, the
// box is deleted, and the run's end note carries the refusal (what
// `observer launch` and `observer terminals show` print).
func TestHostedHostRefusesABoxThatCannotInstallTheTool(t *testing.T) {
	hh := newHostedHarness(t, sandboxremote.VendorE2B)
	check, err := hostedToolDelivery("claude-code", "plain-image", vocab.TierInternet, "")
	if err != nil {
		t.Fatal(err)
	}
	hh.run.plan.Tier, hh.run.plan.ToolCheck = string(vocab.TierInternet), check
	tb := &hostedToolBox{has: []string{"sh"}}
	tb.wrap(hh)
	if c := hh.run.drive(context.Background()); c != sandboxHelperFailCode {
		t.Fatalf("exit %d, want %d", c, sandboxHelperFailCode)
	}
	hh.mu.Lock()
	ptys := len(hh.ptys)
	hh.mu.Unlock()
	if ptys != 0 || len(tb.installs) != 0 {
		t.Fatalf("the agent started (%d) or an install ran (%q)", ptys, tb.installs)
	}
	if !hh.client.boxes["box1"].deleted {
		t.Error("the box outlived the refusal")
	}
	got := runEndNoteReason(hh.run.plan.ObserverDir, "termrun-id", hh.run.plan.RunID, sandboxHelperFailCode)
	for _, want := range []string{"launch_refused: ", "claude-code is not installed in the box (template plain-image)", "it lacks npm, bash, curl and wget", "use a box image"} {
		if !strings.Contains(got, want) {
			t.Errorf("end reason %q lacks %q", got, want)
		}
	}
}

// TestHostedHostDropsNULFromTheBoxTerminal (live run 2026-11-11: a NUL
// came before the in-box Observer's first line): the box terminal's output
// reaches the helper's terminal without NUL bytes; the input side is left
// as it is.
func TestHostedHostDropsNULFromTheBoxTerminal(t *testing.T) {
	hh := newHostedHarness(t, sandboxremote.VendorE2B)
	code := make(chan int, 1)
	go func() { code <- hh.run.drive(context.Background()) }()
	waitFor(t, func() bool { hh.mu.Lock(); defer hh.mu.Unlock(); return len(hh.ptys) > 0 })
	_, _ = hh.termIn.Write([]byte("\x00observer: attach is not available inside the sandbox"))
	waitFor(t, func() bool { return strings.Contains(hh.out.String(), "attach is not available") })
	_, _ = hh.termIn.Write([]byte("exit"))
	select {
	case <-code:
	case <-time.After(20 * time.Second):
		t.Fatal("the run did not end")
	}
	if out := hh.out.String(); strings.ContainsRune(out, 0) {
		t.Fatalf("a NUL reached the terminal: %q", out)
	}
}
