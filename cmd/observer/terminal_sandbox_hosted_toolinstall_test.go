//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// hostedClaudeScript is the registry's grounded Linux script for
// claude-code, read from the registry so the tests follow it.
func hostedClaudeScript(t *testing.T) integration.InstallHint {
	t.Helper()
	spec, _ := integration.For("claude-code")
	for _, h := range spec.Binary.Installs {
		if h.OS == "linux" && h.Channel == "script" {
			return h
		}
	}
	t.Fatal("claude-code has no Linux script in the registry")
	return integration.InstallHint{}
}

// TestHostedToolInstallChoice (live run 2026-11-11: the node's image had
// curl but no npm and no claude, the npm install exited 127 and a template
// skipped the install): the box is asked first, and the path follows what
// it has: nothing when the tool is there, npm when it has npm, the
// vendor's script over curl, else over wget from the same address, else a
// refusal naming what the box lacks and how to fix it. A template is
// checked the same way; below the internet tier it is refused, never
// installed into.
func TestHostedToolInstallChoice(t *testing.T) {
	script := hostedClaudeScript(t)
	internet, err := hostedToolDelivery("claude-code", "", vocab.TierInternet, "")
	if err != nil || internet == nil {
		t.Fatalf("internet check: %v", err)
	}
	has := func(cmds ...string) hostedBoxTools {
		b := hostedBoxTools{Has: map[string]bool{}}
		for _, c := range cmds {
			b.Has[c] = true
		}
		return b
	}
	cases := []struct {
		name    string
		check   hostedToolCheck
		got     hostedBoxTools
		argv    []string // nil: no install
		refusal []string // substrings of the refusal
	}{
		{"the tool is there", *internet, hostedBoxTools{Tool: "/usr/local/bin/claude", Has: map[string]bool{}}, nil, nil},
		{"npm present", *internet, has("npm", "curl", "bash"), []string{"npm", "install", "-g", "@anthropic-ai/claude-code"}, nil},
		{"only curl", *internet, has("curl", "bash", "sh"), script.Argv, nil},
		{"only wget", *internet, has("wget", "bash"), []string{"bash", "-lc", "wget -qO- https://claude.ai/install.sh | bash"}, nil},
		{"neither", *internet, has("bash", "sh"), nil, []string{
			"claude-code is not installed in the box,", "(it lacks npm, curl and wget)",
			"use a box image", "that has claude-code, or one with npm or curl",
		}},
	}
	withTemplate, err := hostedToolDelivery("claude-code", "sealed-box", vocab.TierInternet, "")
	if err != nil || withTemplate == nil || !withTemplate.CanInstall {
		t.Fatalf("a template on the internet tier is checked and may install: %+v %v", withTemplate, err)
	}
	cases = append(cases, struct {
		name    string
		check   hostedToolCheck
		got     hostedBoxTools
		argv    []string
		refusal []string
	}{"template set, internet, only curl", *withTemplate, has("curl", "bash"), script.Argv, nil})
	below, err := hostedToolDelivery("claude-code", "sealed-box", vocab.TierProxyOnly, "")
	if err != nil || below == nil || below.CanInstall {
		t.Fatalf("a template below the internet tier is checked, not installed into: %+v %v", below, err)
	}
	cases = append(cases, struct {
		name    string
		check   hostedToolCheck
		got     hostedBoxTools
		argv    []string
		refusal []string
	}{"template set, proxy_only, missing", *below, has("npm", "curl", "bash"), nil, []string{
		"claude-code is not installed in the box (template sealed-box)", "the proxy_only tier cannot reach its vendor", "or use the internet tier",
	}}, struct {
		name    string
		check   hostedToolCheck
		got     hostedBoxTools
		argv    []string
		refusal []string
	}{"template set, proxy_only, present", *below, hostedBoxTools{Tool: "/opt/bin/claude"}, nil, nil})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opt, err := hostedChooseInstall(c.check, c.got)
			if c.refusal != nil {
				if err == nil {
					t.Fatalf("no refusal; chose %+v", opt)
				}
				for _, want := range c.refusal {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("refusal %q lacks %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case c.argv == nil && opt != nil:
				t.Fatalf("installs %v, want none", opt.Argv)
			case c.argv != nil && (opt == nil || !slices.Equal(opt.Argv, c.argv)):
				t.Fatalf("chose %+v, want %v", opt, c.argv)
			}
		})
	}
}

// TestHostedToolInstallSourcesStayGrounded: every install path is the
// registry's own argv, or its curl fetch swapped for wget's from the same
// https address; the probe's arguments are registry names only (no
// environment, no key, nothing the launch carries); and a script that
// needs more than the curl that fetches it gets no wget path.
func TestHostedToolInstallSourcesStayGrounded(t *testing.T) {
	spec, _ := integration.For("claude-code")
	registry := map[string]bool{}
	for _, h := range spec.Binary.Installs {
		registry[strings.Join(h.Argv, "\x00")] = true
	}
	c := hostedToolCheckFor("claude-code", vocab.TierInternet, "")
	if len(c.Installs) != 3 || c.Installs[0].Channel != "npm" || c.Installs[1].Fetch != "curl" || c.Installs[2].Fetch != "wget" {
		t.Fatalf("install order %+v", c.Installs)
	}
	for _, o := range c.Installs {
		if o.Fetch == "wget" {
			if !strings.HasPrefix(o.Argv[2], "wget -qO- https://claude.ai/install.sh |") {
				t.Errorf("wget path %q", o.Argv[2])
			}
			continue
		}
		if !registry[strings.Join(o.Argv, "\x00")] {
			t.Errorf("%v is not a registry argv", o.Argv)
		}
	}
	argv := hostedToolProbeArgv(*c)
	if !slices.Equal(argv[:5], []string{"sh", "-c", hostedToolProbeScript, "sh", "claude"}) {
		t.Fatalf("probe argv %q", argv)
	}
	for _, a := range argv[4:] {
		if a != "--" && !hostedProbeWordRE.MatchString(a) && !hostedProbeDirRE.MatchString(a) {
			t.Errorf("probe argument %q is not a registry word", a)
		}
	}
	for _, want := range []string{"npm", "bash", "curl", "wget", ".local/bin"} {
		if !slices.Contains(argv, want) {
			t.Errorf("probe argv lacks %q: %q", want, argv)
		}
	}
	// opencode's script also needs tar: no wget path for it.
	for _, o := range hostedToolCheckFor("opencode", vocab.TierInternet, "").Installs {
		if o.Fetch == "wget" {
			t.Errorf("a script needing more than curl got a wget path: %v", o.Argv)
		}
	}
}

// TestHostedToolProbeScriptRuns runs the probe in a real shell: a box with
// only curl says so; the tool on PATH, or under a probe directory of the
// box home, is found and ends the probe.
func TestHostedToolProbeScriptRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a POSIX shell")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	home, bin := t.TempDir(), t.TempDir()
	mk := func(p string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk(filepath.Join(bin, "curl"))
	mk(filepath.Join(bin, "bash"))
	c := hostedToolCheckFor("claude-code", vocab.TierInternet, "")
	run := func() hostedBoxTools {
		argv := hostedToolProbeArgv(*c)
		cmd := exec.Command(sh, argv[1:]...) //nolint:gosec // G204: the probe under test, in temp dirs
		cmd.Env = []string{"HOME=" + home, "PATH=" + bin}
		cmd.Dir = home
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("probe: %v %s", err, out)
		}
		return parseHostedToolProbe(out)
	}
	got := run()
	if got.Tool != "" || !got.Has["curl"] || got.Has["npm"] || got.Has["wget"] {
		t.Fatalf("only curl: %+v", got)
	}
	if opt, err := hostedChooseInstall(*c, got); err != nil || opt == nil || opt.Fetch != "curl" {
		t.Fatalf("only curl chose %+v %v", opt, err)
	}
	mk(filepath.Join(home, ".local", "bin", "claude"))
	if got = run(); got.Tool != filepath.Join(home, ".local", "bin", "claude") {
		t.Fatalf("under the home: %+v", got)
	}
	mk(filepath.Join(bin, "claude"))
	if got = run(); got.Tool != filepath.Join(bin, "claude") {
		t.Fatalf("on PATH: %+v", got)
	}
}

// TestHostedEndNoteRoundTrip: the helper's refusal note, keyed by the
// sandbox run's own id (the only id the helper knows), reads back once as
// launch_refused with the refusal as its detail when the daemon asks with
// both ids; asked by terminal_run.run_id alone (the live gap, 2026-11-11)
// it is not found; a bad run id or a clean exit reads nothing.
func TestHostedEndNoteRoundTrip(t *testing.T) {
	obs := t.TempDir()
	writeHostedEndNote(obs, "ws-n1", "launch_refused", "tool_unavailable_in_image: claude-code is not installed in the box")
	if got := runEndNoteReason(obs, "run-n1", "ws-n1", 0); got != "" {
		t.Errorf("a clean exit read %q", got)
	}
	if got := runEndNoteReason(obs, "run-n1", "", 125); got != "" {
		t.Errorf("an unsandboxed run read the sandbox run's note: %q", got)
	}
	if got := runEndNoteReason(obs, "run-n1", "ws-n1", 125); got != "launch_refused: tool_unavailable_in_image: claude-code is not installed in the box" {
		t.Errorf("note = %q", got)
	}
	if got := runEndNoteReason(obs, "run-n1", "ws-n1", 125); got != "" {
		t.Errorf("read twice: %q", got)
	}
	writeHostedEndNote(obs, "../x", "launch_refused", "x")
	if got := readHostedEndNote(obs, "../x"); got != "" {
		t.Errorf("a bad run id read %q", got)
	}
}

// TestHostedScriptInstallFailsWhenTheFetchFails: a piped vendor script runs
// under pipefail, so a refused download is a failed install, not bash's 0.
func TestHostedScriptInstallFailsWhenTheFetchFails(t *testing.T) {
	for _, o := range hostedToolInstallOptions([]integration.InstallHint{{
		OS: "linux", Channel: "script", Argv: []string{"bash", "-lc", "curl -fsSL https://example.invalid/install.sh | bash"},
		Display: "curl -fsSL https://example.invalid/install.sh | bash", Requires: []string{"curl"},
	}}) {
		if got := hostedPipefail(o.Argv); len(got) != 3 || !strings.HasPrefix(got[2], "set -o pipefail; ") {
			t.Errorf("%s option runs without pipefail: %q", o.Fetch, got)
		}
	}
	got := hostedPipefail([]string{"bash", "-lc", "false | true"})
	if out, err := exec.Command(got[0], got[1:]...).CombinedOutput(); err == nil {
		t.Errorf("a failed fetch passed as success under %q (%s)", got, out)
	}
	if got := hostedPipefail([]string{"npm", "install", "-g", "x"}); strings.Join(got, " ") != "npm install -g x" {
		t.Errorf("non-pipe argv changed: %q", got)
	}
}

// TestHostedToolRunScriptRuns runs the installed tool's start check with
// the host's sh (live run 2026-11-12: a tool npm placed without its native
// binary): a tool that starts exits 0; one that fails gives its own words,
// the home's free room and its status; the words and room parse back.
func TestHostedToolRunScriptRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a POSIX shell")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	home := t.TempDir()
	tool := filepath.Join(home, "claude")
	run := func(body string) ([]byte, int) {
		if err := os.WriteFile(tool, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
		argv := hostedToolRunArgv(tool)
		cmd := exec.Command(sh, argv[1:]...) //nolint:gosec // G204: the check under test, in a temp dir
		cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
		out, err := cmd.Output()
		var ee *exec.ExitError
		switch {
		case err == nil:
			return out, 0
		case errors.As(err, &ee):
			return out, ee.ExitCode()
		}
		t.Fatalf("run check: %v", err)
		return nil, 0
	}
	if out, code := run("echo 2.1.322; exit 0\n"); code != 0 || strings.Contains(string(out), "2.1.322") {
		t.Fatalf("a tool that starts: exit %d, output %q (its version is not repeated)", code, out)
	}
	out, code := run("echo 'Error: claude native binary not installed.' >&2; exit 1\n")
	if code != 1 {
		t.Fatalf("exit %d, output %q", code, out)
	}
	words, sp := parseHostedToolRun(out)
	if words != "Error: claude native binary not installed." {
		t.Errorf("words %q", words)
	}
	if _, err := exec.LookPath("df"); err == nil && (sp.SizeKiB <= 0 || sp.FreeKiB < 0) {
		t.Errorf("no space line in %q", out)
	}
}

// TestHostedSpaceNote: the run check's output parses into the tool's words
// and the store's room; a nearly full store is named as the likely cause.
func TestHostedSpaceNote(t *testing.T) {
	words, sp := parseHostedToolRun([]byte("Error: claude native binary not installed.\n\nOr reinstall.\nspace 9216 524288\n"))
	if words != "Error: claude native binary not installed. ... Or reinstall." || sp != (hostedSpace{9216, 524288}) {
		t.Fatalf("words %q space %+v", words, sp)
	}
	if n := sp.note(""); !strings.Contains(n, "9 MiB free of 512 MiB") || !strings.Contains(n, "ran out of room") {
		t.Errorf("full store: %q", n)
	}
	if n := (hostedSpace{400 << 10, 512 << 10}).note(""); n != "the box home's store has 400 MiB free of 512 MiB" {
		t.Errorf("roomy store: %q", n)
	}
	if n := (hostedSpace{}).note(""); n != "" || hostedWithNote(n) != "" {
		t.Errorf("unknown space: %q", n)
	}
	// npm let the optional native binary fail on ENOSPC, exited 0 and
	// removed what it wrote (live run 2026-11-12): the store looks roomy,
	// the install's own words say it ran out.
	npm := "npm warn tar TAR_ENTRY_ERROR ENOSPC: no space left on device, write\nadded 2 packages in 9s"
	if n := (hostedSpace{332 << 10, 512 << 10}).note(npm); !strings.Contains(n, "ran out of room in the box") || !strings.Contains(n, "332 MiB free of 512 MiB") || !strings.Contains(n, "more storage") {
		t.Errorf("ENOSPC in the install: %q", n)
	}
	if n := (hostedSpace{}).note(npm); !strings.HasPrefix(n, "the install ran out of room in the box: it reported no space left on device; use") {
		t.Errorf("ENOSPC, space unknown: %q", n)
	}
	if _, sp := parseHostedToolRun([]byte("space x 1\nspace 5 0\n")); sp != (hostedSpace{}) {
		t.Errorf("a bad space line parsed: %+v", sp)
	}
}
