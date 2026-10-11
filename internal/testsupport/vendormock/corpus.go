package vendormock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/vendorapi/fixturecorpus"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// Fixture is one corpus file with its decoded sidecar.
type Fixture struct {
	Surface  vendorsurface.SurfaceID
	Scenario fixturecorpus.Scenario
	Sidecar  fixturecorpus.Sidecar
	Body     []byte
	NDJSON   bool
}

// SurfaceFixtures is every fixture of one surface, keyed by scenario.
type SurfaceFixtures struct {
	ID       vendorsurface.SurfaceID
	Vendor   string
	Fixtures map[fixturecorpus.Scenario]Fixture
}

// Has reports whether the surface carries a fixture for the scenario.
func (s *SurfaceFixtures) Has(sc fixturecorpus.Scenario) bool {
	_, ok := s.Fixtures[sc]
	return ok
}

// Success returns the first-page fixture: normal_page, else report_link,
// else enum_sweep, else empty_page.
func (s *SurfaceFixtures) Success() (Fixture, bool) {
	for _, sc := range []fixturecorpus.Scenario{
		fixturecorpus.ScenarioNormalPage, fixturecorpus.ScenarioReportLink,
		fixturecorpus.ScenarioEnumSweep, fixturecorpus.ScenarioEmptyPage,
	} {
		if f, ok := s.Fixtures[sc]; ok {
			return f, true
		}
	}
	return Fixture{}, false
}

// Terminal returns the documented terminal page: last_page, else empty_page.
func (s *SurfaceFixtures) Terminal() (Fixture, bool) {
	for _, sc := range []fixturecorpus.Scenario{fixturecorpus.ScenarioLastPage, fixturecorpus.ScenarioEmptyPage} {
		if f, ok := s.Fixtures[sc]; ok {
			return f, true
		}
	}
	return Fixture{}, false
}

// AuthError returns the surface's documented auth failure body: error_401,
// else error_403.
func (s *SurfaceFixtures) AuthError() (Fixture, bool) {
	for _, sc := range []fixturecorpus.Scenario{fixturecorpus.ScenarioError401, fixturecorpus.ScenarioError403} {
		if f, ok := s.Fixtures[sc]; ok {
			return f, true
		}
	}
	return Fixture{}, false
}

// Corpus is the loaded fixture corpus.
type Corpus struct {
	Root     string
	Surfaces map[vendorsurface.SurfaceID]*SurfaceFixtures
}

// IDs returns the surface ids in sorted order.
func (c *Corpus) IDs() []vendorsurface.SurfaceID {
	out := make([]vendorsurface.SurfaceID, 0, len(c.Surfaces))
	for id := range c.Surfaces {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// VendorFixture returns a fixture of the scenario from any surface of the
// same vendor and plane family, preferring the surface itself. Error envelopes
// are vendor-wide (each vendor documents one error envelope), so a 429 or 400
// body documented once serves every surface of that vendor.
func (c *Corpus) VendorFixture(id vendorsurface.SurfaceID, sc fixturecorpus.Scenario) (Fixture, bool) {
	if sf, ok := c.Surfaces[id]; ok {
		if f, ok := sf.Fixtures[sc]; ok {
			return f, true
		}
	}
	vendor := vendorOf(id)
	for _, other := range c.IDs() {
		if vendorOf(other) != vendor {
			continue
		}
		if f, ok := c.Surfaces[other].Fixtures[sc]; ok {
			return f, true
		}
	}
	return Fixture{}, false
}

func vendorOf(id vendorsurface.SurfaceID) string {
	return strings.SplitN(string(id), ".", 2)[0]
}

// repoRoot locates the repository from this source file, so the corpus loads
// from any test package regardless of its depth.
func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("vendormock.repoRoot: runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), nil
}

// DefaultCorpusRoot is testdata/vendorapi in this repository.
func DefaultCorpusRoot() (string, error) {
	r, err := repoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(r, "testdata", "vendorapi"), nil
}

// DefaultSchemaRoot is testdata/vendorschema in this repository.
func DefaultSchemaRoot() (string, error) {
	r, err := repoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(r, "testdata", "vendorschema"), nil
}

// LoadCorpus walks root (testdata/vendorapi/<vendor>/<surface>/<scenario>)
// and decodes every sidecar strictly. It does not re-validate hashes or
// ledgers; internal/vendorapi/fixturecorpus's corpus tests own that.
func LoadCorpus(root string) (*Corpus, error) {
	c := &Corpus{Root: root, Surfaces: map[vendorsurface.SurfaceID]*SurfaceFixtures{}}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() || !strings.HasSuffix(name, ".source.json") {
			return nil
		}
		base := strings.TrimSuffix(path, ".source.json")
		raw, err := os.ReadFile(path) //nolint:gosec // G122: test-only read of the checked-in fixture corpus
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		side, err := fixturecorpus.DecodeSidecar(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		fixPath, nd := base+".json", false
		if _, statErr := os.Stat(base + ".ndjson"); statErr == nil {
			fixPath, nd = base+".ndjson", true
		}
		body, err := os.ReadFile(fixPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", fixPath, err)
		}
		sf := c.Surfaces[side.SurfaceID]
		if sf == nil {
			sf = &SurfaceFixtures{
				ID: side.SurfaceID, Vendor: vendorOf(side.SurfaceID),
				Fixtures: map[fixturecorpus.Scenario]Fixture{},
			}
			c.Surfaces[side.SurfaceID] = sf
		}
		sf.Fixtures[side.Scenario] = Fixture{
			Surface: side.SurfaceID, Scenario: side.Scenario, Sidecar: side,
			Body: body, NDJSON: nd,
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("vendormock.LoadCorpus: %w", err)
	}
	if len(c.Surfaces) == 0 {
		return nil, fmt.Errorf("vendormock.LoadCorpus: no fixtures under %s", root)
	}
	return c, nil
}

// SchemaExtract is one testdata/vendorschema/<name>.json file: a fully
// dereferenced response schema with its public source.
type SchemaExtract struct {
	Name         string         `json:"name"`
	SourceURL    string         `json:"source_url"`
	SourceKind   string         `json:"source_kind"`
	SpecVersion  string         `json:"sdk_or_spec_version_or_commit"`
	SourceSHA256 string         `json:"source_sha256"`
	CheckedOn    string         `json:"checked_on"`
	Pointer      string         `json:"pointer"`
	Status       string         `json:"status"`
	Note         string         `json:"note"`
	Schema       map[string]any `json:"schema"`
}

// LoadSchema reads one schema extract by name (a surface id).
func LoadSchema(name string) (SchemaExtract, error) {
	root, err := DefaultSchemaRoot()
	if err != nil {
		return SchemaExtract{}, err
	}
	raw, err := os.ReadFile(filepath.Join(root, name+".json"))
	if err != nil {
		return SchemaExtract{}, fmt.Errorf("vendormock.LoadSchema: %w", err)
	}
	var s SchemaExtract
	if err := json.Unmarshal(raw, &s); err != nil {
		return SchemaExtract{}, fmt.Errorf("vendormock.LoadSchema %s: %w", name, err)
	}
	return s, nil
}
