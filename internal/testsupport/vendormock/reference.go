package vendormock

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi/cloudsign"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// ReferenceDriver is a generic, corpus-driven driver used only to prove the
// mocks and the conformance suite are not vacuous before a real driver
// exists. It reads the same profile table and paging classifier the mock
// does, so it is NOT an independent oracle of any vendor (plan 12A.6): a real
// driver (lane F8 onward) is written from the vendor's documentation and run
// through the same suite.
type ReferenceDriver struct {
	Surface vendorsurface.SurfaceID
	Method  string
	Path    string
	Profile Profile
	Family  PagingFamily
	// MaxRetries bounds 429 retries (default 3).
	MaxRetries int

	rootArray bool
	container string
	// served counts the records served so far (the offset family's next
	// offset).
	served int
	// postToken is the page token a POST next-token family sends in its body.
	postToken string
}

// postCursor marks a cursor that travels in a POST body, not the URL.
const postCursor = "post-body:"

// NewReferenceDriver builds the reference driver for a corpus surface.
func NewReferenceDriver(c *Corpus, id vendorsurface.SurfaceID) (*ReferenceDriver, error) {
	sf, ok := c.Surfaces[id]
	if !ok {
		return nil, fmt.Errorf("vendormock.NewReferenceDriver: %s has no fixtures", id)
	}
	p, ok := ProfileFor(id)
	if !ok {
		return nil, fmt.Errorf("vendormock.NewReferenceDriver: %s has no profile", id)
	}
	first, ok := sf.Success()
	if !ok {
		return nil, fmt.Errorf("vendormock.NewReferenceDriver: %s has no success fixture", id)
	}
	// A path template is filled with a synthetic placeholder per variable.
	segs := strings.Split(first.Sidecar.HTTP.Path, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			segs[i] = "synthetic-" + strings.Trim(s, "{}")
		}
	}
	d := &ReferenceDriver{
		Surface: id, Method: first.Sidecar.HTTP.Method, Path: strings.Join(segs, "/"),
		Profile: p, Family: Classify(sf), MaxRetries: 3,
	}
	doc, err := decodeJSON(first.Body)
	if err != nil {
		return nil, fmt.Errorf("vendormock.NewReferenceDriver: %s first page: %w", id, err)
	}
	if key, ok := RecordPath(doc); ok {
		d.rootArray = key == ""
		d.container = key
	}
	return d, nil
}

// Fetch implements Driver.
func (d *ReferenceDriver) Fetch(ctx context.Context, c Conn) (Result, error) {
	var res Result
	maxPages := c.MaxPages
	if maxPages <= 0 {
		maxPages = 100
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return res, classed(ErrOther, err)
	}
	if d.Profile.TokenScope != "" {
		step := d.clientCredentials
		if d.Profile.TokenGrant == GrantJWTBearer {
			step = d.jwtBearer
		}
		tok, err := step(ctx, c)
		if err != nil {
			return res, err
		}
		c.Token = tok
	}
	next := c.BaseURL + d.Path
	page := 1
	seen := map[string]bool{}
	var prev []byte
	for {
		if res.Pages >= maxPages {
			res.Truncated = true
			return res, nil
		}
		var body io.Reader
		if d.Method != http.MethodGet && d.Method != http.MethodDelete {
			payload := "{}"
			if d.Family == FamilyBodyPage {
				payload = fmt.Sprintf(`{"page":%d,"pageSize":100}`, page)
			}
			if d.Family == FamilyNextToken && d.postToken != "" {
				b, _ := json.Marshal(map[string]string{d.Profile.AfterParam: d.postToken})
				payload = string(b)
			}
			body = strings.NewReader(payload)
		}
		resp, raw, err := d.do(ctx, c, d.Method, next, body)
		if err != nil {
			return res, err
		}
		res.Pages++
		// Page-number paging carries no cursor to compare, so a
		// byte-identical page right after the previous one is the loop
		// signal there (cursor families compare the cursor instead).
		// The offset family (lane ENTAPI-L16C) has no cursor either.
		if (d.Family == FamilyBodyPage || d.Family == FamilyOffset) && res.Pages > 1 && bytes.Equal(raw, prev) {
			return res, classed(ErrSchemaDrift, fmt.Errorf("paging loop: page %d repeats page %d", res.Pages,
				res.Pages-1))
		}
		prev = raw
		doc, derr := decodeJSON(raw)
		if derr != nil {
			return res, classed(ErrSchemaDrift, fmt.Errorf("page %d is not JSON: %w", res.Pages, derr))
		}
		if d.Family == FamilyReport {
			n, err := d.fetchReport(ctx, c, doc)
			res.Records += n
			return res, err
		}
		n, err := d.countRecords(doc)
		if err != nil {
			return res, err
		}
		res.Records += n
		d.served = res.Records
		more, cursor, err := d.nextPage(resp, doc, base)
		if err != nil {
			return res, err
		}
		if !more {
			return res, nil
		}
		if cursor != "" && seen[cursor] {
			return res, classed(ErrSchemaDrift, fmt.Errorf("paging loop: cursor %q repeated", cursor))
		}
		seen[cursor] = true
		page++
		next = cursor
		if strings.HasPrefix(next, postCursor) {
			next = c.BaseURL + d.Path
		}
		if d.Family == FamilyBodyPage {
			next = c.BaseURL + d.Path
			if d.Method == http.MethodGet {
				next = fmt.Sprintf("%s%s?page=%d", c.BaseURL, d.Path, page)
			}
		}
	}
}

// do sends one request with the profile's auth, honouring Retry-After on 429
// (delay-seconds or HTTP-date) through c.Sleep, and never retrying an auth
// failure with another scheme.
func (d *ReferenceDriver) do(ctx context.Context, c Conn, method, target string, body io.Reader) (*http.Response,
	[]byte, error,
) {
	var payload []byte
	if body != nil {
		payload, _ = io.ReadAll(body)
	}
	retries := d.MaxRetries
	if retries <= 0 {
		retries = 3
	}
	for attempt := 0; ; attempt++ {
		var rb io.Reader
		if payload != nil {
			rb = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, rb)
		if err != nil {
			return nil, nil, classed(ErrOther, err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range d.Profile.Required {
			req.Header.Set(k, v)
		}
		switch d.Profile.Scheme {
		case SchemeRawHeader:
			req.Header.Set(d.Profile.AuthHeader, c.Token)
		case SchemeBearer:
			req.Header.Set(d.Profile.AuthHeader, "Bearer "+c.Token)
		case SchemeBasicUser:
			req.SetBasicAuth(c.Token, "")
		case SchemePrefixed:
			req.Header.Set(d.Profile.AuthHeader, d.Profile.AuthPrefix+" "+c.Token)
		case SchemeAWSSigV4:
			if err := d.signSigV4(req, payload, c); err != nil {
				return nil, nil, classed(ErrOther, err)
			}
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
				return nil, nil, classed(ErrTimeout, err)
			}
			return nil, nil, classed(ErrOther, err)
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()
		if err != nil {
			if isTimeout(err) {
				return nil, nil, classed(ErrTimeout, err)
			}
			return nil, nil, classed(ErrOther, err)
		}
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden &&
			resp.Header.Get("x-ratelimit-remaining") != "0":
			return resp, raw, classed(ErrAuth, fmt.Errorf("status %d", resp.StatusCode))
		case resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusForbidden && resp.Header.Get("x-ratelimit-remaining") == "0":
			if attempt >= retries {
				return resp, raw, classed(ErrRateLimited, fmt.Errorf("still limited after %d retries", attempt))
			}
			wait, ok := ParseRetryAfter(resp.Header.Get("Retry-After"), c.now())
			if !ok {
				wait = time.Second
			}
			c.sleep(wait)
			continue
		case resp.StatusCode >= 400:
			return resp, raw, classed(ErrOther, fmt.Errorf("status %d", resp.StatusCode))
		}
		return resp, raw, nil
	}
}

func isTimeout(err error) bool {
	var te interface{ Timeout() bool }
	return errors.As(err, &te) && te.Timeout()
}

// ParseRetryAfter reads Retry-After as delay-seconds or an HTTP-date.
func ParseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return time.Duration(n) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

// countRecords counts the records of one page and fails the page when the
// documented record container is missing, the root has the wrong JSON type,
// or the paging flag has the wrong JSON type (plan 12A.5: land nothing for a
// drifted page). The expected shape is read from the corpus first page.
func (d *ReferenceDriver) countRecords(doc any) (int, error) {
	switch {
	case d.rootArray:
		arr, ok := doc.([]any)
		if !ok {
			return 0, classed(ErrSchemaDrift, fmt.Errorf("root is %T, want the documented array", doc))
		}
		return len(arr), nil
	case d.container != "":
		obj, ok := doc.(map[string]any)
		if !ok {
			return 0, classed(ErrSchemaDrift, fmt.Errorf("root is %T, want the documented object", doc))
		}
		if err := flagTypes(obj); err != nil {
			return 0, err
		}
		arr, ok := obj[d.container].([]any)
		if !ok {
			return 0, classed(ErrSchemaDrift, fmt.Errorf("record container %q is missing", d.container))
		}
		return len(arr), nil
	default:
		obj, ok := doc.(map[string]any)
		if !ok {
			return 0, classed(ErrSchemaDrift, fmt.Errorf("root is %T, want the documented object", doc))
		}
		if err := flagTypes(obj); err != nil {
			return 0, err
		}
		return 1, nil
	}
}

func flagTypes(obj map[string]any) error {
	if v, ok := obj["has_more"]; ok {
		if _, isBool := v.(bool); !isBool {
			return classed(ErrSchemaDrift, fmt.Errorf("has_more is %T, want boolean", v))
		}
	}
	if v, ok := obj["@odata.nextLink"]; ok {
		if _, isString := v.(string); !isString {
			return classed(ErrSchemaDrift, fmt.Errorf("@odata.nextLink is %T, want string", v))
		}
	}
	if pg, ok := obj["pagination"].(map[string]any); ok {
		if v, ok := pg["hasNextPage"]; ok {
			if _, isBool := v.(bool); !isBool {
				return classed(ErrSchemaDrift, fmt.Errorf("pagination.hasNextPage is not a boolean"))
			}
		}
	}
	if pg, s, ok := nestedCursor(obj); ok {
		if _, isBool := pg[s.Flag].(bool); !isBool {
			return classed(ErrSchemaDrift, fmt.Errorf("%s.%s is not a boolean", s.Object, s.Flag))
		}
	}
	if v, ok := obj["has_next_page"]; ok {
		if _, isBool := v.(bool); !isBool {
			return classed(ErrSchemaDrift, fmt.Errorf("has_next_page is %T, want boolean", v))
		}
	}
	return nil
}

func (d *ReferenceDriver) nextPage(resp *http.Response, doc any, base *url.URL) (bool, string, error) {
	obj, _ := doc.(map[string]any)
	switch d.Family {
	case FamilyNextPage:
		if more, _ := obj["has_more"].(bool); more {
			tok, _ := obj["next_page"].(string)
			return true, d.withQuery(base, "page", tok), nil
		}
	case FamilyAfterID:
		if more, _ := obj["has_more"].(bool); more {
			tok := scalarString(obj["last_id"])
			return true, d.withQuery(base, d.Profile.AfterParam, tok), nil
		}
	case FamilyEndCursor:
		if more, _ := obj["has_next_page"].(bool); more {
			tok := scalarString(obj["end_cursor"])
			return true, d.withQuery(base, d.Profile.AfterParam, tok), nil
		}
	case FamilyNextToken:
		if k, ok := nextTokenKey(obj); ok {
			if tok, _ := obj[k].(string); tok != "" {
				if d.Method == http.MethodPost {
					// The token rides in the next request's JSON body (AWS
					// JSON, the Logging API; lane ENTAPI-L16D).
					d.postToken = tok
					return true, postCursor + tok, nil
				}
				return true, d.withQuery(base, d.Profile.AfterParam, tok), nil
			}
		}
	case FamilyNestedCursor:
		if pg, s, ok := nestedCursor(obj); ok {
			if more, _ := pg[s.Flag].(bool); more {
				return true, d.withQuery(base, d.Profile.AfterParam, scalarString(pg[s.Cursor])), nil
			}
		}
	case FamilyOffset:
		if s, ok := offsetSpelled(obj); ok {
			total, err := strconv.Atoi(scalarString(obj[s.Total]))
			if err != nil {
				return false, "", classed(ErrSchemaDrift, fmt.Errorf("%s is not an integer", s.Total))
			}
			if d.served < total {
				return true, d.withQuery(base, d.Profile.AfterParam, strconv.Itoa(d.served+s.Base)), nil
			}
		}
	case FamilyLink:
		next := linkNext(resp.Header.Get("Link"))
		if next == "" {
			return false, "", nil
		}
		u, err := url.Parse(next)
		if err != nil || u.Host != base.Host || u.Scheme != base.Scheme {
			return false, "", classed(ErrOther, fmt.Errorf("Link rel=next leaves the API origin: %q", next))
		}
		return true, next, nil
	case FamilyODataNext:
		next, _ := obj["@odata.nextLink"].(string)
		if next == "" {
			return false, "", nil
		}
		u, err := url.Parse(next)
		if err != nil || u.Host != base.Host || u.Scheme != base.Scheme {
			return false, "", classed(ErrOther, fmt.Errorf("@odata.nextLink leaves the API origin: %q", next))
		}
		return true, next, nil
	case FamilyBodyPage:
		pg, _ := obj["pagination"].(map[string]any)
		if more, _ := pg["hasNextPage"].(bool); more {
			return true, "", nil
		}
	}
	return false, "", nil
}

func (d *ReferenceDriver) withQuery(base *url.URL, key, val string) string {
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + d.Path
	q := url.Values{}
	q.Set(key, val)
	u.RawQuery = q.Encode()
	return u.String()
}

// linkNext extracts the rel="next" target of an RFC 8288 Link header.
func linkNext(h string) string {
	for _, part := range strings.Split(h, ",") {
		segs := strings.Split(part, ";")
		if len(segs) < 2 {
			continue
		}
		target := strings.Trim(strings.TrimSpace(segs[0]), "<>")
		for _, p := range segs[1:] {
			if strings.ReplaceAll(strings.TrimSpace(p), " ", "") == `rel="next"` {
				return target
			}
		}
	}
	return ""
}

// fetchReport follows a two-step report: each signed link is fetched with a
// fresh client request that carries NO API credential (plan 6.2 second hop).
func (d *ReferenceDriver) fetchReport(ctx context.Context, c Conn, doc any) (int, error) {
	obj, ok := doc.(map[string]any)
	if !ok {
		return 0, classed(ErrSchemaDrift, fmt.Errorf("report envelope is not an object"))
	}
	links, ok := obj["download_links"].([]any)
	if !ok {
		return 0, classed(ErrSchemaDrift, fmt.Errorf("report envelope has no download_links"))
	}
	total := 0
	for _, l := range links {
		s, _ := l.(string)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s, nil)
		if err != nil {
			return total, classed(ErrOther, err)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return total, classed(ErrOther, err)
		}
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			if !json.Valid(line) {
				_ = resp.Body.Close()
				return total, classed(ErrSchemaDrift, fmt.Errorf("report line is not JSON"))
			}
			total++
		}
		_ = resp.Body.Close()
		if err := sc.Err(); err != nil {
			return total, classed(ErrOther, err)
		}
	}
	return total, nil
}

// referenceAccessKeyID is AWS's published documentation example key id.
const referenceAccessKeyID = "AKIAIOSFODNN7EXAMPLE" //nolint:gosec // G101: AWS's published documentation example key id, not a credential

// signSigV4 signs a reference request with c.Token as the secret access key,
// covering the content type and the profile's required headers.
func (d *ReferenceDriver) signSigV4(req *http.Request, payload []byte, c Conn) error {
	hdr := map[string]string{}
	for k := range d.Profile.Required {
		hdr[strings.ToLower(k)] = req.Header.Get(k)
	}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		hdr["content-type"] = ct
	}
	h, err := cloudsign.SignSigV4(cloudsign.Request{
		Method: req.Method, Host: req.URL.Host, Path: req.URL.Path, Query: req.URL.Query(), Headers: hdr, Payload: payload,
	}, cloudsign.AccessKey{ID: referenceAccessKeyID, Secret: c.Token}, cloudsign.SigV4Params{
		Region: d.Profile.SigV4Region, Service: d.Profile.SigV4Service, Time: c.now(),
	})
	if err != nil {
		return err
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	return nil
}

// jwtBearer performs the Google service account token step: an RS256
// assertion signed with ServiceAccountKey(c.Token), posted to c.TokenURL/token.
func (d *ReferenceDriver) jwtBearer(ctx context.Context, c Conn) (string, error) {
	if c.TokenURL == "" {
		return "", classed(ErrOther, fmt.Errorf("%s needs a token host", d.Surface))
	}
	k := ServiceAccountKey(c.Token)
	a, err := cloudsign.GoogleAssertion(k, cloudsign.JWTClaims{
		Issuer: "reference@synthetic-project.iam.gserviceaccount.com", Scopes: []string{d.Profile.TokenScope},
		Audience: c.TokenURL + "/token", IssuedAt: c.now(), Lifetime: cloudsign.MaxAssertionLifetime,
	})
	if err != nil {
		return "", classed(ErrOther, err)
	}
	form := url.Values{"grant_type": {jwtGrant}, "assertion": {a}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", classed(ErrOther, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if isTimeout(err) {
			return "", classed(ErrTimeout, err)
		}
		return "", classed(ErrOther, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", classed(ErrAuth, fmt.Errorf("token step status %d", resp.StatusCode))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tr); err != nil || tr.AccessToken == "" {
		return "", classed(ErrSchemaDrift, fmt.Errorf("token step answer is not the documented shape"))
	}
	return tr.AccessToken, nil
}

// clientCredentials performs the OAuth 2.0 client-credentials token step of
// a TokenScope profile against the mock identity platform (c.TokenURL) and
// returns the access token. The client secret is c.Token; it is sent only to
// the token host.
func (d *ReferenceDriver) clientCredentials(ctx context.Context, c Conn) (string, error) {
	if c.TokenURL == "" {
		return "", classed(ErrOther, fmt.Errorf("%s needs a token host", d.Surface))
	}
	form := url.Values{
		"client_id": {"00001111-aaaa-2222-bbbb-3333cccc4444"}, "scope": {d.Profile.TokenScope},
		"client_secret": {c.Token}, "grant_type": {"client_credentials"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL+"/synthetic-tenant/oauth2/v2.0/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", classed(ErrOther, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if isTimeout(err) {
			return "", classed(ErrTimeout, err)
		}
		return "", classed(ErrOther, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", classed(ErrAuth, fmt.Errorf("token step status %d", resp.StatusCode))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tr); err != nil || tr.AccessToken == "" {
		return "", classed(ErrSchemaDrift, fmt.Errorf("token step answer is not the documented shape"))
	}
	return tr.AccessToken, nil
}
