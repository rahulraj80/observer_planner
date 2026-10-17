package config

import (
	"fmt"
	"maps"
	"strings"
)

// Profile ASSIGNMENT edits: the one pure owner of "point traffic class X
// at profile Y" for both front doors, `observer profile assign` (CLI) and
// the Settings > Profiles per-tool card (dashboard, T-369). Both resolve
// the target string through ParseProfileTarget and edit a ProfilesConfig
// value through AssignProfile / UnassignProfile; the callers own the I/O
// (config load, profile-name validation against the ProfileStore, the
// atomic write and the reload poke).

// ProfileTargetKind is the closed set of assignment tiers ResolveProfileName
// walks: the table default, a provider class, and one specific tool.
type ProfileTargetKind string

// The three assignment tiers.
const (
	ProfileTargetDefault  ProfileTargetKind = "default"
	ProfileTargetProvider ProfileTargetKind = "provider"
	ProfileTargetTool     ProfileTargetKind = "tool"
)

// ProfileTarget is a parsed traffic-class target. Key is the provider
// class ("anthropic", "openai") or the tool name; empty for the default.
type ProfileTarget struct {
	Kind ProfileTargetKind
	Key  string
}

// String renders the target in the CLI's spelling ("default",
// "anthropic", "tool:cline").
func (t ProfileTarget) String() string {
	switch t.Kind {
	case ProfileTargetDefault:
		return "default"
	case ProfileTargetTool:
		return "tool:" + t.Key
	default:
		return t.Key
	}
}

// profileToolPrefix introduces a per-tool target.
const profileToolPrefix = "tool:"

// profileTargetRules is the ordered parse table, walked top-down: the first
// row whose match accepts the target decides it. One test row per entry.
var profileTargetRules = []struct {
	name  string
	parse func(target string) (ProfileTarget, bool, error)
}{
	{"default", func(t string) (ProfileTarget, bool, error) {
		return ProfileTarget{Kind: ProfileTargetDefault}, t == "default", nil
	}},
	{"provider", func(t string) (ProfileTarget, bool, error) {
		_, known := defaultProfiles().ByProvider[t]
		return ProfileTarget{Kind: ProfileTargetProvider, Key: t}, known, nil
	}},
	{"tool", func(t string) (ProfileTarget, bool, error) {
		if !strings.HasPrefix(t, profileToolPrefix) {
			return ProfileTarget{}, false, nil
		}
		tool := strings.TrimSpace(strings.TrimPrefix(t, profileToolPrefix))
		if tool == "" {
			return ProfileTarget{}, true, fmt.Errorf("tool: prefix needs a tool name (e.g. tool:cline)")
		}
		if strings.ContainsAny(tool, " \t\r\n\"'[]=.") {
			return ProfileTarget{}, true, fmt.Errorf("tool name %q is not a plain tool id (e.g. tool:cline)", tool)
		}
		return ProfileTarget{Kind: ProfileTargetTool, Key: tool}, true, nil
	}},
}

// ParseProfileTarget resolves a target string ("default", "anthropic",
// "openai", "tool:NAME") to its tier. Unknown targets error with the valid
// spellings, the same message `observer profile assign` always printed.
func ParseProfileTarget(target string) (ProfileTarget, error) {
	for _, rule := range profileTargetRules {
		pt, ok, err := rule.parse(target)
		if err != nil {
			return ProfileTarget{}, err
		}
		if ok {
			return pt, nil
		}
	}
	return ProfileTarget{}, fmt.Errorf("unknown traffic class %q (valid: anthropic, openai, default, tool:NAME)", target)
}

// AssignProfile returns a copy of pc with target pointed at name. It does
// NOT check that name resolves; callers validate it against their
// ProfileStore first (built-ins plus user profiles). The input maps are
// never mutated.
func AssignProfile(pc ProfilesConfig, target, name string) (ProfilesConfig, error) {
	pt, err := ParseProfileTarget(target)
	if err != nil {
		return pc, err
	}
	if strings.TrimSpace(name) == "" {
		return pc, fmt.Errorf("profile name required for %s", pt)
	}
	out := cloneProfilesConfig(pc)
	switch pt.Kind {
	case ProfileTargetDefault:
		out.Default = name
	case ProfileTargetProvider:
		if out.ByProvider == nil {
			out.ByProvider = map[string]string{}
		}
		out.ByProvider[pt.Key] = name
	case ProfileTargetTool:
		if out.ByTool == nil {
			out.ByTool = map[string]string{}
		}
		out.ByTool[pt.Key] = name
	}
	return out, nil
}

// UnassignProfile returns a copy of pc with a TOOL target's assignment
// removed, so that tool falls back to its provider assignment again.
// Default and provider targets always resolve to some profile, so they are
// re-pointed with AssignProfile rather than removed; asking to remove one
// errors. Removing a tool that has no assignment is a no-op (removed=false).
func UnassignProfile(pc ProfilesConfig, target string) (out ProfilesConfig, removed bool, err error) {
	pt, err := ParseProfileTarget(target)
	if err != nil {
		return pc, false, err
	}
	if pt.Kind != ProfileTargetTool {
		return pc, false, fmt.Errorf("%s always has a profile; assign a different one instead of removing it", pt)
	}
	out = cloneProfilesConfig(pc)
	if _, ok := out.ByTool[pt.Key]; !ok {
		return out, false, nil
	}
	delete(out.ByTool, pt.Key)
	if len(out.ByTool) == 0 {
		out.ByTool = nil
	}
	return out, true, nil
}

// cloneProfilesConfig deep-copies the two assignment maps.
func cloneProfilesConfig(pc ProfilesConfig) ProfilesConfig {
	out := pc
	if pc.ByProvider != nil {
		out.ByProvider = maps.Clone(pc.ByProvider)
	}
	if pc.ByTool != nil {
		out.ByTool = maps.Clone(pc.ByTool)
	}
	return out
}
