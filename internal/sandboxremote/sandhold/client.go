package sandhold

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// UserAgent is what Observer calls itself on a SandHold node.
const UserAgent = "superbased-observer"

// maxReply caps one reply body read from a node.
const maxReply = 1 << 20

// NodeURL normalises a node address. A key travels on every call, so plain
// http is accepted for this machine only (localhost or a loopback address);
// a user, query or fragment is refused, and the path loses its trailing
// slash.
func NodeURL(in string) (string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", errors.New("the node address is empty")
	}
	if !strings.Contains(in, "://") {
		in = "https://" + in
	}
	u, err := url.Parse(in)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q is not a node address (expected https://host[:port])", Plain(in))
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return "", fmt.Errorf("%s uses http: the key would cross the network unprotected (use https; http is accepted for localhost only)", Plain(in))
		}
	default:
		return "", fmt.Errorf("%q is not a node address (expected https://host[:port])", Plain(in))
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(in, "?#") {
		return "", fmt.Errorf("%q: give the node's address only, without a user, query or fragment", Plain(in))
	}
	return u.Scheme + "://" + strings.ToLower(u.Host) + strings.TrimRight(u.Path, "/"), nil
}

func isLoopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// HTTPClient never follows a redirect: the key goes to the node the person
// named and nowhere a reply points (note 42 section 5).
func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Client calls one node, with or without a key.
type Client struct {
	// URL is the node address, as NodeURL returns it.
	URL string
	// Key is the API key sent as X-API-Key ("" for the sign-in endpoints).
	Key string
	// Auth, when set, is the call's credential in place of Key (auth.go).
	Auth Authorizer
	HTTP *http.Client
}

// NewClient returns a client of node (already normalised) with key.
func NewClient(node, key string) *Client {
	return &Client{URL: node, Key: key, HTTP: HTTPClient(30 * time.Second)}
}

// APIError is a refusal from the node: why, and what fixes it, both in the
// node's own words (every SandHold refusal carries error and fix).
type APIError struct {
	Status int
	Msg    string
	Fix    string
	// Code is the refusal's code a client branches on (pipe_unsupported,
	// network_open_not_granted, ...), "" when the node sent none.
	Code string
	// RetryAfter is the reply's Retry-After in seconds (0 when it sent
	// none, or a date): a full node's 503 says when to try again.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.Fix != "" {
		return e.Msg + " (to fix: " + e.Fix + ")"
	}
	return e.Msg
}

// OAuthError is a device-grant refusal (RFC 8628 section 3.5): Code is for
// the tool, the message is the node's description for people.
type OAuthError struct {
	Code string
	*APIError
}

// ErrRedirect marks a node reply that pointed elsewhere; it is never
// followed with the key.
var ErrRedirect = errors.New("the node redirected")

// ErrUnreachable marks a call that never got an answer.
var ErrUnreachable = errors.New("the node could not be reached")

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return HTTPClient(30 * time.Second)
}

// Do sends JSON (in may be nil) and decodes a JSON reply into out (may be
// nil). Every refusal comes back as an *APIError or *OAuthError whose text
// is the node's, control characters removed.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	ctype := ""
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body, ctype = bytes.NewReader(b), "application/json"
	}
	res, err := c.send(ctx, request{method: method, path: path, body: body, size: -1, contentType: ctype, accept: "application/json"})
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxReply))
	if err != nil {
		return fmt.Errorf("%w: %s: %s", ErrUnreachable, c.URL, Plain(err.Error()))
	}
	if out == nil || res.StatusCode == http.StatusNoContent || len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("%s answered %s with a body that is not the JSON expected", c.URL, path)
	}
	return nil
}

// request is one call to the node.
type request struct {
	method, path string
	query        url.Values
	body         io.Reader
	// size is the body's length (-1 when unknown).
	size        int64
	contentType string
	accept      string
}

// ErrNotSent marks a call whose connection was never made (a dial
// failure), so the node cannot have acted on it.
var ErrNotSent = errors.New("the request never reached the node")

// send makes one call and returns a 2xx reply for the caller to read and
// close. A redirect is never followed with the key, a transport failure is
// ErrUnreachable (and ErrNotSent when no connection was made), and every
// other answer is the node's refusal in its own words.
func (c *Client) send(ctx context.Context, rq request) (*http.Response, error) {
	u := c.URL + rq.path
	if len(rq.query) > 0 {
		u += "?" + rq.query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, rq.method, u, rq.body)
	if err != nil {
		return nil, err
	}
	if rq.body != nil && rq.size >= 0 {
		req.ContentLength = rq.size
	}
	req.Header.Set("User-Agent", UserAgent)
	if rq.accept != "" {
		req.Header.Set("Accept", rq.accept)
	}
	if rq.contentType != "" {
		req.Header.Set("Content-Type", rq.contentType)
	}
	if err := c.authorize(ctx, rq.method, c.URL+rq.path, req.Header); err != nil {
		return nil, err
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return nil, fmt.Errorf("%w (%w): %s: %s", ErrUnreachable, ErrNotSent, c.URL, Plain(rootCause(err).Error()))
		}
		return nil, fmt.Errorf("%w: %s: %s", ErrUnreachable, c.URL, Plain(rootCause(err).Error()))
	}
	if res.StatusCode/100 == 3 {
		_ = res.Body.Close()
		return nil, fmt.Errorf("%w: %s answered %d pointing to %s; not following it with your key (if that is the node's real address, connect to it instead)",
			ErrRedirect, c.URL, res.StatusCode, Plain(res.Header.Get("Location")))
	}
	if res.StatusCode/100 != 2 {
		defer func() { _ = res.Body.Close() }()
		b, err := io.ReadAll(io.LimitReader(res.Body, maxReply))
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %s", ErrUnreachable, c.URL, Plain(err.Error()))
		}
		return nil, refusal(res, b)
	}
	return res, nil
}

// refusal reads a non-2xx reply into the node's own reason and fix.
func refusal(res *http.Response, b []byte) error {
	var e struct {
		Error string `json:"error"`
		Fix   string `json:"fix"`
		Desc  string `json:"error_description"`
		Code  string `json:"code"`
	}
	_ = json.Unmarshal(b, &e)
	ae := &APIError{Status: res.StatusCode, Msg: Plain(e.Error), Fix: Plain(e.Fix), Code: PlainLine(e.Code)}
	if e.Desc != "" || isOAuthCode(e.Error) {
		code := e.Error
		ae.Msg, ae.Fix = Plain(e.Desc), ""
		if ae.Msg == "" {
			ae.Msg = Plain(code)
		}
		return &OAuthError{Code: code, APIError: ae}
	}
	if ae.Msg == "" {
		ae.Msg = "the node answered " + Plain(res.Status)
		if s := strings.TrimSpace(string(b)); s != "" && len(s) < 300 {
			ae.Msg += ": " + Plain(s)
		}
	}
	if ra := strings.TrimSpace(res.Header.Get("Retry-After")); ra != "" {
		if n, err := strconv.Atoi(ra); err == nil && n > 0 {
			ae.RetryAfter = time.Duration(n) * time.Second
		}
		if ae.Fix == "" {
			ae.Fix = "try again in " + Plain(ra) + " seconds"
		}
	}
	return ae
}

// isOAuthCode reports the device grant's error codes (RFC 8628 3.5, RFC
// 6749 5.2) a node may send without a description.
func isOAuthCode(s string) bool {
	switch s {
	case "authorization_pending", "slow_down", "access_denied", "expired_token", "invalid_grant", "invalid_request", "invalid_client", "unsupported_grant_type":
		return true
	}
	return false
}

func rootCause(err error) error {
	for {
		u := errors.Unwrap(err)
		if u == nil {
			return err
		}
		err = u
	}
}

// keyIDPattern is the shape of a key id before it becomes part of an
// address.
var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// ValidKeyID reports whether id may be put in a path.
func ValidKeyID(id string) bool { return keyIDPattern.MatchString(id) }
