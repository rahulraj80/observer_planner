package componentstate

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// customBuildFixture resolves a developer-based manifest that omits one
// omittable component, found from the catalog.
func customBuildFixture(t *testing.T) (components.CustomBuild, string) {
	t.Helper()
	for _, id := range components.InDistro(components.DistroDeveloper) {
		c, _ := components.Get(id)
		if c.Class == components.ClassCore || c.Distros[components.DistroDeveloper] != components.DefaultOn {
			continue
		}
		cb, problems := components.ResolveManifest(components.Manifest{
			Schema: components.ManifestSchemaV1, Base: components.DistroDeveloper, Omit: []string{id},
		})
		if len(problems) == 0 {
			return cb, id
		}
	}
	t.Skip("no omittable default-active developer component")
	return components.CustomBuild{}, ""
}

// TestCustomBuildStartsFromItsManifest pins the custom-build default (plan
// 5.3, lane MP-CUST): an unconfigured custom binary resolves from its
// manifest's default selection, reports base "custom", never writes that
// base back as a [components] distro, and cannot activate what the manifest
// omits even by an add.
func TestCustomBuildStartsFromItsManifest(t *testing.T) {
	cb, omitted := customBuildFixture(t)
	prevD, prevC := buildDistro, buildCustomDefaults
	t.Cleanup(func() { buildDistro, buildCustomDefaults = prevD, prevC })
	buildDistro = func() components.Distro { return "" }
	buildCustomDefaults = func() ([]string, bool) { return cb.DefaultSelection, true }

	s := Resolve(config.ComponentsConfig{}, cb.Compiled)
	if s.Base != BaseCustom {
		t.Fatalf("base %q, want %q", s.Base, BaseCustom)
	}
	for _, id := range cb.DefaultSelection {
		if !s.Active(id) {
			t.Errorf("%s is in the manifest's default selection but inactive", id)
		}
	}
	if s.Active(omitted) {
		t.Fatalf("%s is omitted by the manifest but active", omitted)
	}
	if got := s.Section().Distro; got != "" {
		t.Fatalf("Section().Distro = %q, want empty (custom is not a prebuilt distro)", got)
	}
	v := s.View()
	if v.Base != BaseCustom || v.BinaryDistro != components.DistroCustom {
		t.Fatalf("view base %q binary distro %q", v.Base, v.BinaryDistro)
	}
	added := Resolve(config.ComponentsConfig{Add: []string{omitted}}, cb.Compiled)
	if added.Active(omitted) {
		t.Fatalf("an add activated %s, which the custom build does not offer", omitted)
	}
}
