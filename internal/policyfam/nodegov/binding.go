package nodegov

import (
	"sort"
	"strings"
)

// bindingTokenPrefix selects the node.governance capability tokens a v2
// policy binding covers (nodegov-vocab-, nodegov-schema-, nodegov-keys-).
const bindingTokenPrefix = "nodegov-"

// BindingTarget is the projection-target string a v2 policy binding carries
// for node.governance (orgcontract.PolicyBinding.Target, pass-2 review
// C156-14): the node.governance capability tokens the fetch advertised,
// trimmed, de-duplicated, sorted and comma-joined.
//
// It is the ADVERTISED target, not the one the server serves after clamping
// it to its own vocabulary: the node computes it from its own
// CapabilityTokens, so the two agree for a node's own request whether the
// server is older or newer than the node, while a request that advertised a
// LOWER target (to be served a stripped projection) is bound to that lower
// string, which the node refuses. "" when the fetch advertised none (a
// released node, which never asks for a binding).
func BindingTarget(tokens []string) string {
	seen := map[string]bool{}
	var out []string
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if !strings.HasPrefix(tok, bindingTokenPrefix) || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}
