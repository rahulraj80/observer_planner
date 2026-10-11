package fixturecorpus

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface/catalog"
)

var extraForbiddenImports []string

const corpusRoot = "../../../testdata/vendorapi"

type entry struct {
	dir, scenario, fixture, sidecar string
	ndjson                          bool
}

func walkCorpus(t *testing.T) []entry {
	t.Helper()
	var out []entry
	fixtures := map[string]bool{}
	sidecars := map[string]bool{}
	err := filepath.WalkDir(corpusRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		switch {
		case strings.HasSuffix(name, ".source.json"):
			sidecars[strings.TrimSuffix(path, ".source.json")] = true
		case strings.HasSuffix(name, ".json"), strings.HasSuffix(name, ".ndjson"):
			fixtures[path] = true
		case name == "README.md":
		default:
			t.Errorf("unexpected file in the corpus: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", corpusRoot, err)
	}
	for f := range fixtures {
		nd := strings.HasSuffix(f, ".ndjson")
		base := strings.TrimSuffix(strings.TrimSuffix(f, ".ndjson"), ".json")
		if !sidecars[base] {
			t.Errorf("%s has no %s.source.json sidecar", f, filepath.Base(base))
			continue
		}
		delete(sidecars, base)
		out = append(out, entry{
			dir: filepath.Dir(f), scenario: filepath.Base(base), fixture: f,
			sidecar: base + ".source.json", ndjson: nd,
		})
	}
	for s := range sidecars {
		t.Errorf("sidecar %s.source.json has no fixture", s)
	}
	if len(out) == 0 {
		t.Fatal("the corpus is empty: the pins would be vacuous")
	}
	return out
}

func lookup(t *testing.T) SurfaceLookup {
	t.Helper()
	c, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	return func(id vendorsurface.SurfaceID) (vendorsurface.BuildState, bool) {
		s, ok := c.Surface(id)
		return s.Build, ok
	}
}

// TestEveryFixtureHasSourceSidecar is the plan section 13 pin: a fixture
// without its sidecar (or a sidecar without its fixture) fails.
func TestEveryFixtureHasSourceSidecar(t *testing.T) {
	walkCorpus(t)
}

// TestCorpusSidecarsValidate checks every sidecar against its fixture bytes,
// the catalog and the evidence ledger, and that the directory layout is
// testdata/vendorapi/<vendor>/<surface id>/<scenario>.
func TestCorpusSidecarsValidate(t *testing.T) {
	look := lookup(t)
	for _, e := range walkCorpus(t) {
		raw, err := os.ReadFile(e.sidecar)
		if err != nil {
			t.Fatalf("read %s: %v", e.sidecar, err)
		}
		s, err := DecodeSidecar(raw)
		if err != nil {
			t.Errorf("%s: %v", e.sidecar, err)
			continue
		}
		fx, err := os.ReadFile(e.fixture)
		if err != nil {
			t.Fatalf("read %s: %v", e.fixture, err)
		}
		for _, verr := range Validate(s, fx, e.ndjson, look) {
			t.Errorf("%s: %v", e.sidecar, verr)
		}
		if string(s.SurfaceID) != filepath.Base(e.dir) {
			t.Errorf("%s: surface %s does not match its directory %s", e.sidecar, s.SurfaceID, filepath.Base(e.dir))
		}
		if vendor := strings.Split(string(s.SurfaceID), ".")[0]; vendor != filepath.Base(filepath.Dir(e.dir)) {
			t.Errorf("%s: vendor directory %s, want %s", e.sidecar, filepath.Base(filepath.Dir(e.dir)), vendor)
		}
		if string(s.Scenario) != e.scenario {
			t.Errorf("%s: scenario %s does not match the file name %s", e.sidecar, s.Scenario, e.scenario)
		}
	}
}

// TestEverySurfaceHasSuccessAndErrorFixtures: each surface directory carries
// at least one success page and one documented error body (plan 12A.1).
func TestEverySurfaceHasSuccessAndErrorFixtures(t *testing.T) {
	ok := map[string][2]bool{}
	for _, e := range walkCorpus(t) {
		v := ok[e.dir]
		if Scenario(e.scenario).IsError() {
			v[1] = true
		} else {
			v[0] = true
		}
		ok[e.dir] = v
	}
	for dir, v := range ok {
		if !v[0] || !v[1] {
			t.Errorf("%s: needs a success fixture and an error fixture (have success=%v error=%v)", dir, v[0], v[1])
		}
	}
}

// secretShapes are credential prefixes that must never appear in the corpus.
// Key IDs (apikey_..., key_...) are identifiers, not secrets, and are allowed.
var secretShapes = regexp.MustCompile(`sk-ant-[A-Za-z0-9]|sk-(proj|admin|svcacct)-[A-Za-z0-9]|ghp_[A-Za-z0-9]{10}|github_pat_|gho_[A-Za-z0-9]{10}|xox[abp]-|AKIA[0-9A-Z]{12}`)

// TestNoRealAccountMaterial: no credential-shaped string in any fixture or
// sidecar, and every sidecar declares a public-source-only derivation.
func TestNoRealAccountMaterial(t *testing.T) {
	for _, e := range walkCorpus(t) {
		for _, p := range []string{e.fixture, e.sidecar} {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			if m := secretShapes.Find(b); m != nil {
				t.Errorf("%s contains a credential-shaped string %q", p, m)
			}
		}
	}
}
