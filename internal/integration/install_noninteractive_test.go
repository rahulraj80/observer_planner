package integration

import (
	"strings"
	"testing"
)

// nonInteractiveInstallers lists vendor install scripts that end in an
// INTERACTIVE step unless an env opt-out is set. A guided install runs in a
// setup PTY whose exit settles the install record; an installer that stops at
// a prompt keeps the PTY (and the record) open forever. One row per script.
var nonInteractiveInstallers = []struct {
	tool, scriptMarker string
	// unixOptOut must appear in a unix `curl | ... bash` argv; windowsOptOut
	// in a PowerShell argv.
	unixOptOut, windowsOptOut string
}{
	// goose's download_cli.{sh,ps1} run `goose configure` unless
	// CONFIGURE=false (demo node 1, 2026-10-03: the unix hint lacked it, the
	// setup PTY sat in `goose configure`, the record stayed "installing").
	{"goose", "download_cli", "| CONFIGURE=false bash", "$env:CONFIGURE='false'"},
	// T-034j sweep (2026-10-07, each vendor script re-read): hermes ends in
	// `hermes setup` + `hermes gateway install`; openclaw runs
	// `openclaw onboard`; open-interpreter and codex ask "Start ... now?";
	// prime-agent asks to install and to set up its Python kernel runtime.
	{"hermes", "hermes-agent.nousresearch.com/install", "| bash -s -- --non-interactive", "))) -NonInteractive"},
	{"openclaw", "openclaw.ai/install", "| bash -s -- --no-onboard --no-prompt", "$env:OPENCLAW_NO_ONBOARD='1'"},
	{"open-interpreter", "openinterpreter.com/install", "| OPEN_INTERPRETER_NONINTERACTIVE=1 sh", "$env:OPEN_INTERPRETER_NONINTERACTIVE='1'"},
	{"codex", "codex/install.ps1", "", "$env:CODEX_NON_INTERACTIVE='1'"},
	{"prime-agent", "prime-agent/install.sh", "| PRIME_AGENT_INSTALLER_NONINTERACTIVE=1 PRIME_AGENT_BOOTSTRAP_KERNEL_ON_INSTALL=1 sh", ""},
}

func TestScriptInstallsAreNonInteractive(t *testing.T) {
	for _, row := range nonInteractiveInstallers {
		c, ok := For(row.tool)
		if !ok || c.Binary == nil {
			t.Fatalf("%s: no registry row with install hints", row.tool)
		}
		matched := 0
		for _, h := range c.Binary.Installs {
			joined := strings.Join(h.Argv, " ")
			if !strings.Contains(joined, row.scriptMarker) {
				continue
			}
			matched++
			want := row.unixOptOut
			if h.OS == "windows" {
				want = row.windowsOptOut
			}
			if !strings.Contains(joined, want) {
				t.Errorf("%s %s/%s install runs %q without %q: it stops at an interactive prompt in the setup PTY",
					row.tool, h.OS, h.Channel, joined, want)
			}
			if !strings.Contains(h.Display, want) {
				t.Errorf("%s %s/%s Display %q does not show %q (it must match what runs)", row.tool, h.OS, h.Channel, h.Display, want)
			}
		}
		if matched == 0 {
			t.Errorf("%s: no install hint runs %s; update this table", row.tool, row.scriptMarker)
		}
	}
}
