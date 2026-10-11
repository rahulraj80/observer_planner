package nodecomponents

import (
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// vocab.go is the node.components AUTHORING VOCABULARY (lane PICKERS, D-571):
// every selector the org composer may offer as a choice, so the admin picks
// from a list instead of typing ids. The anti-drift property is the one the
// node.governance vocabulary has: every entry is derived from the compiler's
// own tables (governable(), the closed class and stability vocabularies) and
// is accepted by Compile, pinned by TestVocabularyCompiles. Nothing here is
// hand-authored.

// VocabComponent is one governable node component a selector can name.
type VocabComponent struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Class     string `json:"class"`
	Stability string `json:"stability,omitempty"`
}

// VocabSelector is one group selector (a class, a prefix glob or a
// stability level) with the governable ids it matches today.
type VocabSelector struct {
	Selector string   `json:"selector"`
	Kind     string   `json:"kind"`
	Label    string   `json:"label"`
	Matches  []string `json:"matches"`
}

// Vocab is the whole authoring vocabulary.
type Vocab struct {
	CatalogVersion int              `json:"catalog_version"`
	Components     []VocabComponent `json:"components"`
	Selectors      []VocabSelector  `json:"selectors"`
}

// classLabels is the display copy for the node classes a selector may name.
var classLabels = map[components.Class]string{
	components.ClassFeature:     "Every feature",
	components.ClassAdapter:     "Every adapter",
	components.ClassIntegration: "Every integration",
}

// Vocabulary returns the authoring vocabulary of this build's catalog: the
// governable node components (core excluded: it is always active and cannot
// be denied) in catalog order, then the class selectors, the prefix globs
// that match two or more components, and the stability selectors that match
// at least one.
func Vocabulary() Vocab {
	rows := governable()
	v := Vocab{CatalogVersion: components.CatalogVersion}
	for _, r := range rows {
		v.Components = append(v.Components, VocabComponent{
			ID: r.ID, Title: r.Title, Class: string(r.Class), Stability: string(r.Stability),
		})
	}
	matching := func(s Selector) []string {
		out := []string{}
		for _, r := range rows {
			if s.Matches(r) {
				out = append(out, r.ID)
			}
		}
		return out
	}
	for _, c := range components.Classes() {
		label, ok := classLabels[c]
		if !ok {
			continue
		}
		raw := "class:" + string(c)
		if s, err := ParseSelector(raw); err == nil {
			if m := matching(s); len(m) > 0 {
				v.Selectors = append(v.Selectors, VocabSelector{Selector: raw, Kind: string(KindClass), Label: label, Matches: m})
			}
		}
	}
	prefixes := map[string]bool{}
	for _, r := range rows {
		parts := strings.Split(r.ID, ".")
		for k := 1; k < len(parts); k++ {
			prefixes[strings.Join(parts[:k], ".")+".*"] = true
		}
	}
	globs := make([]string, 0, len(prefixes))
	for p := range prefixes {
		globs = append(globs, p)
	}
	sort.Strings(globs)
	for _, raw := range globs {
		s, err := ParseSelector(raw)
		if err != nil {
			continue
		}
		if m := matching(s); len(m) >= 2 {
			v.Selectors = append(v.Selectors, VocabSelector{
				Selector: raw, Kind: string(KindPrefix), Label: "Every " + strings.TrimSuffix(raw, "*") + " component", Matches: m,
			})
		}
	}
	for _, st := range []components.Stability{
		components.StabilityStable, components.StabilityPreview, components.StabilityExperimental, components.StabilityDeprecated,
	} {
		raw := "stability:" + string(st)
		if s, err := ParseSelector(raw); err == nil {
			if m := matching(s); len(m) > 0 {
				v.Selectors = append(v.Selectors, VocabSelector{
					Selector: raw, Kind: string(KindStability), Label: "Every " + string(st) + " component", Matches: m,
				})
			}
		}
	}
	return v
}
