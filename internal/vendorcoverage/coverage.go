package vendorcoverage

import (
	"fmt"
	"sort"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface/catalog"
)

// Target is a coverage target that is not a registry row.
type Target struct {
	Name        string
	Kind        vendorsurface.TargetKind
	Description string
	Surfaces    []vendorsurface.SurfaceID
}

// Targets returns the non-registry coverage targets, sorted by name.
func Targets() []Target {
	out := append([]Target(nil), targets...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// maxInheritDepth bounds InheritsFrom chains; the registry uses depth 1.
const maxInheritDepth = 4

// SurfacesFor returns the surfaces covering a registry tool: its own plus
// every ancestor's along InheritsFrom, sorted and de-duplicated. ok is false
// for an unknown tool, a None answer, or a broken or cyclic chain.
func SurfacesFor(tool string) ([]vendorsurface.SurfaceID, bool) {
	seen := map[string]bool{}
	set := map[vendorsurface.SurfaceID]bool{}
	for depth := 0; ; depth++ {
		if depth > maxInheritDepth {
			return nil, false
		}
		c, ok := integration.For(tool)
		if !ok || seen[tool] || c.Enterprise.None != nil {
			return nil, false
		}
		seen[tool] = true
		for _, id := range c.Enterprise.Surfaces {
			set[id] = true
		}
		if c.Enterprise.InheritsFrom == "" {
			break
		}
		tool = c.Enterprise.InheritsFrom
	}
	if len(set) == 0 {
		return nil, false
	}
	out := make([]vendorsurface.SurfaceID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, true
}

// ReverseIndex maps each surface to the registry tools it covers (inheritance
// resolved), each tool list sorted.
func ReverseIndex() map[vendorsurface.SurfaceID][]string {
	out := map[vendorsurface.SurfaceID][]string{}
	for _, tool := range integration.Tools() {
		ids, ok := SurfacesFor(tool)
		if !ok {
			continue
		}
		for _, id := range ids {
			out[id] = append(out[id], tool)
		}
	}
	for id := range out {
		sort.Strings(out[id])
	}
	return out
}

// Check returns every inconsistency between the registry, the catalog and the
// target table: an unanswered or doubly answered registry row, an Enterprise
// key with no registry row, a surface id that is not a catalog row, a broken
// inheritance, a target that shadows a registry tool or cites an unknown
// surface. Dead registry rows (harness lifecycle) still need an answer only
// when they are advertised; rows are never deleted.
func Check(cat *catalog.Catalog) []error {
	var errs []error
	tools := map[string]bool{}
	for _, t := range integration.Tools() {
		tools[t] = true
	}
	for _, t := range integration.EnterpriseTools() {
		if !tools[t] {
			errs = append(errs, fmt.Errorf("enterprise answer for %q has no registry row", t))
		}
	}
	for _, t := range integration.Tools() {
		c, _ := integration.For(t)
		a := c.Enterprise
		if !a.Answered() {
			if c.Lifecycle.Advertised() {
				errs = append(errs, fmt.Errorf("registry row %q has no single enterprise answer", t))
			}
			continue
		}
		for _, id := range a.Surfaces {
			if !cat.Has(id) {
				errs = append(errs, fmt.Errorf("registry row %q cites %q, which is not a catalog row", t, id))
			}
		}
		if a.InheritsFrom != "" {
			if _, ok := SurfacesFor(t); !ok {
				errs = append(errs, fmt.Errorf("registry row %q inherits from %q, which does not resolve to surfaces", t, a.InheritsFrom))
			}
		}
		if a.None != nil && (!vendorsurface.ValidDate(a.None.Checked) || len(a.None.SourcesSearched) == 0) {
			errs = append(errs, fmt.Errorf("registry row %q has a None answer without a date and the sources searched", t))
		}
	}
	names := map[string]bool{}
	for _, tg := range targets {
		if tools[tg.Name] {
			errs = append(errs, fmt.Errorf("target %q shadows a registry row; native coverage belongs in integration", tg.Name))
		}
		if names[tg.Name] {
			errs = append(errs, fmt.Errorf("target %q listed twice", tg.Name))
		}
		names[tg.Name] = true
		if tg.Kind == vendorsurface.TargetAdapterNative {
			errs = append(errs, fmt.Errorf("target %q: adapter_native targets are registry rows, not table entries", tg.Name))
		}
		for _, id := range tg.Surfaces {
			if !cat.Has(id) {
				errs = append(errs, fmt.Errorf("target %q cites %q, which is not a catalog row", tg.Name, id))
			}
		}
	}
	return errs
}
