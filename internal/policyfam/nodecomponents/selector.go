package nodecomponents

import (
	"fmt"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// SelectorKind is the closed set of selector forms (plan 6.2, the VS Code
// extensions.allowed precedent).
type SelectorKind string

// The selector kinds, from most to least specific.
const (
	// KindExact names one component id: "adapter.codex".
	KindExact SelectorKind = "exact"
	// KindPrefix is a trailing-glob prefix: "adapter.*", "egress.*". The
	// only wildcard is a final ".*".
	KindPrefix SelectorKind = "prefix"
	// KindClass matches a catalog class: "class:adapter".
	KindClass SelectorKind = "class"
	// KindStability matches a stability level: "stability:stable".
	KindStability SelectorKind = "stability"
)

// Selector is one parsed selector.
type Selector struct {
	// Raw is the selector exactly as it appears in the canonical body.
	Raw  string
	Kind SelectorKind
	// Value is the id, the prefix (without ".*", with the trailing dot),
	// the class or the stability.
	Value string
}

// specificityRank orders the kinds; a larger rank is more specific.
var specificityRank = map[SelectorKind]int{
	KindExact:     3,
	KindPrefix:    2,
	KindClass:     1,
	KindStability: 1,
}

// Specificity is a comparable measure: rank first, then the prefix length
// (a longer prefix glob is more specific than a shorter one).
type Specificity struct {
	Rank   int
	Length int
}

// Less reports whether s is less specific than o.
func (s Specificity) Less(o Specificity) bool {
	if s.Rank != o.Rank {
		return s.Rank < o.Rank
	}
	return s.Length < o.Length
}

// Specificity returns the selector's specificity.
func (s Selector) Specificity() Specificity {
	sp := Specificity{Rank: specificityRank[s.Kind]}
	if s.Kind == KindPrefix {
		sp.Length = len(s.Value)
	}
	return sp
}

// Matches reports whether the selector matches catalog row c.
func (s Selector) Matches(c components.Component) bool {
	switch s.Kind {
	case KindExact:
		return c.ID == s.Value
	case KindPrefix:
		return strings.HasPrefix(c.ID, s.Value)
	case KindClass:
		return string(c.Class) == s.Value
	case KindStability:
		return string(c.Stability) == s.Value
	}
	return false
}

// ParseSelector parses one selector. It checks the syntax and the closed
// class / stability vocabularies; whether an exact id exists in this
// build's catalog is the compiler's question (strict on publish, lenient on
// the node).
func ParseSelector(raw string) (Selector, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Selector{}, fmt.Errorf("empty selector")
	}
	if s != raw {
		return Selector{}, fmt.Errorf("selector %q has surrounding whitespace", raw)
	}
	switch {
	case strings.HasPrefix(s, "class:"):
		v := strings.TrimPrefix(s, "class:")
		c := components.Class(v)
		if !c.Valid() || c == components.ClassServer {
			return Selector{}, fmt.Errorf("selector %q: %q is not a node component class", raw, v)
		}
		return Selector{Raw: s, Kind: KindClass, Value: v}, nil
	case strings.HasPrefix(s, "stability:"):
		v := strings.TrimPrefix(s, "stability:")
		if !components.Stability(v).Valid() {
			return Selector{}, fmt.Errorf("selector %q: %q is not a stability level", raw, v)
		}
		return Selector{Raw: s, Kind: KindStability, Value: v}, nil
	case strings.HasSuffix(s, ".*"):
		p := strings.TrimSuffix(s, "*")
		if !validIDChars(strings.TrimSuffix(p, ".")) || strings.Contains(strings.TrimSuffix(p, "."), "*") {
			return Selector{}, fmt.Errorf("selector %q: a glob is a component id prefix followed by \".*\"", raw)
		}
		return Selector{Raw: s, Kind: KindPrefix, Value: p}, nil
	}
	if !validIDChars(s) {
		return Selector{}, fmt.Errorf("selector %q: not a component id, a prefix glob, class:<class> or stability:<level>", raw)
	}
	return Selector{Raw: s, Kind: KindExact, Value: s}, nil
}

// validIDChars reports whether s uses only the catalog id alphabet
// (lowercase letters, digits, '.', '-', '_') and has no empty segment.
func validIDChars(s string) bool {
	if s == "" || strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") || strings.Contains(s, "..") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
