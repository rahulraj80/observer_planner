package agentconfig

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TransformID names one entry of the closed transform table (plan section
// 2.4). Every MapRow names exactly one; each has a forward (canonical ->
// native) and a reverse (native -> canonical) pure function.
type TransformID string

const (
	TransformIdentity       TransformID = "identity"
	TransformEnumMap        TransformID = "enum_map"
	TransformBoolInvert     TransformID = "bool_invert"
	TransformScale          TransformID = "scale"
	TransformEffortToBudget TransformID = "effort_to_budget"
	TransformListJoin       TransformID = "list_join"
	TransformRuleTranslate  TransformID = "rule_translate"
	TransformModelAlias     TransformID = "model_alias"
	TransformMCPShape       TransformID = "mcp_shape"
)

// Transforms returns the closed transform table's ids, sorted.
func Transforms() []TransformID {
	out := make([]TransformID, 0, len(transformTable))
	for id := range transformTable {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Errors a transform returns. Each wraps one of these so callers branch
// with errors.Is.
var (
	// ErrRefused: the canonical value cannot be expressed in this tool's
	// native vocabulary (a mode it lacks, a rule its grammar cannot spell,
	// a model its catalogue cannot serve). The write is refused for this
	// tool; the fan-out preview names it.
	ErrRefused = errors.New("agentconfig: not expressible for this tool")
	// ErrNoCanonical: a native value has no canonical spelling (a named
	// theme, a vendor-only mode). The common form shows it as tool-specific.
	ErrNoCanonical = errors.New("agentconfig: native value has no canonical form")
	// ErrBadValue: the value has the wrong type for the key.
	ErrBadValue = errors.New("agentconfig: value has the wrong type")
	// ErrBadMapRow: the map row itself is malformed (pinned by the tests).
	ErrBadMapRow = errors.New("agentconfig: malformed map row")
)

// Encoding says how a native value is spelled on disk. Env-backed keys
// (an `env` block) hold strings whatever the logical type.
type Encoding string

const (
	EncNative    Encoding = ""
	EncEnvString Encoding = "env_string"
	// EncEnvBool spells true as "1" and false as "0".
	EncEnvBool Encoding = "env_bool"
	EncEnvInt  Encoding = "env_int"
)

// EnumPair maps one canonical value to the native value of every setting
// the row fans out to (same order as MapRow.Settings; nil = the setting is
// absent). The first pair whose Native matches wins on the reverse.
type EnumPair struct {
	Canonical any
	Native    []any
}

// EffortBudget maps a canonical reasoning effort to a thinking-token budget.
type EffortBudget struct {
	Effort string
	Tokens int64
}

// RuleStyle is how a tool spells one rule kind.
type RuleStyle string

const (
	// RuleBare: the tool name alone; a pattern is not expressible.
	RuleBare RuleStyle = "bare"
	// RuleCall: Name, or Name(pattern).
	RuleCall RuleStyle = "call"
	// RulePrefix: Name + pattern, with "/" in the pattern spelled Sep
	// (MCP rules: mcp__server__tool).
	RulePrefix RuleStyle = "prefix"
)

// RuleForm is one canonical rule kind's native spelling.
type RuleForm struct {
	Name  string
	Style RuleStyle
	// PatternPrefix is prepended to the pattern inside a call form
	// (Claude WebFetch(domain:example.com)).
	PatternPrefix string
	Sep           string
}

// RuleGrammar maps canonical rule kinds onto one tool's spelling. A kind
// missing from the grammar is refused.
type RuleGrammar map[string]RuleForm

// RuleKinds is the closed set of canonical rule kinds.
func RuleKinds() []string {
	return []string{"edit", "fetch", "mcp", "read", "search", "shell", "write"}
}

// nativeRulePrefix marks a native rule the reverse could not translate; it
// round-trips into the same tool only.
const nativeRulePrefix = "native:"

// ModelSpelling says how the tool spells a canonical "<provider>/<model>".
type ModelSpelling struct {
	// Qualified tools take "provider/model" verbatim (OpenCode, Kilo);
	// the rest take the bare model id.
	Qualified bool
}

// MCPShape describes a tool's MCP server entry shape. The canonical entry
// is {command, args, env} for a local server or {type, url} for a remote
// one (type "http" or "sse").
type MCPShape struct {
	// CommandArray: the command and its args are ONE array (OpenCode).
	CommandArray bool
	// EnvKey is the entry's environment key ("env", OpenCode "environment").
	EnvKey string
	// TypeKey is the entry's transport key ("" = the tool infers it).
	TypeKey string
	// TypeNames maps canonical transports (stdio, http, sse) to the
	// tool's spelling; a missing transport is refused.
	TypeNames map[string]string
	// URLKeys maps a canonical remote transport to the key holding its URL
	// (Gemini: http -> httpUrl, sse -> url).
	URLKeys map[string]string
	// StdioTypeExplicit: local entries carry the type key too.
	StdioTypeExplicit bool
}

// TransformParams carries one MapRow's transform data. Only the fields its
// transform reads are set.
type TransformParams struct {
	Enum     []EnumPair
	Factor   float64
	Round    bool
	Budgets  []EffortBudget
	Sep      string
	Grammar  RuleGrammar
	Model    ModelSpelling
	MCP      MCPShape
	Encoding Encoding
}

type transformImpl struct {
	// fanOut: the transform may write several settings (enum_map only).
	fanOut  bool
	forward func(m MapRow, v any) ([]any, error)
	reverse func(m MapRow, natives []any) (any, error)
}

var transformTable = map[TransformID]transformImpl{
	TransformIdentity:       {forward: fwdIdentity, reverse: revIdentity},
	TransformEnumMap:        {fanOut: true, forward: fwdEnumMap, reverse: revEnumMap},
	TransformBoolInvert:     {forward: fwdBoolInvert, reverse: revBoolInvert},
	TransformScale:          {forward: fwdScale, reverse: revScale},
	TransformEffortToBudget: {forward: fwdEffort, reverse: revEffort},
	TransformListJoin:       {forward: fwdListJoin, reverse: revListJoin},
	TransformRuleTranslate:  {forward: fwdRules, reverse: revRules},
	TransformModelAlias:     {forward: fwdModel, reverse: revModel},
	TransformMCPShape:       {forward: fwdMCP, reverse: revMCP},
}

// Forward translates a canonical value into one native value per setting
// of the row (in Settings order), encoded for the file.
func (m MapRow) Forward(v any) ([]any, error) {
	impl, err := m.impl()
	if err != nil {
		return nil, err
	}
	nv, err := cfgfmt.Normalize(v)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadValue, err)
	}
	out, err := impl.forward(m, nv)
	if err != nil {
		return nil, err
	}
	if len(out) != len(m.Settings) {
		return nil, fmt.Errorf("%w: %s gave %d values for %d settings", ErrBadMapRow, m.Transform, len(out), len(m.Settings))
	}
	for i := range out {
		if out[i], err = encode(m.Params.Encoding, out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Reverse projects native values (one per setting, nil = absent) onto the
// canonical value. present is false when every setting is absent.
func (m MapRow) Reverse(natives []any) (v any, present bool, err error) {
	impl, err := m.impl()
	if err != nil {
		return nil, false, err
	}
	if len(natives) != len(m.Settings) {
		return nil, false, fmt.Errorf("%w: %d natives for %d settings", ErrBadMapRow, len(natives), len(m.Settings))
	}
	dec := make([]any, len(natives))
	for i, n := range natives {
		if n == nil {
			continue
		}
		present = true
		nn, err := cfgfmt.Normalize(n)
		if err != nil {
			return nil, true, fmt.Errorf("%w: %w", ErrBadValue, err)
		}
		if dec[i], err = decode(m.Params.Encoding, nn); err != nil {
			return nil, true, err
		}
	}
	if !present {
		return nil, false, nil
	}
	v, err = impl.reverse(m, dec)
	return v, true, err
}

func (m MapRow) impl() (transformImpl, error) {
	impl, ok := transformTable[m.Transform]
	if !ok {
		return transformImpl{}, fmt.Errorf("%w: unknown transform %q", ErrBadMapRow, m.Transform)
	}
	if len(m.Settings) == 0 || (len(m.Settings) > 1 && !impl.fanOut) {
		return transformImpl{}, fmt.Errorf("%w: %s over %d settings", ErrBadMapRow, m.Transform, len(m.Settings))
	}
	return impl, nil
}

// --- encoding ---------------------------------------------------------------

func encode(e Encoding, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch e {
	case EncNative:
		return v, nil
	case EncEnvString:
		if s, ok := v.(string); ok {
			return s, nil
		}
	case EncEnvBool:
		if b, ok := v.(bool); ok {
			if b {
				return "1", nil
			}
			return "0", nil
		}
	case EncEnvInt:
		if n, ok := asInt(v); ok {
			return strconv.FormatInt(n, 10), nil
		}
	}
	return nil, fmt.Errorf("%w: %T for %s encoding", ErrBadValue, v, e)
}

func decode(e Encoding, v any) (any, error) {
	if e == EncNative {
		return v, nil
	}
	s, ok := v.(string)
	if !ok {
		// A typed value in an env block (a YAML int) still decodes.
		s = fmt.Sprint(v)
	}
	s = strings.TrimSpace(s)
	switch e {
	case EncEnvString:
		return s, nil
	case EncEnvBool:
		switch strings.ToLower(s) {
		case "1", "true", "yes", "on":
			return true, nil
		case "0", "false", "no", "off", "":
			return false, nil
		}
	case EncEnvInt:
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, nil
		}
	}
	return nil, fmt.Errorf("%w: %q is not a %s value", ErrNoCanonical, s, e)
}

// --- the transforms --------------------------------------------------------

func fwdIdentity(_ MapRow, v any) ([]any, error) { return []any{v}, nil }

// revIdentity accepts a scalar string where the canonical form is a list
// (Gemini context.fileName takes a string or an array).
func revIdentity(m MapRow, n []any) (any, error) {
	if s, ok := n[0].(string); ok {
		if spec, ok := CommonSpecFor(m.Key); ok && spec.Type == TypeStringList {
			return []any{s}, nil
		}
	}
	return n[0], nil
}

func fwdEnumMap(m MapRow, v any) ([]any, error) {
	for _, p := range m.Params.Enum {
		if cfgfmt.Equal(normalized(p.Canonical), v) {
			return normalizedAll(p.Native), nil
		}
	}
	return nil, fmt.Errorf("%w: %s=%v", ErrRefused, m.Key, v)
}

func revEnumMap(m MapRow, n []any) (any, error) {
	for _, p := range m.Params.Enum {
		if len(p.Native) != len(n) {
			return nil, fmt.Errorf("%w: enum pair width", ErrBadMapRow)
		}
		match := true
		for i, want := range p.Native {
			if !cfgfmt.Equal(normalized(want), n[i]) {
				match = false
				break
			}
		}
		if match {
			return normalized(p.Canonical), nil
		}
	}
	return nil, fmt.Errorf("%w: %v", ErrNoCanonical, n)
}

func fwdBoolInvert(m MapRow, v any) ([]any, error) {
	b, ok := v.(bool)
	if !ok {
		return nil, fmt.Errorf("%w: %s wants a bool", ErrBadValue, m.Key)
	}
	return []any{!b}, nil
}

func revBoolInvert(m MapRow, n []any) (any, error) {
	b, ok := n[0].(bool)
	if !ok {
		return nil, fmt.Errorf("%w: %v", ErrNoCanonical, n[0])
	}
	return !b, nil
}

func fwdScale(m MapRow, v any) ([]any, error) {
	f, ok := asFloat(v)
	if !ok || m.Params.Factor == 0 {
		return nil, fmt.Errorf("%w: %s wants a number", ErrBadValue, m.Key)
	}
	out := f * m.Params.Factor
	if m.Params.Round {
		return []any{int64(math.Round(out))}, nil
	}
	return []any{trimFloat(out)}, nil
}

// trimFloat drops binary-float noise past nine decimals (70 * 0.01 is
// 0.7000000000000001), so a written fraction reads as the user expects.
func trimFloat(f float64) float64 { return math.Round(f*1e9) / 1e9 }

func revScale(m MapRow, n []any) (any, error) {
	f, ok := asFloat(n[0])
	if !ok || m.Params.Factor == 0 {
		return nil, fmt.Errorf("%w: %v", ErrNoCanonical, n[0])
	}
	c := f / m.Params.Factor
	if spec, ok := CommonSpecFor(m.Key); ok && spec.Type == TypeInt {
		return int64(math.Round(c)), nil
	}
	return trimFloat(c), nil
}

func fwdEffort(m MapRow, v any) ([]any, error) {
	s, _ := v.(string)
	for _, b := range m.Params.Budgets {
		if b.Effort == s {
			return []any{b.Tokens}, nil
		}
	}
	return nil, fmt.Errorf("%w: effort %v", ErrRefused, v)
}

// revEffort picks the budget nearest the native token count (ties go to the
// lower effort): the reason the transform is lossy.
func revEffort(m MapRow, n []any) (any, error) {
	t, ok := asFloat(n[0])
	if !ok || len(m.Params.Budgets) == 0 {
		return nil, fmt.Errorf("%w: %v", ErrNoCanonical, n[0])
	}
	best, dist := "", math.Inf(1)
	for _, b := range m.Params.Budgets {
		if d := math.Abs(float64(b.Tokens) - t); d < dist {
			best, dist = b.Effort, d
		}
	}
	return best, nil
}

func fwdListJoin(m MapRow, v any) ([]any, error) {
	list, err := stringList(v)
	if err != nil {
		return nil, err
	}
	return []any{strings.Join(list, m.Params.Sep)}, nil
}

func revListJoin(m MapRow, n []any) (any, error) {
	s, ok := n[0].(string)
	if !ok || m.Params.Sep == "" {
		return nil, fmt.Errorf("%w: %v", ErrNoCanonical, n[0])
	}
	out := []any{}
	for _, part := range strings.Split(s, m.Params.Sep) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out, nil
}

func fwdRules(m MapRow, v any) ([]any, error) {
	list, err := stringList(v)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(list))
	for _, r := range list {
		native, err := translateRule(m.Params.Grammar, r)
		if err != nil {
			return nil, err
		}
		out = append(out, native)
	}
	return []any{out}, nil
}

func revRules(m MapRow, n []any) (any, error) {
	list, err := stringList(n[0])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoCanonical, n[0])
	}
	out := make([]any, 0, len(list))
	for _, s := range list {
		out = append(out, parseRule(m.Params.Grammar, s))
	}
	return out, nil
}

// translateRule spells one canonical rule ("kind" or "kind:pattern").
func translateRule(g RuleGrammar, rule string) (string, error) {
	if strings.HasPrefix(rule, nativeRulePrefix) {
		return "", fmt.Errorf("%w: %q is another tool's native rule", ErrRefused, rule)
	}
	kind, pattern, _ := strings.Cut(rule, ":")
	f, ok := g[kind]
	if !ok {
		return "", fmt.Errorf("%w: rule kind %q", ErrRefused, kind)
	}
	switch f.Style {
	case RuleBare:
		if pattern != "" {
			return "", fmt.Errorf("%w: %s takes no pattern (%q)", ErrRefused, f.Name, rule)
		}
		return f.Name, nil
	case RuleCall:
		if pattern == "" {
			return f.Name, nil
		}
		return f.Name + "(" + f.PatternPrefix + pattern + ")", nil
	case RulePrefix:
		if pattern == "" {
			return "", fmt.Errorf("%w: %s needs a server name", ErrRefused, kind)
		}
		return f.Name + strings.ReplaceAll(pattern, "/", f.Sep), nil
	}
	return "", fmt.Errorf("%w: style %q", ErrBadMapRow, f.Style)
}

// parseRule is translateRule's inverse; a native rule it cannot read stays
// "native:<raw>".
func parseRule(g RuleGrammar, s string) string {
	kinds := make([]string, 0, len(g))
	for k := range g {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		f := g[kind]
		switch f.Style {
		case RuleBare:
			if s == f.Name {
				return kind
			}
		case RuleCall:
			if s == f.Name {
				return kind
			}
			if inner, ok := strings.CutPrefix(s, f.Name+"("); ok && strings.HasSuffix(inner, ")") {
				inner = strings.TrimSuffix(inner, ")")
				if p, ok := strings.CutPrefix(inner, f.PatternPrefix); ok && p != "" {
					return kind + ":" + p
				}
			}
		case RulePrefix:
			if rest, ok := strings.CutPrefix(s, f.Name); ok && rest != "" {
				return kind + ":" + strings.ReplaceAll(rest, f.Sep, "/")
			}
		}
	}
	return nativeRulePrefix + s
}

func fwdModel(m MapRow, v any) ([]any, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil, fmt.Errorf("%w: %s wants a model id", ErrBadValue, m.Key)
	}
	provider, model, qualified := strings.Cut(s, "/")
	if !qualified {
		provider, model = "", s
	}
	cat := modelCatalog(m.Tool)
	if provider != "" && cat.Kind == integration.ModelCatalogVendor && !containsString(cat.Providers, provider) {
		return nil, fmt.Errorf("%w: %s serves %v, not %q", ErrRefused, m.Tool, cat.Providers, provider)
	}
	if m.Params.Model.Qualified {
		if provider == "" {
			if len(cat.Providers) != 1 {
				return nil, fmt.Errorf("%w: %s needs provider/model, got %q", ErrRefused, m.Tool, s)
			}
			provider = cat.Providers[0]
		}
		return []any{provider + "/" + model}, nil
	}
	return []any{model}, nil
}

func revModel(m MapRow, n []any) (any, error) {
	s, ok := n[0].(string)
	if !ok || s == "" {
		return nil, fmt.Errorf("%w: %v", ErrNoCanonical, n[0])
	}
	if m.Params.Model.Qualified || strings.Contains(s, "/") {
		return s, nil
	}
	if cat := modelCatalog(m.Tool); cat.Kind == integration.ModelCatalogVendor && len(cat.Providers) == 1 {
		return cat.Providers[0] + "/" + s, nil
	}
	return s, nil
}

func modelCatalog(tool string) integration.ModelCatalog {
	c, _ := integration.For(tool)
	return c.Model.Catalog
}

func fwdMCP(m MapRow, v any) ([]any, error) {
	servers, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s wants a server map", ErrBadValue, m.Key)
	}
	sh := m.Params.MCP
	out := map[string]any{}
	for name, raw := range servers {
		e, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: server %q", ErrBadValue, name)
		}
		native := map[string]any{}
		typ, _ := e["type"].(string)
		if typ == "" {
			typ = "stdio"
		}
		nt, ok := sh.TypeNames[typ]
		if !ok {
			return nil, fmt.Errorf("%w: %s has no %s transport (server %q)", ErrRefused, m.Tool, typ, name)
		}
		if sh.TypeKey != "" && (typ != "stdio" || sh.StdioTypeExplicit) {
			native[sh.TypeKey] = nt
		}
		if typ == "stdio" {
			cmd, _ := e["command"].(string)
			args, err := stringList(orEmpty(e["args"]))
			if cmd == "" || err != nil {
				return nil, fmt.Errorf("%w: server %q needs a command", ErrBadValue, name)
			}
			if sh.CommandArray {
				arr := []any{cmd}
				for _, a := range args {
					arr = append(arr, a)
				}
				native["command"] = arr
			} else {
				native["command"] = cmd
				if len(args) > 0 {
					native["args"] = toAnyList(args)
				}
			}
			if env, ok := e["env"].(map[string]any); ok && len(env) > 0 {
				native[sh.EnvKey] = env
			}
		} else {
			url, _ := e["url"].(string)
			key := sh.URLKeys[typ]
			if url == "" || key == "" {
				return nil, fmt.Errorf("%w: server %q needs a url", ErrBadValue, name)
			}
			native[key] = url
		}
		out[name] = native
	}
	return []any{out}, nil
}

func revMCP(m MapRow, n []any) (any, error) {
	servers, ok := n[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: %v", ErrNoCanonical, n[0])
	}
	sh := m.Params.MCP
	canonType := map[string]string{}
	for c, nt := range sh.TypeNames {
		canonType[nt] = c
	}
	out := map[string]any{}
	for name, raw := range servers {
		e, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: server %q", ErrNoCanonical, name)
		}
		typ := ""
		if sh.TypeKey != "" {
			if nt, ok := e[sh.TypeKey].(string); ok {
				typ = canonType[nt]
				if typ == "" {
					return nil, fmt.Errorf("%w: transport %q (server %q)", ErrNoCanonical, nt, name)
				}
			}
		}
		if typ == "" {
			typ = "stdio"
			for _, t := range []string{"http", "sse"} {
				if k := sh.URLKeys[t]; k != "" && e[k] != nil {
					typ = t
					break
				}
			}
		}
		c := map[string]any{}
		if typ == "stdio" {
			if sh.CommandArray {
				arr, err := stringList(e["command"])
				if err != nil || len(arr) == 0 {
					return nil, fmt.Errorf("%w: server %q command", ErrNoCanonical, name)
				}
				c["command"] = arr[0]
				if len(arr) > 1 {
					c["args"] = toAnyList(arr[1:])
				}
			} else {
				cmd, _ := e["command"].(string)
				if cmd == "" {
					return nil, fmt.Errorf("%w: server %q command", ErrNoCanonical, name)
				}
				c["command"] = cmd
				if args, err := stringList(orEmpty(e["args"])); err == nil && len(args) > 0 {
					c["args"] = toAnyList(args)
				}
			}
			if env, ok := e[sh.EnvKey].(map[string]any); ok && len(env) > 0 {
				c["env"] = env
			}
		} else {
			c["type"] = typ
			c["url"] = e[sh.URLKeys[typ]]
		}
		out[name] = c
	}
	return out, nil
}

// --- helpers ---------------------------------------------------------------

func normalized(v any) any {
	n, err := cfgfmt.Normalize(v)
	if err != nil {
		return v
	}
	return n
}

func normalizedAll(vs []any) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = normalized(v)
	}
	return out
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func asInt(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case float64:
		if x == math.Trunc(x) {
			return int64(x), true
		}
	}
	return 0, false
}

func stringList(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: want a list, got %T", ErrBadValue, v)
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("%w: list element %T", ErrBadValue, e)
		}
		out = append(out, s)
	}
	return out, nil
}

func toAnyList(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func orEmpty(v any) any {
	if v == nil {
		return []any{}
	}
	return v
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
