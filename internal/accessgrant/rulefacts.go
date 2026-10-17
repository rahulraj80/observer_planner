package accessgrant

// rulefacts.go - what the org knows about the rule a guard exemption names,
// resolved on the SERVER from data the server owns (review C156-05): the
// built-in rule catalog and the member's composed org guard bundle. Nothing
// the node sent (the request's context severity, a form field) decides the
// approval tier or the lock.

// RuleSources is what the org server read for one member's guard rules. The
// zero value is "nothing could be read": every rule then resolves as unknown,
// the strict reading.
type RuleSources struct {
	// Read is true when the member's org guard bundle was read, including the
	// confirmed answer "the org publishes no bundle".
	Read bool
	// Bundle is true when an org guard bundle applies to the member.
	Bundle bool
	// Overridable lists the rule ids the composed bundle marks
	// `overridable = true`; every other rule the bundle governs is hard.
	Overridable map[string]bool
	// Severity maps a rule id to its severity: the built-in catalog, then the
	// org's own [[rule]] rows. A rule absent here is not one the org knows.
	Severity map[string]string
}

// ruleFactRule is one row of the resolution table, walked top-down; the first
// row that decides stops the walk.
type ruleFactRule struct {
	name    string
	decides func(ruleID string, src RuleSources) (RuleFacts, bool)
}

// ruleFactRules resolves Known, Locked and Severity (D-082). The lock reading
// is the conservative one the node uses when it cannot tell the tenancy of the
// machine (guard override.go, "unresolved"): under an org bundle a rule is
// locked unless the bundle marks it overridable. With no bundle at all the org
// layer locks nothing.
var ruleFactRules = []ruleFactRule{
	{"bundle unreadable: unknown", func(_ string, src RuleSources) (RuleFacts, bool) {
		return RuleFacts{}, !src.Read
	}},
	{"rule the org does not know: unknown", func(id string, src RuleSources) (RuleFacts, bool) {
		_, ok := src.Severity[id]
		return RuleFacts{}, !ok || id == ""
	}},
	{"no org bundle: known, not locked", func(id string, src RuleSources) (RuleFacts, bool) {
		return RuleFacts{Known: true, Severity: src.Severity[id]}, !src.Bundle
	}},
	{"org bundle: locked unless overridable", func(id string, src RuleSources) (RuleFacts, bool) {
		return RuleFacts{Known: true, Locked: !src.Overridable[id], Severity: src.Severity[id]}, true
	}},
}

// ResolveRuleFacts answers what the org knows about ruleID from src.
func ResolveRuleFacts(ruleID string, src RuleSources) RuleFacts {
	for _, r := range ruleFactRules {
		if f, ok := r.decides(ruleID, src); ok {
			return f
		}
	}
	return RuleFacts{}
}
