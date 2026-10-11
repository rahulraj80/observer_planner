package hook

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// Two non-JSON tool-gate writers (hook-parity Lane A6):
//
//   - Mistral vibe: ~/.vibe/hooks.toml, an array of [[hooks]] tables
//     ({name, type, match, command, timeout, strict}); pre_tool is the gate.
//   - Cline (SDK hook runtime, extension and CLI): one executable per event
//     in a hooks directory; the file named PreToolUse is the gate.

// observerVibeHookName is the [[hooks]] name Observer owns.
const observerVibeHookName = "superbased-observer"

// vibeHookTimeoutSeconds bounds Observer's pre_tool hook (vibe's default is
// 60 s; strict stays false so a failed hook is a warning, the vendor's own
// fail-open default, and Observer's own fail-closed answer under
// enforcement is a JSON deny written before vibe's timeout).
const vibeHookTimeoutSeconds = 30

// vibeHooksPath is ~/.vibe/hooks.toml.
func (r *Registry) vibeHooksPath() (dir, path string) {
	dir = filepath.Join(r.opts.HomeDir, ".vibe")
	return dir, filepath.Join(dir, "hooks.toml")
}

// readVibeHooks reads hooks.toml into its top-level table (a missing file is
// empty) and its [[hooks]] list.
func readVibeHooks(path string) (raw []byte, top map[string]any, hooks []map[string]any, err error) {
	raw, err = readSettingsFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil, fmt.Errorf("hook.readVibeHooks: read: %w", err)
	}
	top = map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := toml.Unmarshal(raw, &top); err != nil {
			return nil, nil, nil, fmt.Errorf("hook.readVibeHooks: parse %s: %w", path, err)
		}
	}
	switch v := top["hooks"].(type) {
	case nil:
	case []map[string]any:
		hooks = v
	case []any:
		for _, e := range v {
			m, ok := e.(map[string]any)
			if !ok {
				return nil, nil, nil, fmt.Errorf("hook.readVibeHooks: %s: hooks holds a non-table entry", path)
			}
			hooks = append(hooks, m)
		}
	default:
		return nil, nil, nil, fmt.Errorf("hook.readVibeHooks: %s: hooks is not an array of tables", path)
	}
	return raw, top, hooks, nil
}

// vibeHookIsObservers reports whether a [[hooks]] table is Observer's.
func vibeHookIsObservers(h map[string]any) bool {
	cmd, _ := h["command"].(string)
	return isObserverAnyHookEntry(cmd, "mistral-code")
}

// writeVibeHooks encodes top back to hooks.toml (comments and key order of a
// user's file are not preserved by the TOML round trip; every value is).
func writeVibeHooks(pinned pinnedTarget, top map[string]any) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(top); err != nil {
		return fmt.Errorf("hook.writeVibeHooks: encode: %w", err)
	}
	return atomicWriteFile(pinned, buf.Bytes())
}

// registerMistralVibe installs Observer's pre_tool hook into
// ~/.vibe/hooks.toml.
func (r *Registry) registerMistralVibe() RegistrationResult {
	const event = "pre_tool"
	res := RegistrationResult{Tool: "mistral-code", DryRun: r.opts.DryRun}
	dir, path := r.vibeHooksPath()
	res.ConfigPath = path
	unlock, err := r.lockSettings(path)
	if err != nil {
		res.Error = fmt.Errorf("hook.registerMistralVibe: %w", err)
		return res
	}
	defer unlock()
	pinned := pinWriteTarget(path)

	_, top, hooks, err := readVibeHooks(path)
	if err != nil {
		res.Error = err
		return res
	}
	cmd := shellQuoteIfNeeded(forwardSlashPath(r.opts.BinaryPath)) + " hook mistral-code " + event + r.configFlagSuffixForwardSlash()
	want := map[string]any{
		"name": observerVibeHookName, "type": event, "match": "*",
		"command": cmd, "timeout": int64(vibeHookTimeoutSeconds),
	}
	kept := make([]map[string]any, 0, len(hooks)+1)
	for _, h := range hooks {
		if vibeHookIsObservers(h) {
			if fmt.Sprint(h) == fmt.Sprint(want) {
				res.AlreadySet = append(res.AlreadySet, event)
				return res
			}
			continue
		}
		if t, _ := h["type"].(string); t == event && !r.opts.Force {
			res.Error = fmt.Errorf("hook.registerMistralVibe: %s already has a non-observer pre_tool hook; pass --force to overwrite", path)
			return res
		}
		kept = append(kept, h)
	}
	top["hooks"] = append(kept, want)
	res.HooksAdded = append(res.HooksAdded, event)
	if r.opts.DryRun {
		return res
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		res.Error = fmt.Errorf("hook.registerMistralVibe: %w", err)
		return res
	}
	if err := pinned.verifyUnmoved(); err != nil {
		res.Error = fmt.Errorf("hook.registerMistralVibe: %w", err)
		return res
	}
	if err := writeVibeHooks(pinned, top); err != nil {
		res.Error = err
		return res
	}
	if err := r.recordChecksum(path); err != nil {
		res.Error = err
	}
	return res
}

// unregisterMistralVibe removes ONLY Observer's [[hooks]] table.
func (r *Registry) unregisterMistralVibe() UnregistrationResult {
	res := UnregistrationResult{Tool: "mistral-code", DryRun: r.opts.DryRun}
	_, path := r.vibeHooksPath()
	res.ConfigPath = path
	unlock, err := r.lockSettings(path)
	if err != nil {
		res.Error = fmt.Errorf("hook.unregisterMistralVibe: %w", err)
		return res
	}
	defer unlock()
	pinned := pinWriteTarget(path)
	raw, top, hooks, err := readVibeHooks(path)
	if err != nil {
		res.Error = err
		return res
	}
	kept := make([]map[string]any, 0, len(hooks))
	for _, h := range hooks {
		if vibeHookIsObservers(h) {
			continue
		}
		kept = append(kept, h)
	}
	if len(kept) == len(hooks) {
		return res
	}
	res.HooksRemoved = append(res.HooksRemoved, "pre_tool")
	if r.opts.DryRun {
		return res
	}
	if len(kept) == 0 {
		delete(top, "hooks")
	} else {
		top["hooks"] = kept
	}
	if len(top) == 0 {
		if err := removeEmptyConfigFile(path, raw); err != nil {
			res.Error = fmt.Errorf("hook.unregisterMistralVibe: %w", err)
			return res
		}
	} else if err := writeVibeHooks(pinned, top); err != nil {
		res.Error = err
		return res
	}
	if err := r.removeChecksum(path); err != nil {
		res.Error = err
	}
	return res
}

// clineHookExtensions are the file extensions the Cline SDK maps onto an
// event file (hook-file-config.ts): a foreign PreToolUse under any of them
// is a conflict.
var clineHookExtensions = []string{"", ".sh", ".bash", ".zsh", ".js", ".mjs", ".cjs", ".ts", ".mts", ".cts", ".py", ".ps1"}

// clineHookMarker is the second line of the script Observer owns.
const clineHookMarker = "# superbased-observer: Cline PreToolUse guard (observer init writes this file; observer uninstall removes it)"

// clineHooksDir is the user-level hooks directory the Cline SDK reads
// (~/.cline/hooks; ~/Documents/Cline/Hooks is the other one).
func (r *Registry) clineHooksDir() string {
	return filepath.Join(r.opts.HomeDir, ".cline", "hooks")
}

// clineHookTool is the registry row the ~/.cline/hooks tool gate belongs to
// (T-342): the Cline CLI reads the directory (live 2026-10-04), so the
// script invokes `observer hook cline-cli` and verdicts are recorded under
// cline-cli. clineHookLegacyTools are the tokens an earlier version wrote
// (`observer hook cline`): still Observer's script, so a register rewrites
// it and an unregister removes it.
const clineHookTool = "cline-cli"

var clineHookLegacyTools = []string{"cline"}

// clineHookScript is Observer's PreToolUse executable.
func (r *Registry) clineHookScript() []byte {
	cmd := shellQuoteIfNeeded(forwardSlashPath(r.opts.BinaryPath)) + " hook " + clineHookTool + " PreToolUse" + r.configFlagSuffixForwardSlash()
	return []byte("#!/usr/bin/env bash\n" + clineHookMarker + "\nexec " + cmd + "\n")
}

// clineHooksUnsupportedOS reports whether goos has no working Cline hook
// file runtime (a Windows host: the SDK runs .ps1 hooks detached and never
// reads their reply).
func clineHooksUnsupportedOS(goos string) bool { return goos == "windows" }

// clineHookIsObservers reports whether data is a script Observer wrote.
func clineHookIsObservers(data []byte) bool {
	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 || lines[1] != clineHookMarker {
		return false
	}
	cmd, ok := strings.CutPrefix(lines[2], "exec ")
	if !ok {
		return false
	}
	for _, tool := range append([]string{clineHookTool}, clineHookLegacyTools...) {
		if isObserverAnyHookEntry(cmd, tool) {
			return true
		}
	}
	return false
}

// clineHookExecutable reports whether path is a regular file its owner can
// execute. Cline runs the hook file as a program, so the right bytes in a
// file that is not executable (a 0644 copy, a restored backup) are no hook
// at all and must not read as current (review M2-08); the writer below
// rewrites it with 0755.
func clineHookExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o100 != 0
}

// registerCline writes Observer's PreToolUse script into the Cline hooks
// directory (macOS / Linux: the SDK's Windows .ps1 detached hooks are
// broken, cline/cline #14767, so no Windows target exists).
func (r *Registry) registerCline() RegistrationResult {
	const event = "PreToolUse"
	res := RegistrationResult{Tool: clineHookTool, DryRun: r.opts.DryRun}
	dir := r.clineHooksDir()
	path := filepath.Join(dir, event)
	res.ConfigPath = path
	if clineHooksUnsupportedOS(runtime.GOOS) {
		res.Skipped = true
		res.SkipKind = SkipNotApplicable
		res.SkipReason = "Cline hook files are macOS / Linux only (the SDK's Windows .ps1 hooks are broken, cline/cline #14767)"
		return res
	}
	unlock, err := r.lockSettings(path)
	if err != nil {
		res.Error = fmt.Errorf("hook.registerCline: %w", err)
		return res
	}
	defer unlock()
	want := r.clineHookScript()
	for _, ext := range clineHookExtensions {
		p := path + ext
		have, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			res.Error = fmt.Errorf("hook.registerCline: read %s: %w", p, err)
			return res
		}
		if ext == "" && bytes.Equal(have, want) && clineHookExecutable(p) {
			res.AlreadySet = append(res.AlreadySet, event)
			return res
		}
		if !clineHookIsObservers(have) && !r.opts.Force {
			res.Error = fmt.Errorf("hook.registerCline: %s already exists and is not observer's; pass --force to overwrite", p)
			return res
		}
	}
	res.HooksAdded = append(res.HooksAdded, event)
	if r.opts.DryRun {
		return res
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		res.Error = fmt.Errorf("hook.registerCline: %w", err)
		return res
	}
	pinned := pinWriteTarget(path)
	if err := atomicWriteFile(pinned, want); err != nil {
		res.Error = fmt.Errorf("hook.registerCline: write: %w", err)
		return res
	}
	if err := os.Chmod(pinned.target, 0o755); err != nil {
		res.Error = fmt.Errorf("hook.registerCline: chmod: %w", err)
		return res
	}
	if err := r.recordChecksum(path); err != nil {
		res.Error = err
	}
	return res
}

// unregisterCline removes Observer's script (never a file it did not write).
func (r *Registry) unregisterCline() UnregistrationResult {
	res := UnregistrationResult{Tool: clineHookTool, DryRun: r.opts.DryRun}
	path := filepath.Join(r.clineHooksDir(), "PreToolUse")
	res.ConfigPath = path
	unlock, err := r.lockSettings(path)
	if err != nil {
		res.Error = fmt.Errorf("hook.unregisterCline: %w", err)
		return res
	}
	defer unlock()
	have, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			res.Error = fmt.Errorf("hook.unregisterCline: %w", err)
		}
		return res
	}
	if !clineHookIsObservers(have) {
		res.HooksKept = append(res.HooksKept, "PreToolUse")
		return res
	}
	res.HooksRemoved = append(res.HooksRemoved, "PreToolUse")
	if r.opts.DryRun {
		return res
	}
	if err := removeEmptyConfigFile(path, have); err != nil {
		res.Error = fmt.Errorf("hook.unregisterCline: %w", err)
		return res
	}
	if err := r.removeChecksum(path); err != nil {
		res.Error = err
	}
	return res
}
