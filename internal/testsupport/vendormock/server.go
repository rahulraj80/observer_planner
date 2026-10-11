package vendormock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi/fixturecorpus"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// Drift is a schema-drift or mutation the mock applies to the page it serves
// (plan 12A.5 field-policy matrix, 12A.6 mutation tests).
type Drift string

// Drift kinds.
const (
	DriftNone Drift = ""
	// DriftContainer renames the record container (or changes the root JSON
	// type of a single document): a declared required field disappears.
	DriftContainer Drift = "container_renamed"
	// DriftFlagType serves the paging flag with the wrong JSON type
	// (has_more as a string, hasNextPage as a string).
	DriftFlagType Drift = "flag_type"
	// DriftLoopCursor never serves the terminal page: page two repeats page
	// one with the same cursor.
	DriftLoopCursor Drift = "loop_cursor"
	// DriftForeignLink points Link rel="next" at another host.
	DriftForeignLink Drift = "foreign_link"
)

// Options configures one mock. Tokens are synthetic strings chosen by the
// test; the mock never sees a real credential.
type Options struct {
	ValidToken      string
	WrongPlaneToken string
	// RateLimitFirst answers the first N API requests with 429.
	RateLimitFirst int
	// RetryAfter is the Retry-After value of those 429s: delay-seconds
	// ("2") or an HTTP-date.
	RetryAfter string
	// PageSizeParam and MaxPageSize reproduce a documented page-size limit:
	// a larger value is answered 400 with the vendor's error body.
	PageSizeParam string
	MaxPageSize   int
	// Delay holds every API response (slow responses and timeouts).
	Delay time.Duration
	Drift Drift
}

// Recorded is one request the mock saw (no header values are kept).
type Recorded struct {
	SecondHost bool
	Method     string
	Path       string
	RawQuery   string
	HasAuth    bool
	Status     int
}

// Mock is a vendor-shaped httptest server for one surface.
type Mock struct {
	URL       string
	ReportURL string
	// TokenURL is the mock identity platform of a TokenScope profile ("" for
	// every other surface): POST /{tenant}/oauth2/v2.0/token.
	TokenURL string

	corpus  *Corpus
	sf      *SurfaceFixtures
	profile Profile
	family  PagingFamily
	opts    Options
	first   Fixture
	srv     *httptest.Server
	report  *httptest.Server
	token   *httptest.Server

	mu       sync.Mutex
	reqs     []Recorded
	limited  int
	nextTok  string
	afterTok string
	endTok   string
	nestTok  string
	pageTok  string
	offTok   string
	offFirst string // the offset family's first index ("0", or "1" for SCIM)
	skipTok  string
	tokReqs  []Recorded
}

// New starts a mock for one surface of the corpus.
func New(c *Corpus, id vendorsurface.SurfaceID, opts Options) (*Mock, error) {
	sf, ok := c.Surfaces[id]
	if !ok {
		return nil, fmt.Errorf("vendormock.New: %s has no fixtures", id)
	}
	p, ok := ProfileFor(id)
	if !ok {
		return nil, fmt.Errorf("vendormock.New: %s has no vendor profile", id)
	}
	first, ok := sf.Success()
	if !ok {
		return nil, fmt.Errorf("vendormock.New: %s has no success fixture", id)
	}
	if _, ok := sf.AuthError(); !ok {
		return nil, fmt.Errorf("vendormock.New: %s has no documented auth error body", id)
	}
	m := &Mock{corpus: c, sf: sf, profile: p, family: Classify(sf), opts: opts, first: first}
	if doc, err := decodeJSON(first.Body); err == nil {
		if obj, ok := doc.(map[string]any); ok {
			m.nextTok = scalarString(obj["next_page"])
			m.afterTok = scalarString(obj["last_id"])
			m.endTok = scalarString(obj["end_cursor"])
			if pg, s, ok := nestedCursor(obj); ok {
				m.nestTok = scalarString(pg[s.Cursor])
			}
			if k, ok := nextTokenKey(obj); ok {
				m.pageTok = scalarString(obj[k])
			}
			// Offset family (lane ENTAPI-L16C): page two starts at the number
			// of records page one served.
			if s, ok := offsetSpelled(obj); ok {
				recs, _ := obj[s.Records].([]any)
				m.offTok = strconv.Itoa(len(recs) + s.Base)
				m.offFirst = strconv.Itoa(s.Base)
			}
		}
	}
	m.skipTok = odataSkipToken(first.Body)
	m.srv = httptest.NewServer(http.HandlerFunc(m.serveAPI))
	m.URL = m.srv.URL
	if m.family == FamilyReport {
		m.report = httptest.NewServer(http.HandlerFunc(m.serveReport))
		m.ReportURL = m.report.URL
	}
	if p.TokenScope != "" {
		m.token = httptest.NewServer(http.HandlerFunc(m.serveToken))
		m.TokenURL = m.token.URL
	}
	return m, nil
}

// Close stops the servers.
func (m *Mock) Close() {
	m.srv.Close()
	if m.report != nil {
		m.report.Close()
	}
	if m.token != nil {
		m.token.Close()
	}
}

// Family is the paging family the mock reproduces.
func (m *Mock) Family() PagingFamily { return m.family }

// Profile is the vendor mechanics row the mock enforces.
func (m *Mock) Profile() Profile { return m.profile }

// FirstPage is the fixture served as page one.
func (m *Mock) FirstPage() Fixture { return m.first }

// Requests returns a copy of what the mock saw.
func (m *Mock) Requests() []Recorded {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Recorded(nil), m.reqs...)
}

// SecondHopWithAuth counts report downloads that carried an Authorization
// header (the API credential must never reach the signed-link host).
func (m *Mock) SecondHopWithAuth() int {
	n := 0
	for _, r := range m.Requests() {
		if r.SecondHost && r.HasAuth {
			n++
		}
	}
	return n
}

func (m *Mock) record(r *http.Request, second bool, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs = append(m.reqs, Recorded{
		SecondHost: second, Method: r.Method, Path: r.URL.Path,
		RawQuery: r.URL.RawQuery, HasAuth: r.Header.Get("Authorization") != "", Status: status,
	})
}

func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return ""
}

// matchTemplate matches a request path against a fixture path template whose
// {name} segments match exactly one non-empty segment.
func matchTemplate(tpl, path string) bool {
	ts := strings.Split(strings.Trim(tpl, "/"), "/")
	ps := strings.Split(strings.Trim(path, "/"), "/")
	if len(ts) != len(ps) {
		return false
	}
	for i := range ts {
		if strings.HasPrefix(ts[i], "{") && strings.HasSuffix(ts[i], "}") {
			if ps[i] == "" {
				return false
			}
			continue
		}
		if ts[i] != ps[i] {
			return false
		}
	}
	return true
}

func (m *Mock) write(w http.ResponseWriter, r *http.Request, second bool, status int, hdr map[string]string,
	body []byte,
) {
	for k, v := range hdr {
		w.Header().Set(k, v)
	}
	if len(body) > 0 && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
	m.record(r, second, status)
}

func (m *Mock) writeFixture(w http.ResponseWriter, r *http.Request, f Fixture, status int, extra map[string]string) {
	hdr := map[string]string{}
	for k, v := range f.Sidecar.HTTP.Headers {
		if !strings.EqualFold(k, "link") {
			hdr[http.CanonicalHeaderKey(k)] = v
		}
	}
	for k, v := range extra {
		hdr[http.CanonicalHeaderKey(k)] = v
	}
	m.write(w, r, false, status, hdr, f.Body)
}

// vendorError answers with a documented vendor error body. A vendor documents
// one error envelope; when the surface's own scenario fixture is absent the
// same vendor's fixture of that scenario is used, then the surface's auth
// error envelope with the requested status.
func (m *Mock) vendorError(w http.ResponseWriter, r *http.Request, sc fixturecorpus.Scenario, status int,
	extra map[string]string,
) {
	if f, ok := m.corpus.VendorFixture(m.sf.ID, sc); ok {
		m.writeFixture(w, r, f, status, extra)
		return
	}
	f, _ := m.sf.AuthError()
	m.writeFixture(w, r, f, status, extra)
}

func (m *Mock) authorized(r *http.Request, body []byte) (ok bool) {
	tok := ""
	switch m.profile.Scheme {
	case SchemeAWSSigV4:
		return m.sigV4OK(r, body)
	case SchemeRawHeader:
		tok = r.Header.Get(m.profile.AuthHeader)
	case SchemeBearer:
		v := r.Header.Get(m.profile.AuthHeader)
		if !strings.HasPrefix(v, "Bearer ") {
			return false
		}
		tok = strings.TrimPrefix(v, "Bearer ")
	case SchemeBasicUser:
		u, pw, has := r.BasicAuth()
		if !has || pw != "" {
			return false
		}
		tok = u
	case SchemePrefixed:
		v := r.Header.Get(m.profile.AuthHeader)
		if m.profile.AuthPrefix == "" || !strings.HasPrefix(v, m.profile.AuthPrefix+" ") {
			return false
		}
		tok = strings.TrimPrefix(v, m.profile.AuthPrefix+" ")
	}
	if m.profile.TokenScope != "" {
		// Only an access token the mock identity platform minted for the
		// valid client secret opens the API.
		return tok != "" && tok == AccessTokenFor(m.opts.ValidToken)
	}
	return tok != "" && tok == m.opts.ValidToken
}

func (m *Mock) serveAPI(w http.ResponseWriter, r *http.Request) {
	// Read the request body first: the server only notices a client that
	// gave up (a timeout) once the body has been consumed.
	var reqBody []byte
	if r.Body != nil {
		reqBody, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
	}
	if m.opts.Delay > 0 {
		select {
		case <-time.After(m.opts.Delay):
		case <-r.Context().Done():
			m.record(r, false, 0)
			return
		}
	}
	route := m.first.Sidecar.HTTP
	if r.Method != route.Method || !matchTemplate(route.Path, r.URL.Path) {
		m.write(w, r, false, http.StatusNotFound, nil, []byte(`{"mock":"no such route for this surface"}`))
		return
	}
	authErr, _ := m.sf.AuthError()
	if !m.authorized(r, reqBody) {
		m.writeFixture(w, r, authErr, authErr.Sidecar.HTTP.Status, nil)
		return
	}
	for h := range m.profile.Required {
		if r.Header.Get(h) == "" {
			m.vendorError(w, r, fixturecorpus.ScenarioError400, http.StatusBadRequest, nil)
			return
		}
	}
	m.mu.Lock()
	limit := m.limited < m.opts.RateLimitFirst
	if limit {
		m.limited++
	}
	m.mu.Unlock()
	if limit {
		hdr := map[string]string{"Retry-After": m.opts.RetryAfter}
		if m.sf.Vendor == "github" {
			hdr["x-ratelimit-remaining"] = "0"
		}
		m.vendorError(w, r, fixturecorpus.ScenarioError429, http.StatusTooManyRequests, hdr)
		return
	}
	if m.opts.MaxPageSize > 0 && m.opts.PageSizeParam != "" {
		if n, err := strconv.Atoi(r.URL.Query().Get(m.opts.PageSizeParam)); err == nil && n > m.opts.MaxPageSize {
			m.vendorError(w, r, fixturecorpus.ScenarioError400, http.StatusBadRequest, nil)
			return
		}
	}
	page, ok := m.pageOf(r, reqBody)
	if !ok {
		m.vendorError(w, r, fixturecorpus.ScenarioError400, http.StatusBadRequest, nil)
		return
	}
	body := m.first.Body
	if page > 1 && m.opts.Drift != DriftLoopCursor {
		body = m.terminalBody()
	}
	if m.family == FamilyReport {
		body = m.rewriteLinks(body)
	}
	if m.family == FamilyODataNext {
		body = m.rewriteNextLink(body, r.URL.Path)
	}
	body = applyDrift(body, m.opts.Drift)
	hdr := map[string]string{}
	for k, v := range m.first.Sidecar.HTTP.Headers {
		if !strings.EqualFold(k, "link") {
			hdr[http.CanonicalHeaderKey(k)] = v
		}
	}
	if m.family == FamilyLink && (page == 1 || m.opts.Drift == DriftLoopCursor) {
		next := m.URL + r.URL.Path + "?page=2"
		if m.opts.Drift == DriftForeignLink {
			next = "http://203.0.113.9" + r.URL.Path + "?page=2"
		}
		hdr["Link"] = "<" + next + `>; rel="next"`
	}
	m.write(w, r, false, m.first.Sidecar.HTTP.Status, hdr, body)
}

// tokenPage maps an opaque cursor to a page: none is page one, the token the
// first page handed out is page two, anything else is invalid.
func tokenPage(got, issued string) (int, bool) {
	switch got {
	case "":
		return 1, true
	case issued:
		return 2, true
	default:
		return 0, false
	}
}

// pageOf reads the page a request asks for; false means an invalid cursor.
func (m *Mock) pageOf(r *http.Request, reqBody []byte) (int, bool) {
	q := r.URL.Query()
	switch m.family {
	case FamilyNextPage:
		return tokenPage(q.Get("page"), m.nextTok)
	case FamilyAfterID:
		return tokenPage(q.Get(m.profile.AfterParam), m.afterTok)
	case FamilyEndCursor:
		return tokenPage(q.Get(m.profile.AfterParam), m.endTok)
	case FamilyNestedCursor:
		return tokenPage(q.Get(m.profile.AfterParam), m.nestTok)
	case FamilyNextToken:
		if r.Method == http.MethodPost {
			// AWS JSON and the Logging API carry the token in the JSON body
			// under the profile's AfterParam (lane ENTAPI-L16D).
			var b map[string]any
			if len(bytes.TrimSpace(reqBody)) > 0 && json.Unmarshal(reqBody, &b) != nil {
				return 0, false
			}
			return tokenPage(scalarString(b[m.profile.AfterParam]), m.pageTok)
		}
		return tokenPage(q.Get(m.profile.AfterParam), m.pageTok)
	case FamilyOffset:
		if q.Get(m.profile.AfterParam) == m.offFirst {
			return 1, true
		}
		return tokenPage(q.Get(m.profile.AfterParam), m.offTok)
	case FamilyLink:
		switch q.Get("page") {
		case "", "1":
			return 1, true
		case "2":
			return 2, true
		default:
			return 0, false
		}
	case FamilyODataNext:
		switch tok := q.Get("$skiptoken"); tok {
		case "":
			return 1, true
		case m.skipTok:
			return 2, true
		default:
			return 0, false
		}
	case FamilyBodyPage:
		// A POST carries the page number in its JSON body; a GET in the
		// page query parameter (both documented by Cursor).
		n := 1
		if r.Method == http.MethodGet {
			if v := q.Get("page"); v != "" {
				p, err := strconv.Atoi(v)
				if err != nil {
					return 0, false
				}
				n = p
			}
		} else if len(bytes.TrimSpace(reqBody)) > 0 {
			var b struct {
				Page json.Number `json:"page"`
			}
			if err := json.Unmarshal(reqBody, &b); err != nil {
				return 0, false
			}
			if v, err := b.Page.Int64(); err == nil {
				n = int(v)
			}
		}
		if n < 1 {
			return 0, false
		}
		if n > 1 {
			return 2, true
		}
		return 1, true
	}
	return 1, true
}

// terminalBody is the documented terminal page: the corpus last_page or
// empty_page, else the first page with its paging flag set terminal.
func (m *Mock) terminalBody() []byte {
	if f, ok := m.sf.Terminal(); ok {
		return f.Body
	}
	doc, err := decodeJSON(m.first.Body)
	if err != nil {
		return m.first.Body
	}
	if obj, ok := doc.(map[string]any); ok {
		if _, ok := obj["has_more"]; ok {
			obj["has_more"] = false
			if _, ok := obj["next_page"]; ok {
				obj["next_page"] = nil
			}
		}
		if pg, ok := obj["pagination"].(map[string]any); ok {
			if _, ok := pg["hasNextPage"]; ok {
				pg["hasNextPage"] = false
			}
		}
		if _, ok := obj["has_next_page"]; ok {
			obj["has_next_page"] = false
			obj["end_cursor"] = nil
		}
		if pg, s, ok := nestedCursor(obj); ok {
			pg[s.Flag] = false
			pg[s.Cursor] = nil
		}
		for _, k := range nextTokenKeys {
			delete(obj, k)
		}
		b, err := json.Marshal(obj)
		if err == nil {
			return b
		}
	}
	return m.first.Body
}

// rewriteLinks points a report's download_links at the mock's second host.
func (m *Mock) rewriteLinks(body []byte) []byte {
	doc, err := decodeJSON(body)
	if err != nil {
		return body
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return body
	}
	links, _ := obj["download_links"].([]any)
	out := make([]any, 0, len(links))
	for i := range links {
		out = append(out, fmt.Sprintf("%s/copilot-usage-report-%d.ndjson?sig=SYNTH", m.ReportURL, i+1))
	}
	obj["download_links"] = out
	b, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return b
}

func (m *Mock) serveReport(w http.ResponseWriter, r *http.Request) {
	f, ok := m.sf.Fixtures[fixturecorpus.ScenarioReportFile]
	if !ok || !strings.HasPrefix(r.URL.Path, "/copilot-usage-report-") {
		w.WriteHeader(http.StatusNotFound)
		m.record(r, true, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(f.Body)
	m.record(r, true, http.StatusOK)
}

// applyDrift mutates a page body.
func applyDrift(body []byte, d Drift) []byte {
	if d != DriftContainer && d != DriftFlagType {
		return body
	}
	doc, err := decodeJSON(body)
	if err != nil {
		return body
	}
	if d == DriftFlagType {
		if obj, ok := doc.(map[string]any); ok {
			if _, ok := obj["@odata.nextLink"]; ok {
				obj["@odata.nextLink"] = 1
				return mustJSON(obj, body)
			}
			if v, ok := obj["has_more"]; ok {
				obj["has_more"] = fmt.Sprint(v)
				return mustJSON(obj, body)
			}
			if pg, ok := obj["pagination"].(map[string]any); ok {
				if v, ok := pg["hasNextPage"]; ok {
					pg["hasNextPage"] = fmt.Sprint(v)
					return mustJSON(obj, body)
				}
			}
			if v, ok := obj["has_next_page"]; ok {
				obj["has_next_page"] = fmt.Sprint(v)
				return mustJSON(obj, body)
			}
			if pg, s, ok := nestedCursor(obj); ok {
				pg[s.Flag] = fmt.Sprint(pg[s.Flag])
				return mustJSON(obj, body)
			}
		}
	}
	switch v := doc.(type) {
	case []any:
		return mustJSON(map[string]any{"items_renamed": v}, body)
	case map[string]any:
		if k, ok := RecordPath(v); ok {
			v[k+"_renamed"] = v[k]
			delete(v, k)
			return mustJSON(v, body)
		}
		if links, ok := v["download_links"]; ok {
			v["links_renamed"] = links
			delete(v, "download_links")
			return mustJSON(v, body)
		}
		return mustJSON([]any{v}, body)
	}
	return body
}

func mustJSON(v any, fallback []byte) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return fallback
	}
	return b
}

// AccessTokenFor is the access token the mock identity platform mints for a
// client secret (synthetic, never credential-shaped).
func AccessTokenFor(secret string) string { return "synthetic-access-" + secret }

// odataSkipToken reads the $skiptoken of a page's @odata.nextLink ("" when
// the page has none).
func odataSkipToken(body []byte) string {
	doc, err := decodeJSON(body)
	if err != nil {
		return ""
	}
	obj, _ := doc.(map[string]any)
	link, _ := obj["@odata.nextLink"].(string)
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return u.Query().Get("$skiptoken")
}

// rewriteNextLink points a page's @odata.nextLink at the mock (or, under
// DriftForeignLink, at another host), keeping the documented form: the same
// function path, $format and the $skiptoken.
func (m *Mock) rewriteNextLink(body []byte, path string) []byte {
	doc, err := decodeJSON(body)
	if err != nil {
		return body
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return body
	}
	if _, ok := obj["@odata.nextLink"].(string); !ok {
		return body
	}
	host := m.URL
	if m.opts.Drift == DriftForeignLink {
		host = "http://203.0.113.9"
	}
	obj["@odata.nextLink"] = host + path + "?$format=application/json&$skiptoken=" + url.QueryEscape(m.skipTok)
	return mustJSON(obj, body)
}

// TokenRequests lists the requests the mock identity platform saw.
func (m *Mock) TokenRequests() []Recorded {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Recorded(nil), m.tokReqs...)
}

// serveToken is the mock identity platform's token endpoint
// (POST /{tenant}/oauth2/v2.0/token, form body, documented at
// https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-client-creds-grant-flow):
// a well-formed client-credentials request for the profile's scope gets the
// documented success shape with a minted token; anything else gets the
// documented 400 error shape. Both the valid and the wrong-plane secret are
// minted a token (a wrong-plane app authenticates, then the API refuses it).
func (m *Mock) serveToken(w http.ResponseWriter, r *http.Request) {
	if m.profile.TokenGrant == GrantJWTBearer {
		m.serveJWTToken(w, r)
		return
	}
	rec := Recorded{Method: r.Method, Path: r.URL.Path, HasAuth: r.Header.Get("Authorization") != ""}
	segs := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	_ = r.ParseForm()
	secret := r.PostForm.Get("client_secret")
	ok := r.Method == http.MethodPost && len(segs) == 4 && segs[0] != "" && segs[1] == "oauth2" && segs[2] == "v2.0" &&
		segs[3] == "token" && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") &&
		r.PostForm.Get("grant_type") == "client_credentials" && r.PostForm.Get("scope") == m.profile.TokenScope &&
		r.PostForm.Get("client_id") != "" && (secret == m.opts.ValidToken || secret == m.opts.WrongPlaneToken) && !rec.HasAuth
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"synthetic","error_codes":[7000215]}`))
		rec.Status = http.StatusBadRequest
	} else {
		_, _ = w.Write([]byte(`{"token_type":"Bearer","expires_in":3599,"access_token":"` + AccessTokenFor(secret) + `"}`)) //nolint:gosec // G705: test-only mock answering JSON
		rec.Status = http.StatusOK
	}
	m.mu.Lock()
	m.tokReqs = append(m.tokReqs, rec)
	m.mu.Unlock()
}
