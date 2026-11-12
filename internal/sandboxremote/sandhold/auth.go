package sandhold

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// auth.go is how a call proves who sends it. A node accepts two
// credentials, and both travel in headers only, never in an address:
//
//   - an API key (the key `observer sandbox connect` saved, or
//     SANDHOLD_API_KEY), sent as X-API-Key: Client.Key, or APIKey;
//   - an enrolled org member's short-lived access token from the org's
//     token service, bound to this machine's key by DPoP (RFC 9449): sent as
//     "Authorization: DPoP <token>" with a fresh "DPoP" proof per call, whose
//     htm and htu are the call's method and address without its query
//     (DPoP). The token source lives in the cmd layer (it reads the org
//     enrolment); this package only asks it to sign one call.
//
// Every other rule of the client (no redirect followed, https or wss except
// loopback, the command never in the address) is the same for both.

// Authorizer puts a call's credential in its headers. method and target are
// the call's method and its address without the query.
type Authorizer interface {
	Authorize(ctx context.Context, method, target string, h http.Header) error
}

// APIKey is the X-API-Key credential.
type APIKey string

// Authorize sets X-API-Key.
func (k APIKey) Authorize(_ context.Context, _, _ string, h http.Header) error {
	if strings.TrimSpace(string(k)) == "" {
		return errors.New("sandhold: no API key")
	}
	h.Set("X-API-Key", string(k))
	return nil
}

// String names the credential without its value.
func (k APIKey) String() string { return "sandhold API key" }

// Format prints String for every verb, so the key never reaches a log line.
func (k APIKey) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(k.String())) }

// DPoPSigner returns the access token and a fresh DPoP proof for one call
// (htm, htu). The token is bound to the key the proof is signed with.
type DPoPSigner interface {
	SignCall(ctx context.Context, htm, htu string) (token, proof string, err error)
}

// DPoP is the enrolled org member's credential over signer.
func DPoP(signer DPoPSigner) Authorizer { return dpopAuth{signer} }

type dpopAuth struct{ s DPoPSigner }

func (d dpopAuth) Authorize(ctx context.Context, method, target string, h http.Header) error {
	if d.s == nil {
		return errors.New("sandhold: no org token source")
	}
	if i := strings.IndexAny(target, "?#"); i >= 0 {
		target = target[:i] // htu carries no query or fragment (RFC 9449 4.2)
	}
	tok, proof, err := d.s.SignCall(ctx, method, target)
	if err != nil {
		return fmt.Errorf("sandhold: the org token: %w", err)
	}
	if tok == "" || proof == "" {
		return errors.New("sandhold: the org token service gave no token")
	}
	h.Set("Authorization", "DPoP "+tok)
	h.Set("DPoP", proof)
	return nil
}

// authorize puts the client's credential in h: Auth when set, else Key as
// X-API-Key, else nothing (the sign-in endpoints).
func (c *Client) authorize(ctx context.Context, method, target string, h http.Header) error {
	if c.Auth != nil {
		return c.Auth.Authorize(ctx, method, target, h)
	}
	if c.Key != "" {
		h.Set("X-API-Key", c.Key)
	}
	return nil
}

// NewBoxAuth is NewBox for a credential other than a plain key (an org
// member's DPoP-bound token). httpc, when not nil, is the caller's
// egress-guarded client; it is copied with redirects refused and no overall
// timeout (a stream and a terminal run as long as their command).
func NewBoxAuth(node string, auth Authorizer, httpc *http.Client) (*Box, error) {
	u, err := NodeURL(node)
	if err != nil {
		return nil, err
	}
	if auth == nil {
		return nil, errors.New("sandhold: no credential for " + u)
	}
	return &Box{api: &Client{URL: u, Auth: auth, HTTP: NoRedirect(httpc, 0)}}, nil
}

// NewClientAuth is NewClient for a credential other than a plain key; httpc
// is copied as NewBoxAuth copies it, with timeout as its overall bound.
func NewClientAuth(node string, auth Authorizer, httpc *http.Client) *Client {
	return &Client{URL: node, Auth: auth, HTTP: NoRedirect(httpc, defaultCallTimeout)}
}

// defaultCallTimeout bounds one plain API call.
const defaultCallTimeout = 30 * time.Second

// NoRedirect returns a copy of httpc (a fresh client when nil) that follows
// no redirect, with timeout as its overall bound (0: none).
func NoRedirect(httpc *http.Client, timeout time.Duration) *http.Client {
	var c http.Client
	if httpc != nil {
		c = *httpc
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.Timeout = timeout
	return &c
}
