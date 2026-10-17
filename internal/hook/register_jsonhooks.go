package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// One JSON hooks-file writer for the hook-parity Lane A6 vendors whose
// config is a JSON object holding an event map (data: jsonHooksTarget),
// instead of one hand-written writer per vendor. Every target keeps the
// writers' common guarantees: the settings lock, the pinned write target,
// raw round-trip of every key and sibling entry Observer does not own (B5),
// the checksum record, the --force conflict guard, dry-run and a symmetric
// unregister. A vendor is a row: where the file is, where its event map
// sits, whether entries are matcher groups or flat handlers, and the
// handler shape.

// jsonSetting is a value Observer ensures at a dotted path of the file (a
// vendor enable switch such as zcode's hooks.enabled, or a format version
// such as Copilot CLI's version: 1).
type jsonSetting struct {
	path  []string
	value any
}

// jsonHooksTarget is one vendor's JSON hooks file.
type jsonHooksTarget struct {
	// tool is the `observer hook <tool>` token and the result label.
	tool string
	// label overrides the result label (a "-windows" bridge target).
	label string
	// wrapper prefixes every command (the cross-OS `wsl.exe -d <distro> --`
	// bridge); "" for a native target.
	wrapper string
	// dir / file locate the hooks file.
	dir, file string
	// eventsPath is the key path from the top-level object to the event
	// map ({"hooks": {...}} is ["hooks"]; zcode's is ["hooks", "events"]).
	eventsPath []string
	// events are the native events registered, in order.
	events []string
	// grouped is true when an event's entries are matcher groups
	// ({"matcher": ..., "hooks": [handler]}); false for a flat list of
	// handlers.
	grouped bool
	// matcher is the group matcher Observer writes (grouped only; "" omits
	// the key).
	matcher string
	// handler builds Observer's handler object for cmd.
	handler func(cmd string) map[string]any
	// commandKey is the handler key that carries the command ("command",
	// or Copilot CLI's "bash").
	commandKey string
	// settings are ensured on register (never removed on unregister: a
	// vendor switch another hook may rely on is the user's after that).
	settings []jsonSetting
	// dedicated is true when the file is Observer's own (a hooks
	// directory where every file is one hook set): unregister deletes it
	// once it holds no hook entry.
	dedicated bool
	// extraFiles are written beside the hooks file on register and removed
	// on unregister (Goose's plugin manifest), keyed by path relative to
	// dir.
	extraFiles map[string][]byte
	// errPrefix names the registrar in wrapped errors.
	errPrefix string
}

// command is Observer's command for event.
func (r *Registry) jsonHooksCommand(t jsonHooksTarget, event string) string {
	return t.wrapper + shellQuoteIfNeeded(forwardSlashPath(r.opts.BinaryPath)) + " hook " + t.tool + " " + event + r.configFlagSuffixForwardSlash()
}

// entry is Observer's entry for event, as JSON.
func (r *Registry) jsonHooksEntry(t jsonHooksTarget, event string) (json.RawMessage, error) {
	h := t.handler(r.jsonHooksCommand(t, event))
	if !t.grouped {
		return json.Marshal(h)
	}
	g := map[string]any{"hooks": []any{h}}
	if t.matcher != "" {
		g["matcher"] = t.matcher
	}
	return json.Marshal(g)
}

// jsonHooksEntryIsObservers reports whether a raw entry is positively
// Observer's: every handler it holds names an observer `hook <tool>`
// invocation under commandKey, and it holds at least one.
func jsonHooksEntryIsObservers(t jsonHooksTarget, raw json.RawMessage) bool {
	handlers := []json.RawMessage{raw}
	if t.grouped {
		var g struct {
			Hooks []json.RawMessage `json:"hooks"`
		}
		if json.Unmarshal(raw, &g) != nil || len(g.Hooks) == 0 {
			return false
		}
		handlers = g.Hooks
	}
	for _, h := range handlers {
		var m map[string]any
		if json.Unmarshal(h, &m) != nil {
			return false
		}
		cmd, _ := m[t.commandKey].(string)
		if !isObserverAnyHookEntry(cmd, t.tool) {
			return false
		}
	}
	return true
}

// jsonObject decodes raw as a JSON object of raw values (nil/empty raw is an
// empty object).
func jsonObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	return m, nil
}

// readJSONHooksFile reads the file's top-level object and its event map.
func readJSONHooksFile(t jsonHooksTarget, path string) (top map[string]json.RawMessage, raw []byte, eventMap map[string][]json.RawMessage, err error) {
	raw, err = readSettingsFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil, fmt.Errorf("%s: read: %w", t.errPrefix, err)
	}
	top = map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if top, err = decodeSettingsObject(path, raw); err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", t.errPrefix, err)
		}
	}
	cur := top
	for i, k := range t.eventsPath {
		next, err := jsonObject(cur[k])
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: parse %v: %w", t.errPrefix, t.eventsPath[:i+1], err)
		}
		if i == len(t.eventsPath)-1 {
			eventMap = map[string][]json.RawMessage{}
			for ev, v := range next {
				var list []json.RawMessage
				if err := json.Unmarshal(v, &list); err != nil {
					return nil, nil, nil, fmt.Errorf("%s: parse event %s: %w", t.errPrefix, ev, err)
				}
				eventMap[ev] = list
			}
		}
		cur = next
	}
	return top, raw, eventMap, nil
}

// setJSONPath writes value at path under top, creating intermediate objects
// and keeping every sibling key raw.
func setJSONPath(top map[string]json.RawMessage, path []string, value json.RawMessage) error {
	if len(path) == 1 {
		top[path[0]] = value
		return nil
	}
	child, err := jsonObject(top[path[0]])
	if err != nil {
		return err
	}
	if err := setJSONPath(child, path[1:], value); err != nil {
		return err
	}
	b, err := json.Marshal(child)
	if err != nil {
		return err
	}
	top[path[0]] = b
	return nil
}

// storeEventMap writes eventMap back under t.eventsPath (an empty event is
// dropped).
func storeEventMap(t jsonHooksTarget, top map[string]json.RawMessage, eventMap map[string][]json.RawMessage) error {
	parent := t.eventsPath[:len(t.eventsPath)-1]
	cur := top
	for _, k := range parent {
		next, err := jsonObject(cur[k])
		if err != nil {
			return err
		}
		cur = next
	}
	existing, err := jsonObject(cur[t.eventsPath[len(t.eventsPath)-1]])
	if err != nil {
		return err
	}
	for ev, list := range eventMap {
		if len(list) == 0 {
			delete(existing, ev)
			continue
		}
		b, err := json.Marshal(list)
		if err != nil {
			return err
		}
		existing[ev] = b
	}
	b, err := json.Marshal(existing)
	if err != nil {
		return err
	}
	return setJSONPath(top, t.eventsPath, b)
}

// registerJSONHooks installs Observer's entry for every t.events event.
func (r *Registry) registerJSONHooks(t jsonHooksTarget) RegistrationResult {
	res := RegistrationResult{Tool: t.resultLabel(), DryRun: r.opts.DryRun}
	path := filepath.Join(t.dir, t.file)
	res.ConfigPath = path

	unlock, err := r.lockSettings(path)
	if err != nil {
		res.Error = fmt.Errorf("%s: %w", t.errPrefix, err)
		return res
	}
	defer unlock()
	pinned := pinWriteTarget(path)

	top, raw, eventMap, err := readJSONHooksFile(t, path)
	if err != nil {
		res.Error = err
		return res
	}
	changed := false
	for _, event := range t.events {
		if r.skipEvent(t.tool, event) {
			continue
		}
		want, err := r.jsonHooksEntry(t, event)
		if err != nil {
			res.Error = fmt.Errorf("%s: marshal: %w", t.errPrefix, err)
			return res
		}
		entries := eventMap[event]
		idx := -1
		for i, e := range entries {
			if jsonHooksEntryIsObservers(t, e) {
				idx = i
			}
		}
		if idx >= 0 {
			if jsonEqual(entries[idx], want) {
				res.AlreadySet = append(res.AlreadySet, event)
				continue
			}
			entries = append(entries[:idx:idx], entries[idx+1:]...)
		}
		if !r.opts.Force && len(entries) > 0 {
			res.Error = hookConflictError(t.errPrefix, event)
			return res
		}
		eventMap[event] = append(entries, want)
		res.HooksAdded = append(res.HooksAdded, event)
		changed = true
	}
	for _, s := range t.settings {
		b, _ := json.Marshal(s.value)
		if cur, ok := lookupJSONPath(top, s.path); ok && jsonEqual(cur, b) {
			continue
		}
		if err := setJSONPath(top, s.path, b); err != nil {
			res.Error = fmt.Errorf("%s: set %v: %w", t.errPrefix, s.path, err)
			return res
		}
		changed = true
	}
	if !changed && r.jsonHooksExtrasPresent(t) {
		return res
	}
	if err := storeEventMap(t, top, eventMap); err != nil {
		res.Error = fmt.Errorf("%s: %w", t.errPrefix, err)
		return res
	}
	if r.opts.DryRun {
		return res
	}
	for rel, data := range t.extraFiles {
		p := filepath.Join(t.dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			res.Error = fmt.Errorf("%s: %w", t.errPrefix, err)
			return res
		}
		if err := atomicWriteFile(pinWriteTarget(p), data); err != nil {
			res.Error = fmt.Errorf("%s: write %s: %w", t.errPrefix, rel, err)
			return res
		}
	}
	if err := r.writeHookSettingsJSON(t.tool, filepath.Dir(path), pinned, raw, top); err != nil {
		res.Error = err
		return res
	}
	if err := r.recordHookChecksum(t.tool, path); err != nil {
		res.Error = err
	}
	return res
}

// jsonHooksExtrasPresent reports whether every extra file already holds its
// exact content.
func (r *Registry) jsonHooksExtrasPresent(t jsonHooksTarget) bool {
	for rel, data := range t.extraFiles {
		have, err := os.ReadFile(filepath.Join(t.dir, rel))
		if err != nil || !bytes.Equal(have, data) {
			return false
		}
	}
	return true
}

// unregisterJSONHooks removes ONLY Observer's entries for t.events.
func (r *Registry) unregisterJSONHooks(t jsonHooksTarget) UnregistrationResult {
	res := UnregistrationResult{Tool: t.resultLabel(), DryRun: r.opts.DryRun}
	path := filepath.Join(t.dir, t.file)
	res.ConfigPath = path

	// A target with extra files owns its directory (a plugin dir): once
	// its files are gone, the directory goes too. This runs AFTER the
	// lock is released (the lock sentinel sits beside the file), and
	// os.Remove never deletes a directory that still holds anything.
	removeOwnedDir := false
	defer func() {
		if removeOwnedDir {
			_ = os.Remove(filepath.Dir(path))
			_ = os.Remove(t.dir)
		}
	}()
	unlock, err := r.lockSettings(path)
	if err != nil {
		res.Error = fmt.Errorf("%s: %w", t.errPrefix, err)
		return res
	}
	defer unlock()
	pinned := pinWriteTarget(path)

	top, raw, eventMap, err := readJSONHooksFile(t, path)
	if err != nil {
		res.Error = err
		return res
	}
	for _, event := range t.events {
		entries := eventMap[event]
		kept := make([]json.RawMessage, 0, len(entries))
		removed := false
		for _, e := range entries {
			if jsonHooksEntryIsObservers(t, e) {
				removed = true
				continue
			}
			kept = append(kept, e)
		}
		if removed {
			eventMap[event] = kept
			res.HooksRemoved = append(res.HooksRemoved, event)
		}
	}
	if len(res.HooksRemoved) == 0 || r.opts.DryRun {
		return res
	}
	for rel := range t.extraFiles {
		_ = os.Remove(filepath.Join(t.dir, rel))
	}
	if err := storeEventMap(t, top, eventMap); err != nil {
		res.Error = fmt.Errorf("%s: %w", t.errPrefix, err)
		return res
	}
	if t.dedicated && jsonHooksEmpty(eventMap) {
		if err := removeEmptyConfigFile(path, raw); err != nil {
			res.Error = fmt.Errorf("%s: %w", t.errPrefix, err)
			return res
		}
		_ = r.removeChecksum(path)
		removeOwnedDir = len(t.extraFiles) > 0
		return res
	}
	if err := r.writeHookSettingsJSON(t.tool, filepath.Dir(path), pinned, raw, top); err != nil {
		res.Error = err
		return res
	}
	if err := r.removeChecksum(path); err != nil {
		res.Error = err
	}
	return res
}

// jsonHooksEmpty reports whether no event holds an entry.
func jsonHooksEmpty(eventMap map[string][]json.RawMessage) bool {
	for _, l := range eventMap {
		if len(l) > 0 {
			return false
		}
	}
	return true
}

// lookupJSONPath returns the raw value at path.
func lookupJSONPath(top map[string]json.RawMessage, path []string) (json.RawMessage, bool) {
	cur := top
	for i, k := range path {
		v, ok := cur[k]
		if !ok {
			return nil, false
		}
		if i == len(path)-1 {
			return v, true
		}
		next, err := jsonObject(v)
		if err != nil {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// jsonEqual compares two JSON values semantically (key order and spacing
// ignored).
func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return bytes.Equal(xb, yb)
}

// resultLabel is the RegistrationResult.Tool label.
func (t jsonHooksTarget) resultLabel() string {
	if t.label != "" {
		return t.label
	}
	return t.tool
}
