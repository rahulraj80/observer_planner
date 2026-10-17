package drivers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/vendorapi/fixturecorpus"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface/catalog"
)

// TestDriverTableMatchesBuiltCatalogRows: a driver exists exactly for the
// Built pull surfaces of the catalog. An AwaitingReference or CatalogOnly
// row never has a driver (TestAwaitingReferenceSurfacesHaveNoDriver), and a
// row is moved to Built only together with its driver.
func TestDriverTableMatchesBuiltCatalogRows(t *testing.T) {
	c, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	built := map[vs.SurfaceID]bool{}
	for _, s := range c.Surfaces() {
		if s.Build == vs.BuildBuilt && (s.Reach == vs.ReachPullAPI || s.Reach == vs.ReachInvokeAPI) {
			built[s.ID] = true
		}
	}
	for _, id := range Surfaces() {
		s, ok := c.Surface(id)
		if !ok {
			t.Errorf("driver %s has no catalog row", id)
			continue
		}
		if s.Build != vs.BuildBuilt {
			t.Errorf("driver %s: catalog row is %s, want built", id, s.Build)
		}
		d, _ := Lookup(id)
		if d.Surface != id {
			t.Errorf("driver %s reports surface %s", id, d.Surface)
		}
		if d.Auth.Kind != s.Auth.Kind {
			t.Errorf("driver %s auth kind %s, catalog %s", id, d.Auth.Kind, s.Auth.Kind)
		}
		if (d.Parse == nil) == (d.ParseResponse == nil) || d.Build == nil {
			t.Errorf("driver %s needs Build and exactly one of Parse or ParseResponse", id)
		}
		if len(d.Supplies) == 0 {
			t.Errorf("driver %s declares no supply", id)
		}
		for _, cls := range d.Supplies {
			found := false
			for _, sp := range s.Supplies {
				found = found || sp.Class == cls
			}
			if !found {
				t.Errorf("driver %s produces %s, which the catalog row does not supply", id, cls)
			}
		}
		delete(built, id)
	}
	for id := range built {
		t.Errorf("catalog row %s is built but has no driver", id)
	}
}

func TestAwaitingReferenceSurfacesHaveNoDriver(t *testing.T) {
	c, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range Surfaces() {
		if s, _ := c.Surface(id); s.Build == vs.BuildAwaitingReference || s.Build == vs.BuildCatalogOnly {
			t.Errorf("%s is %s and must not have a driver", id, s.Build)
		}
	}
	if _, ok := Lookup("github.no_such_surface"); ok {
		t.Error("lookup of an unknown surface succeeded")
	}
}

// TestThirdPartyCeilingIsAssumptionTested (driver-table half): a Built
// surface's supply never claims a synthetic rung above the evidence ceiling
// of its success fixtures' field ledgers (plan 12A.6). A surface resting on
// a ThirdParty or Inferred field reaches at most assumption_tested.
func TestThirdPartyCeilingIsAssumptionTested(t *testing.T) {
	c, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range Surfaces() {
		s, _ := c.Surface(id)
		dir := filepath.Join("..", "..", "..", "testdata", "vendorapi", s.Vendor, string(id))
		sidecars, err := filepath.Glob(filepath.Join(dir, "*.source.json"))
		if err != nil || len(sidecars) == 0 {
			t.Errorf("%s: no fixture sidecars under %s", id, dir)
			continue
		}
		var grades []vs.EvidenceGrade
		for _, p := range sidecars {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			sc, err := fixturecorpus.DecodeSidecar(b)
			if err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			if sc.HTTP.Status >= 200 && sc.HTTP.Status < 300 {
				grades = append(grades, fixturecorpus.RequiredGrades(sc.FieldLedger)...)
			}
		}
		ceiling := vs.SyntheticCeiling(grades)
		for _, sp := range s.Supplies {
			if vs.SyntheticRank(sp.Verification.Synthetic) > vs.SyntheticRank(ceiling) {
				t.Errorf("%s/%s claims %s above the fixture ceiling %s", id, sp.Class, sp.Verification.Synthetic, ceiling)
			}
		}
	}
}

// TestKnownPathsMatchCorpusLedgers (lane F11, plan 12A.7): every driver's
// KnownPaths equal the union of its success fixtures' field ledgers, so the
// runner's shape digest keeps exactly the documented member names and
// wildcards everything else.
func TestKnownPathsMatchCorpusLedgers(t *testing.T) {
	c, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range Surfaces() {
		s, _ := c.Surface(id)
		d, _ := Lookup(id)
		sidecars, _ := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "vendorapi", s.Vendor, string(id), "*.source.json"))
		want := map[string]bool{}
		for _, p := range sidecars {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			sc, err := fixturecorpus.DecodeSidecar(b)
			if err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			if sc.HTTP.Status >= 200 && sc.HTTP.Status < 300 {
				for k := range sc.FieldLedger {
					want[k] = true
				}
			}
		}
		got := map[string]bool{}
		for _, k := range d.KnownPaths {
			got[k] = true
		}
		for k := range want {
			if !got[k] {
				t.Errorf("%s: ledger path %q missing from KnownPaths", id, k)
			}
		}
		for k := range got {
			if !want[k] {
				t.Errorf("%s: KnownPaths has %q, which no ledger documents", id, k)
			}
		}
	}
}
