package vendormock

import (
	"bufio"
	"bytes"
	"context"
	_ "embed" // declarations.json
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi/fixturecorpus"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// ErrorClass is the class of a driver failure the suite asserts on.
type ErrorClass string

// Error classes.
const (
	ErrNone        ErrorClass = ""
	ErrAuth        ErrorClass = "auth"
	ErrSchemaDrift ErrorClass = "schema_drift"
	ErrRateLimited ErrorClass = "rate_limited"
	ErrTimeout     ErrorClass = "timeout"
	ErrOther       ErrorClass = "other"
)

// ClassedError is implemented by a driver error that names its class. A real
// driver returns errors implementing it; an error that does not is ErrOther.
type ClassedError interface {
	error
	VendorErrorClass() ErrorClass
}

type classedError struct {
	class ErrorClass
	err   error
}

func (e *classedError) Error() string                { return string(e.class) + ": " + e.err.Error() }
func (e *classedError) Unwrap() error                { return e.err }
func (e *classedError) VendorErrorClass() ErrorClass { return e.class }

func classed(c ErrorClass, err error) error { return &classedError{class: c, err: err} }

// ClassOf returns the class of a driver error (ErrNone for nil).
func ClassOf(err error) ErrorClass {
	if err == nil {
		return ErrNone
	}
	var ce ClassedError
	if errors.As(err, &ce) {
		return ce.VendorErrorClass()
	}
	return ErrOther
}

// Conn is what a driver gets for one run against a mock: the base URL, the
// credential, an HTTP client, a page cap, and an injectable clock so the
// suite can assert Retry-After handling without sleeping.
type Conn struct {
	BaseURL string
	// TokenURL is the mock identity platform of a token-step surface ("" for
	// every other surface): the only host the client secret may reach.
	TokenURL string
	// ReportURL is the mock's second host for a two-step report surface
	// ("" otherwise): the only host a report download may reach.
	ReportURL string
	Token     string
	HTTP      *http.Client
	MaxPages  int
	Sleep     func(time.Duration)
	Now       func() time.Time
}

func (c Conn) sleep(d time.Duration) {
	if c.Sleep != nil {
		c.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (c Conn) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Result is what a driver reports for one run.
type Result struct {
	Records   int
	Pages     int
	Truncated bool
}

// Driver is the seam the conformance suite drives. A real driver adapts its
// own entry point to it (lane F8 onward).
type Driver interface {
	Fetch(ctx context.Context, c Conn) (Result, error)
}

// ScenarioID names one conformance scenario (plan 12A.2).
type ScenarioID string

// Scenarios.
const (
	SFirstPage         ScenarioID = "first_page"
	SPagination        ScenarioID = "pagination"
	SPageCap           ScenarioID = "page_cap"
	SAuthWrongScheme   ScenarioID = "auth_wrong_scheme"
	SAuthWrongPlane    ScenarioID = "auth_wrong_plane"
	SRateLimitSeconds  ScenarioID = "rate_limit_seconds"
	SRateLimitHTTPDate ScenarioID = "rate_limit_http_date"
	SSchemaDrift       ScenarioID = "schema_drift"
	SFlagTypeDrift     ScenarioID = "flag_type_drift"
	SLoopCursor        ScenarioID = "loop_cursor"
	SForeignLink       ScenarioID = "foreign_link"
	STimeout           ScenarioID = "timeout"
	SSecondHopNoAuth   ScenarioID = "second_hop_no_auth"
)

// applicabilityRule is one row of the applicability table: when Match holds
// for a surface's paging family, Scenarios apply.
type applicabilityRule struct {
	Name      string
	Match     func(PagingFamily) bool
	Scenarios []ScenarioID
}

func paged(f PagingFamily) bool {
	return f == FamilyNextPage || f == FamilyAfterID || f == FamilyLink || f == FamilyBodyPage || f == FamilyODataNext ||
		f == FamilyEndCursor || f == FamilyNestedCursor || f == FamilyNextToken || f == FamilyOffset
}

// applicability is walked top to bottom; every matching row adds its
// scenarios (plan 12A.2 mechanics, 12A.5 field policy, 12A.6 mutations).
var applicability = []applicabilityRule{
	{
		Name: "every surface", Match: func(PagingFamily) bool { return true },
		Scenarios: []ScenarioID{
			SFirstPage, SAuthWrongScheme, SAuthWrongPlane, SRateLimitSeconds, SRateLimitHTTPDate,
			SSchemaDrift, STimeout,
		},
	},
	{Name: "paged surfaces", Match: paged, Scenarios: []ScenarioID{SPagination, SPageCap, SLoopCursor}},
	{Name: "in-body paging flag", Match: func(f PagingFamily) bool {
		return f == FamilyNextPage || f == FamilyAfterID || f == FamilyBodyPage || f == FamilyODataNext ||
			f == FamilyEndCursor || f == FamilyNestedCursor
	}, Scenarios: []ScenarioID{SFlagTypeDrift}},
	{
		Name: "next link to an absolute URL", Match: func(f PagingFamily) bool { return f == FamilyLink || f == FamilyODataNext },
		Scenarios: []ScenarioID{SForeignLink},
	},
	{
		Name: "two-step report", Match: func(f PagingFamily) bool { return f == FamilyReport },
		Scenarios: []ScenarioID{SSecondHopNoAuth},
	},
}

// Applicable returns the scenarios that apply to a surface, sorted.
func Applicable(sf *SurfaceFixtures) []ScenarioID {
	fam := Classify(sf)
	set := map[ScenarioID]bool{}
	for _, r := range applicability {
		if r.Match(fam) {
			for _, s := range r.Scenarios {
				set[s] = true
			}
		}
	}
	out := make([]ScenarioID, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

//go:embed declarations.json
var declarationsJSON []byte

var (
	declOnce sync.Once
	decl     map[vendorsurface.SurfaceID][]ScenarioID
	declErr  error
)

// Declarations returns the committed per-surface scenario declarations
// (declarations.json). TestConformanceScenariosDeclaredPerSurface fails when
// a corpus surface has no declaration or its declaration differs from what
// Applicable derives, so a fixture or paging change that makes a new scenario
// applicable cannot land undeclared.
func Declarations() (map[vendorsurface.SurfaceID][]ScenarioID, error) {
	declOnce.Do(func() {
		decl = map[vendorsurface.SurfaceID][]ScenarioID{}
		declErr = json.Unmarshal(declarationsJSON, &decl)
	})
	return decl, declErr
}

// Synthetic credentials the suite hands to mocks and drivers. They are not
// credential-shaped and never leave the test process.
const (
	ValidToken      = "synthetic-valid-token"
	WrongPlaneToken = "synthetic-wrong-plane-token"
)

// Expected record counts derived from the corpus.
type expectation struct {
	first, all int
}

func countFixture(f Fixture) int {
	if f.NDJSON {
		n := 0
		sc := bufio.NewScanner(bytes.NewReader(f.Body))
		sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
		for sc.Scan() {
			if len(bytes.TrimSpace(sc.Bytes())) > 0 {
				n++
			}
		}
		return n
	}
	doc, err := decodeJSON(f.Body)
	if err != nil {
		return 0
	}
	key, ok := RecordPath(doc)
	switch {
	case !ok:
		return 1
	case key == "":
		return len(doc.([]any))
	default:
		return len(doc.(map[string]any)[key].([]any))
	}
}

func expect(m *Mock, sf *SurfaceFixtures) expectation {
	fam := m.Family()
	if fam == FamilyReport {
		rf, ok := sf.Fixtures[fixturecorpus.ScenarioReportFile]
		links := 0
		if doc, err := decodeJSON(m.FirstPage().Body); err == nil {
			if obj, ok := doc.(map[string]any); ok {
				l, _ := obj["download_links"].([]any)
				links = len(l)
			}
		}
		if !ok {
			return expectation{}
		}
		n := countFixture(rf) * links
		return expectation{first: n, all: n}
	}
	first := countFixture(m.FirstPage())
	if !paged(fam) {
		return expectation{first: first, all: first}
	}
	last := first
	if f, ok := sf.Terminal(); ok {
		last = countFixture(f)
	}
	return expectation{first: first, all: first + last}
}

type sleeps struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *sleeps) sleep(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d = append(s.d, d)
}

func (s *sleeps) all() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.d...)
}

var fixedNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// Run drives a driver through every scenario declared for a surface, each
// against a fresh mock and a fresh driver.
func Run(t *testing.T, c *Corpus, id vendorsurface.SurfaceID, newDriver func() Driver) {
	t.Helper()
	sf, ok := c.Surfaces[id]
	if !ok {
		t.Fatalf("%s has no fixtures", id)
	}
	d, err := Declarations()
	if err != nil {
		t.Fatalf("declarations.json: %v", err)
	}
	scen, ok := d[id]
	if !ok || len(scen) == 0 {
		t.Fatalf("%s declares no conformance scenarios", id)
	}
	for _, s := range scen {
		s := s
		t.Run(string(s), func(t *testing.T) { runScenario(t, c, sf, s, newDriver) })
	}
}

func apiRequests(m *Mock) int {
	n := 0
	for _, r := range m.Requests() {
		if !r.SecondHost {
			n++
		}
	}
	return n
}

// scenarioSetup is the per-scenario mock configuration and connection tweak,
// one row per scenario that needs one.
var scenarioSetup = map[ScenarioID]func(*Options, *Conn){
	SRateLimitSeconds: func(o *Options, _ *Conn) { o.RateLimitFirst, o.RetryAfter = 1, "7" },
	SRateLimitHTTPDate: func(o *Options, _ *Conn) {
		o.RateLimitFirst, o.RetryAfter = 1, fixedNow.Add(90*time.Second).Format(http.TimeFormat)
	},
	SSchemaDrift:   func(o *Options, _ *Conn) { o.Drift = DriftContainer },
	SFlagTypeDrift: func(o *Options, _ *Conn) { o.Drift = DriftFlagType },
	SLoopCursor:    func(o *Options, _ *Conn) { o.Drift = DriftLoopCursor },
	SForeignLink:   func(o *Options, _ *Conn) { o.Drift = DriftForeignLink },
	STimeout: func(o *Options, c *Conn) {
		o.Delay = 3 * time.Second
		c.HTTP = &http.Client{Timeout: 80 * time.Millisecond}
	},
	SAuthWrongPlane: func(_ *Options, c *Conn) { c.Token = WrongPlaneToken },
}

// outcome is what one scenario run produced, for its assertion row.
type outcome struct {
	t   *testing.T
	s   ScenarioID
	m   *Mock
	sf  *SurfaceFixtures
	res Result
	err error
	cls ErrorClass
	exp expectation
	sl  *sleeps
}

func (o *outcome) mustSucceed() {
	if o.cls != ErrNone {
		o.t.Fatalf("unexpected error: %v", o.err)
	}
}

func (o *outcome) wantClass(c ErrorClass) {
	if o.cls != c {
		o.t.Errorf("%s: class %q (%v), want %q", o.s, o.cls, o.err, c)
	}
}

func assertFirstPage(o *outcome) {
	o.mustSucceed()
	if o.res.Records != o.exp.first {
		o.t.Errorf("records = %d, want %d from the corpus first page", o.res.Records, o.exp.first)
	}
}

func assertRetryAfter(want time.Duration) func(*outcome) {
	return func(o *outcome) {
		assertFirstPage(o)
		if got := o.sl.all(); len(got) != 1 || got[0] != want {
			o.t.Errorf("Retry-After honoured as %v, want exactly one wait of %v", got, want)
		}
	}
}

// scenarioAssert is the assertion row of every scenario.
var scenarioAssert = map[ScenarioID]func(*outcome){
	SFirstPage:         assertFirstPage,
	SRateLimitSeconds:  assertRetryAfter(7 * time.Second),
	SRateLimitHTTPDate: assertRetryAfter(90 * time.Second),
	SPagination: func(o *outcome) {
		o.mustSucceed()
		if o.res.Records != o.exp.all || o.res.Pages != 2 || o.res.Truncated {
			o.t.Errorf("got %+v, want %d records over 2 pages to the documented terminal page", o.res, o.exp.all)
		}
	},
	SPageCap: func(o *outcome) {
		if o.cls != ErrNone || !o.res.Truncated || apiRequests(o.m) != 1 {
			o.t.Errorf("page cap 1: got %+v err=%v requests=%d, want truncated after one request", o.res, o.err,
				apiRequests(o.m))
		}
	},
	SAuthWrongPlane: func(o *outcome) {
		o.wantClass(ErrAuth)
		if n := apiRequests(o.m); n != 1 {
			o.t.Errorf("wrong-plane key: %d requests, want exactly 1 (no retry with another scheme)", n)
		}
	},
	SAuthWrongScheme: func(o *outcome) {
		checkWrongScheme(o.t, o.m, o.sf)
		if o.cls != ErrNone {
			o.t.Errorf("valid credential in the documented scheme failed: %v", o.err)
		}
	},
	SSchemaDrift: func(o *outcome) {
		o.wantClass(ErrSchemaDrift)
		if o.res.Records != 0 {
			o.t.Errorf("drifted page landed %d records, want 0", o.res.Records)
		}
	},
	SFlagTypeDrift: func(o *outcome) { o.wantClass(ErrSchemaDrift) },
	SLoopCursor: func(o *outcome) {
		o.wantClass(ErrSchemaDrift)
		if n := apiRequests(o.m); n > 3 {
			o.t.Errorf("paging loop not stopped: %d requests", n)
		}
	},
	SForeignLink: func(o *outcome) {
		if o.cls == ErrNone {
			o.t.Errorf("Link rel=next to another host was followed")
		}
	},
	STimeout: func(o *outcome) { o.wantClass(ErrTimeout) },
	SSecondHopNoAuth: func(o *outcome) {
		o.mustSucceed()
		if o.res.Records != o.exp.all || o.exp.all == 0 {
			o.t.Errorf("report records = %d, want %d (report_file lines x links)", o.res.Records, o.exp.all)
		}
		if n := o.m.SecondHopWithAuth(); n != 0 {
			o.t.Errorf("%d report downloads carried an Authorization header", n)
		}
	},
}

// capOnePage lists the scenarios that stop a paged surface after page one.
var capOnePage = map[ScenarioID]bool{SFirstPage: true, SPageCap: true, SRateLimitSeconds: true, SRateLimitHTTPDate: true}

func runScenario(t *testing.T, c *Corpus, sf *SurfaceFixtures, s ScenarioID, newDriver func() Driver) {
	check, ok := scenarioAssert[s]
	if !ok {
		t.Fatalf("scenario %q has no assertion", s)
	}
	opts := Options{ValidToken: ValidToken, WrongPlaneToken: WrongPlaneToken}
	sl := &sleeps{}
	conn := Conn{
		Token: ValidToken, HTTP: &http.Client{Timeout: 5 * time.Second}, Sleep: sl.sleep,
		Now: func() time.Time { return fixedNow },
	}
	if setup, ok := scenarioSetup[s]; ok {
		setup(&opts, &conn)
	}
	m, err := New(c, sf.ID, opts)
	if err != nil {
		t.Fatalf("mock: %v", err)
	}
	defer m.Close()
	conn.BaseURL, conn.ReportURL, conn.TokenURL = m.URL, m.ReportURL, m.TokenURL
	if paged(m.Family()) && capOnePage[s] {
		conn.MaxPages = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := newDriver().Fetch(ctx, conn)
	check(&outcome{t: t, s: s, m: m, sf: sf, res: res, err: err, cls: ClassOf(err), exp: expect(m, sf), sl: sl})
}

// checkWrongScheme sends the valid token in every other scheme and expects
// the vendor's documented auth error, byte for byte.
func checkWrongScheme(t *testing.T, m *Mock, sf *SurfaceFixtures) {
	t.Helper()
	authErr, _ := sf.AuthError()
	route := m.FirstPage().Sidecar.HTTP
	path := strings.NewReplacer("{", "x", "}", "x").Replace(route.Path)
	for _, scheme := range []AuthScheme{SchemeRawHeader, SchemeBearer, SchemeBasicUser} {
		if scheme == m.Profile().Scheme {
			continue
		}
		req, err := http.NewRequest(route.Method, m.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range m.Profile().Required {
			req.Header.Set(k, v)
		}
		switch scheme {
		case SchemeRawHeader:
			req.Header.Set("X-Api-Key", ValidToken)
		case SchemeBearer:
			req.Header.Set("Authorization", "Bearer "+ValidToken)
		case SchemeBasicUser:
			req.SetBasicAuth(ValidToken, "")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != authErr.Sidecar.HTTP.Status || !bytes.Equal(buf.Bytes(), authErr.Body) {
			t.Errorf("scheme %s: status %d, want the documented %d auth error body", scheme, resp.StatusCode,
				authErr.Sidecar.HTTP.Status)
		}
	}
}

// DeclarationsFor renders the declarations a corpus implies, for regenerating
// declarations.json after a reviewed corpus change.
func DeclarationsFor(c *Corpus) ([]byte, error) {
	out := map[vendorsurface.SurfaceID][]ScenarioID{}
	for _, id := range c.IDs() {
		out[id] = Applicable(c.Surfaces[id])
	}
	b, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		return nil, fmt.Errorf("vendormock.DeclarationsFor: %w", err)
	}
	return append(b, '\n'), nil
}
