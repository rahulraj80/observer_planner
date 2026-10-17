package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// findClaudeGroupWithObserver returns the index of a group whose
// genericSettingsHookTarget parameterizes registerGenericSettingsHooks
// (Part B item 1, docs/plans/prompt-submit-intervention-exploration-2026-09-07.md
// §6.7): a single-event, Claude-Code-shaped
// {"hooks":{<event>:[{"hooks":[{"type":"command","command":…}]}]}}
// settings.json writer — generalized from registerClaudeCode's own
// body via the ALREADY tool-parameterized isObserverAnyHookEntry
// (the C2/IDE-11 cross-OS-ownership fix). Reused for every tool whose
// registration file matches this exact shape and needs only ONE event
// registered — Gemini CLI (BeforeAgent) and Qwen Code (UserPromptSubmit)
// today. Unlike registerClaudeCode's 21-event lifecycle registration,
// these two tools have no OTHER hook this repo captures, so a single
// event is the complete, honest scope — not a partial implementation
// of a larger one.
type genericSettingsHookTarget struct {
	// tool is the hookReceivers/`hook <tool>` dispatch token embedded in
	// the registered command — the BASE tool name (e.g. "gemini-cli"),
	// never a "-windows" suffix, because the hook receiver on the far
	// end (cmd/observer/hook.go's hookReceivers map) is keyed by base
	// tool name regardless of which config file wrote the command.
	tool string
	// resultTool is the RegistrationResult.Tool label. Empty means
	// "same as tool" (the native target); the cross-OS bridge targets
	// set this to the "-windows" name so callers can tell which config
	// file a result came from while the wire command still names the
	// base tool.
	resultTool string
	// dir is the settings directory (e.g. ~/.gemini, ~/.qwen).
	dir string
	// event is the tool's prompt-submit event: the fallback event list
	// when the registry row declares no registered bindings. The writer
	// registers every event the row's bindings mark Registered (events):
	// the prompt-submit event, the tool gate where the vendor offers one on
	// the same file (hook-parity Lane A3) and the lifecycle events Lane C
	// grounded.
	event string
	// errPrefix names the calling registrar in wrapped errors.
	errPrefix string
	// wrapper prefixes every registered command; "" for the native
	// target, `MSYS_NO_PATHCONV=1 wsl.exe -d <distro> -- ` for the
	// cross-OS bridge — same convention as registerClaudeCodeWindows /
	// registerCursorWindows (these tools spawn hooks through Git Bash
	// on Windows too, per the same vendor-doc precedent those two
	// registrars already established), NOT codex's cmd.exe convention.
	wrapper string
}

// label returns t.resultTool when set, else t.tool — the
// RegistrationResult.Tool value for both registerGenericSettingsHooks
// and unregisterGenericSettingsHooks.
func (t genericSettingsHookTarget) label() string {
	if t.resultTool != "" {
		return t.resultTool
	}
	return t.tool
}

// events is the event list this target registers / unregisters: the
// registry row's Registered bindings (hook-parity audit section 5.4), or the
// single prompt-submit event for a row with none.
func (t genericSettingsHookTarget) events() []string {
	return eventsOr(t.tool, t.event)
}

// registerGenericSettingsHooks is the shared writer behind
// registerGeminiCLI, registerQwenCode and registerQoder. Conflict/refresh
// discipline mirrors registerClaudeCode exactly, per event in t.events():
// an entry isObserverAnyHookEntry recognises as ours is refreshed silently
// on drift; anything else blocks without --force.
func (r *Registry) registerGenericSettingsHooks(t genericSettingsHookTarget) RegistrationResult {
	res := RegistrationResult{Tool: t.label(), DryRun: r.opts.DryRun}
	path := filepath.Join(t.dir, "settings.json")
	res.ConfigPath = path

	unlock, err := r.lockSettings(path)
	if err != nil {
		res.Error = fmt.Errorf("%s: %w", t.errPrefix, err)
		return res
	}
	defer unlock()

	// Pin the write target now, before the read — see pinnedTarget (F6).
	pinned := pinWriteTarget(path)

	raw, err := readSettingsFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		res.Error = fmt.Errorf("%s: read: %w", t.errPrefix, err)
		return res
	}
	// Preserve unknown top-level fields (model config, MCP servers, …)
	// via map[string]json.RawMessage — same discipline as
	// registerClaudeCode.
	settings := map[string]json.RawMessage{}
	if len(raw) > 0 {
		settings, err = decodeSettingsObject(path, raw)
		if err != nil {
			res.Error = fmt.Errorf("%s: %w", t.errPrefix, err)
			return res
		}
	}
	var hooks map[string][]claudeHookGroup
	if existing, ok := settings["hooks"]; ok {
		if err := json.Unmarshal(existing, &hooks); err != nil {
			res.Error = fmt.Errorf("%s: parse hooks: %w", t.errPrefix, err)
			return res
		}
	}
	if hooks == nil {
		hooks = map[string][]claudeHookGroup{}
	}

	binPath := forwardSlashPath(r.opts.BinaryPath)
	for _, event := range t.events() {
		if r.skipEvent(t.tool, event) {
			continue
		}
		cmd := t.wrapper + shellQuoteIfNeeded(binPath) + " hook " + t.tool + " " + event + r.configFlagSuffixForwardSlash()
		groups := hooks[event]
		idx := -1
		for i, g := range groups {
			for _, h := range g.Hooks {
				if h.Type == "command" && isObserverAnyHookEntry(h.Command, t.tool) {
					idx = i
				}
			}
		}
		if idx >= 0 {
			if len(groups[idx].Hooks) == 1 && groups[idx].Hooks[0].Type == "command" && groups[idx].Hooks[0].Command == cmd {
				res.AlreadySet = append(res.AlreadySet, event)
				continue
			}
			// Stale-observer-args / cross-binary refresh. Drop the stale
			// group; the fresh append below restores.
			groups = append(groups[:idx], groups[idx+1:]...)
		}
		if !r.opts.Force {
			for _, g := range groups {
				for _, h := range g.Hooks {
					if h.Type == "command" && !isObserverAnyHookEntry(h.Command, t.tool) {
						res.Error = hookConflictError(t.errPrefix, event)
						return res
					}
				}
			}
		}
		groups = append(groups, claudeHookGroup{Hooks: []claudeHookCommand{{Type: "command", Command: cmd}}})
		hooks[event] = groups
		res.HooksAdded = append(res.HooksAdded, event)
	}
	if len(res.HooksAdded) == 0 {
		return res
	}

	patched, err := json.Marshal(hooks)
	if err != nil {
		res.Error = fmt.Errorf("%s: marshal hooks: %w", t.errPrefix, err)
		return res
	}
	settings["hooks"] = patched

	if r.opts.DryRun {
		return res
	}
	if err := r.writeHookSettingsJSON(t.tool, t.dir, pinned, raw, settings); err != nil {
		res.Error = err
		return res
	}
	if err := r.recordHookChecksum(t.tool, path); err != nil {
		res.Error = err
		return res
	}
	return res
}

// registerGeminiCLI installs the prompt-submit hook into
// ~/.gemini/settings.json's "hooks" block (contract §6.7: new
// HookGeminiSettings mechanism), plus the BeforeTool tool gate
// (every Registered binding of the row, t.events()).
func (r *Registry) registerGeminiCLI() RegistrationResult {
	return r.registerGenericSettingsHooks(genericSettingsHookTarget{
		tool:      "gemini-cli",
		dir:       filepath.Join(r.opts.HomeDir, ".gemini"),
		event:     "BeforeAgent",
		errPrefix: "hook.registerGeminiCLI",
	})
}

// registerQwenCode installs the prompt-submit hook into
// ~/.qwen/settings.json's "hooks" block (contract §6.7: new
// HookQwenSettings mechanism), plus the PreToolUse tool gate and the
// grounded lifecycle events (every Registered binding of the row).
func (r *Registry) registerQwenCode() RegistrationResult {
	return r.registerGenericSettingsHooks(genericSettingsHookTarget{
		tool:      "qwen-code",
		dir:       filepath.Join(r.opts.HomeDir, ".qwen"),
		event:     "UserPromptSubmit",
		errPrefix: "hook.registerQwenCode",
	})
}

// registerQoder installs the prompt-submit hook into
// ~/.qoder/settings.json's "hooks" block (Part B item 2, phase-3a:
// new HookQoderJSON mechanism). Live-fetched 2026-09-07
// (docs.qoder.com/en/cli/hooks): Qoder's settings.json hooks schema
// ({"hooks":{"UserPromptSubmit":[{"matcher":…,"hooks":[{"type":
// "command","command":…,"timeout":…}]}]}}) is byte-identical to
// Claude Code's own — the same shape registerGenericSettingsHooks
// already generalizes for Gemini CLI/Qwen Code. Events: UserPromptSubmit
// and the PreToolUse tool gate plus the grounded lifecycle events (every
// Registered binding of the row; NIT, phase-3a review:
// re-counted 2026-09-07 — Qoder's own Event Reference overview table
// lists 23 distinct hook event names, not 24 as an earlier pass here
// miscounted).
func (r *Registry) registerQoder() RegistrationResult {
	return r.registerGenericSettingsHooks(genericSettingsHookTarget{
		tool:      "qoder",
		dir:       filepath.Join(r.opts.HomeDir, ".qoder"),
		event:     "UserPromptSubmit",
		errPrefix: "hook.registerQoder",
	})
}

// unregisterGenericSettingsHooks removes ONLY the observer-owned hook
// groups for t.events() from the settings.json's "hooks" block, leaving
// every other key (and every other event) untouched. Symmetric
// counterpart of registerGenericSettingsHooks.
func (r *Registry) unregisterGenericSettingsHooks(t genericSettingsHookTarget) UnregistrationResult {
	res := UnregistrationResult{Tool: t.label(), DryRun: r.opts.DryRun}
	path := filepath.Join(t.dir, "settings.json")
	res.ConfigPath = path

	unlock, err := r.lockSettings(path)
	if err != nil {
		res.Error = fmt.Errorf("%s: %w", t.errPrefix, err)
		return res
	}
	defer unlock()

	// Pin the write target now, before the read — see pinnedTarget (F6).
	pinned := pinWriteTarget(path)

	raw, err := readSettingsFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return res
		}
		res.Error = fmt.Errorf("%s: read: %w", t.errPrefix, err)
		return res
	}
	settings, err := decodeSettingsObject(path, raw)
	if err != nil {
		res.Error = fmt.Errorf("%s: %w", t.errPrefix, err)
		return res
	}
	existing, ok := settings["hooks"]
	if !ok {
		return res
	}
	var hooks map[string][]claudeHookGroup
	if err := json.Unmarshal(existing, &hooks); err != nil {
		res.Error = fmt.Errorf("%s: parse hooks: %w", t.errPrefix, err)
		return res
	}
	for _, event := range t.events() {
		groups := hooks[event]
		kept := groups[:0]
		removed := false
		for _, g := range groups {
			isOurs := len(g.Hooks) > 0
			for _, h := range g.Hooks {
				if h.Type != "command" || !isObserverAnyHookEntry(h.Command, t.tool) {
					isOurs = false
				}
			}
			if isOurs {
				removed = true
				continue
			}
			kept = append(kept, g)
		}
		if !removed {
			continue
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
		res.HooksRemoved = append(res.HooksRemoved, event)
	}
	if len(res.HooksRemoved) == 0 {
		return res
	}
	patched, err := json.Marshal(hooks)
	if err != nil {
		res.Error = fmt.Errorf("%s: marshal hooks: %w", t.errPrefix, err)
		return res
	}
	settings["hooks"] = patched

	if r.opts.DryRun {
		return res
	}
	if err := r.writeHookSettingsJSON(t.tool, t.dir, pinned, raw, settings); err != nil {
		res.Error = err
		return res
	}
	if err := r.removeChecksum(path); err != nil {
		res.Error = err
		return res
	}
	return res
}

func (r *Registry) unregisterGeminiCLI() UnregistrationResult {
	return r.unregisterGenericSettingsHooks(genericSettingsHookTarget{
		tool: "gemini-cli", dir: filepath.Join(r.opts.HomeDir, ".gemini"),
		event: "BeforeAgent", errPrefix: "hook.unregisterGeminiCLI",
	})
}

func (r *Registry) unregisterQwenCode() UnregistrationResult {
	return r.unregisterGenericSettingsHooks(genericSettingsHookTarget{
		tool: "qwen-code", dir: filepath.Join(r.opts.HomeDir, ".qwen"),
		event: "UserPromptSubmit", errPrefix: "hook.unregisterQwenCode",
	})
}

func (r *Registry) unregisterQoder() UnregistrationResult {
	return r.unregisterGenericSettingsHooks(genericSettingsHookTarget{
		tool: "qoder", dir: filepath.Join(r.opts.HomeDir, ".qoder"),
		event: "UserPromptSubmit", errPrefix: "hook.unregisterQoder",
	})
}
