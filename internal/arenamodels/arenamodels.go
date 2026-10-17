package arenamodels

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// Usage is one model this node has used (a store.ModelUsage, restated here so
// this package never imports the store).
type Usage struct {
	Model    string
	Count    int
	LastUsed string
	Tools    []string
}

// Input is everything Compose needs for one harness.
type Input struct {
	// Tool is the harness id ("claude-code").
	Tool string
	// Capability is the harness's registry row.
	Capability integration.Capability
	// ToolUsage is this harness's own model history, most-used first.
	ToolUsage []Usage
	// NodeUsage is every tool's model history on this node, most-used first.
	NodeUsage []Usage
	// Catalog is the compiled price-snapshot model ids.
	Catalog []string
	// Routed reports that the Arena drives this harness through the
	// Observer proxy (it always does today; a direct drive lifts the
	// provider-prefix constraint).
	Routed bool
}

// Group is the picker section an option belongs to, in display order.
type Group string

const (
	// GroupUsed: a model this node has run.
	GroupUsed Group = "used"
	// GroupAvailable: a grounded alias or a catalogue model for the harness.
	GroupAvailable Group = "available"
	// GroupUnroutable: a used model the harness's routed lane cannot reach.
	GroupUnroutable Group = "unroutable"
)

// groupOrder is the display order of the groups.
var groupOrder = map[Group]int{GroupUsed: 0, GroupAvailable: 1, GroupUnroutable: 2}

// Source says where an option came from.
type Source string

const (
	// SourceToolHistory: this harness ran the model on this machine.
	SourceToolHistory Source = "tool_history"
	// SourceNodeHistory: another tool on this machine ran the model.
	SourceNodeHistory Source = "node_history"
	// SourceAlias: a registry-grounded alias the harness resolves itself.
	SourceAlias Source = "alias"
	// SourceCatalog: a model the compiled price snapshot names for the
	// harness's provider family.
	SourceCatalog Source = "catalog"
)

// Option is one picker row.
type Option struct {
	// Model is the value sent to the harness.
	Model  string `json:"model"`
	Group  Group  `json:"group"`
	Source Source `json:"source"`
	// Count / LastUsed / Tools are set for history rows only.
	Count    int      `json:"count,omitempty"`
	LastUsed string   `json:"last_used,omitempty"`
	Tools    []string `json:"tools,omitempty"`
	// UsedAs is the history id when Model was rewritten for a routed lane
	// (stealth/ox-alpha -> openrouter/stealth/ox-alpha).
	UsedAs string `json:"used_as,omitempty"`
	// Disabled rows stay visible with Reason (the honest-disabled rule).
	Disabled bool   `json:"disabled,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Harness is one harness's picker model.
type Harness struct {
	Tool string `json:"tool"`
	// Selectable is false when no model override can reach the harness;
	// Reason then says why in plain words.
	Selectable bool   `json:"selectable"`
	Reason     string `json:"reason,omitempty"`
	// Catalog is the registry catalogue kind ("vendor" / "open" / "").
	Catalog   string   `json:"catalog"`
	Providers []string `json:"providers,omitempty"`
	// DefaultModel is what "Default" resolves to when the Arena itself
	// picks it (a routed lane's grounded default); empty means the
	// harness's own default, which Observer cannot see in advance.
	DefaultModel string `json:"default_model,omitempty"`
	// Note is the registry's caveat copy for selecting a model here.
	Note    string   `json:"note,omitempty"`
	Options []Option `json:"options"`
}

// family maps a provider family to the model-id prefixes its catalogue rows
// carry, and the substrings that mark a priced row as not a selectable chat
// model (a fast-mode pricing variant, a realtime/audio SKU, open weights the
// vendor API does not serve).
type family struct {
	Provider string
	Prefixes []string
	Exclude  []string
}

// families is the provider-family vocabulary, walked top-down.
var families = []family{
	{Provider: "anthropic", Prefixes: []string{"claude"}, Exclude: []string{"-fast"}},
	{Provider: "openai", Prefixes: []string{"gpt-", "o1", "o3", "o4", "codex-"}, Exclude: []string{"realtime", "gpt-oss", "audio", "tts", "transcribe", "embedding", "image"}},
	{Provider: "xai", Prefixes: []string{"grok"}, Exclude: []string{"image", "imagine"}},
	{Provider: "google", Prefixes: []string{"gemini"}, Exclude: []string{"image", "embedding", "tts", "live"}},
}

// sentinelModels are recorded model values that name no real model, so they
// are never offered (an auto-router placeholder or a gateway marker).
var sentinelModels = map[string]bool{
	"default": true, "auto": true, "unknown": true, "gateway-injected": true,
}

// Compose builds the picker model for one harness.
func Compose(in Input) Harness {
	c := in.Capability
	h := Harness{
		Tool:      in.Tool,
		Catalog:   string(c.Model.Catalog.Kind),
		Providers: append([]string(nil), c.Model.Catalog.Providers...),
		Note:      c.Model.Catalog.Note,
		Options:   []Option{},
	}
	if c.Headless == nil {
		h.Reason = fmt.Sprintf("%s has no grounded one-shot contract, so the Arena cannot run it", in.Tool)
		return h
	}
	if c.Model.Kind == integration.ModelNone {
		h.Reason = fmt.Sprintf("%s has no grounded model flag, so it always runs on its own default model", in.Tool)
		return h
	}
	h.Selectable = true

	b := &builder{tool: in.Tool, seen: map[string]bool{}}
	if in.Routed && c.Headless.ProxyModelPrefix != "" {
		b.prefix = c.Headless.ProxyModelPrefix
		h.DefaultModel = c.Headless.ProxyDefaultModel
	}

	switch c.Model.Catalog.Kind {
	case integration.ModelCatalogVendor:
		fams := familiesFor(c.Model.Catalog.Providers)
		for _, u := range in.ToolUsage {
			if matchesFamily(fams, u.Model) {
				b.addUsage(u, SourceToolHistory)
			}
		}
		b.addAliases(c.Model.Known)
		var cat []string
		for _, m := range in.Catalog {
			if matchesFamily(fams, m) {
				cat = append(cat, m)
			}
		}
		sort.SliceStable(cat, func(i, j int) bool { return naturalLess(cat[j], cat[i]) })
		for _, m := range cat {
			b.add(Option{Model: m, Group: GroupAvailable, Source: SourceCatalog})
		}
	case integration.ModelCatalogOpen:
		for _, u := range mergeUsage(in.ToolUsage, in.NodeUsage) {
			src := SourceNodeHistory
			if containsString(u.Tools, in.Tool) {
				src = SourceToolHistory
			}
			b.addUsage(u, src)
		}
		b.addAliases(c.Model.Known)
	default:
		for _, u := range in.ToolUsage {
			b.addUsage(u, SourceToolHistory)
		}
		b.addAliases(c.Model.Known)
	}

	h.Options = b.out
	if h.Options == nil {
		h.Options = []Option{}
	}
	sort.SliceStable(h.Options, func(i, j int) bool {
		return groupOrder[h.Options[i].Group] < groupOrder[h.Options[j].Group]
	})
	return h
}

// builder accumulates options, de-duplicated on the value sent to the
// harness, applying the routed lane's provider-prefix rule to each.
type builder struct {
	tool   string
	prefix string
	seen   map[string]bool
	out    []Option
}

func (b *builder) addUsage(u Usage, src Source) {
	if isSentinel(u.Model) {
		return
	}
	b.add(Option{
		Model: u.Model, Group: GroupUsed, Source: src,
		Count: u.Count, LastUsed: u.LastUsed, Tools: append([]string(nil), u.Tools...),
	})
}

func (b *builder) addAliases(known []string) {
	for _, m := range known {
		b.add(Option{Model: m, Group: GroupAvailable, Source: SourceAlias})
	}
}

// add applies the routed-lane rule and appends the option once. A routed
// lane that pins a prefix (opencode's openrouter/) accepts an id already on
// it, rewrites a router-shaped vendor/model id onto it, and keeps anything
// else visible but disabled.
func (b *builder) add(o Option) {
	o.Model = strings.TrimSpace(o.Model)
	if o.Model == "" {
		return
	}
	if b.prefix != "" && !strings.HasPrefix(o.Model, b.prefix) {
		if strings.Contains(o.Model, "/") {
			o.UsedAs = o.Model
			o.Model = b.prefix + o.Model
		} else {
			o.Group = GroupUnroutable
			o.Disabled = true
			o.Reason = fmt.Sprintf("the Arena's routed %s lane needs a %s<vendor>/<model> id", b.tool, b.prefix)
		}
	}
	if b.seen[o.Model] {
		return
	}
	b.seen[o.Model] = true
	b.out = append(b.out, o)
}

// mergeUsage unions a harness's own history with the node-wide history,
// keyed by model: node-wide counts win (they include the harness's own
// rows), a model only the harness's capped list carries is kept, and the
// result is re-sorted most-used first (count, then recency, then id).
func mergeUsage(tool, node []Usage) []Usage {
	byModel := map[string]Usage{}
	var order []string
	for _, list := range [][]Usage{node, tool} {
		for _, u := range list {
			if _, ok := byModel[u.Model]; ok {
				continue
			}
			byModel[u.Model] = u
			order = append(order, u.Model)
		}
	}
	out := make([]Usage, 0, len(order))
	for _, m := range order {
		out = append(out, byModel[m])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].LastUsed != out[j].LastUsed {
			return out[i].LastUsed > out[j].LastUsed
		}
		return out[i].Model < out[j].Model
	})
	return out
}

func familiesFor(providers []string) []family {
	var out []family
	for _, f := range families {
		if containsString(providers, f.Provider) {
			out = append(out, f)
		}
	}
	return out
}

// matchesFamily reports whether model belongs to one of fams. Leading
// provider path segments are stripped first, so anthropic/claude-sonnet-4.6
// matches like claude-sonnet-4.6.
func matchesFamily(fams []family, model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	if isSentinel(m) {
		return false
	}
	for _, f := range fams {
		if !hasAnyPrefix(m, f.Prefixes) {
			continue
		}
		excluded := false
		for _, x := range f.Exclude {
			if strings.Contains(m, x) {
				excluded = true
				break
			}
		}
		if !excluded {
			return true
		}
	}
	return false
}

func isSentinel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return m == "" || strings.HasPrefix(m, "<") || sentinelModels[m]
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// naturalLess orders model ids with digit runs compared numerically, so
// claude-opus-4-10 sorts after claude-opus-4-9 and gpt-5.10 after gpt-5.9.
func naturalLess(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si := i
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			sj := j
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na := strings.TrimLeft(string(ra[si:i]), "0")
			nb := strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if ra[i] != rb[j] {
			return ra[i] < rb[j]
		}
		i++
		j++
	}
	return len(ra)-i < len(rb)-j
}
