package agentconfigorg

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// Issue severities.
const (
	SevError   = "error"
	SevWarning = "warning"
)

// Issue is one lint finding. Entry is the index into Spec.Settings, or -1
// for a document-level finding.
type Issue struct {
	Entry    int    `json:"entry"`
	Field    string `json:"field,omitempty"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Blocking reports whether any issue blocks publishing.
func Blocking(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == SevError {
			return true
		}
	}
	return false
}

// SecretDetector reports a value that looks like a credential.
type SecretDetector func(string) bool

var projectHashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Lint runs the publish-time sanity check on one document. detect may be
// nil (only catalogue-marked secrets and credential-named keys are caught).
func Lint(s Spec, detect SecretDetector) []Issue {
	s = Normalize(s)
	var out []Issue
	add := func(entry int, field, code, sev, msg string) {
		out = append(out, Issue{Entry: entry, Field: field, Code: code, Severity: sev, Message: msg})
	}
	for _, k := range knobRules {
		if v := k.get(s); v != "" && !k.ok(v) {
			add(-1, k.field, "bad_value", SevError, fmt.Sprintf("%s must be one of %s.", k.field, strings.Join(k.values, ", ")))
		}
	}
	for _, g := range s.Grants {
		if msg := checkGrant(g); msg != "" {
			add(-1, "grants", "bad_grant", SevError, msg)
		}
	}
	seen := map[string]int{}
	for i, e := range s.Settings {
		if prev, dup := seen[entryKey(e)]; dup {
			add(i, "setting", "duplicate", SevError, fmt.Sprintf("Entry %d already sets this for the same scope and mode.", prev+1))
		}
		seen[entryKey(e)] = i
		for _, is := range lintEntry(e, detect) {
			add(i, is.Field, is.Code, is.Severity, is.Message)
		}
	}
	seenInstr := map[string]int{}
	for i, in := range s.Instructions {
		field := fmt.Sprintf("instructions.%d", i)
		if prev, dup := seenInstr[in.Project]; dup {
			add(-1, field, "duplicate", SevError, fmt.Sprintf("Instruction %d already covers this scope; put the text in one instruction.", prev+1))
		}
		seenInstr[in.Project] = i
		for _, is := range lintInstruction(in, detect) {
			add(-1, field, is.Code, is.Severity, fmt.Sprintf("Instruction %d: %s", i+1, is.Message))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Entry < out[j].Entry })
	return out
}

// observerMarkerRE matches an Observer block marker anywhere in a text.
var observerMarkerRE = regexp.MustCompile(`<!--\s*observer:`)

// lintInstruction checks one org instruction (D-107, T-426).
func lintInstruction(in InstructionEntry, detect SecretDetector) []Issue {
	var out []Issue
	add := func(code, sev, msg string) {
		out = append(out, Issue{Code: code, Severity: sev, Message: msg})
	}
	switch {
	case strings.TrimSpace(in.Text) == "":
		add("empty_instruction", SevError, "The text is empty.")
	case len(in.Text) > orgcontract.AgentConfigInstructionMaxBytes:
		add("too_large", SevError, fmt.Sprintf("The text is larger than %d bytes.", orgcontract.AgentConfigInstructionMaxBytes))
	case !utf8.ValidString(in.Text) || strings.IndexByte(in.Text, 0) >= 0:
		add("bad_text", SevError, "The text must be plain UTF-8 without NUL bytes.")
	case observerMarkerRE.MatchString(in.Text):
		add("observer_marker", SevError, "The text may not contain an Observer block marker (<!-- observer:...).")
	}
	if detect != nil && detect(in.Text) {
		add("secret_value", SevError, "The text looks like it holds a credential. The organization's policy never distributes secrets.")
	}
	if in.Project != "" {
		if !projectHashRE.MatchString(in.Project) {
			add("bad_project", SevError, "A project is addressed by its identity (a 64-character hash); pick it from the project list.")
		}
		switch in.Placement {
		case orgcontract.AgentConfigPlacementLocalOnly:
			add("local_only_instructions", SevWarning, "Tools without a personal (local-only) instruction file, which is most of them, get no project block; choose shared placement to reach them through the file the repository tracks.")
		case orgcontract.AgentConfigPlacementShared:
			add("tracked_file", SevWarning, "Shared placement writes the block into an instruction file the repository tracks: developers may commit it into the repository.")
		default:
			add("bad_value", SevError, "placement must be local_only or shared.")
		}
	}
	return out
}

// knobRule is one document-level closed field.
type knobRule struct {
	field  string
	values []string
	get    func(Spec) string
}

func (k knobRule) ok(v string) bool {
	for _, x := range k.values {
		if x == v {
			return true
		}
	}
	return false
}

var knobRules = []knobRule{
	{"secrets_posture", []string{orgcontract.AgentConfigSecretsRefuse, orgcontract.AgentConfigSecretsWarn, orgcontract.AgentConfigSecretsAllow}, func(s Spec) string { return s.SecretsPosture }},
	{"on_drift", []string{orgcontract.AgentConfigOnDriftReassert, orgcontract.AgentConfigOnDriftReport}, func(s Spec) string { return s.OnDrift }},
	{"developer_access", []string{orgcontract.AgentConfigAccessReadOnly, orgcontract.AgentConfigAccessEditable}, func(s Spec) string { return s.DeveloperAccess }},
}

// checkGrant validates one grant.
func checkGrant(g string) string {
	if tool, ok := strings.CutSuffix(g, "/*"); ok {
		if !catalogTool(tool) {
			return fmt.Sprintf("Grant %q names a tool Observer's catalogue does not know.", g)
		}
		return ""
	}
	r, ok := agentconfig.Setting(g)
	if !ok {
		return fmt.Sprintf("Grant %q is not a catalogue setting id (or <tool>/*).", g)
	}
	if !r.Writable {
		return fmt.Sprintf("Grant %q names a setting nobody can edit (%s).", g, r.Owner)
	}
	return ""
}

// ValidEditTarget reports whether g is something an edit grant may name: a
// writable catalogue setting id or "<tool>/*" for a catalogue tool (the
// same vocabulary as a policy's own grants; T-436's access-grant target).
func ValidEditTarget(g string) bool { return checkGrant(g) == "" }

func catalogTool(tool string) bool {
	for _, c := range agentconfig.Coverage() {
		if c.Tool == tool {
			return true
		}
	}
	return false
}

func lintEntry(e Entry, detect SecretDetector) []Issue {
	var out []Issue
	add := func(field, code, sev, msg string) {
		out = append(out, Issue{Field: field, Code: code, Severity: sev, Message: msg})
	}
	if e.Mode != orgcontract.AgentConfigModeEnforced && e.Mode != orgcontract.AgentConfigModeDefault {
		add("mode", "bad_value", SevError, "mode must be enforced or default.")
	}
	if e.Project != "" {
		if !projectHashRE.MatchString(e.Project) {
			add("project", "bad_project", SevError, "A project is addressed by its identity (a 64-character hash); pick it from the project list.")
		}
		switch e.Placement {
		case orgcontract.AgentConfigPlacementLocalOnly:
		case orgcontract.AgentConfigPlacementShared:
			add("placement", "tracked_file", SevWarning, "Shared placement writes into a file the repository tracks: developers may commit it into the repository.")
		default:
			add("placement", "bad_value", SevError, "placement must be local_only or shared.")
		}
	}
	if secretIn(e.Value, detect) {
		add("value", "secret_value", SevError, "This value looks like a credential. The organization's policy never distributes secrets; use an environment-variable reference instead.")
	}
	switch {
	case (e.Setting == "") == (e.Common == ""):
		add("setting", "bad_entry", SevError, "Set exactly one of a tool setting or a common key.")
	case e.Setting != "":
		out = append(out, lintSetting(e)...)
	default:
		out = append(out, lintCommon(e)...)
	}
	return out
}

func lintSetting(e Entry) []Issue {
	var out []Issue
	add := func(field, code, sev, msg string) {
		out = append(out, Issue{Field: field, Code: code, Severity: sev, Message: msg})
	}
	r, ok := agentconfig.Setting(e.Setting)
	if !ok {
		add("setting", "unknown_setting", SevError, fmt.Sprintf("%q is not in Observer's settings catalogue.", e.Setting))
		return out
	}
	for _, rule := range settingRules {
		if code, sev, msg := rule(r, e); code != "" {
			add("setting", code, sev, msg)
			if sev == SevError {
				return out
			}
		}
	}
	if msg := agentconfig.CheckValue(r, e.Value); msg != "" {
		add("value", "invalid_value", SevError, msg)
	}
	return out
}

// settingRules are the per-setting refusals and warnings, walked in order;
// the first error stops the walk.
var settingRules = []func(r agentconfig.SettingRow, e Entry) (code, sev, msg string){
	func(r agentconfig.SettingRow, _ Entry) (string, string, string) {
		if r.Secret {
			return "secret_setting", SevError, "This setting holds a credential. The organization's policy never distributes secrets."
		}
		return "", "", ""
	},
	func(r agentconfig.SettingRow, _ Entry) (string, string, string) {
		if r.Owner != agentconfig.OwnerUser || !r.Writable {
			return "not_writable", SevError, "Observer does not write this setting (" + string(r.Owner) + "); it cannot be set by policy."
		}
		return "", "", ""
	},
	func(r agentconfig.SettingRow, _ Entry) (string, string, string) {
		if len(agentconfig.SpansFor(r.ID)) > 0 {
			return "observer_span", SevError, "Observer's own hook, MCP or proxy entries live inside this setting; setting it by policy would overwrite them."
		}
		return "", "", ""
	},
	func(r agentconfig.SettingRow, e Entry) (string, string, string) {
		switch {
		case e.Project == "" && agentconfig.UserFileFor(r.ID) == "":
			return "no_user_file", SevError, "The tool does not read this setting from a user-level file Observer can write; target a project instead."
		case e.Project != "" && agentconfig.ProjectFileFor(r.ID, e.Placement != orgcontract.AgentConfigPlacementShared) == "":
			if e.Placement == orgcontract.AgentConfigPlacementShared {
				return "no_project_file", SevError, "The tool does not read this setting from a shared project file."
			}
			return "no_local_file", SevError, "The tool has no personal (local-only) project file for this setting; choose shared placement or the user level."
		}
		return "", "", ""
	},
	func(r agentconfig.SettingRow, e Entry) (string, string, string) {
		if e.Project == "" && e.Mode == orgcontract.AgentConfigModeEnforced && agentconfig.TrackedShadow(r.ID) {
			return "tracked_shadow", SevWarning, "Where a repository's own project file sets this key, managed machines write the enforced value into that file (the tool has no local-only file to override it), so the change can be committed with the repository."
		}
		return "", "", ""
	},
	func(r agentconfig.SettingRow, _ Entry) (string, string, string) {
		if r.Verified == agentconfig.VerifiedUnverified {
			return "unverified", SevWarning, "This setting is not verified against a current version of the tool."
		}
		return "", "", ""
	},
	func(r agentconfig.SettingRow, _ Entry) (string, string, string) {
		switch r.Danger {
		case agentconfig.DangerConfirm:
			return "dangerous_value", SevWarning, "This setting can loosen the tool's safety (approvals, sandbox, or a command it runs)."
		case agentconfig.DangerRefuseWhileIntegrated:
			return "refused_while_integrated", SevWarning, "A machine where Observer integrates this tool refuses a value that switches off its capture, hooks or MCP server; it reports the refusal."
		}
		return "", "", ""
	},
}

func lintCommon(e Entry) []Issue {
	var out []Issue
	add := func(field, code, sev, msg string) {
		out = append(out, Issue{Field: field, Code: code, Severity: sev, Message: msg})
	}
	spec, ok := agentconfig.CommonSpecFor(agentconfig.CommonKey(e.Common))
	if !ok || spec.Tier == agentconfig.TierObserverOwned {
		add("common", "unknown_common", SevError, fmt.Sprintf("%q is not a common setting the organization can set.", e.Common))
		return out
	}
	natives, skipped, err := ExpandCommon(agentconfig.CommonKey(e.Common), e.Value, e.Project, e.Placement)
	if err != nil {
		add("value", "invalid_value", SevError, err.Error())
		return out
	}
	if len(natives) == 0 {
		add("common", "no_tool", SevError, "No tool can take this value at this scope.")
	} else if len(skipped) > 0 {
		add("common", "partial", SevWarning, fmt.Sprintf("Applies to %s; %s cannot take it.", agentconfig.CountNoun(len(toolsOf(natives)), "tool", ""), agentconfig.CountNoun(len(skipped), "tool", "")))
	}
	if agentconfig.CommonDanger(agentconfig.CommonKey(e.Common), e.Value) {
		add("value", "dangerous_value", SevWarning, "This value loosens the tools' safety.")
	}
	return out
}

func toolsOf(ns []Native) map[string]bool {
	out := map[string]bool{}
	for _, n := range ns {
		out[n.Tool] = true
	}
	return out
}

// Native is one native setting a common value expands to.
type Native struct {
	Tool    string
	Setting string
	Value   any
}

// Skip is one tool a common value does not reach, with the reason.
type Skip struct {
	Tool   string `json:"tool"`
	Reason string `json:"reason"`
}

// ExpandCommon turns a canonical common value into native settings for
// every tool that exposes the key at the scope (the node-independent part
// of the L1 fan-out: no installed check, which the node does). An
// inexpressible value for one tool skips that tool, never approximates.
func ExpandCommon(key agentconfig.CommonKey, value any, project, placement string) ([]Native, []Skip, error) {
	targets, err := agentconfig.PlanFanOut(key, value, false, nil)
	if err != nil {
		return nil, nil, err
	}
	var natives []Native
	var skips []Skip
	for _, t := range targets {
		// A project target does not need a user file: it is re-planned below.
		projectOnly := project != "" && t.Status == agentconfig.FanOutNoUserFile
		if t.Status != agentconfig.FanOutWrite && !projectOnly {
			skips = append(skips, Skip{Tool: t.Tool, Reason: t.Reason})
			continue
		}
		if len(t.Natives) == 0 {
			ns, err := forwardProject(key, value, t.Tool)
			if err != nil {
				skips = append(skips, Skip{Tool: t.Tool, Reason: err.Error()})
				continue
			}
			t.Natives = ns
		}
		ok := true
		for _, n := range t.Natives {
			if project != "" && agentconfig.ProjectFileFor(n.SettingID, placement != orgcontract.AgentConfigPlacementShared) == "" {
				ok = false
				break
			}
		}
		if !ok {
			skips = append(skips, Skip{Tool: t.Tool, Reason: "The tool has no project file for this setting at the chosen placement."})
			continue
		}
		for _, n := range t.Natives {
			natives = append(natives, Native{Tool: t.Tool, Setting: n.SettingID, Value: n.Value})
		}
	}
	return natives, skips, nil
}

// errNotWritable is a map row whose native setting Observer does not write.
var errNotWritable = errors.New("a native setting is not editable")

// forwardProject expands a common value for one tool without the user-file
// requirement (a project-only setting).
func forwardProject(key agentconfig.CommonKey, value any, tool string) ([]agentconfig.NativeWrite, error) {
	m, ok := agentconfig.MapRowFor(tool, key)
	if !ok {
		return nil, fmt.Errorf("%s does not expose %s", tool, key)
	}
	vals, err := m.Forward(value)
	if err != nil {
		return nil, err
	}
	out := make([]agentconfig.NativeWrite, 0, len(m.Settings))
	for i, id := range m.Settings {
		r, ok := agentconfig.Setting(id)
		if !ok || !r.Writable {
			return nil, errNotWritable
		}
		out = append(out, agentconfig.NativeWrite{SettingID: id, Path: r.Path.String(), Value: vals[i]})
	}
	return out, nil
}

// secretIn reports a credential anywhere in a value: a detector hit on a
// string, or a non-reference string under a credential-named key.
func secretIn(v any, detect SecretDetector) bool {
	switch x := v.(type) {
	case string:
		return detect != nil && detect(x)
	case []any:
		for _, e := range x {
			if secretIn(e, detect) {
				return true
			}
		}
	case map[string]any:
		for k, e := range x {
			if s, ok := e.(string); ok && agentconfig.SecretKeyName(k) && s != "" && !isReference(s) {
				return true
			}
			if secretIn(e, detect) {
				return true
			}
		}
	}
	return false
}

// isReference reports an environment reference rather than a value.
func isReference(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") || strings.HasPrefix(s, "$") && !strings.ContainsAny(s, " /")
}
