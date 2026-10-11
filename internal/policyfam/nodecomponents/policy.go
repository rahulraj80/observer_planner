package nodecomponents

import (
	"fmt"
	"sort"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// DenySelector is a parsed deny entry.
type DenySelector struct {
	Selector
	Grantable bool
}

// OnRemove is the compiled removal behaviour.
type OnRemove struct {
	Teardown       string
	Data           string
	PurgeAfterDays int
}

// PolicySpec is a compiled node.components body: parsed selectors, ready to
// reduce to exact ids for one catalog with Sets.
type PolicySpec struct {
	Distro   string
	Allow    []Selector
	Require  []Selector
	Deny     []DenySelector
	OnRemove OnRemove
	// IgnoredIDs lists, as "<list>:<selector>", exact ids the lenient
	// compile dropped because this build does not know them.
	IgnoredIDs []string
	// Warnings are composer warnings that do not stop publication: an id
	// matched by an allow and a deny selector of equal specificity (deny
	// wins, plan 6.2.1).
	Warnings []string
}

// Compile compiles a decoded body strictly against this build's catalog.
func Compile(b BodyV1) (PolicySpec, error) { return compile(b, true) }

func compile(b BodyV1, strict bool) (PolicySpec, error) {
	c, err := Canonicalize(b)
	if err != nil {
		return PolicySpec{}, err
	}
	spec := PolicySpec{Distro: c.Distro}
	if c.Distro != "" && !distroValid(c.Distro) {
		return PolicySpec{}, fmt.Errorf("policyfam/nodecomponents.Compile: distro %q is not a prebuilt node distro", c.Distro)
	}
	if c.Unlisted != UnlistedInert {
		return PolicySpec{}, fmt.Errorf("policyfam/nodecomponents.Compile: unlisted %q is not supported (only %q)", c.Unlisted, UnlistedInert)
	}
	if spec.OnRemove, err = compileOnRemove(*c.OnRemove); err != nil {
		return PolicySpec{}, err
	}
	sc := selectorCompiler{strict: strict}
	if spec.Allow, err = sc.list("allow", c.Allow); err != nil {
		return PolicySpec{}, err
	}
	if spec.Require, err = sc.list("require", c.Require); err != nil {
		return PolicySpec{}, err
	}
	for _, d := range c.Deny {
		s, keep, err := sc.one("deny", d.Selector)
		if err != nil {
			return PolicySpec{}, err
		}
		if keep {
			spec.Deny = append(spec.Deny, DenySelector{Selector: s, Grantable: d.Grantable})
		}
	}
	spec.IgnoredIDs = sc.ignored
	if err := checkConflicts(spec); err != nil {
		return PolicySpec{}, err
	}
	spec.Warnings = equalSpecificityWarnings(spec)
	return spec, nil
}

// compileOnRemove validates the removal behaviour (plan 6.3: 0 days means
// never; a scheduled purge is explicit).
func compileOnRemove(r OnRemoveV1) (OnRemove, error) {
	switch {
	case r.Teardown != TeardownAuto && r.Teardown != TeardownManual:
		return OnRemove{}, fmt.Errorf("policyfam/nodecomponents.Compile: on_remove.teardown %q is not %q or %q", r.Teardown, TeardownAuto, TeardownManual)
	case r.Data != DataKeep && r.Data != DataPurge:
		return OnRemove{}, fmt.Errorf("policyfam/nodecomponents.Compile: on_remove.data %q is not %q or %q", r.Data, DataKeep, DataPurge)
	case r.PurgeAfterDays < 0 || r.PurgeAfterDays > MaxPurgeAfterDays:
		return OnRemove{}, fmt.Errorf("policyfam/nodecomponents.Compile: on_remove.purge_after_days must be 0 to %d", MaxPurgeAfterDays)
	case r.Data == DataPurge && r.PurgeAfterDays == 0:
		return OnRemove{}, fmt.Errorf("policyfam/nodecomponents.Compile: on_remove.data %q needs purge_after_days > 0 (0 means never)", DataPurge)
	case r.Data == DataKeep && r.PurgeAfterDays != 0:
		return OnRemove{}, fmt.Errorf("policyfam/nodecomponents.Compile: on_remove.purge_after_days needs data %q", DataPurge)
	}
	return OnRemove{Teardown: r.Teardown, Data: r.Data, PurgeAfterDays: r.PurgeAfterDays}, nil
}

// selectorCompiler parses selectors against this build's catalog: strict
// refuses an exact id it does not know, lenient drops it into ignored.
type selectorCompiler struct {
	strict  bool
	ignored []string
}

func (sc *selectorCompiler) list(name string, raws []string) ([]Selector, error) {
	var out []Selector
	for _, raw := range raws {
		s, keep, err := sc.one(name, raw)
		if err != nil {
			return nil, err
		}
		if keep {
			out = append(out, s)
		}
	}
	return out, nil
}

func (sc *selectorCompiler) one(list, raw string) (Selector, bool, error) {
	s, err := ParseSelector(raw)
	if err != nil {
		return Selector{}, false, fmt.Errorf("policyfam/nodecomponents.Compile: %s: %w", list, err)
	}
	if list == "deny" && s.Kind == KindClass && s.Value == string(components.ClassCore) {
		return Selector{}, false, fmt.Errorf("policyfam/nodecomponents.Compile: deny: %q names the core class, and core cannot be denied", s.Raw)
	}
	if s.Kind != KindExact {
		return s, true, nil
	}
	row, ok := components.Get(s.Value)
	switch {
	case ok && row.Binary == components.BinaryNode && list == "deny" && row.Class == components.ClassCore:
		return Selector{}, false, fmt.Errorf("policyfam/nodecomponents.Compile: deny: %q is core and cannot be denied", s.Value)
	case ok && row.Binary == components.BinaryNode:
		return s, true, nil
	case sc.strict && ok:
		return Selector{}, false, fmt.Errorf("policyfam/nodecomponents.Compile: %s: %q is an org server component, not a node one", list, s.Value)
	case sc.strict:
		return Selector{}, false, fmt.Errorf("policyfam/nodecomponents.Compile: %s: %q is not a component of catalog version %d", list, s.Value, components.CatalogVersion)
	}
	sc.ignored = append(sc.ignored, list+":"+s.Value)
	return Selector{}, false, nil
}

// checkConflicts is normative conflict table row 1 (plan 6.2.1): a selector
// set that puts the same id in both require and deny cannot be published.
func checkConflicts(spec PolicySpec) error {
	for _, row := range governable() {
		req, den := firstMatch(spec.Require, row), firstDenyMatch(spec.Deny, row)
		if req != "" && den != "" {
			return fmt.Errorf("policyfam/nodecomponents.Compile: %q is matched by require %q and by deny %q", row.ID, req, den)
		}
	}
	return nil
}

// A broader deny selector (a glob or stability) that happens to match a core
// row is allowed: core is never blocked (the resolver's first row), so the
// match is inert for core and real for everything else.

func firstMatch(sels []Selector, c components.Component) string {
	for _, s := range sels {
		if s.Matches(c) {
			return s.Raw
		}
	}
	return ""
}

func firstDenyMatch(sels []DenySelector, c components.Component) string {
	for _, s := range sels {
		if s.Matches(c) {
			return s.Raw
		}
	}
	return ""
}

// verdict is what one selector list says about one id.
type verdict int

// Ordered so that, at equal specificity, a larger verdict wins: deny beats
// require beats allow (plan 6.2: deny beats allow; equal specificity, deny
// wins).
const (
	verdictNone verdict = iota
	verdictAllow
	verdictRequire
	verdictDeny
)

// best is the winning match for one id.
type best struct {
	v  verdict
	sp Specificity
	// grantable is true only when every deny entry at the winning
	// specificity is grantable.
	grantable bool
}

func (b *best) offer(v verdict, sp Specificity, grantable bool) {
	switch {
	case b.v == verdictNone || b.sp.Less(sp):
		*b = best{v: v, sp: sp, grantable: grantable}
	case sp == b.sp && v == verdictDeny && b.v == verdictDeny:
		b.grantable = b.grantable && grantable
	case sp == b.sp && v > b.v:
		*b = best{v: v, sp: sp, grantable: grantable}
	}
}

// Sets reduces the policy to exact ids over the node rows of this build's
// catalog: for each id the most specific matching selector wins, and at
// equal specificity deny beats require beats allow. A grantable result
// needs every winning deny entry to be grantable. Source is copied into the
// result for posture.
func (s PolicySpec) Sets(source string) components.PolicySets {
	out := components.PolicySets{Source: source}
	for _, row := range governable() {
		var b best
		for _, sel := range s.Allow {
			if sel.Matches(row) {
				b.offer(verdictAllow, sel.Specificity(), false)
			}
		}
		for _, sel := range s.Require {
			if sel.Matches(row) {
				b.offer(verdictRequire, sel.Specificity(), false)
			}
		}
		for _, sel := range s.Deny {
			if sel.Matches(row) {
				b.offer(verdictDeny, sel.Specificity(), sel.Grantable)
			}
		}
		switch b.v {
		case verdictAllow:
			out.Allow = append(out.Allow, row.ID)
		case verdictRequire:
			out.Require = append(out.Require, row.ID)
		case verdictDeny:
			out.Deny = append(out.Deny, row.ID)
			if b.grantable {
				out.Grantable = append(out.Grantable, row.ID)
			}
		}
	}
	sort.Strings(out.Allow)
	sort.Strings(out.Require)
	sort.Strings(out.Deny)
	sort.Strings(out.Grantable)
	return out
}

// equalSpecificityWarnings lists ids an allow and a deny selector match at
// the same specificity (the composer warns; deny wins).
func equalSpecificityWarnings(s PolicySpec) []string {
	var out []string
	for _, row := range governable() {
		for _, a := range s.Allow {
			if !a.Matches(row) {
				continue
			}
			for _, d := range s.Deny {
				if d.Matches(row) && d.Specificity() == a.Specificity() {
					out = append(out, fmt.Sprintf("%s: allow %q and deny %q are equally specific; deny wins", row.ID, a.Raw, d.Raw))
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// governable returns the node rows a policy can say anything about: every
// node row except core, which is always active whatever a selector matches.
func governable() []components.Component {
	var out []components.Component
	for _, row := range components.ForBinary(components.BinaryNode) {
		if row.Class != components.ClassCore {
			out = append(out, row)
		}
	}
	return out
}
