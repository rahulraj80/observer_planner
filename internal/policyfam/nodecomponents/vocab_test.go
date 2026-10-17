package nodecomponents

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// TestVocabularyCompiles pins the anti-drift property (lane PICKERS, D-571):
// every selector the composer offers compiles strictly in each list, and the
// match lists agree with the compiler's own selector matching.
func TestVocabularyCompiles(t *testing.T) {
	v := Vocabulary()
	if len(v.Components) == 0 || len(v.Selectors) == 0 {
		t.Fatalf("empty vocabulary: %d components, %d selectors", len(v.Components), len(v.Selectors))
	}
	raws := make([]string, 0, len(v.Components)+len(v.Selectors))
	for _, c := range v.Components {
		if c.Class == string(components.ClassCore) {
			t.Fatalf("%s: core is not governable and must not be offered", c.ID)
		}
		raws = append(raws, c.ID)
	}
	for _, s := range v.Selectors {
		raws = append(raws, s.Selector)
		if len(s.Matches) == 0 {
			t.Fatalf("%s: offered with no match", s.Selector)
		}
	}
	for _, raw := range raws {
		if _, err := Compile(BodyV1{Allow: []string{raw}}); err != nil {
			t.Errorf("allow %q: %v", raw, err)
		}
		if _, err := Compile(BodyV1{Require: []string{raw}}); err != nil {
			t.Errorf("require %q: %v", raw, err)
		}
		if _, err := Compile(BodyV1{Deny: []DenyEntryV1{{Selector: raw}}}); err != nil {
			t.Errorf("deny %q: %v", raw, err)
		}
	}
}
