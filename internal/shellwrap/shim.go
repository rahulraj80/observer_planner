package shellwrap

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/shimguard"
)

// ShimMarker is carried on the second line of every shim. It is how a shim
// recognizes ANOTHER shim while walking PATH for the real command, how the
// observer launchers' resolver skips shims (toolresolve.Env.ExcludeMarker),
// and how the applier recognizes files it owns in the shim directory - it
// never deletes a file that does not carry it.
//
// Its owner is the core package internal/shimguard (lane MP-L1E), which the
// resolver uses in a build without this package.
const ShimMarker = shimguard.ShimMarker

// ShimVersion is the shim template version stamped next to ShimMarker; bump
// it when the template changes shape so status reports older shims as stale.
const ShimVersion = "v1"

// Environment variables a shim reads.
const (
	// BypassEnv, when non-empty, makes every shim run the real command
	// directly - the operator's escape hatch (`SBO_SHIM_BYPASS=1 claude`).
	BypassEnv = "SBO_SHIM_BYPASS" //nolint:gosec // an environment variable NAME, not a credential
	// GuardEnv is exported by a shim (set to its own command name) before it
	// hands over to observer. A shim that finds its own name here runs the
	// real command: observer resolved back into a shim (a recursion the
	// resolver's shim exclusion should already prevent), or the wrapped tool
	// is spawning itself (its child already inherits the routing env).
	GuardEnv = "SBO_SHIM_GUARD"
)

// ShimSpec is everything one shim file needs.
type ShimSpec struct {
	// Name is the replaced command name without any extension ("claude").
	Name string
	// ToolID is the registry tool key or GUI launch id the shim wraps.
	ToolID string
	// Kind is the wrapped form's launch shape.
	Kind integration.LaunchKind
	// Args is the argv after the observer executable (WrappedCommand.Args).
	Args []string
	// ProjectDirArgv (GUI only): a single directory argument is forwarded as
	// the project dir (`code .` -> `observer ide vscode .`).
	ProjectDirArgv bool
	// HandoffSegments (GUI only): when the real command resolves under one of
	// these path segments it is a hand-off client to an already-running
	// instance (VS Code's remote-cli), which a wrapped launch cannot reach -
	// the shim runs it unchanged.
	HandoffSegments []string
	// ObserverPath is the absolute observer executable baked into the shim.
	ObserverPath string
	// ShimDir is the absolute shim directory (skipped when resolving the
	// real command).
	ShimDir string
	// Bridge, when set, renders a WSL bridge shim (RenderBridgeCmdShim):
	// ObserverPath is then the observer INSIDE the distro.
	Bridge *WSLBridge
	// Side is where the real program runs (bridge shims only).
	Side Side
	// Env are the base-URL variables a SideHost bridge shim accepts from
	// `observer shell-wrap launch-env` (BridgeEnvFor).
	Env []EnvVar
	// ConfigPath, when set, is the observer config.toml the shim was enabled
	// with (P3-09): every observer call the shim makes carries
	// `--config <ConfigPath>`, so a shim written under a non-default config
	// keeps that config's proxy port, DB, allowed tools and budget state.
	// Empty means the default config (the observer resolves it itself, and
	// the shim is byte-identical to one written before this field existed).
	ConfigPath string
}

// posixConfigArg is the ` --config '<path>'` suffix a POSIX shim appends to
// the observer argv ("" for the default config).
func (s ShimSpec) posixConfigArg() string {
	if s.ConfigPath == "" {
		return ""
	}
	return " --config " + shQuote(s.ConfigPath)
}

// cmdConfigArg is the native .cmd shim's ` --config "<path>"` suffix.
func (s ShimSpec) cmdConfigArg() string {
	if s.ConfigPath == "" {
		return ""
	}
	return ` --config "` + cmdLiteral(s.ConfigPath) + `"`
}

// bridgeConfigArg is the bridge shim's suffix: the value is set once as
// SBO_CONFIG (validated like the in-distro observer path) and referenced. It
// is spliced in as a format ARGUMENT, so its percent signs are literal.
func (s ShimSpec) bridgeConfigArg() string {
	if s.ConfigPath == "" {
		return ""
	}
	return ` --config "%SBO_CONFIG%"`
}

func (s ShimSpec) validate() error {
	if !ValidCommandName(s.Name) {
		return fmt.Errorf("shellwrap: %q is not a command name a shim can replace", s.Name)
	}
	if len(s.Args) == 0 {
		return fmt.Errorf("shellwrap: %s: no wrapped command", s.ToolID)
	}
	for _, a := range append(append([]string{s.ToolID}, s.Args...), s.HandoffSegments...) {
		if !segmentRE.MatchString(a) {
			return fmt.Errorf("shellwrap: %s: %q cannot be rendered into a shim", s.ToolID, a)
		}
	}
	if err := checkPath(s.ObserverPath); err != nil {
		return err
	}
	if s.ConfigPath != "" {
		if err := checkPath(s.ConfigPath); err != nil {
			return err
		}
	}
	return checkPath(s.ShimDir)
}

// markerLine is the second line of every shim (after the shebang / @echo off).
func (s ShimSpec) markerLine(comment string) string {
	return fmt.Sprintf("%s %s %s tool=%s command=%s", comment, ShimMarker, ShimVersion, s.ToolID, s.Name)
}

// ParseMarkerLine extracts the tool id and command name from a shim's head
// bytes. ok=false when the head carries no marker line.
func ParseMarkerLine(head string) (tool, command, version string, ok bool) {
	for _, line := range strings.SplitN(head, "\n", 4) {
		i := strings.Index(line, ShimMarker)
		if i < 0 {
			continue
		}
		fields := strings.Fields(line[i+len(ShimMarker):])
		for _, f := range fields {
			switch {
			case strings.HasPrefix(f, "tool="):
				tool = strings.TrimPrefix(f, "tool=")
			case strings.HasPrefix(f, "command="):
				command = strings.TrimPrefix(f, "command=")
			case strings.HasPrefix(f, "v"):
				version = f
			}
		}
		return tool, command, version, true
	}
	return "", "", "", false
}

// IsShim reports whether a file's head bytes carry the shim marker.
func IsShim(head []byte) bool { return shimguard.IsShim(head) }

// The one line of each shim template that carries the baked observer path.
// The renderers write it and EmbeddedObserverPath reads it back, so the two
// can never drift apart (pinned by a round-trip test).
const (
	posixObserverPrefix  = "sbo_observer="
	cmdObserverPrefix    = `if not exist "`
	cmdObserverSuffix    = `" (`
	bridgeObserverPrefix = `set "SBO_OBSERVER=`
	bridgeObserverSuffix = `"`
)

// EmbeddedObserverPath returns the absolute observer path a rendered shim
// runs (the value its template baked in). ok=false when content is not a
// marked shim, or its observer line is missing or not in the exact quoting
// the renderer produces - a file somebody edited by hand is never guessed at.
func EmbeddedObserverPath(content string) (string, bool) {
	if !IsShim([]byte(content)) {
		return "", false
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, posixObserverPrefix):
			raw := strings.TrimPrefix(line, posixObserverPrefix)
			if len(raw) < 2 || raw[0] != '\'' || raw[len(raw)-1] != '\'' {
				return "", false
			}
			p := strings.ReplaceAll(raw[1:len(raw)-1], `'\''`, `'`)
			if shQuote(p) != raw || checkPath(p) != nil {
				return "", false
			}
			return p, true
		case strings.HasPrefix(line, cmdObserverPrefix) && strings.HasSuffix(line, cmdObserverSuffix):
			raw := line[len(cmdObserverPrefix) : len(line)-len(cmdObserverSuffix)]
			p := strings.ReplaceAll(raw, "%%", "%")
			if cmdLiteral(p) != raw || checkPath(p) != nil {
				return "", false
			}
			return p, true
		case strings.HasPrefix(line, bridgeObserverPrefix) && strings.HasSuffix(line, bridgeObserverSuffix) &&
			len(line) > len(bridgeObserverPrefix)+len(bridgeObserverSuffix):
			p := line[len(bridgeObserverPrefix) : len(line)-len(bridgeObserverSuffix)]
			if checkBridgeObserverPath(p) != nil {
				return "", false
			}
			return p, true
		}
	}
	return "", false
}

// shArgs renders the observer argv (after the executable) for sh.
func shArgs(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = shQuote(a)
	}
	return strings.Join(q, " ")
}

// RenderPOSIXShim renders the /bin/sh shim for Linux / macOS.
func RenderPOSIXShim(s ShimSpec) (string, error) {
	if err := s.validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("#!/bin/sh")
	w("%s", s.markerLine("#"))
	w("# Managed by SuperBased Observer: `%s` runs `observer %s`.", s.Name, strings.Join(s.Args, " "))
	w("# `observer shell-wrap disable` removes every shim; SBO_SHIM_BYPASS=1 runs the")
	w("# real %s directly. Rewritten on every apply - do not edit.", s.Name)
	w("sbo_name=%s", shQuote(s.Name))
	w("sbo_shim_dir=%s", shQuote(s.ShimDir))
	w("%s%s", posixObserverPrefix, shQuote(s.ObserverPath))
	w("")
	w("sbo_real() {")
	w("\tsbo_ifs=$IFS")
	w("\tIFS=:")
	w("\tset -f")
	w("\tfor sbo_d in $PATH; do")
	w("\t\tcase $sbo_d in /*) ;; *) continue ;; esac")
	w("\t\t[ \"$sbo_d\" = \"$sbo_shim_dir\" ] && continue")
	w("\t\tsbo_c=$sbo_d/$sbo_name")
	w("\t\t{ [ -f \"$sbo_c\" ] && [ -x \"$sbo_c\" ]; } || continue")
	w("\t\tif head -c 512 \"$sbo_c\" 2>/dev/null | grep -q '%s'; then", ShimMarker)
	w("\t\t\tcontinue")
	w("\t\tfi")
	w("\t\tIFS=$sbo_ifs")
	w("\t\tset +f")
	w("\t\tprintf '%%s\\n' \"$sbo_c\"")
	w("\t\treturn 0")
	w("\tdone")
	w("\tIFS=$sbo_ifs")
	w("\tset +f")
	w("\treturn 1")
	w("}")
	w("")
	w("sbo_exec_real() {")
	w("\tif sbo_r=$(sbo_real); then")
	w("\t\texec \"$sbo_r\" \"$@\"")
	w("\tfi")
	w("\tprintf '%%s: command not found (the observer shim in %%s found no other %%s on PATH)\\n' \"$sbo_name\" \"$sbo_shim_dir\" \"$sbo_name\" >&2")
	w("\texit 127")
	w("}")
	w("")
	w("if [ -n \"${%s:-}\" ] || [ \"${%s:-}\" = \"$sbo_name\" ]; then", BypassEnv, GuardEnv)
	w("\tsbo_exec_real \"$@\"")
	w("fi")
	w("if [ ! -x \"$sbo_observer\" ]; then")
	w("\tsbo_found=$(command -v observer 2>/dev/null) || sbo_found=")
	w("\tif [ -n \"$sbo_found\" ] && [ -x \"$sbo_found\" ]; then")
	w("\t\tsbo_observer=$sbo_found")
	w("\telse")
	w("\t\tprintf '%%s: observer not found at %%s - running the real %%s (reinstall observer or run: observer shell-wrap disable)\\n' \"$sbo_name\" \"$sbo_observer\" \"$sbo_name\" >&2")
	w("\t\tsbo_exec_real \"$@\"")
	w("\tfi")
	w("fi")
	if s.Kind == integration.LaunchKindGUI {
		w("sbo_r=$(sbo_real) || sbo_r=")
		for _, seg := range s.HandoffSegments {
			w("case \"$sbo_r/\" in */%s/*) sbo_exec_real \"$@\" ;; esac", seg)
		}
		w("%s=$sbo_name", GuardEnv)
		w("export %s", GuardEnv)
		w("if [ \"$#\" -eq 0 ]; then")
		w("\texec \"$sbo_observer\" %s%s", shArgs(s.Args), s.posixConfigArg())
		w("fi")
		if s.ProjectDirArgv {
			w("if [ \"$#\" -eq 1 ] && [ -d \"$1\" ]; then")
			w("\texec \"$sbo_observer\" %s%s \"$1\"", shArgs(s.Args), s.posixConfigArg())
			w("fi")
		}
		w("# Any other invocation (files, flags) is a CLI use the wrapped launch cannot")
		w("# carry: run the real command unchanged.")
		w("unset %s", GuardEnv)
		w("sbo_exec_real \"$@\"")
		return b.String(), nil
	}
	w("%s=$sbo_name", GuardEnv)
	w("export %s", GuardEnv)
	w("exec \"$sbo_observer\" %s%s -- \"$@\"", shArgs(s.Args), s.posixConfigArg())
	return b.String(), nil
}

// RenderCmdShim renders the cmd.exe shim (<name>.cmd) for Windows. It is
// resolved by cmd.exe and PowerShell alike, since both walk PATH with
// PATHEXT and the shim directory comes first.
func RenderCmdShim(s ShimSpec) (string, error) {
	if err := s.validate(); err != nil {
		return "", err
	}
	obs := cmdLiteral(s.ObserverPath)
	args := strings.Join(s.Args, " ")
	// cfgArg is passed as a format ARGUMENT below, so cmdLiteral's %% stays
	// doubled in the output, as cmd.exe needs it.
	cfgArg := s.cmdConfigArg()
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\r\n", a...) }
	w("@echo off")
	w("%s", s.markerLine("rem"))
	w("rem Managed by SuperBased Observer: `%s` runs `observer %s`.", s.Name, args)
	w("rem `observer shell-wrap disable` removes every shim; set SBO_SHIM_BYPASS=1 to run")
	w("rem the real %s directly. Rewritten on every apply - do not edit.", s.Name)
	w("setlocal")
	w("set \"SBO_REAL=\"")
	w("for /f \"delims=\" %%%%B in ('where \"$PATH:%s\" 2^>nul') do (", s.Name)
	w("  if not defined SBO_REAL if /i not \"%%%%~dpB\"==\"%%~dp0\" call :sbo_consider \"%%%%B\"")
	w(")")
	w("if defined %s goto sbo_real", BypassEnv)
	w("if /i \"%%%s%%\"==\"%s\" goto sbo_real", GuardEnv, s.Name)
	w("%s%s%s", cmdObserverPrefix, obs, cmdObserverSuffix)
	w("  >&2 echo %s: observer not found at \"%s\" - running the real %s ^(reinstall observer or run: observer shell-wrap disable^)", s.Name, obs, s.Name)
	w("  goto sbo_real")
	w(")")
	if s.Kind == integration.LaunchKindGUI {
		for _, seg := range s.HandoffSegments {
			// %VAR:find=% deletes every (case-insensitive) occurrence: a
			// changed string means the resolved path carries the segment.
			w("if defined SBO_REAL if not \"%%SBO_REAL:\\%s\\=%%\"==\"%%SBO_REAL%%\" goto sbo_real", seg)
		}
		w("set \"%s=%s\"", GuardEnv, s.Name)
		w("if \"%%~1\"==\"\" (")
		w("  \"%s\" %s%s", obs, args, cfgArg)
		w("  exit /b")
		w(")")
		if s.ProjectDirArgv {
			w("if \"%%~2\"==\"\" if exist \"%%~1\\\" (")
			w("  \"%s\" %s%s \"%%~1\"", obs, args, cfgArg)
			w("  exit /b")
			w(")")
		}
		w("set \"%s=\"", GuardEnv)
		w("goto sbo_real")
	} else {
		w("set \"%s=%s\"", GuardEnv, s.Name)
		w("\"%s\" %s%s -- %%*", obs, args, cfgArg)
		w("exit /b")
	}
	w("")
	w(":sbo_real")
	w("if not defined SBO_REAL (")
	w("  >&2 echo %s: command not found ^(the observer shim found no other %s on PATH^)", s.Name, s.Name)
	w("  exit /b 9009")
	w(")")
	w("\"%%SBO_REAL%%\" %%*")
	w("exit /b")
	w("")
	writeCmdConsider(w)
	return b.String(), nil
}

// writeCmdConsider writes the :sbo_consider label shared by the native and
// the bridge .cmd shims: a PATH hit is the real command unless it is
// extensionless or another observer shim. An npm install puts a POSIX `sh`
// stub named `claude` next to claude.cmd and `where` lists the stub FIRST
// (grounded on Windows 11, 2026-10-05). Running "<dir>\claude" happens to
// reach the sibling claude.cmd (cmd.exe tries PATHEXT before the bare name),
// but a stub with no runnable sibling would be handed to cmd.exe as-is, so an
// extensionless hit is skipped and the walk goes on.
func writeCmdConsider(w func(format string, a ...any)) {
	w(":sbo_consider")
	w("if \"%%~x1\"==\"\" exit /b 0")
	w("if /i \"%%~x1\"==\".cmd\" findstr /m /c:\"%s\" \"%%~1\" >nul 2>&1 && exit /b 0", ShimMarker)
	w("set \"SBO_REAL=%%~1\"")
	w("exit /b 0")
}

// bridgeObserverRE is the closed spelling of the in-distro observer path a
// bridge shim bakes in. The path is spliced into a `for /f` command line,
// where cmd.exe parses it OUTSIDE quotes, so every cmd metacharacter
// (& | < > ( ) ^ % !) and the double quote are refused, never escaped.
var bridgeObserverRE = regexp.MustCompile(`^/[A-Za-z0-9 ._/+@~-]*[A-Za-z0-9._+@~-]$`)

func checkBridgeObserverPath(p string) error {
	if !bridgeObserverRE.MatchString(p) || strings.Contains(p, "/../") || strings.HasSuffix(p, "/..") {
		return fmt.Errorf("shellwrap: observer path %q is not an absolute WSL path a bridge shim can carry", p)
	}
	return nil
}

func (s ShimSpec) validateBridge() error {
	if err := s.Bridge.validate(); err != nil {
		return err
	}
	if err := checkBridgeObserverPath(s.ObserverPath); err != nil {
		return err
	}
	if s.ConfigPath != "" && checkBridgeObserverPath(s.ConfigPath) != nil {
		// The same closed spelling as the observer path: it is spliced into
		// the same `for /f` command line.
		return fmt.Errorf("shellwrap: config path %q is not an absolute WSL path a bridge shim can carry (move config.toml to a path without cmd.exe special characters)", s.ConfigPath)
	}
	if s.Side != SideHost && s.Side != SideWSL {
		return fmt.Errorf("shellwrap: %s: unknown side %q", s.ToolID, s.Side)
	}
	for _, v := range s.Env {
		if !envNameRE.MatchString(v.Name) {
			return fmt.Errorf("shellwrap: %s: variable %q cannot be rendered into a shim", s.ToolID, v.Name)
		}
	}
	return nil
}

// RenderBridgeCmdShim renders a Windows <name>.cmd shim that reaches the
// observer daemon inside WSL (see bridge.go). SideHost runs the real Windows
// program with the routing environment `observer shell-wrap launch-env`
// returns; SideWSL runs `observer <verb>` inside the distro. Both fall back to
// the real program, with a one-line note, when the daemon side cannot answer.
func RenderBridgeCmdShim(s ShimSpec) (string, error) {
	if s.Bridge == nil {
		return "", fmt.Errorf("shellwrap: %s: a bridge shim needs a WSL bridge", s.ToolID)
	}
	if err := s.validate(); err != nil {
		return "", err
	}
	if err := s.validateBridge(); err != nil {
		return "", err
	}
	args := strings.Join(s.Args, " ")
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\r\n", a...) }
	w("@echo off")
	w("%s", s.markerLine("rem"))
	if s.Side == SideWSL {
		w("rem Managed by SuperBased Observer: `%s` has no Windows build, so it runs `observer %s` inside WSL (%s).", s.Name, args, s.Bridge.Distro)
	} else {
		w("rem Managed by SuperBased Observer: `%s` runs the Windows %s with the routing `observer %s` applies, asked from the observer in WSL (%s).", s.Name, s.Name, args, s.Bridge.Distro)
	}
	w("rem `observer shell-wrap disable --target windows` removes every shim; set SBO_SHIM_BYPASS=1 to")
	w("rem run the real %s directly. Rewritten on every apply - do not edit.", s.Name)
	w("setlocal")
	w("set \"SBO_DISTRO=%s\"", s.Bridge.Distro)
	w("%s%s%s", bridgeObserverPrefix, s.ObserverPath, bridgeObserverSuffix)
	if s.ConfigPath != "" {
		w("set \"SBO_CONFIG=%s\"", s.ConfigPath)
	}
	w("set \"SBO_WSL=%%SystemRoot%%\\System32\\wsl.exe\"")
	if s.Side == SideWSL {
		writeBridgeWSLSide(w, s, args)
		return b.String(), nil
	}
	writeBridgeHostSide(w, s)
	return b.String(), nil
}

// writeBridgeWSLSide renders the body of a shim whose program lives only in
// the distro.
func writeBridgeWSLSide(w func(format string, a ...any), s ShimSpec, args string) {
	w("if not exist \"%%SBO_WSL%%\" (")
	w("  >&2 echo %s: wsl.exe not found - %s runs only inside WSL", s.Name, s.Name)
	w("  exit /b 9009")
	w(")")
	w("if defined %s goto sbo_real", BypassEnv)
	// wsl.exe forwards its own exit code; a missing observer is told apart
	// from the tool's own failures by asking first.
	w("\"%%SBO_WSL%%\" -d %%SBO_DISTRO%% -e /bin/sh -c \"[ -x \\\"$0\\\" ]\" \"%%SBO_OBSERVER%%\"")
	w("if errorlevel 1 (")
	w("  >&2 echo %s: observer not found at %%SBO_OBSERVER%% in WSL - running the real %s ^(reinstall observer or run: observer shell-wrap disable --target windows^)", s.Name, s.Name)
	w("  goto sbo_real")
	w(")")
	w("\"%%SBO_WSL%%\" -d %%SBO_DISTRO%% -e \"%%SBO_OBSERVER%%\" %s%s -- %%*", args, s.bridgeConfigArg())
	w("exit /b")
	w("")
	w(":sbo_real")
	// A login shell so the distro's own PATH (~/.local/bin and the like)
	// finds the real program.
	w("\"%%SBO_WSL%%\" -d %%SBO_DISTRO%% -e /bin/sh -lc \"exec \\\"$0\\\" \\\"$@\\\"\" %s %%*", s.Name)
	w("exit /b")
}

// writeBridgeHostSide renders the body of a shim whose program is the
// Windows build: resolve it like the native shim does, ask the WSL observer
// for the routing environment, then run it.
func writeBridgeHostSide(w func(format string, a ...any), s ShimSpec) {
	w("set \"SBO_REAL=\"")
	w("for /f \"delims=\" %%%%B in ('where \"$PATH:%s\" 2^>nul') do (", s.Name)
	w("  if not defined SBO_REAL if /i not \"%%%%~dpB\"==\"%%~dp0\" call :sbo_consider \"%%%%B\"")
	w(")")
	w("if defined %s goto sbo_real", BypassEnv)
	w("if /i \"%%%s%%\"==\"%s\" goto sbo_real", GuardEnv, s.Name)
	w("if not defined SBO_REAL goto sbo_real")
	// The real program is started at the TOP level of the script, never inside
	// a parenthesized block: when it is itself a batch file (VS Code's
	// bin\code.cmd), cmd.exe chains into it, and chaining from inside a block
	// ends the CALLER's script too (grounded on Windows 11, 2026-10-05).
	launch := func(argv string) {
		w("call :sbo_env")
		w("if defined SBO_DENY exit /b 1")
		w("set \"%s=%s\"", GuardEnv, s.Name)
		w("\"%%SBO_REAL%%\"%s", argv)
		w("exit /b")
	}
	if s.Kind == integration.LaunchKindGUI {
		for _, seg := range s.HandoffSegments {
			w("if not \"%%SBO_REAL:\\%s\\=%%\"==\"%%SBO_REAL%%\" goto sbo_real", seg)
		}
		w("if \"%%~1\"==\"\" goto sbo_bare")
		if s.ProjectDirArgv {
			w("if \"%%~2\"==\"\" if exist \"%%~1\\\" goto sbo_dir")
		}
		w("rem Any other invocation (files, flags) is a CLI use the wrapped launch cannot")
		w("rem carry: run the real command unchanged.")
		w("goto sbo_real")
		w("")
		w(":sbo_bare")
		launch("")
		if s.ProjectDirArgv {
			w("")
			w(":sbo_dir")
			launch(" \"%~1\"")
		}
	} else {
		launch(" %*")
	}
	w("")
	w(":sbo_real")
	w("if not defined SBO_REAL (")
	w("  >&2 echo %s: command not found ^(the observer shim found no other %s on PATH^)", s.Name, s.Name)
	w("  exit /b 9009")
	w(")")
	w("\"%%SBO_REAL%%\" %%*")
	w("exit /b")
	w("")
	w(":sbo_env")
	w("set \"SBO_OK=\"")
	w("set \"SBO_DENY=\"")
	w("if not exist \"%%SBO_WSL%%\" (")
	w("  >&2 echo %s: wsl.exe not found - running the real %s unrouted", s.Name, s.Name)
	w("  exit /b 0")
	w(")")
	if len(s.Env) > 0 {
		// WSLENV hands the operator's own values of these variables to the
		// observer, so `launch-env` keeps a value they exported (the launcher
		// rule) instead of overwriting it. Restored right after the call.
		var names []string
		for _, v := range s.Env {
			names = append(names, v.Name+"/u")
		}
		list := strings.Join(names, ":")
		w("set \"SBO_WSLENV=%%WSLENV%%\"")
		w("if defined WSLENV (set \"WSLENV=%s:%%WSLENV%%\") else (set \"WSLENV=%s\")", list, list)
	}
	w("for /f \"usebackq tokens=1,2,*\" %%%%A in (`\"\"%%SBO_WSL%%\" -d %%SBO_DISTRO%% -e \"%%SBO_OBSERVER%%\" shell-wrap launch-env %s%s\"`) do (", s.ToolID, s.bridgeConfigArg())
	for _, v := range s.Env {
		// Only this shim's own variables are ever set from the answer.
		w("  if \"%%%%A\"==\"SBO_ENV\" if /i \"%%%%B\"==\"%s\" set \"%s=%%%%C\"", v.Name, v.Name)
	}
	w("  if \"%%%%A\"==\"SBO_OK\" set \"SBO_OK=1\"")
	w("  if \"%%%%A\"==\"SBO_DENY\" set \"SBO_DENY=1\"")
	w(")")
	if len(s.Env) > 0 {
		w("if defined SBO_WSLENV (set \"WSLENV=%%SBO_WSLENV%%\") else (set \"WSLENV=\")")
		w("set \"SBO_WSLENV=\"")
	}
	w("if not defined SBO_OK if not defined SBO_DENY >&2 echo %s: the observer in WSL ^(%%SBO_DISTRO%%^) did not answer - running the real %s unrouted", s.Name, s.Name)
	w("exit /b 0")
	w("")
	writeCmdConsider(w)
}
