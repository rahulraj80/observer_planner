package vendormock

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface/catalog"
)

func loadCorpus(t *testing.T) *Corpus {
	t.Helper()
	root, err := DefaultCorpusRoot()
	if err != nil {
		t.Fatal(err)
	}
	c, err := LoadCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestEverySurfaceHasAProfile: the mock and the suite refuse a surface the
// mechanics table does not cover, so every corpus surface needs a row.
func TestEverySurfaceHasAProfile(t *testing.T) {
	c := loadCorpus(t)
	for _, id := range c.IDs() {
		if _, ok := ProfileFor(id); !ok {
			t.Errorf("%s: no vendor profile row", id)
		}
		if _, ok := c.Surfaces[id].Success(); !ok {
			t.Errorf("%s: no success fixture to serve", id)
		}
		if _, ok := c.Surfaces[id].AuthError(); !ok {
			t.Errorf("%s: no documented auth error body", id)
		}
	}
	for _, p := range Profiles {
		if !strings.HasPrefix(p.Source, "https://") {
			t.Errorf("profile %s: source %q is not a public https URL", p.Prefix, p.Source)
		}
	}
}

// TestConformanceScenariosDeclaredPerSurface is the plan section 13 pin: every
// corpus surface declares its scenarios, and the declaration equals what the
// applicability table derives (an undeclared applicable scenario fails, and so
// does a declared one that does not apply). Regenerate after a reviewed corpus
// change with VENDORMOCK_WRITE_DECLARATIONS=1.
func TestConformanceScenariosDeclaredPerSurface(t *testing.T) {
	c := loadCorpus(t)
	if os.Getenv("VENDORMOCK_WRITE_DECLARATIONS") == "1" {
		b, err := DeclarationsFor(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("declarations.json", b, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Skip("declarations.json rewritten")
	}
	d, err := Declarations()
	if err != nil {
		t.Fatalf("declarations.json: %v", err)
	}
	for _, id := range c.IDs() {
		got, ok := d[id]
		if !ok {
			t.Errorf("%s: no conformance declaration", id)
			continue
		}
		want := Applicable(c.Surfaces[id])
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: declared %v, applicable %v", id, got, want)
		}
	}
	for id := range d {
		if _, ok := c.Surfaces[id]; !ok {
			t.Errorf("declaration for %s, which has no fixtures", id)
		}
	}
}

// TestDeclaredSurfacesAreDriverableCatalogRows: a conformance declaration is
// only meaningful for a surface a driver may be built for.
func TestDeclaredSurfacesAreDriverableCatalogRows(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	d, err := Declarations()
	if err != nil {
		t.Fatal(err)
	}
	for id := range d {
		s, ok := cat.Surface(id)
		if !ok || !vendorsurface.Driverable(s.Build) {
			t.Errorf("%s: declared but not a driverable catalog row", id)
		}
	}
}

// TestClassifyCoversEveryFamily keeps the suite non-vacuous: the corpus
// exercises every paging family the mock reproduces.
func TestClassifyCoversEveryFamily(t *testing.T) {
	c := loadCorpus(t)
	seen := map[PagingFamily]int{}
	for _, id := range c.IDs() {
		seen[Classify(c.Surfaces[id])]++
	}
	for _, f := range []PagingFamily{
		FamilyNone, FamilyNextPage, FamilyAfterID, FamilyLink, FamilyBodyPage,
		FamilyReport, FamilyODataNext, FamilyEndCursor, FamilyNestedCursor, FamilyNextToken, FamilyOffset,
	} {
		if seen[f] == 0 {
			t.Errorf("no corpus surface in paging family %s", f)
		}
	}
	t.Logf("paging families: %v", seen)
}

// TestReferenceDriverConformance runs the shared suite over every corpus
// surface with the reference driver: proof that each mock reproduces its
// documented mechanics and every scenario assertion can pass and fail.
func TestReferenceDriverConformance(t *testing.T) {
	c := loadCorpus(t)
	for _, id := range c.IDs() {
		id := id
		t.Run(string(id), func(t *testing.T) {
			Run(t, c, id, func() Driver {
				d, err := NewReferenceDriver(c, id)
				if err != nil {
					t.Fatal(err)
				}
				return d
			})
		})
	}
}

// TestMockRejectsWrongPlaneKey is the plan section 13 pin: a credential of
// another plane presented in the documented scheme gets the vendor's
// documented auth error body, byte for byte.
func TestMockRejectsWrongPlaneKey(t *testing.T) {
	c := loadCorpus(t)
	for _, id := range c.IDs() {
		m, err := New(c, id, Options{ValidToken: ValidToken, WrongPlaneToken: WrongPlaneToken})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		d, err := NewReferenceDriver(c, id)
		if err != nil {
			t.Fatal(err)
		}
		_, ferr := d.Fetch(context.Background(), Conn{
			BaseURL: m.URL, TokenURL: m.TokenURL, Token: WrongPlaneToken,
			HTTP: &http.Client{Timeout: 5 * time.Second},
		})
		if ClassOf(ferr) != ErrAuth {
			t.Errorf("%s: wrong-plane key class %q", id, ClassOf(ferr))
		}
		reqs := m.Requests()
		authErr, _ := c.Surfaces[id].AuthError()
		if len(reqs) != 1 || reqs[0].Status != authErr.Sidecar.HTTP.Status {
			t.Errorf("%s: requests %+v, want one answered %d", id, reqs, authErr.Sidecar.HTTP.Status)
		}
		m.Close()
	}
}

// TestMutationsRejected is the plan section 13 pin for 12A.6: each mutation of
// mock behaviour (record container renamed or root type changed, paging flag
// type changed, a paging loop, a Link leaving the API origin) is rejected or
// flagged by the driver, never landed silently.
func TestMutationsRejected(t *testing.T) {
	c := loadCorpus(t)
	counts := map[ScenarioID]int{}
	for _, id := range c.IDs() {
		sf := c.Surfaces[id]
		for _, s := range Applicable(sf) {
			switch s {
			case SSchemaDrift, SFlagTypeDrift, SLoopCursor, SForeignLink:
			default:
				continue
			}
			counts[s]++
			id, s := id, s
			t.Run(string(id)+"/"+string(s), func(t *testing.T) {
				runScenario(t, c, sf, s, func() Driver {
					d, err := NewReferenceDriver(c, id)
					if err != nil {
						t.Fatal(err)
					}
					return d
				})
			})
		}
	}
	for _, s := range []ScenarioID{SSchemaDrift, SFlagTypeDrift, SLoopCursor, SForeignLink} {
		if counts[s] == 0 {
			t.Errorf("mutation %s exercised on no surface", s)
		}
	}
}

// TestMockRequiresDocumentedHeaders: a request without a header every
// reference example sends (anthropic-version) gets the vendor's 400 body.
func TestMockRequiresDocumentedHeaders(t *testing.T) {
	c := loadCorpus(t)
	id := vendorsurface.SurfaceID("anthropic.console.users")
	m, err := New(c, id, Options{ValidToken: ValidToken})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	req, _ := http.NewRequest(http.MethodGet, m.URL+"/v1/organizations/users", nil)
	req.Header.Set("X-Api-Key", ValidToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing anthropic-version: status %d, want 400", resp.StatusCode)
	}
}

// TestMockPageSizeLimit reproduces OpenAI's documented 400 for daily usage
// buckets above 31.
func TestMockPageSizeLimit(t *testing.T) {
	c := loadCorpus(t)
	id := vendorsurface.SurfaceID("openai.platform.usage_completions")
	m, err := New(c, id, Options{ValidToken: ValidToken, PageSizeParam: "limit", MaxPageSize: 31})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	req, _ := http.NewRequest(http.MethodGet, m.URL+"/v1/organization/usage/completions?bucket_width=1d&limit=180",
		nil)
	req.Header.Set("Authorization", "Bearer "+ValidToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || body["error"] == nil {
		t.Fatalf("limit 180: status %d body %v, want the documented 400 error envelope", resp.StatusCode, body)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"0", 0, true},
		{"30", 30 * time.Second, true},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second, true},
		{now.Add(-time.Hour).Format(http.TimeFormat), 0, true},
		{"", 0, false},
		{"soon", 0, false},
		{"-5", 0, false},
	}
	for _, tc := range cases {
		got, ok := ParseRetryAfter(tc.in, now)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseRetryAfter(%q) = %v,%v want %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLinkNext(t *testing.T) {
	cases := map[string]string{
		`<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=9>; rel="last"`: "https://api.github.com/x?page=2",
		`<https://api.github.com/x?page=9>; rel="last"`:                                                "",
		``: "",
	}
	for in, want := range cases {
		if got := linkNext(in); got != want {
			t.Errorf("linkNext(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSchemaExtractsHaveProvenance: every schema extract names its public
// source, its hash and the honesty label.
func TestSchemaExtractsHaveProvenance(t *testing.T) {
	root, err := DefaultSchemaRoot()
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no schema extracts under %s (%v)", root, err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		s, err := LoadSchema(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !strings.HasPrefix(s.SourceURL, "https://") || len(s.SourceSHA256) != 64 ||
			s.Status != vendorsurface.StatusLabel || len(s.Schema) == 0 || s.Name != name {
			t.Errorf("%s: incomplete provenance %+v", name, s)
		}
	}
}

// TestGenerateHonoursRequired: generated instances are JSON, objects carry
// every required member, and the knobs (null, absent, empty, extra) all fire
// across a run.
func TestGenerateHonoursRequired(t *testing.T) {
	s, err := LoadSchema("openai.platform.usage_completions")
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(7))
	sawExtra := false
	for i := 0; i < 200; i++ {
		v := Generate(s.Schema, rng, GenOptions{})
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("instance %d not JSON: %v", i, err)
		}
		if strings.Contains(string(raw), "x_unknown_extra_field") {
			sawExtra = true
		}
		obj, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("instance %d is %T, want object", i, v)
		}
		for _, r := range s.Schema["required"].([]any) {
			if _, ok := obj[r.(string)]; !ok {
				t.Errorf("instance %d lacks required %s", i, r)
			}
		}
	}
	if !sawExtra {
		t.Error("no instance carried an unknown extra field")
	}
}

// TestCheckDeclaredFieldsDetectsLoss: the loss checker is not vacuous.
func TestCheckDeclaredFieldsDetectsLoss(t *testing.T) {
	type narrow struct {
		Big  float32 `json:"big"`
		Name string  `json:"name"`
	}
	losses, err := CheckDeclaredFields([]byte(`{"big":16777217,"name":"a","extra":1}`), &narrow{})
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 1 || !strings.Contains(losses[0], "$.big") {
		t.Fatalf("losses %v, want exactly the float32 precision loss on big", losses)
	}
	if _, err := CheckDeclaredFields([]byte(`{"name":5}`), &narrow{}); err == nil {
		t.Fatal("a wrong JSON type decoded without error")
	}
	clean, err := CheckDeclaredFields([]byte(`{"big":null,"unknown":{"a":1}}`), &narrow{})
	if err != nil || len(clean) != 0 {
		t.Fatalf("null and unknown members: %v %v", clean, err)
	}
}
