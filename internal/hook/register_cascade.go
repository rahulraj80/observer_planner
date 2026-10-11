package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// cascadeHookEntry is one entry of Windsurf/Devin Desktop Cascade's
// hooks.json "pre_user_prompt" list (Part B item 2, phase-3a;
// live-fetched 2026-09-07, docs.devin.ai/desktop/cascade/hooks):
//
//	{"hooks":{"pre_user_prompt":[{"command":"…","powershell":"…"}]}}
//
// No matcher, no "type" field — a flat command list, the vendor's OWN
// shape distinct from every other hooks.json this repo writes.
// PowerShell is left empty here: Devin Desktop's only grounded
// install channel today is the macOS Homebrew cask (internal/
// integration's devin row, GUI.Binary.Installs) — a Windows cross-OS
// bridge is a documented, deferred gap for this mechanism, same as
// HookGeminiSettings/HookQwenSettings/HookFactoryJSON.
//
// B5: this type is now used ONLY to construct observer's OWN entry.
// A sibling entry already in the list — one this repo doesn't own,
// which may carry a documented field this struct doesn't model (e.g.
// a per-entry `timeout`) — is never decoded into it; see
// cascadeEntryProbe and readCascadeHooksFile below.
type cascadeHookEntry struct {
	Command    string `json:"command,omitempty"`
	PowerShell string `json:"powershell,omitempty"`
}

// cascadeHooksConfig describes hooks.json's body shape
// (`{"hooks": {<event>: [<entry>...]}}`) for callers (tests) that want
// a typed READ of the file this package wrote. It is never used to
// decode a file this package is about to re-write — see
// readCascadeHooksFile.
type cascadeHooksConfig struct {
	Hooks map[string][]cascadeHookEntry `json:"hooks"`
}

// cascadeEntryProbe decodes just the "command" field from an
// otherwise-opaque raw Cascade hook entry — enough for
// isObserverAnyHookEntry to tell whether the entry is observer's own,
// without decoding (and thereby truncating) any other field the entry
// carries.
type cascadeEntryProbe struct {
	Command string `json:"command"`
}

// readCascadeHooksFile reads hooks.json's top-level object and its
// "hooks" subtree with the RawMessage boundary pushed one level deeper
// than a single map[string]json.RawMessage (B5): top holds every
// top-level key verbatim (so a key beside "hooks" that Cascade itself
// — or a human — wrote round-trips byte-for-byte), and hooksByEvent
// holds, per event, the list of RAW hook entries (so an event beside
// "pre_user_prompt", and any entry within an event's list this repo
// doesn't own, both round-trip byte-for-byte too — decodePoolsideHooksRaw
// and registerClaudeCode's hooksRaw use the identical shape of fix for
// their own file formats). A missing or empty file yields empty maps
// and no error, matching every other readSettingsFile-style helper in
// this package.
func readCascadeHooksFile(path string) (map[string]json.RawMessage, map[string][]json.RawMessage, error) {
	hooksByEvent := map[string][]json.RawMessage{}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]json.RawMessage{}, hooksByEvent, nil
		}
		return nil, nil, fmt.Errorf("hook.readCascadeHooksFile: read: %w", err)
	}
	if len(raw) == 0 {
		return map[string]json.RawMessage{}, hooksByEvent, nil
	}
	top, err := decodeSettingsObject(path, raw)
	if err != nil {
		return nil, nil, fmt.Errorf("hook.readCascadeHooksFile: %w", err)
	}
	if hRaw, ok := top["hooks"]; ok && len(hRaw) > 0 {
		if err := json.Unmarshal(hRaw, &hooksByEvent); err != nil {
			return nil, nil, fmt.Errorf("hook.readCascadeHooksFile: parse hooks: %w", err)
		}
		if hooksByEvent == nil {
			hooksByEvent = map[string][]json.RawMessage{}
		}
	}
	return top, hooksByEvent, nil
}

// cascadeEvents are the events Observer registers in Cascade's hooks.json:
// the prompt gate (Part B) and the four per-action pre hooks that make up
// the tool gate (hook-parity Lane A5; exit code 2 blocks, stderr is the
// reason shown to the developer). Equal to the registry row's Registered
// bindings (event_lists golden).
var cascadeEvents = []string{"pre_user_prompt", "pre_run_command", "pre_write_code", "pre_read_code", "pre_mcp_tool_use"}

// registerCascade installs Observer's hooks into
// ~/.codeium/windsurf/hooks.json, one entry per cascadeEvents event (Part B
// item 2, phase-3a: the HookCascadeJSON mechanism).
func (r *Registry) registerCascade() RegistrationResult {
	res := RegistrationResult{Tool: "devin", DryRun: r.opts.DryRun}
	dir := filepath.Join(r.opts.HomeDir, ".codeium", "windsurf")
	path := filepath.Join(dir, "hooks.json")
	res.ConfigPath = path

	unlock, err := r.lockSettings(path)
	if err != nil {
		res.Error = fmt.Errorf("hook.registerCascade: %w", err)
		return res
	}
	defer unlock()

	pinned := pinWriteTarget(path)

	top, hooksByEvent, err := readCascadeHooksFile(path)
	if err != nil {
		res.Error = err
		return res
	}

	for _, event := range cascadeEvents {
		if r.skipEvent("devin", event) {
			continue
		}
		cmd := shellQuoteIfNeeded(forwardSlashPath(r.opts.BinaryPath)) + " hook devin " + event + r.configFlagSuffixForwardSlash()
		entries := hooksByEvent[event]
		idx := -1
		for i, raw := range entries {
			var probe cascadeEntryProbe
			if err := json.Unmarshal(raw, &probe); err != nil {
				continue
			}
			if isObserverAnyHookEntry(probe.Command, "devin") {
				idx = i
			}
		}
		if idx >= 0 {
			var probe cascadeEntryProbe
			if err := json.Unmarshal(entries[idx], &probe); err == nil && probe.Command == cmd {
				res.AlreadySet = append(res.AlreadySet, event)
				continue
			}
			entries = append(entries[:idx:idx], entries[idx+1:]...)
		}
		if !r.opts.Force {
			for _, raw := range entries {
				var probe cascadeEntryProbe
				if err := json.Unmarshal(raw, &probe); err != nil {
					continue
				}
				if !isObserverAnyHookEntry(probe.Command, "devin") {
					res.Error = fmt.Errorf("hook.registerCascade: event %s already has a non-observer hook; pass --force to overwrite", event)
					return res
				}
			}
		}
		entryJSON, err := json.Marshal(cascadeHookEntry{Command: cmd})
		if err != nil {
			res.Error = fmt.Errorf("hook.registerCascade: marshal entry: %w", err)
			return res
		}
		hooksByEvent[event] = append(entries, entryJSON)
		res.HooksAdded = append(res.HooksAdded, event)
	}
	if len(res.HooksAdded) == 0 {
		return res
	}

	hooksJSON, err := json.Marshal(hooksByEvent)
	if err != nil {
		res.Error = fmt.Errorf("hook.registerCascade: marshal hooks: %w", err)
		return res
	}
	top["hooks"] = hooksJSON

	if r.opts.DryRun {
		return res
	}
	if err := writeJSONIndented(dir, pinned, top); err != nil {
		res.Error = err
		return res
	}
	if err := r.recordChecksum(path); err != nil {
		res.Error = err
		return res
	}
	return res
}

// unregisterCascade removes ONLY the observer-owned pre_user_prompt
// entry from ~/.codeium/windsurf/hooks.json, leaving every other
// top-level key, every other event, and every sibling entry in
// pre_user_prompt's own list byte-for-byte untouched.
func (r *Registry) unregisterCascade() UnregistrationResult {
	res := UnregistrationResult{Tool: "devin", DryRun: r.opts.DryRun}
	dir := filepath.Join(r.opts.HomeDir, ".codeium", "windsurf")
	path := filepath.Join(dir, "hooks.json")
	res.ConfigPath = path

	unlock, err := r.lockSettings(path)
	if err != nil {
		res.Error = fmt.Errorf("hook.unregisterCascade: %w", err)
		return res
	}
	defer unlock()

	pinned := pinWriteTarget(path)

	top, hooksByEvent, err := readCascadeHooksFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return res
		}
		res.Error = err
		return res
	}
	for _, event := range cascadeEvents {
		entries := hooksByEvent[event]
		var kept []json.RawMessage
		removed := false
		for _, raw := range entries {
			var probe cascadeEntryProbe
			if err := json.Unmarshal(raw, &probe); err == nil && isObserverAnyHookEntry(probe.Command, "devin") {
				removed = true
				continue
			}
			kept = append(kept, raw)
		}
		if !removed {
			continue
		}
		if len(kept) == 0 {
			delete(hooksByEvent, event)
		} else {
			hooksByEvent[event] = kept
		}
		res.HooksRemoved = append(res.HooksRemoved, event)
	}
	if len(res.HooksRemoved) == 0 {
		return res
	}

	if len(hooksByEvent) == 0 {
		delete(top, "hooks")
	} else {
		hooksJSON, err := json.Marshal(hooksByEvent)
		if err != nil {
			res.Error = fmt.Errorf("hook.unregisterCascade: marshal hooks: %w", err)
			return res
		}
		top["hooks"] = hooksJSON
	}

	if r.opts.DryRun {
		return res
	}
	if err := writeJSONIndented(dir, pinned, top); err != nil {
		res.Error = err
		return res
	}
	if err := r.removeChecksum(path); err != nil {
		res.Error = err
		return res
	}
	return res
}
