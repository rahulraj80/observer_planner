//go:build linux && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"context"
	"fmt"
	"strings"

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
	r.say("%s is not in the box; installing it %s (first start, internet tier): %s", c.Tool, how, opt.Display)
	if err := r.execOK(ctx, "install "+c.Tool, sandboxremote.ExecSpec{Argv: opt.Argv, Cwd: r.layout.Home}); err != nil {
		return fmt.Errorf("%s: %s is not installed in the box, and its install (%s) failed: %s", verdictHostedTool, c.Tool, opt.Display, hostedLastLine(err.Error()))
	}
	if got, err = r.probeTool(ctx, *c); err != nil {
		return err
	}
	if got.Tool == "" {
		return fmt.Errorf("%s: %s's install (%s) finished, but %s is still not found in the box (on PATH or under %s)", verdictHostedTool, c.Tool, opt.Display, c.Tool, r.layout.Home)
	}
	r.say("%s installed at %s", c.Tool, got.Tool)
	return nil
}

// hostedLastLine is s cut to its first and last non-empty lines, for a
// bounded refusal (an install's output can run to many lines).
func hostedLastLine(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	switch len(lines) {
	case 0:
		return ""
	case 1:
		return lines[0]
	}
	return lines[0] + " ... " + lines[len(lines)-1]
}
