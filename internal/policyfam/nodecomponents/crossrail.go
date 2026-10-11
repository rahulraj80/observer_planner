package nodecomponents

import (
	"fmt"
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/policyfam/nodefeatures"
)

// crossrail.go is the publish-time cross-rail lint of a node.components body
// (modular packaging plan 6.2.1, T-682): component state says "is it
// installed and allowed to run", node.features says "is the feature switched
// on", and a policy that disagrees with the other rail is legal but surprising,
// so the composer warns before publish. The checks are one ordered table, one
// test row each. Every finding is a warning except the plan's one publish-time
// error: a node.governance feature.lock forcing a feature ON whose component
// this policy blocks (deny or unlisted) yields "blocked" on every managed node,
// so neither policy may be published while the other says so (T-707). The
// feature.lock vocabulary is mapped to components by lockComponents below.

// featureComponents maps a node.features feature to the component that
// carries it. A feature with no row here has no component of its own.
var featureComponents = []struct {
	feature   string
	component string
	rule      func(nodefeatures.PolicySpec) nodefeatures.FeatureRule
}{
	{nodefeatures.FeatureTerminals, "terminals", func(s nodefeatures.PolicySpec) nodefeatures.FeatureRule { return s.Terminals.FeatureRule }},
	{nodefeatures.FeatureRemote, "remote", func(s nodefeatures.PolicySpec) nodefeatures.FeatureRule { return s.Remote }},
	{nodefeatures.FeatureRoutingApply, "proxy.routing", func(s nodefeatures.PolicySpec) nodefeatures.FeatureRule { return s.RoutingApply }},
}

// lockComponents maps a node.governance feature.lock id (nodegov.Features) to
// the component that carries the feature. "" means the feature belongs to
// core (no component can block it). Every nodegov.Features id has a row:
// TestLockComponentsCoverTheFeatureLockVocabulary.
var lockComponents = map[string]string{
	"guard":                    "guard",
	"secrets":                  "",
	"codeintel":                "codeintel",
	"compression_conversation": "proxy.compression",
	"browser_capture":          "capture.browser",
	"process_observer":         "processobs",
	"remote_access":            "remote",
}

// LockComponent returns the component a feature.lock id forces on ("" for a
// core feature) and whether the id is in the mapped vocabulary.
func LockComponent(feature string) (string, bool) {
	c, ok := lockComponents[feature]
	return c, ok
}

// Severity values of a CrossRailFinding.
const (
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// CrossRailFinding is one cross-rail lint result.
type CrossRailFinding struct {
	// Severity is "warning" (publication goes ahead) or "error" (the
	// publication is refused while the other rail says otherwise).
	Severity string `json:"severity"`
	// Component is the component id the finding is about.
	Component string `json:"component"`
	// Rule names the table row.
	Rule string `json:"rule"`
	// Message is the composer copy.
	Message string `json:"message"`
}

// crossRailRow is one row of the lint table: it inspects one component's
// policy verdict against the node.features spec.
type crossRailRow struct {
	name string
	run  func(c crossRailCase) []CrossRailFinding
}

// crossRailCase is the lint's view of one policy.
type crossRailCase struct {
	sets     components.PolicySets
	features nodefeatures.PolicySpec
	// locks is the node.governance feature.lock block in force (feature id
	// to forced value); nil when none is published.
	locks map[string]bool
}

func (c crossRailCase) verdict(id string) string {
	for _, x := range c.sets.Require {
		if x == id {
			return "require"
		}
	}
	for _, x := range c.sets.Allow {
		if x == id {
			return "allow"
		}
	}
	for _, x := range c.sets.Deny {
		if x == id {
			return "deny"
		}
	}
	return "unlisted"
}

var crossRailRows = []crossRailRow{
	{
		name: "required component, feature disabled by node.features",
		run: func(c crossRailCase) []CrossRailFinding {
			var out []CrossRailFinding
			for _, f := range featureComponents {
				r := f.rule(c.features)
				if r.Governed && !r.Enabled && c.verdict(f.component) == "require" {
					out = append(out, CrossRailFinding{
						Severity: "warning", Component: f.component,
						Message: fmt.Sprintf("%s is required, but node.features disables the %s feature: the component will be active and its feature disabled by policy", f.component, f.feature),
					})
				}
			}
			return out
		},
	},
	{
		name: "feature enabled by node.features, component blocked",
		run: func(c crossRailCase) []CrossRailFinding {
			var out []CrossRailFinding
			for _, f := range featureComponents {
				r := f.rule(c.features)
				if v := c.verdict(f.component); r.Governed && r.Enabled && (v == "deny" || v == "unlisted") {
					out = append(out, CrossRailFinding{
						Severity: "warning", Component: f.component,
						Message: fmt.Sprintf("node.features enables %s, but this policy blocks the %s component (%s): the feature will be unavailable on managed nodes", f.feature, f.component, v),
					})
				}
			}
			return out
		},
	},
	{
		name: "adapter allowed, launch disallowed by tools.disallow",
		run: func(c crossRailCase) []CrossRailFinding {
			if !c.features.Tools.Governed {
				return nil
			}
			var out []CrossRailFinding
			for _, id := range append(append([]string{}, c.sets.Require...), c.sets.Allow...) {
				tool, ok := strings.CutPrefix(id, "adapter.")
				if !ok || !c.features.Tools.Disallow[strings.ToLower(tool)] {
					continue
				}
				out = append(out, CrossRailFinding{
					Severity: "warning", Component: id,
					Message: fmt.Sprintf("%s is allowed, but node.features tools.disallow lists %s: capture stays active and launching it is blocked (tools.disallow governs launchers, not capture)", id, tool),
				})
			}
			return out
		},
	},
	{
		name: "feature.lock forces a feature on, component blocked",
		run: func(c crossRailCase) []CrossRailFinding {
			ids := make([]string, 0, len(c.locks))
			for id := range c.locks {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			var out []CrossRailFinding
			for _, feature := range ids {
				if !c.locks[feature] {
					continue // a lock forcing a feature OFF never conflicts with a block
				}
				comp, ok := lockComponents[feature]
				if !ok || comp == "" {
					continue
				}
				if row, known := components.Get(comp); !known || row.Class == components.ClassCore {
					continue
				}
				if v := c.verdict(comp); v == "deny" || v == "unlisted" {
					out = append(out, CrossRailFinding{
						Severity: SeverityError, Component: comp,
						Message: fmt.Sprintf("node.governance feature.lock forces %s on, but this policy blocks the %s component (%s): managed nodes would show it blocked; allow or require %s, or remove the lock", feature, comp, v, comp),
					})
				}
			}
			return out
		},
	},
}

// HasError reports whether any finding has error severity.
func HasError(findings []CrossRailFinding) bool {
	for _, f := range findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}

// CrossRail lints a compiled node.components policy against the node.features
// policy in force (nil when none is published) and the node.governance
// feature.lock block in force (nil when none). Findings are sorted by
// component then rule.
func CrossRail(spec PolicySpec, features *nodefeatures.PolicySpec, locks map[string]bool) []CrossRailFinding {
	if features == nil && len(locks) == 0 {
		return nil
	}
	c := crossRailCase{sets: spec.Sets(""), locks: locks}
	if features != nil {
		c.features = *features
	}
	var out []CrossRailFinding
	for _, row := range crossRailRows {
		for _, f := range row.run(c) {
			f.Rule = row.name
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Component != out[j].Component {
			return out[i].Component < out[j].Component
		}
		return out[i].Rule < out[j].Rule
	})
	return out
}
