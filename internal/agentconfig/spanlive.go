package agentconfig

import (
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
)

// Span liveness (phases P4 / P5). OwnedSpans() says where each Observer
// writer COULD write; whether it actually wrote on this machine is read off
// the file's own content, through one predicate per span owner (a table,
// never a switch on a tool). A live span renders locked with the owner's
// verb, and the hygiene pipeline (plan section 5.2 step 5) refuses a save
// that removes or changes it.

// SpanEnv is what liveness needs to know about this machine.
type SpanEnv struct {
	// ProxyAddrs are the host:port spellings Observer's proxy answers on
	// ("127.0.0.1:8820", "localhost:8820"). A value naming one is the
	// proxy route's.
	ProxyAddrs []string
}

// spanOwnerRule is one owner's liveness and ownership predicate.
type spanOwnerRule struct {
	// liveWhole reports whether a whole-key value is Observer's.
	liveWhole func(env SpanEnv, path KeyPath, v any) bool
	// ownsLeaf reports whether a string inside an entries span is one of
	// Observer's entries.
	ownsLeaf func(env SpanEnv, s string) bool
	// never: the span cannot be judged from the file alone (the routing
	// apply ledger owns that knowledge); it is shown, never enforced here.
	never bool
}

var spanOwnerRules = map[string]spanOwnerRule{
	SpanOwnerHook: {
		liveWhole: func(_ SpanEnv, _ KeyPath, v any) bool { return v == true },
		ownsLeaf:  func(env SpanEnv, s string) bool { return isObserverInvocation(s) || env.namesProxy(s) },
	},
	SpanOwnerStatusline: {
		liveWhole: func(env SpanEnv, _ KeyPath, v any) bool { return deepAny(v, isObserverInvocation) },
	},
	SpanOwnerMCP: {
		liveWhole: func(_ SpanEnv, _ KeyPath, v any) bool { return v != nil },
	},
	SpanOwnerProxyRoute: {
		liveWhole: func(env SpanEnv, p KeyPath, v any) bool {
			if v == nil {
				return false
			}
			if len(p) > 0 && strings.Contains(strings.ToLower(p[len(p)-1]), "observer") {
				return true
			}
			return deepAny(v, func(s string) bool {
				return env.namesProxy(s) || strings.Contains(strings.ToLower(s), "observer")
			})
		},
		ownsLeaf: func(env SpanEnv, s string) bool { return env.namesProxy(s) },
	},
	SpanOwnerRelay: {
		ownsLeaf: func(_ SpanEnv, s string) bool { return isObserverInvocation(s) },
	},
	SpanOwnerRoutingApply: {never: true},
}

func (env SpanEnv) namesProxy(s string) bool {
	for _, a := range env.ProxyAddrs {
		if a != "" && strings.Contains(s, a) {
			return true
		}
	}
	return false
}

// isObserverInvocation reports a command string that runs the observer
// binary (any path, optional .exe, an observer-* or superbased* build), the
// shape every hook / relay / statusline writer registers.
func isObserverInvocation(s string) bool {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '"' || r == '\'' })
	for _, f := range fields {
		base := f
		if i := strings.LastIndexAny(base, `/\`); i >= 0 {
			base = base[i+1:]
		}
		base = strings.TrimSuffix(strings.ToLower(base), ".exe")
		if base == "observer" || strings.HasPrefix(base, "observer-") || strings.HasPrefix(base, "superbased") {
			return true
		}
	}
	return false
}

func deepAny(v any, pred func(string) bool) bool {
	switch x := v.(type) {
	case string:
		return pred(x)
	case []any:
		for _, e := range x {
			if deepAny(e, pred) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if deepAny(e, pred) {
				return true
			}
		}
	}
	return false
}

// deepStrings collects every string leaf a predicate accepts (a multiset).
func deepStrings(v any, pred func(string) bool, out *[]string) {
	switch x := v.(type) {
	case string:
		if pred(x) {
			*out = append(*out, x)
		}
	case []any:
		for _, e := range x {
			deepStrings(e, pred, out)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			deepStrings(x[k], pred, out)
		}
	}
}

// SpanState is one owned span as it stands in one file.
type SpanState struct {
	Span OwnedSpan `json:"-"`
	// Path is the span's dotted path ("" for a whole-file span).
	Path  string    `json:"path"`
	Owner string    `json:"owner"`
	Match SpanMatch `json:"match"`
	Entry string    `json:"entry,omitempty"`
	Verb  string    `json:"verb"`
	// Live: Observer's writer has content here right now.
	Live bool `json:"live"`
	// Enforced: the hygiene pipeline protects it (false for spans whose
	// ownership only a ledger elsewhere can establish).
	Enforced bool   `json:"enforced"`
	Note     string `json:"note,omitempty"`
}

// spanContent is what a live span owns in a document: the value to keep
// identical (whole key / named entry) or the owned leaves to keep present.
type spanContent struct {
	live   bool
	whole  any
	leaves []string
}

// readSpan evaluates one span against a decoded document (nil tree = the
// file is absent) and its raw bytes (whole-file spans).
func readSpan(env SpanEnv, s OwnedSpan, tree map[string]any, raw []byte, present bool) spanContent {
	rule, ok := spanOwnerRules[s.Owner]
	if !ok || rule.never {
		return spanContent{}
	}
	switch s.Match {
	case SpanWholeFile:
		if !present {
			return spanContent{}
		}
		return spanContent{live: true, whole: string(raw)}
	case SpanWholeKey:
		v, found := lookupTree(tree, s.Path)
		if !found || rule.liveWhole == nil || !rule.liveWhole(env, s.Path, v) {
			return spanContent{}
		}
		return spanContent{live: true, whole: v}
	case SpanEntries:
		v, found := lookupTree(tree, s.Path)
		if !found {
			return spanContent{}
		}
		if s.Entry != "" {
			if m, ok := v.(map[string]any); ok {
				if e, ok := m[s.Entry]; ok {
					return spanContent{live: true, whole: e}
				}
			}
			if l, ok := v.([]any); ok {
				for _, e := range l {
					if e == s.Entry {
						return spanContent{live: true, leaves: []string{s.Entry}}
					}
				}
			}
			return spanContent{}
		}
		if rule.ownsLeaf == nil {
			return spanContent{}
		}
		var leaves []string
		deepStrings(v, func(x string) bool { return rule.ownsLeaf(env, x) }, &leaves)
		if len(leaves) == 0 {
			return spanContent{}
		}
		return spanContent{live: true, leaves: leaves}
	}
	return spanContent{}
}

// lookupTree reads a path from a decoded tree.
func lookupTree(tree map[string]any, p KeyPath) (any, bool) {
	if tree == nil {
		return nil, false
	}
	var cur any = tree
	for _, seg := range p {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// spanKept reports whether after still holds everything before owned.
func spanKept(before, after spanContent) bool {
	if !before.live {
		return true
	}
	if !after.live {
		return false
	}
	if before.whole != nil || after.whole != nil {
		if !cfgfmt.Equal(before.whole, after.whole) {
			return false
		}
	}
	// Multiset containment: every owned leaf before is still there after.
	count := map[string]int{}
	for _, s := range after.leaves {
		count[s]++
	}
	for _, s := range before.leaves {
		if count[s] == 0 {
			return false
		}
		count[s]--
	}
	return true
}

// spansOfFile returns the owned spans declared in one file row.
func spansOfFile(fileID string) []OwnedSpan {
	var out []OwnedSpan
	for _, s := range OwnedSpans() {
		if s.FileID == fileID {
			out = append(out, s)
		}
	}
	return out
}

// SpanStates evaluates every span of a file against its current content.
func SpanStates(env SpanEnv, fileID string, tree map[string]any, raw []byte, present bool) []SpanState {
	var out []SpanState
	for _, s := range spansOfFile(fileID) {
		c := readSpan(env, s, tree, raw, present)
		rule := spanOwnerRules[s.Owner]
		out = append(out, SpanState{
			Span: s, Path: s.Path.String(), Owner: s.Owner, Match: s.Match, Entry: s.Entry, Verb: s.Verb,
			Live: c.live, Enforced: !rule.never, Note: s.Note,
		})
	}
	return out
}
