package hook

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"

	"github.com/marmutapp/superbased-observer/internal/hook/commandcodemod"
)

// Tool-gate registrations for the hook-parity Lane A6 vendors that speak a
// JSON hooks file (register_jsonhooks.go). Each is a jsonHooksTarget row:
// the file, where its event map sits, the entry shape and the handler. The
// event lists are the registry rows' Registered bindings (pinned by the
// hook-parity event_lists golden).

// installedToolGateDirs is the install signal of each Lane A6 target: the
// vendor's own state directories under the home (any one present), the
// same config-directory convention Installed() uses for every other tool.
var installedToolGateDirs = []struct {
	tool string
	dirs [][]string
}{
	{"grok", [][]string{{".grok"}}},
	{"copilot-cli", [][]string{{".copilot"}}},
	{"goose", [][]string{{".config", "goose"}, {".local", "share", "goose"}}},
	{"junie", [][]string{{".junie"}}},
	{"crush", [][]string{{".config", "crush"}, {".local", "share", "crush"}}},
	{"open-interpreter", [][]string{{".openinterpreter"}}},
	{"zcode", [][]string{{".zcode"}}},
	{"mistral-code", [][]string{{".vibe"}}},
	{"cline-cli", [][]string{{".cline"}}},
	{"muse", [][]string{{".config", "muse"}}},
	{"kimi-code", [][]string{{".kimi-code"}}},
}

// Handler timeouts Observer registers where the vendor default is too
// tight for a process spawn plus a guard evaluation (Grok 5 s, Junie 2 s).
const (
	grokHookTimeoutSeconds  = 10
	junieHookTimeoutSeconds = 10
)

// commandHandler is the {type: command, command, timeout?} handler most
// JSON hooks files share; timeoutKey/timeout add the vendor's timeout field
// when non-zero.
func commandHandler(timeoutKey string, timeout int) func(string) map[string]any {
	return func(cmd string) map[string]any {
		h := map[string]any{"type": "command", "command": cmd}
		if timeoutKey != "" && timeout > 0 {
			h[timeoutKey] = timeout
		}
		return h
	}
}

// grokHooksTarget: Grok Build loads every ~/.grok/hooks/*.json (always
// trusted); Observer owns one file there.
func (r *Registry) grokHooksTarget() jsonHooksTarget {
	return jsonHooksTarget{
		tool: "grok", dir: filepath.Join(r.opts.HomeDir, ".grok", "hooks"), file: "superbased-observer.json",
		eventsPath: []string{"hooks"}, events: []string{"PreToolUse"},
		grouped: true, handler: commandHandler("timeout", grokHookTimeoutSeconds), commandKey: "command",
		dedicated: true, errPrefix: "hook.registerGrok",
	}
}

// copilotCLIHooksTarget: Copilot CLI loads every ~/.copilot/hooks/*.json
// (version 1); the handler's shell command is the "bash" field.
func (r *Registry) copilotCLIHooksTarget() jsonHooksTarget {
	return jsonHooksTarget{
		tool: "copilot-cli", dir: filepath.Join(r.opts.HomeDir, ".copilot", "hooks"), file: "superbased-observer.json",
		eventsPath: []string{"hooks"}, events: []string{"preToolUse"},
		handler: func(cmd string) map[string]any {
			return map[string]any{"type": "command", "bash": cmd, "timeoutSec": 30}
		},
		commandKey: "bash",
		settings:   []jsonSetting{{path: []string{"version"}, value: 1}},
		dedicated:  true, errPrefix: "hook.registerCopilotCLI",
	}
}

// goosePluginManifest is the plugin.json an Open Plugins directory needs.
var goosePluginManifest = []byte(`{
  "name": "superbased-observer",
  "version": "1.0.0",
  "description": "SuperBased Observer guard: judges each tool call before it runs"
}
`)

// gooseHooksTarget: Goose loads Open Plugins from ~/.agents/plugins/<name>;
// Observer owns one plugin directory (manifest + hooks/hooks.json).
func (r *Registry) gooseHooksTarget() jsonHooksTarget {
	return jsonHooksTarget{
		tool: "goose", dir: filepath.Join(r.opts.HomeDir, ".agents", "plugins", "superbased-observer"), file: filepath.Join("hooks", "hooks.json"),
		eventsPath: []string{"hooks"}, events: []string{"PreToolUse"},
		grouped: true, matcher: ".*", handler: commandHandler("timeout", 30), commandKey: "command",
		dedicated: true, extraFiles: map[string][]byte{"plugin.json": goosePluginManifest},
		errPrefix: "hook.registerGoose",
	}
}

// junieHooksTarget: Junie CLI reads the "hooks" key of ~/.junie/config.json
// (Claude-shaped matcher groups; CLI only, not the IDE / ACP mode).
func (r *Registry) junieHooksTarget() jsonHooksTarget {
	return jsonHooksTarget{
		tool: "junie", dir: filepath.Join(r.opts.HomeDir, ".junie"), file: "config.json",
		eventsPath: []string{"hooks"}, events: []string{"PreToolUse"},
		grouped: true, handler: commandHandler("timeout", junieHookTimeoutSeconds), commandKey: "command",
		errPrefix: "hook.registerJunie",
	}
}

// crushHooksTarget: Crush reads flat {name, matcher, command, timeout}
// entries under crush.json "hooks" (global ~/.config/crush/crush.json).
func (r *Registry) crushHooksTarget() jsonHooksTarget {
	return jsonHooksTarget{
		tool: "crush", dir: filepath.Join(r.opts.HomeDir, ".config", "crush"), file: "crush.json",
		eventsPath: []string{"hooks"}, events: []string{"PreToolUse"},
		handler: func(cmd string) map[string]any {
			return map[string]any{"name": "superbased-observer", "command": cmd, "timeout": 30}
		},
		commandKey: "command", errPrefix: "hook.registerCrush",
	}
}

// openInterpreterHooksTarget: Open Interpreter (the Codex CLI rebadge)
// reads the Codex hooks.json shape under its OWN home, ~/.openinterpreter.
// The [features] hooks switch lives in ~/.openinterpreter/config.toml,
// which can hold plaintext provider keys: Observer never reads or rewrites
// it (the developer enables hooks and trusts the entry with /hooks).
func (r *Registry) openInterpreterHooksTarget() jsonHooksTarget {
	return jsonHooksTarget{
		tool: "open-interpreter", dir: filepath.Join(r.opts.HomeDir, ".openinterpreter"), file: "hooks.json",
		eventsPath: []string{"hooks"}, events: []string{"PreToolUse"},
		grouped: true, matcher: "*", handler: commandHandler("", 0), commandKey: "command",
		errPrefix: "hook.registerOpenInterpreter",
	}
}

// zcodeHooksTarget: zcode reads ~/.zcode/cli/config.json "hooks.events"
// and runs none of them unless "hooks.enabled" is true.
func (r *Registry) zcodeHooksTarget() jsonHooksTarget {
	return jsonHooksTarget{
		tool: "zcode", dir: filepath.Join(r.opts.HomeDir, ".zcode", "cli"), file: "config.json",
		eventsPath: []string{"hooks", "events"}, events: []string{"UserPromptSubmit", "PreToolUse"},
		grouped: true, handler: commandHandler("timeoutMs", 30000), commandKey: "command",
		settings:  []jsonSetting{{path: []string{"hooks", "enabled"}, value: true}},
		errPrefix: "hook.registerZcode",
	}
}

// commandCodeShellTarget: Command Code's SHELL hook layer,
// <home>/settings.json "hooks" (Claude-shaped matcher groups), separate
// from its Mods SDK. home is the .commandcode directory; wrapper / label
// are set for the cross-OS bridge target.
func commandCodeShellTarget(home, wrapper, label string) jsonHooksTarget {
	return jsonHooksTarget{
		tool: "command-code", label: label, wrapper: wrapper, dir: home, file: "settings.json",
		eventsPath: []string{"hooks"}, events: []string{"PreToolUse"},
		grouped: true, handler: commandHandler("", 0), commandKey: "command",
		errPrefix: "hook.registerCommandCodeShell",
	}
}

// commandCodeModGatesTools reports whether the Observer mod under home/mods
// gates tool calls itself (it subscribes to the Mods beforeToolCall hook:
// the in-process plugin shim, hook-parity Lane D). Content-shaped, never a
// version check.
func commandCodeModGatesTools(home string) bool {
	data, err := os.ReadFile(filepath.Join(home, "mods", commandcodemod.ModFileName))
	return err == nil && bytes.Contains(data, []byte("beforeToolCall"))
}

// withCommandCodeShellGate adds the shell-hook PreToolUse gate to a Mods
// registration result, unless the mod gates tool calls itself or the
// registry does not register the shell binding. The two are never both
// gates: when the mod subscribes to beforeToolCall (Lane D) it is the gate
// of record (in-process, one evaluation per call) and any shell entry
// Observer wrote earlier is removed, so a call is never judged twice. Since
// Lane D the registry's shell row is dialect-only, so a mod written WITHOUT
// beforeToolCall (`observer start` while that gate still owes a live test)
// does not fall back to writing the shell gate either (Integration 5).
func (r *Registry) withCommandCodeShellGate(res RegistrationResult, t jsonHooksTarget) RegistrationResult {
	if res.Error != nil || res.Skipped {
		return res
	}
	if commandCodeModGatesTools(t.dir) || !commandcodemod.ShellGateRegistered() {
		if un := r.unregisterJSONHooks(t); un.Error != nil {
			res.Error = un.Error
		}
		return res
	}
	sh := r.registerJSONHooks(t)
	res.HooksAdded = append(res.HooksAdded, sh.HooksAdded...)
	res.AlreadySet = append(res.AlreadySet, sh.AlreadySet...)
	if sh.Error != nil {
		res.Error = sh.Error
	}
	return res
}

// withoutCommandCodeShellGate removes Observer's shell-hook entry beside a
// Mods unregistration.
func (r *Registry) withoutCommandCodeShellGate(res UnregistrationResult, t jsonHooksTarget) UnregistrationResult {
	sh := r.unregisterJSONHooks(t)
	res.HooksRemoved = append(res.HooksRemoved, sh.HooksRemoved...)
	if res.Error == nil && sh.Error != nil {
		res.Error = sh.Error
	}
	return res
}

// registerCommandCodeAll registers the Mods bridge and the shell-hook gate
// in ~/.commandcode.
func (r *Registry) registerCommandCodeAll() RegistrationResult {
	return r.withCommandCodeShellGate(r.registerCommandCode(),
		commandCodeShellTarget(filepath.Join(r.opts.HomeDir, ".commandcode"), "", ""))
}

// unregisterCommandCodeAll removes the mod and Observer's shell-hook entry.
func (r *Registry) unregisterCommandCodeAll() UnregistrationResult {
	return r.withoutCommandCodeShellGate(r.unregisterCommandCode(),
		commandCodeShellTarget(filepath.Join(r.opts.HomeDir, ".commandcode"), "", ""))
}

// registerCommandCodeWindowsAll is registerCommandCodeAll for the cross-OS
// bridge: the Windows-side .commandcode, every command behind
// `wsl.exe -d <distro> --` (the form cmd.exe and PowerShell both run).
func (r *Registry) registerCommandCodeWindowsAll() RegistrationResult {
	res := r.registerCommandCodeWindows()
	winHome := r.detectWindowsCommandCodeHome()
	if res.Error != nil || winHome == "" {
		return res
	}
	distro, err := r.resolveWSLDistro("hook.registerCommandCodeWindows")
	if err != nil {
		res.Error = err
		return res
	}
	return r.withCommandCodeShellGate(res,
		commandCodeShellTarget(winHome, "wsl.exe -d "+shellQuoteIfNeeded(distro)+" -- ", "command-code-windows"))
}

// unregisterCommandCodeWindowsAll removes the Windows-side mod and shell
// entry.
func (r *Registry) unregisterCommandCodeWindowsAll() UnregistrationResult {
	res := r.unregisterCommandCodeWindows()
	winHome := r.detectWindowsCommandCodeHome()
	if winHome == "" {
		return res
	}
	return r.withoutCommandCodeShellGate(res, commandCodeShellTarget(winHome, "", "command-code-windows"))
}

func (r *Registry) registerGrok() RegistrationResult { return r.registerJSONHooks(r.grokHooksTarget()) }

func (r *Registry) unregisterGrok() UnregistrationResult {
	return r.unregisterJSONHooks(r.grokHooksTarget())
}

func (r *Registry) registerCopilotCLI() RegistrationResult {
	return r.registerJSONHooks(r.copilotCLIHooksTarget())
}

func (r *Registry) unregisterCopilotCLI() UnregistrationResult {
	return r.unregisterJSONHooks(r.copilotCLIHooksTarget())
}

func (r *Registry) registerGoose() RegistrationResult {
	return r.registerJSONHooks(r.gooseHooksTarget())
}

func (r *Registry) unregisterGoose() UnregistrationResult {
	return r.unregisterJSONHooks(r.gooseHooksTarget())
}

func (r *Registry) registerJunie() RegistrationResult {
	return r.registerJSONHooks(r.junieHooksTarget())
}

func (r *Registry) unregisterJunie() UnregistrationResult {
	return r.unregisterJSONHooks(r.junieHooksTarget())
}

func (r *Registry) registerCrush() RegistrationResult {
	return r.registerJSONHooks(r.crushHooksTarget())
}

func (r *Registry) unregisterCrush() UnregistrationResult {
	return r.unregisterJSONHooks(r.crushHooksTarget())
}

func (r *Registry) registerOpenInterpreter() RegistrationResult {
	return r.registerJSONHooks(r.openInterpreterHooksTarget())
}

func (r *Registry) unregisterOpenInterpreter() UnregistrationResult {
	return r.unregisterJSONHooks(r.openInterpreterHooksTarget())
}

func (r *Registry) registerZcode() RegistrationResult {
	return r.registerJSONHooks(r.zcodeHooksTarget())
}

func (r *Registry) unregisterZcode() UnregistrationResult {
	return r.unregisterJSONHooks(r.zcodeHooksTarget())
}

// devinCLIEvents are the Devin CLI events Observer registers in the CLI's
// user config (the registry row's Registered Devin CLI bindings; the
// Cascade events are cascadeEvents).
var devinCLIEvents = []string{"PreToolUse"}

// devinCLIConfigDir is the Devin CLI's user config directory:
// ~/.config/devin (Linux, macOS), %APPDATA%\devin (Windows), per
// docs.devin.ai/cli/extensibility/hooks/overview (fetched 2026-10-06).
func (r *Registry) devinCLIConfigDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(r.opts.HomeDir, "AppData", "Roaming", "devin")
	}
	return filepath.Join(r.opts.HomeDir, ".config", "devin")
}

// devinCLIHooksTarget: the Devin CLI reads the "hooks" key of its user
// config.json (Claude Code's matcher-group shape; devin 3000.11.3 on demo
// node 1 parses the same ClaudeHookConfig). T-478.
func (r *Registry) devinCLIHooksTarget() jsonHooksTarget {
	return jsonHooksTarget{
		tool: "devin", label: "devin-cli", dir: r.devinCLIConfigDir(), file: "config.json",
		eventsPath: []string{"hooks"}, events: devinCLIEvents,
		grouped: true, handler: commandHandler("timeout", 30), commandKey: "command",
		errPrefix: "hook.registerDevinCLI",
	}
}

// registerDevinAll registers the Windsurf / Devin Desktop Cascade hooks and,
// when the Devin CLI is installed (its config directory exists), the CLI's
// tool gate. The two never judge the same call: Cascade and the CLI are
// different hosts with different event names. A home with only the CLI gets
// no Cascade file; a home with neither keeps the earlier behaviour (Cascade).
func (r *Registry) registerDevinAll() RegistrationResult {
	t := r.devinCLIHooksTarget()
	cli := r.dirExists(t.dir)
	res := RegistrationResult{Tool: "devin", DryRun: r.opts.DryRun}
	if r.dirExists(filepath.Join(r.opts.HomeDir, ".codeium", "windsurf")) || !cli {
		res = r.registerCascade()
	}
	if res.Error != nil || !cli {
		return res
	}
	c := r.registerJSONHooks(t)
	if res.ConfigPath == "" {
		res.ConfigPath = c.ConfigPath
	}
	res.HooksAdded = append(res.HooksAdded, c.HooksAdded...)
	res.AlreadySet = append(res.AlreadySet, c.AlreadySet...)
	if c.Error != nil {
		res.Error = c.Error
	}
	return res
}

// unregisterDevinAll removes Observer's Cascade hooks and its Devin CLI
// entry.
func (r *Registry) unregisterDevinAll() UnregistrationResult {
	res := r.unregisterCascade()
	cli := r.unregisterJSONHooks(r.devinCLIHooksTarget())
	// Report the file that actually changed: a CLI-only home removes
	// nothing from the Cascade file.
	if len(res.HooksRemoved) == 0 && len(cli.HooksRemoved) > 0 {
		res.ConfigPath = cli.ConfigPath
	}
	res.HooksRemoved = append(res.HooksRemoved, cli.HooksRemoved...)
	if res.Error == nil && cli.Error != nil {
		res.Error = cli.Error
	}
	return res
}

// museHooksTarget: Muse Code reads Claude-shaped matcher groups under the
// "hooks" key of its user settings, ~/.config/muse/settings.json, beside
// "schema_version": 1 (dev.meta.ai/docs/muse-code/extending; Muse 1.4.3 on
// demo node 1 ran a hook from it with no trust step, T-479). The file holds
// the model and provider choice; the sign-in lives in auth.json, which is
// never read.
func (r *Registry) museHooksTarget() jsonHooksTarget {
	return jsonHooksTarget{
		tool: "muse", dir: filepath.Join(r.opts.HomeDir, ".config", "muse"), file: "settings.json",
		eventsPath: []string{"hooks"}, events: eventsOr("muse", "PreToolUse"),
		grouped: true, handler: commandHandler("timeout", 30), commandKey: "command",
		settings:  []jsonSetting{{path: []string{"schema_version"}, value: 1}},
		errPrefix: "hook.registerMuse",
	}
}

func (r *Registry) registerMuse() RegistrationResult {
	return r.registerJSONHooks(r.museHooksTarget())
}

func (r *Registry) unregisterMuse() UnregistrationResult {
	return r.unregisterJSONHooks(r.museHooksTarget())
}
