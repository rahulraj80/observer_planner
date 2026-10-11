package hookdisable

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

// Scope says whose file a switch lives in.
type Scope string

const (
	// ScopeUser is a file under the developer's home directory.
	ScopeUser Scope = "user"
	// ScopeManaged is an administrator-managed system file (MDM or
	// policy); a developer cannot override it.
	ScopeManaged Scope = "managed"
)

// groundingConfigSurface is the audit every row is grounded in.
const groundingConfigSurface = "docs/audits/adapter-config-surface-2026-10-03.md"

// claudeSettingsReplayHosts is every tool on which Observer stops observing
// when Claude Code's hook settings are switched off: Claude Code itself plus
// the hosts that run ~/.claude/settings.json hooks (cursor-agent and Grok
// Build live-grounded on node 1 2026-10-03; Copilot CLI, VS Code Copilot,
// Devin CLI and DeepSeek Harness per the config-surface audit finding 5).
var claudeSettingsReplayHosts = []string{"claude-code", "copilot-cli", "copilot", "cursor", "devin", "grok", "deepseek"}

// Switch is one row of the table: a JSON file, the key path inside it, and
// the value that means "hooks are off".
type Switch struct {
	// ID is a stable identifier, e.g. "claude.disableAllHooks".
	ID string
	// Scope is user or managed.
	Scope Scope
	// HomeRel is the file path relative to the home directory (user
	// scope); Abs is the absolute path per GOOS (managed scope).
	HomeRel string
	Abs     map[string]string
	// Key is the dotted JSON key path.
	Key string
	// Off is the value of Key that switches the hooks off.
	Off bool
	// Tools are the tool ids Observer goes silent on when the switch is set.
	Tools []string
	// Effect is the one-line consequence, for doctor and the guard event.
	Effect string
	// Grounding names where the switch is documented.
	Grounding string
}

// Switches is the ordered table (one test row each).
var Switches = []Switch{
	{
		ID: "claude.disableAllHooks", Scope: ScopeUser, HomeRel: ".claude/settings.json",
		Key: "disableAllHooks", Off: true, Tools: claudeSettingsReplayHosts,
		Effect:    "every Claude Code hook is off, including the hosts that replay Claude Code's hook settings",
		Grounding: groundingConfigSurface + " (claude-code V18; finding 5)",
	},
	{
		ID: "claude.managed.disableAllHooks", Scope: ScopeManaged, Abs: claudeManagedSettings,
		Key: "disableAllHooks", Off: true, Tools: claudeSettingsReplayHosts,
		Effect:    "the managed policy turns every Claude Code hook off",
		Grounding: groundingConfigSurface + " (claude-code V18 managed scope; finding 6)",
	},
	{
		ID: "claude.managed.allowManagedHooksOnly", Scope: ScopeManaged, Abs: claudeManagedSettings,
		Key: "allowManagedHooksOnly", Off: true, Tools: claudeSettingsReplayHosts,
		Effect:    "the managed policy runs only managed hooks, so Observer's user-level hooks never run",
		Grounding: groundingConfigSurface + " (claude-code V18 managed allowManagedHooksOnly; finding 6)",
	},
	{
		ID: "copilot-cli.disableAllHooks", Scope: ScopeUser, HomeRel: ".copilot/settings.json",
		Key: "disableAllHooks", Off: true, Tools: []string{"copilot-cli"},
		Effect:    "every Copilot CLI hook is off",
		Grounding: groundingConfigSurface + " (copilot-cli V18)",
	},
	{
		ID: "qwen.disableAllHooks", Scope: ScopeUser, HomeRel: ".qwen/settings.json",
		Key: "disableAllHooks", Off: true, Tools: []string{"qwen-code"},
		Effect:    "every Qwen Code hook is off",
		Grounding: groundingConfigSurface + " (qwen-code V18)",
	},
	{
		ID: "gemini.hooksConfig.enabled", Scope: ScopeUser, HomeRel: ".gemini/settings.json",
		Key: "hooksConfig.enabled", Off: false, Tools: []string{"gemini-cli"},
		Effect:    "the Gemini CLI hook system is disabled",
		Grounding: groundingConfigSurface + " (gemini-cli V18; finding 6)",
	},
	{
		ID: "factory.hooksDisabled", Scope: ScopeUser, HomeRel: ".factory/settings.json",
		Key: "hooksDisabled", Off: true, Tools: []string{"droid"},
		Effect:    "every Factory Droid hook is off",
		Grounding: groundingConfigSurface + " (droid; finding 6)",
	},
}

// claudeManagedSettings is Claude Code's managed-settings file per GOOS.
var claudeManagedSettings = map[string]string{
	"linux":   "/etc/claude-code/managed-settings.json",
	"darwin":  "/Library/Application Support/ClaudeCode/managed-settings.json",
	"windows": `C:\ProgramData\ClaudeCode\managed-settings.json`,
}

// Finding is one switch found set.
type Finding struct {
	SwitchID string
	Scope    Scope
	Path     string
	Key      string
	Tools    []string
	Effect   string
}

// Options are the injected inputs.
type Options struct {
	// Home is the home directory user-scope paths resolve against ("" skips
	// every user-scope row).
	Home string
	// GOOS selects the managed path ("" skips every managed-scope row, as
	// a sandboxed doctor run does).
	GOOS string
	// ReadFile reads one file (os.ReadFile in production). An error means
	// the file is absent or unreadable, which is never a finding.
	ReadFile func(path string) ([]byte, error)
}

// Detect walks the table and returns every switch currently set, in table
// order. An absent, unreadable or malformed file is not a finding: unknown
// is never reported as off.
func Detect(o Options) []Finding {
	if o.ReadFile == nil {
		return nil
	}
	cache := map[string]map[string]any{}
	var out []Finding
	for _, s := range Switches {
		path := s.path(o)
		if path == "" {
			continue
		}
		doc, seen := cache[path]
		if !seen {
			doc = decode(o.ReadFile, path)
			cache[path] = doc
		}
		if v, ok := lookupBool(doc, s.Key); ok && v == s.Off {
			out = append(out, Finding{SwitchID: s.ID, Scope: s.Scope, Path: path, Key: s.Key, Tools: s.Tools, Effect: s.Effect})
		}
	}
	return out
}

func (s Switch) path(o Options) string {
	switch s.Scope {
	case ScopeUser:
		if o.Home == "" || s.HomeRel == "" {
			return ""
		}
		return filepath.Join(o.Home, filepath.FromSlash(s.HomeRel))
	case ScopeManaged:
		if o.GOOS == "" {
			return ""
		}
		return s.Abs[o.GOOS]
	}
	return ""
}

func decode(read func(string) ([]byte, error), path string) map[string]any {
	b, err := read(path)
	if err != nil {
		return nil
	}
	b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	return doc
}

// lookupBool resolves a dotted key to a JSON boolean (ok=false when any
// segment is missing or the leaf is not a boolean).
func lookupBool(doc map[string]any, key string) (bool, bool) {
	cur := any(doc)
	for _, seg := range strings.Split(key, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return false, false
		}
		if cur, ok = m[seg]; !ok {
			return false, false
		}
	}
	v, ok := cur.(bool)
	return v, ok
}

// AffectedTools is the sorted union of the tools every finding silences.
func AffectedTools(fs []Finding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fs {
		for _, t := range f.Tools {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Tracker turns successive Detect results into transitions: a switch is
// reported once when it is first seen set and again only after it was seen
// clear in between. One Tracker per daemon process (the daemon's hook loop
// owns it).
type Tracker struct {
	on map[string]bool
}

// Turned returns the findings that switched on since the previous call.
func (t *Tracker) Turned(fs []Finding) []Finding {
	if t.on == nil {
		t.on = map[string]bool{}
	}
	now := map[string]bool{}
	var out []Finding
	for _, f := range fs {
		k := f.SwitchID + "\x00" + f.Path
		now[k] = true
		if !t.on[k] {
			out = append(out, f)
		}
	}
	t.on = now
	return out
}
