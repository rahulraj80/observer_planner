//go:build !sbo_omit_integration_shell_wrap

// shellwrap_bridge.go - the WSL side of the Windows shell-wrap bridge (T-419).
//
// A Windows bridge shim (internal/shellwrap.RenderBridgeCmdShim) for a tool
// that ships a Windows build runs that Windows program AS Windows, after
// asking this process, through `wsl.exe -d <distro> -e <observer> shell-wrap
// launch-env <id>`, for the routing environment the wrapped form applies.
// launch-env is that answer: the same pre-launch gates the in-distro launchers
// run (the org tools.disallow list, managed-budget admission) and the
// base-URL variables shellwrap.BridgeEnvFor names, pointed at this daemon's
// proxy (WSL forwards localhost, so the Windows program reaches it at the same
// URL). It never starts the tool and never reads a credential.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/shellwrap"
	"github.com/marmutapp/superbased-observer/internal/toolresolve"
)

// The launch-env protocol: one line each, space-separated, on stdout. The
// shim reads them with `for /f "tokens=1,2,*"` and sets only the variables
// it was rendered with; diagnostics go to stderr (shown on the Windows
// console).
const (
	launchEnvLine = "SBO_ENV"  // SBO_ENV <NAME> <value>
	launchEnvOK   = "SBO_OK"   // the answer is complete; run the program
	launchEnvDeny = "SBO_DENY" // a gate refused; the shim exits 1
)

// launchEnvDeps are the gates and probes launch-env calls (production: the
// same functions the launchers call; tests substitute them).
type launchEnvDeps struct {
	refuseDisallowed func(dbPath, tool string, stderr io.Writer) error
	admit            func(ctx context.Context, configPath, tool string, evidence budgetLaunchEvidence) error
	reachable        func(proxyURL string, timeout time.Duration) bool
	environ          func() []string
	// orgGovernedWithoutConfig reports, with NO config loaded, whether this
	// node carries an organization's posture (P3-09). Production:
	// defaultOrgGovernedWithoutConfig.
	orgGovernedWithoutConfig func() bool
}

func defaultLaunchEnvDeps() launchEnvDeps {
	return launchEnvDeps{
		refuseDisallowed:         refuseIfToolDisallowedDB,
		admit:                    enforceBudgetControlledLaunch,
		reachable:                proxyReachable,
		environ:                  os.Environ,
		orgGovernedWithoutConfig: defaultOrgGovernedWithoutConfig,
	}
}

// defaultOrgGovernedWithoutConfig is the config-free "is this node governed
// by an organization" probe: the resolved governance sidecar or the
// node.features sidecar exists beside the DEFAULT database path. Both are
// written only by an enrolled daemon and removed on unenrol, and both sit
// beside the DB precisely so a reader can find them without resolving an org
// key. A node whose broken config also moved the DB elsewhere is not seen
// here (it degrades to the unrouted answer, as before).
func defaultOrgGovernedWithoutConfig() bool {
	def := config.Default()
	for _, p := range []string{
		config.ResolveGovernanceSidecarPath(def, ""),
		config.ResolveFeaturesSidecarPath(def, ""),
	} {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// launchEnvValueSafe reports whether a value can be handed to a cmd.exe
// `set "NAME=value"` through a `for /f` token: no whitespace (it would split
// the token), no quote and no cmd metacharacter.
func launchEnvValueSafe(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if r <= ' ' || r == 0x7f || strings.ContainsRune(`"%!^&|<>()`, r) {
			return false
		}
	}
	return true
}

// runShellWrapLaunchEnv answers one bridge shim. It always ends in exactly
// one of SBO_OK / SBO_DENY on stdout; anything it cannot work out degrades to
// an unrouted SBO_OK with a note (never locked out), except a gate refusal.
func runShellWrapLaunchEnv(ctx context.Context, stdout, stderr io.Writer, configPath, id string, deps launchEnvDeps) error {
	vars := shellwrap.BridgeEnvFor(id)
	gateTool, isTool := id, true
	if _, ok := integration.For(id); !ok {
		isTool = false
		g, ok := integration.GUILaunchFor(id)
		if !ok {
			fmt.Fprintf(stderr, "observer shell-wrap: %q is not a wrappable id (the shim is stale - run `observer shell-wrap enable --target windows`); running it unrouted\n", id)
			fmt.Fprintln(stdout, launchEnvOK)
			return nil
		}
		gateTool = g.Adapter
	}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: configPath})
	if err != nil {
		// FAIL CLOSED on an org-governed node (P3-09): with no config the
		// org's tools.disallow list and managed-budget admission cannot be
		// checked, and the in-distro launchers refuse in the same state. An
		// unmanaged node keeps the never-locked-out unrouted answer.
		if deps.orgGovernedWithoutConfig != nil && deps.orgGovernedWithoutConfig() {
			fmt.Fprintf(stderr, "observer shell-wrap: config unreadable (%v); this node is governed by an organization, so %s is refused until config.toml loads again (its tool list and budget cannot be checked)\n", err, id)
			fmt.Fprintln(stdout, launchEnvDeny)
			return nil
		}
		fmt.Fprintf(stderr, "observer shell-wrap: config unreadable (%v); running %s unrouted\n", err, id)
		fmt.Fprintln(stdout, launchEnvOK)
		return nil
	}
	if gateTool != "" {
		if err := deps.refuseDisallowed(cfg.Observer.DBPath, gateTool, stderr); err != nil {
			fmt.Fprintln(stdout, launchEnvDeny)
			return nil
		}
	}
	proxyURL := resolveProxyURL(cfg.Proxy.Port, "")
	inject := map[string]string{}
	for _, v := range vars {
		inject[v.Name] = proxyURL + v.Suffix
	}
	if len(inject) > 0 && !deps.reachable(proxyURL, 250*time.Millisecond) {
		// A Windows program pointed at a proxy that is not listening fails
		// every request; launching it unrouted is the never-locked-out answer.
		fmt.Fprintf(stderr, "observer %s: the observer proxy is not reachable at %s from WSL - running the Windows program unrouted (start the daemon: observer start)\n", id, proxyURL)
		inject = map[string]string{}
	}
	childEnv, applied, presets := applyBaseURLEnv(deps.environ(), inject)
	evidence := guiBudgetEvidence(id)
	if isTool {
		evidence = envBudgetLaunchEvidence(id, proxyURL, inject, childEnv, nil)
	}
	if err := deps.admit(ctx, configPath, id, evidence); err != nil {
		msg := err.Error()
		var visible launcherMessageError
		if errors.As(err, &visible) {
			msg = visible.launcherMessage()
		}
		fmt.Fprintln(stderr, msg)
		fmt.Fprintln(stdout, launchEnvDeny)
		return nil
	}
	for _, k := range presets {
		fmt.Fprintf(stderr, "observer %s: %s already set in env; using yours.\n", id, k)
	}
	var set []string
	for _, k := range applied {
		v := inject[k]
		if !launchEnvValueSafe(v) {
			fmt.Fprintf(stderr, "observer %s: %s=%q cannot be handed to a Windows shell safely - not set\n", id, k, v)
			continue
		}
		fmt.Fprintf(stdout, "%s %s %s\n", launchEnvLine, k, v)
		set = append(set, k)
	}
	if len(set) > 0 {
		fmt.Fprintf(stderr, "observer %s: routing the Windows program via %s (set %s)\n", id, proxyURL, strings.Join(set, ", "))
	}
	fmt.Fprintln(stdout, launchEnvOK)
	return nil
}

// newShellWrapLaunchEnvCmd is the hidden `observer shell-wrap launch-env`.
func newShellWrapLaunchEnvCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:    "launch-env <tool-id>",
		Short:  "Print the routing environment for a Windows bridge shim (internal; called through wsl.exe)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShellWrapLaunchEnv(cmdContext(cmd), cmd.OutOrStdout(), cmd.ErrOrStderr(), *configPath, args[0], defaultLaunchEnvDeps())
		},
	}
}

// shellWrapWindowsInstalledSeam reports whether a bridge row is installed on
// the side its shim runs: a Windows install the WSL resolver found over the
// drive mount (a foreign candidate) for SideHost, an in-distro install for
// SideWSL.
func shellWrapWindowsInstalledSeam(env func() toolresolve.Env) func(id string, side shellwrap.Side) bool {
	return func(id string, side shellwrap.Side) bool {
		var r toolresolve.Resolution
		if c, ok := integration.For(id); ok && c.Binary != nil {
			r = toolresolve.Resolve(*c.Binary, env())
		} else if g, ok := integration.GUILaunchFor(id); ok {
			r = toolresolve.ResolveGUI(g.Spec, env())
		} else {
			return false
		}
		if side == shellwrap.SideWSL {
			switch r.Verdict {
			case toolresolve.VerdictOK, toolresolve.VerdictOKOffPath, toolresolve.VerdictShadowed:
				return true
			}
			return false
		}
		for _, c := range r.Considered {
			if c.Foreign {
				return true
			}
		}
		return false
	}
}
