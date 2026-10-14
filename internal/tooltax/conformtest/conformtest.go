package conformtest

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/tooltax"
	"github.com/marmutapp/superbased-observer/internal/tooltax/conformast"
)

// Classifier maps one native tool name to an action type, returning "" or
// models.ActionUnknown when the adapter does not classify the name. It must
// call the adapter's REAL private classifier, never a re-implementation.
type Classifier func(native string) string

// AgreesWithTable walks every tool-specific, non-glob tooltax row for each
// of tools and requires the classifier, where it classifies the name at
// all, to agree with the row. tooltax knowing MORE names is the allowed
// direction. A tool for which no row reached the classifier fails: the pin
// would be vacuous.
//
// On its own this is not a subset proof (it never visits a name tooltax
// has not heard of); pair it with DomainIsSubset or MapIsSubset.
func AgreesWithTable(t testing.TB, tools []string, classify Classifier) {
	t.Helper()
	if len(tools) == 0 {
		t.Errorf("conformtest: no tool ids given: the pin would be vacuous")
		return
	}
	for _, tool := range tools {
		checked := 0
		for _, e := range tooltax.Table() {
			if e.Tool != tool || e.IsGlob() {
				continue
			}
			got := classify(e.Native)
			if got == "" || got == models.ActionUnknown {
				continue
			}
			checked++
			if got != e.ActionType {
				t.Errorf("drift: %s %q: adapter classifies as %q, tooltax table says %q",
					tool, e.Native, got, e.ActionType)
			}
		}
		if checked == 0 {
			t.Errorf("no %s row in the tooltax table reached the adapter's classifier: "+
				"the pin is vacuous (did the tool id or the classifier change?)", tool)
		}
	}
}

// MapIsSubset requires every entry of an enumerable private classifier map
// to resolve in tooltax, for tool, to the SAME action type. It catches "the
// adapter learned a new tool and nobody told the taxonomy".
func MapIsSubset(t testing.TB, tool string, m map[string]string) {
	t.Helper()
	if len(m) == 0 {
		t.Errorf("conformtest: the %s classifier map is empty: the pin is vacuous", tool)
		return
	}
	for native, want := range m {
		got, ok := tooltax.ResolveActionType(tool, native)
		if !ok {
			t.Errorf("tooltax has no row for %s %q (the adapter maps it to %q): "+
				"add the name to internal/tooltax's table", tool, native, want)
			continue
		}
		if got != want {
			t.Errorf("drift: %s %q: adapter map says %q, tooltax says %q",
				tool, native, want, got)
		}
	}
}

// Domain describes a switch classifier whose case labels are not
// enumerable at runtime.
type Domain struct {
	// Dir is the package directory scanned (normally ".").
	Dir string
	// Func is the classifier function whose switch case labels form the
	// domain.
	Func string
	// Tools are the tool ids the classifier feeds.
	Tools []string
	// Classify is the REAL private classifier.
	Classify Classifier
	// Unclassified freezes labels that resolve to ActionUnknown. Each value
	// must start with "deliberate: " or "dead: " followed by a reason.
	Unclassified map[string]string
	// Missing freezes labels the classifier maps but tooltax carries no
	// tool-specific row for yet. A freeze that outlives its gap fails.
	Missing map[string]string
	// MinLabels is the smallest plausible domain size (default 3).
	MinLabels int
}

// DomainIsSubset is the CLASSIFIER to TABLE half that makes the pair a real
// subset proof: it extracts the classifier's case labels from source,
// feeds each back through the real classifier, and requires a
// tool-specific tooltax row that agrees. It catches a new `case "x":
// return models.ActionRunCommand` added with no tooltax row. Stale freezes
// fail too, so a freeze can never outlive what it acknowledged.
func DomainIsSubset(t testing.TB, d Domain) {
	t.Helper()
	dir := d.Dir
	if dir == "" {
		dir = "."
	}
	minLabels := d.MinLabels
	if minLabels == 0 {
		minLabels = 3
	}
	domain, err := conformast.SwitchCaseStrings(dir, d.Func)
	if err != nil {
		t.Errorf("extracting the classifier domain of %s: %v", d.Func, err)
		return
	}
	if len(domain) < minLabels {
		t.Errorf("extracted only %d case labels (%q) from %s: too few to be the real domain; "+
			"the classifier shape probably changed", len(domain), domain, d.Func)
		return
	}
	inDomain := make(map[string]bool, len(domain))
	for _, n := range domain {
		inDomain[n] = true
	}
	for _, native := range domain {
		got := d.Classify(native)
		if got == "" || got == models.ActionUnknown {
			reason, frozen := d.Unclassified[native]
			if !frozen {
				t.Errorf("case %q resolves to %q when fed back through the classifier: "+
					"either a DELIBERATE unknown or a DEAD case whose label the "+
					"classifier's own normalization can never produce; say which in "+
					"the Unclassified freeze", native, got)
			} else if !ValidUnclassifiedReason(reason) {
				t.Errorf("Unclassified[%q] = %q does not start with %q or %q "+
					"followed by a non-empty explanation", native, reason, "deliberate: ", "dead: ")
			}
			continue
		}
		for _, tool := range d.Tools {
			e, ok := tooltax.Resolve(tool, native)
			if !ok || e.Tool != tool {
				if _, known := d.Missing[native]; known {
					continue
				}
				t.Errorf("the classifier maps %q to %q but internal/tooltax has no "+
					"%s-specific row for it: add the name to tooltax's table",
					native, got, tool)
				continue
			}
			if got != e.ActionType {
				t.Errorf("drift: %s %q: classifier says %q, tooltax says %q",
					tool, native, got, e.ActionType)
			}
		}
	}
	for native := range d.Unclassified {
		if !inDomain[native] {
			t.Errorf("Unclassified entry %q is no longer a case label of the "+
				"classifier: remove the stale freeze", native)
		}
	}
	for native := range d.Missing {
		if !inDomain[native] {
			t.Errorf("Missing entry %q is no longer a case label of the "+
				"classifier: remove the stale freeze", native)
			continue
		}
		for _, tool := range d.Tools {
			if e, ok := tooltax.Resolve(tool, native); ok && e.Tool == tool {
				t.Errorf("Missing still lists %q but tooltax now carries a "+
					"%s-specific row for it: delete the freeze", native, tool)
			}
		}
	}
}

// ValidUnclassifiedReason enforces the Unclassified value contract:
// "deliberate: <why>" or "dead: <why>" with a non-empty <why>.
func ValidUnclassifiedReason(reason string) bool {
	for _, prefix := range []string{"deliberate: ", "dead: "} {
		if strings.HasPrefix(reason, prefix) && strings.TrimSpace(strings.TrimPrefix(reason, prefix)) != "" {
			return true
		}
	}
	return false
}
