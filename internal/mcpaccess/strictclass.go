package mcpaccess

import "github.com/marmutapp/superbased-observer/internal/mcpaccess/strictvocab"

// Org audit strict classes: the closed vocabulary of
// agent_access_settings.audit_strict_classes (doc3 R9.6 / §9.1). Naming a
// class makes every call it matches audit-before-act SYNCHRONOUSLY (a chained
// commit before Pass, fail-closed on store unavailability) instead of the
// durable-async default. The default set is NONE.
//
// The VALUES, the retirement table and the normalization live in the pure
// core package internal/mcpaccess/strictvocab (moved there by modular
// packaging lane MP-L1G so the org server's config validator builds without
// Agent Access); this file re-exports them under their historical names and
// keeps what each value MATCHES: internal/mcpgw/pdp decides strictness
// through MatchStrictClass, and every accepted value has exactly one row in
// strictClassTable, so an accepted value can never again be a value the
// gateway does not act on (security ledger AA-69).
const (
	// StrictClassGoverned is the GOVERNED decision class (strictvocab.Governed).
	StrictClassGoverned = strictvocab.Governed
	// StrictClassCatalogue is the CATALOGUE decision class (strictvocab.Catalogue).
	StrictClassCatalogue = strictvocab.Catalogue
	// StrictClassDestructive is a destructive or unclassifiable tools/call
	// (strictvocab.Destructive).
	StrictClassDestructive = strictvocab.Destructive
	// StrictClassWrite is every call whose grant ACTION carries the write
	// effect class (strictvocab.Write).
	StrictClassWrite = strictvocab.Write
)

// StrictClasses is the accepted audit_strict_classes vocabulary, in the
// order the settings form lists it.
var StrictClasses = strictvocab.Classes

// StrictCall is what a strict-class predicate reads about one decided call.
// The PDP resolves it at its boundary (method table + snapshot pin), so the
// predicates below never see a tool name or a source identity.
type StrictCall struct {
	// DecisionClass is the method's decision class: StrictClassGoverned or
	// StrictClassCatalogue (pdp.ClassGoverned / pdp.ClassCatalogue).
	DecisionClass string
	// Action is the method's grant action (the dispatch table's Action).
	Action Action
	// Destructive is true for a tools/call whose tool is marked destructive
	// by the approved snapshot or cannot be classified (see
	// StrictClassDestructive).
	Destructive bool
}

// strictClassRule is one row of strictClassTable: the class value and the
// predicate it names.
type strictClassRule struct {
	class string
	match func(StrictCall) bool
}

// strictClassTable maps each accepted class onto its predicate (CLAUDE.md
// "Module Boundaries" #5: a table, one test case per row). Every value in
// StrictClasses has exactly one row (TestStrictClassTableCoversVocabulary).
var strictClassTable = []strictClassRule{
	{StrictClassGoverned, func(c StrictCall) bool { return c.DecisionClass == StrictClassGoverned }},
	{StrictClassCatalogue, func(c StrictCall) bool { return c.DecisionClass == StrictClassCatalogue }},
	{StrictClassDestructive, func(c StrictCall) bool { return c.Destructive }},
	{StrictClassWrite, func(c StrictCall) bool { return actionResource[c.Action].effectClass == "write" }},
}

// MatchStrictClass reports whether class matches call, and whether class is
// a known (accepted) value at all. A caller must treat an unknown class as
// the STRICTER reading (the PDP makes every call strict), never as a no-op.
func MatchStrictClass(class string, call StrictCall) (matched, known bool) {
	for _, r := range strictClassTable {
		if r.class == class {
			return r.match(call), true
		}
	}
	return false, false
}

// StrictClassRetirement is one RETIRED audit_strict_classes value
// (strictvocab.Retirement).
type StrictClassRetirement = strictvocab.Retirement

// RetiredStrictClasses returns a copy of the retirement table.
func RetiredStrictClasses() []StrictClassRetirement { return strictvocab.Retired() }

// StrictClassRetired reports the retirement row for value, if value is
// retired.
func StrictClassRetired(value string) (StrictClassRetirement, bool) {
	return strictvocab.RetiredValue(value)
}

// NormalizedStrictClasses is NormalizeStrictClasses' result
// (strictvocab.Normalized).
type NormalizedStrictClasses = strictvocab.Normalized

// NormalizeStrictClasses canonicalizes an audit_strict_classes list. It is
// the one conversion every reader applies: the org server's settings load,
// its boot migration of the stored row, the TOML seed and the PDP's policy
// compile.
func NormalizeStrictClasses(in []string) NormalizedStrictClasses { return strictvocab.Normalize(in) }
