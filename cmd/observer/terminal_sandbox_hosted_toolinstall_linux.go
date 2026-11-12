//go:build linux && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
)

// hostedToolProbeLimit bounds the probe's output.
const hostedToolProbeLimit = 4 << 10

// probeTool asks the box what it has (hostedToolProbeScript), as the box
// user with the run's home.
func (r *hostedRun) probeTool(ctx context.Context, c hostedToolCheck) (hostedBoxTools, error) {
	out, code, err := hostedExecOutput(ctx, r.client, r.rec.VendorID, hostedToolProbeArgv(c), r.boxEnv(), hostedToolProbeLimit)
	if err != nil {
		return hostedBoxTools{}, fmt.Errorf("%s: the box could not be asked whether %s is installed: %w", verdictHostedTool, c.Tool, err)
	}
	if code != 0 {
		return hostedBoxTools{}, fmt.Errorf("%s: the box could not be asked whether %s is installed (the probe exited %d)", verdictHostedTool, c.Tool, code)
	}
	return parseHostedToolProbe(out), nil
}

// ensureTool makes sure the agent tool is in the box before the agent
// starts: there already, or installed by the path the box can take, then
// looked for again; otherwise the launch is refused with what the box
// lacks (terminal_sandbox_hosted_toolinstall.go).
func (r *hostedRun) ensureTool(ctx context.Context) error {
	c := r.plan.ToolCheck
	if c == nil || len(c.Names) == 0 {
		return nil
	}
	got, err := r.probeTool(ctx, *c)
	if err != nil {
		return err
	}
	opt, err := hostedChooseInstall(*c, got)
	if err != nil || opt == nil {
		return err
	}
	how := "with npm"
	if opt.Channel == "script" {
		how = "with its vendor's install script"
		if opt.Fetch != "" {
			how += " over " + opt.Fetch
		}
	}
	spec := sandboxremote.ExecSpec{Argv: hostedPipefail(opt.Argv), Cwd: r.layout.Home}
	if opt.Channel == "npm" {
		// npm's global prefix is the image's (/usr/local, root's), and the
		// box runs the agent as its own non-root user: npm install -g there
		// fails with EACCES (live run 2026-11-11, node:22-slim on a SandHold
		// node). The tool goes under the box user's home instead, its bin
		// in ~/.local/bin, which the check looks in (hostedNPMBinDir).
		spec.Env = r.boxEnv()
		spec.Env["NPM_CONFIG_PREFIX"] = path.Join(r.layout.Home, hostedNPMPrefixRel)
		// npm's download cache gets a directory of its own, removed once
		// the install is done: it is as large again as half the package
		// (Claude Code: about 160 MB of cache beside 330 MB installed), and
		// a box whose home is on a small store (SandHold's Shared tier:
		// /tmp and /workspace share at most 512 MiB) would keep it for
		// nothing. A cache the image already had in ~/.npm is left alone.
		spec.Env["NPM_CONFIG_CACHE"] = path.Join(r.layout.Home, hostedNPMCacheRel)
		how += " into " + spec.Env["NPM_CONFIG_PREFIX"]
	}
	r.say("%s is not in the box; installing it %s (first start, internet tier): %s", c.Tool, how, opt.Display)
	installOut, err := r.execOKOut(ctx, "install "+c.Tool, spec)
	if opt.Channel == "npm" {
		if cerr := r.execOK(ctx, "remove npm's install cache", sandboxremote.ExecSpec{Argv: []string{"rm", "-rf", "--", spec.Env["NPM_CONFIG_CACHE"]}, Cwd: r.layout.Home}); cerr != nil {
			r.say("npm's install cache stays in the box (%s): %s", spec.Env["NPM_CONFIG_CACHE"], hostedLastLine(cerr.Error()))
		}
	}
	if err != nil {
		return fmt.Errorf("%s: %s is not installed in the box, and its install (%s) failed: %s%s", verdictHostedTool, c.Tool, opt.Display, hostedLastLine(err.Error()), hostedWithNote(r.boxSpace(ctx).note(installOut)))
	}
	if got, err = r.probeTool(ctx, *c); err != nil {
		return err
	}
	if got.Tool == "" {
		return fmt.Errorf("%s: %s's install (%s) finished, but %s is still not found in the box (on PATH or under %s)%s", verdictHostedTool, c.Tool, opt.Display, c.Tool, r.layout.Home, hostedWithNote(r.boxSpace(ctx).note(installOut)))
	}
	if err := r.checkToolRuns(ctx, *c, opt.Display, got.Tool, installOut); err != nil {
		return err
	}
	r.say("%s installed at %s", c.Tool, got.Tool)
	return nil
}

// hostedToolRunWait bounds the installed tool's first start.
var hostedToolRunWait = 2 * time.Minute

// checkToolRuns starts the tool an install just placed, once (--version),
// so a tool that cannot start refuses the launch with its own words and the
// box's free room, instead of the agent exiting at once with no word of why
// (hostedToolRunScript).
// installOut is the install's own output, read for a write that ran out of
// room.
func (r *hostedRun) checkToolRuns(ctx context.Context, c hostedToolCheck, display, at, installOut string) error {
	rctx, cancel := context.WithTimeout(ctx, hostedToolRunWait)
	defer cancel()
	out, code, err := hostedExecOutput(rctx, r.client, r.rec.VendorID, hostedToolRunArgv(at), r.boxEnv(), hostedToolProbeLimit)
	if err != nil {
		return fmt.Errorf("%s: %s was installed at %s, but the box could not be asked whether it starts: %w", verdictHostedTool, c.Tool, at, err)
	}
	if code == 0 {
		return nil
	}
	words, sp := parseHostedToolRun(out)
	if words == "" {
		words = "no output"
	}
	// The cause comes before the tool's words: a run's end note is bounded.
	why := ""
	if n := sp.note(installOut); n != "" {
		why = ": " + n
	}
	return fmt.Errorf("%s: %s was installed at %s (%s), but it does not start%s (`%s --version` exited %d: %s)", verdictHostedTool, c.Tool, at, display, why, path.Base(at), code, sandboxremote.Redact(words, r.cred.Secrets()))
}

// boxSpace asks the box how much room its home has (zero when it does not
// say).
func (r *hostedRun) boxSpace(ctx context.Context) hostedSpace {
	out, code, err := hostedExecOutput(ctx, r.client, r.rec.VendorID, hostedBoxSpaceArgv(), r.boxEnv(), 256)
	if err != nil || code != 0 {
		return hostedSpace{}
	}
	_, sp := parseHostedToolRun(out)
	return sp
}
