// runtime_preflight.go — the ONE seam that checks a tool's grounded minimum
// host runtime (integration.BinaryResolveSpec.Runtime) before Observer installs
// or launches it.
//
// Why: an npm-distributed CLI runs on the host's own `node`. Live 2026-10-04 on
// demo node 1 the dashboard's guided install put Gemini CLI 0.62.0 (engines
// node >= 20) on a host with Node.js 18.19.1, and `observer gemini` then died
// at startup with "SyntaxError: Invalid regular expression flags". Neither the
// install nor the launch looked at the runtime version.
//
// Every surface goes through the two functions below, so they say one thing:
//   - launchRuntimeCheck: resolveToolBin (every `observer <tool>` launcher, and
//     so every dashboard New Terminal, which re-enters the launcher), the
//     dashboard preflight (toolPreflightSeam) and `observer doctor <tool>`.
//   - installRuntimeCheck: the guided-install plan (preflight CanInstall,
//     POST /api/terminal/install's 409 via toolInstallBlockedSeam) and doctor.
//
// Capability-driven: it dispatches on the registry row's Runtime data and on
// the install channel's HostRuntime column, never on a tool name. Only the
// `--version` probe is I/O; the decision is internal/toolinstall.CheckRuntime.

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/toolinstall"
	"github.com/marmutapp/superbased-observer/internal/toolresolve"
)

// runtimeProbeTimeout bounds one `<interpreter> --version` run.
const runtimeProbeTimeout = 3 * time.Second

// execRuntimeVersion runs `<path> --version` in dir ("" = this process's
// working directory) and returns its output. A package var so tests can drive
// the check without a real interpreter.
var execRuntimeVersion = func(path, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runtimeProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version") //nolint:gosec // path is a resolved interpreter, argv is constant
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

// launchDirHere is the directory a tool launched by THIS process runs in: the
// launcher and `observer doctor` start the tool in their own working
// directory. "" when it cannot be read.
func launchDirHere() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// runtimeProbeCache memoizes a probe per interpreter file identity (path, size,
// mtime) and probe directory, so a preflight poll does not spawn node again
// until the file changes (an upgrade replaces it).
var runtimeProbeCache = struct {
	sync.Mutex
	m map[string]runtimeProbeEntry
}{m: map[string]runtimeProbeEntry{}}

type runtimeProbeEntry struct {
	key string
	out string
	err error
}

// observeRuntime probes the interpreter at path, running it in launchDir: the
// directory the tool itself would run in, "" when the caller does not know it
// (the daemon's dashboard preflight and guided install). "" path yields an
// empty observation (nothing resolves), which CheckRuntime reports as missing.
//
// Review M3-18: a version-manager shim (asdf, mise, nodenv, Volta) picks the
// real node from the directory it runs in, so probing it from the daemon's
// directory answers for the wrong place. Such a shim with no known launch
// directory is not probed at all and is reported DirDependent (unknown, never
// blocking); the launcher checks again where the tool actually starts.
func observeRuntime(path, launchDir string) toolinstall.RuntimeObservation {
	if strings.TrimSpace(path) == "" {
		return toolinstall.RuntimeObservation{}
	}
	if launchDir == "" && toolinstall.DirDependentInterpreter(path) {
		return toolinstall.RuntimeObservation{Path: path, DirDependent: true}
	}
	key := path
	if fi, err := os.Stat(path); err == nil {
		key = fmt.Sprintf("%s|%d|%d", path, fi.Size(), fi.ModTime().UnixNano())
	}
	slot := path + "|" + launchDir
	runtimeProbeCache.Lock()
	e, ok := runtimeProbeCache.m[slot]
	runtimeProbeCache.Unlock()
	if !ok || e.key != key {
		out, err := execRuntimeVersion(path, launchDir)
		e = runtimeProbeEntry{key: key, out: out, err: err}
		runtimeProbeCache.Lock()
		runtimeProbeCache.m[slot] = e
		runtimeProbeCache.Unlock()
	}
	return toolinstall.RuntimeObservation{Path: path, Output: e.out, ProbeErr: e.err}
}

// launchRuntimeCheck evaluates spec's runtime requirement for a launch of the
// resolved binary res. ok is false when nothing applies: no grounded
// requirement, or the resolved binary does not run on that runtime (a native
// binary, or a shim for another interpreter). The interpreter checked is the
// one the launcher's composed child PATH resolves (toolresolve.
// ChildInterpreterPath), never a guess, probed in launchDir (see
// observeRuntime; "" = not known here).
func launchRuntimeCheck(spec *integration.BinaryResolveSpec, res toolresolve.Resolution, env toolresolve.Env, goos, launchDir string) (toolinstall.RuntimeCheck, bool) {
	if spec == nil || spec.Runtime == nil || res.Bin == "" || res.Interpreter == "" {
		return toolinstall.RuntimeCheck{}, false
	}
	if res.Interpreter != toolinstall.RuntimeProgram(spec.Runtime.Runtime, goos) {
		return toolinstall.RuntimeCheck{}, false
	}
	req, fromInstalled := installedRequirement(*spec.Runtime, res, env)
	obs := observeRuntime(toolresolve.ChildInterpreterPath(res, env), launchDir)
	c := toolinstall.CheckRuntime(req, obs, goos, hostPackageManager())
	if fromInstalled {
		c.FloorFrom = toolinstall.FloorFromInstalled
	}
	return c, true
}

// packageJSONHeadBytes bounds the installed package.json read. A real npm
// package.json is a few KiB; a truncated read fails to parse and falls back
// to the registry floor.
const packageJSONHeadBytes = 256 << 10

// installedRequirement swaps the registry floor for the INSTALLED package's
// own engines.node when the resolved binary is an npm package whose
// package.json declares a parseable range with a lower bound: that is exact
// for the version actually on disk (a newer release may raise its floor
// before the registry is re-grounded). Otherwise req is returned unchanged
// (absent, unparseable, or a range with no floor) and ok is false.
func installedRequirement(req integration.RuntimeRequirement, res toolresolve.Resolution, env toolresolve.Env) (integration.RuntimeRequirement, bool) {
	root := toolresolve.NPMPackageRoot(env, res.Bin)
	if root == "" || env.ReadHead == nil {
		return req, false
	}
	body, err := env.ReadHead(filepath.Join(root, "package.json"), packageJSONHeadBytes)
	if err != nil {
		return req, false
	}
	floor, rng, ok := toolinstall.InstalledEngines(body)
	if !ok {
		return req, false
	}
	req.MinVersion = floor
	req.Range = rng
	req.Source = "installed " + filepath.Join(root, "package.json") + " engines.node"
	return req, true
}

// installRuntimeCheck evaluates spec's runtime requirement for a guided install
// on channel. ok is false when the channel does not run the tool on the host
// runtime (a vendor script, brew, winget, ...) or nothing is grounded. The
// interpreter checked is the one a later launch of the installed shim would
// use (the child PATH order, with no shim dir yet), probed in launchDir (see
// observeRuntime; "" = not known here).
func installRuntimeCheck(spec *integration.BinaryResolveSpec, channel string, env toolresolve.Env, goos, launchDir string) (toolinstall.RuntimeCheck, bool) {
	if spec == nil || spec.Runtime == nil {
		return toolinstall.RuntimeCheck{}, false
	}
	if toolinstall.ChannelHostRuntime(channel) != spec.Runtime.Runtime {
		return toolinstall.RuntimeCheck{}, false
	}
	program := toolinstall.RuntimeProgram(spec.Runtime.Runtime, goos)
	obs := observeRuntime(toolresolve.FindInChildPath(env, "", program), launchDir)
	return toolinstall.CheckRuntime(*spec.Runtime, obs, goos, hostPackageManager()), true
}

// installRuntimeFor is installRuntimeCheck for a guided-install plan of the
// registry tool id (GUI rows carry no runtime requirement and report false).
func installRuntimeFor(id string, plan dashboardInstallPlan) (toolinstall.RuntimeCheck, bool) {
	ic, ok := integration.For(id)
	if !ok || ic.Binary == nil {
		return toolinstall.RuntimeCheck{}, false
	}
	return installRuntimeCheck(ic.Binary, plan.Channel, dashResolveEnv(), runtime.GOOS, "")
}

// runtimeDoctorLine is `observer doctor <tool>`'s runtime line: the launch
// check when the tool resolves, otherwise the guided-install check. ok is
// false when the tool has no grounded requirement. blocking marks a line the
// operator must act on before the tool can start (or be installed from the
// dashboard); its fix rides in details, the command on a line of its own.
func runtimeDoctorLine(tool string) (msg string, details []string, blocking, ok bool) {
	ic, found := integration.For(tool)
	if !found || ic.Binary == nil || ic.Binary.Runtime == nil {
		return "", nil, false, false
	}
	req := ic.Binary.Runtime
	env := dashResolveEnv()
	r := toolresolve.Resolve(*ic.Binary, env)
	need := fmt.Sprintf("needs %s %s or newer", toolinstall.RuntimeDisplay(req.Runtime), toolinstall.DisplayRuntimeMin(req.MinVersion))
	switch r.Verdict {
	case toolresolve.VerdictOK, toolresolve.VerdictOKOffPath, toolresolve.VerdictShadowed:
		c, applies := launchRuntimeCheck(ic.Binary, r, env, runtime.GOOS, launchDirHere())
		if !applies {
			return runtimeNotApplicable(filepath.Base(r.Bin), r.Interpreter, *req), nil, false, true
		}
		return runtimeNeed(c) + "; " + runtimeStatusPhrase(c, "the launch"), runtimeFixDetails(c), c.Blocking(), true
	}
	plan, planned := dashboardInstallPlanFor(ic.Binary.Installs, runtime.GOOS, dashboardInstallHome(), dashInstallArgv0)
	if !planned {
		return need + "; not installed", nil, false, true
	}
	c, applies := installRuntimeCheck(ic.Binary, plan.Channel, env, runtime.GOOS, launchDirHere())
	if !applies {
		return need + " (not installed; the guided install uses " + plan.Channel + ", which brings its own runtime)", nil, false, true
	}
	return runtimeNeed(c) + "; not installed, and " + runtimeStatusPhrase(c, "the guided install ("+plan.Channel+")"), runtimeFixDetails(c), c.Blocking(), true
}

// runtimeNotApplicable is doctor's runtime line when the installed binary
// does not run on the requirement's runtime (T-367): a native binary needs no
// interpreter at all, and a script for another interpreter is not the one
// the requirement names. Either way it says so plainly; the requirement
// applies to the npm install only.
func runtimeNotApplicable(bin, interpreter string, req integration.RuntimeRequirement) string {
	display := toolinstall.RuntimeDisplay(req.Runtime)
	npmOnly := fmt.Sprintf("(%s %s or newer applies to its npm install only)", display, toolinstall.DisplayRuntimeMin(req.MinVersion))
	if interpreter == "" {
		return fmt.Sprintf("the installed %s is a native binary, so it needs no %s check %s", bin, display, npmOnly)
	}
	return fmt.Sprintf("the installed %s runs on %s, not %s, so no %s check applies %s", bin, interpreter, display, display, npmOnly)
}

// runtimeNeed is "needs Node.js 20 or newer" for one evaluated check, naming
// the installed package as the source when its own engines field set it.
func runtimeNeed(c toolinstall.RuntimeCheck) string {
	need := fmt.Sprintf("needs %s %s or newer", c.Display, c.Min)
	if c.FloorFrom == toolinstall.FloorFromInstalled {
		need += " (from the installed package)"
	}
	return need
}

// runtimeStatusPhrase renders one RuntimeCheck for doctor's message line.
func runtimeStatusPhrase(c toolinstall.RuntimeCheck, subject string) string {
	switch c.Status {
	case toolinstall.RuntimeOK:
		return fmt.Sprintf("%s runs on %s %s (%s): ok", subject, c.Display, c.Have, c.Path)
	case toolinstall.RuntimeTooOld:
		return fmt.Sprintf("%s runs on %s %s (%s): too old", subject, c.Display, c.Have, c.Path)
	case toolinstall.RuntimeOutsideRange:
		return fmt.Sprintf("%s runs on %s %s (%s): outside the declared range %s, so it may fail to start; not blocked", subject, c.Display, c.Have, c.Path, c.Range)
	case toolinstall.RuntimeMissing:
		return fmt.Sprintf("%s finds no %s on its PATH", subject, c.Display)
	default:
		if c.DirDependent {
			return fmt.Sprintf("%s runs on %s, a version-manager shim whose version depends on the directory; checked when the tool starts", subject, c.Path)
		}
		return fmt.Sprintf("%s runs on %s, whose version could not be read; not checked", subject, c.Path)
	}
}

// runtimeFixDetails is doctor's detail lines for a too_old check: the lead-in,
// the command alone on its own line, then the note. nil otherwise.
func runtimeFixDetails(c toolinstall.RuntimeCheck) []string {
	if !c.Blocking() {
		return nil
	}
	if c.FixCommand == "" {
		return []string{"Install a newer " + c.Display + " first. " + c.FixNote}
	}
	out := []string{"Install a newer " + c.Display + " first:", "  " + c.FixCommand}
	if c.FixNote != "" {
		out = append(out, c.FixNote)
	}
	return out
}
