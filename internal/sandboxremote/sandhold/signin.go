package sandhold

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// signin.go is the tool side of the node's browser sign-in (RFC 8628, note
// 42 section 2): ask for a code, show the person the page and the code, then
// poll at the node's interval until the person approves, refuses, or the
// code expires. One approval gives one key, an ordinary API key of the
// member who approved.

// Start is what POST /v1/device/code answered.
type Start struct {
	// DeviceCode is the tool's secret for polling; never shown.
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	URI        string `json:"verification_uri"`
	Complete   string `json:"verification_uri_complete"`
	ExpiresIn  int    `json:"expires_in"`
	Interval   int    `json:"interval"`
}

// Page is the approval page to show (the complete one when the node sent
// it), control characters removed.
func (s Start) Page() string {
	if s.Complete != "" {
		return Plain(s.Complete)
	}
	return Plain(s.URI)
}

// Grant is what POST /v1/device/token answered once approved.
type Grant struct {
	Key       string    `json:"access_token"`
	KeyID     string    `json:"key_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Org       string    `json:"org"`
	User      string    `json:"user"`
}

// The sign-in's own refusals (beyond a node's *APIError).
var (
	// ErrNoBrowserSignIn: the node has no device sign-in (404).
	ErrNoBrowserSignIn = errors.New("this node has no browser sign-in")
	// ErrDenied: the person refused the sign-in on the page.
	ErrDenied = errors.New("the sign-in was refused")
	// ErrExpired: the code expired before it was approved.
	ErrExpired = errors.New("the sign-in was not approved in time")
	// ErrBadPage: the node sent an approval address that is not a web page.
	ErrBadPage = errors.New("the node sent an approval address that is not a web page")
)

// SignIn runs one device sign-in.
type SignIn struct {
	Client *Client
	// ClientID is what the tool calls itself on the approval page (the node
	// cuts it to 60 printable characters).
	ClientID string
	// Shown is called once with the page and code before polling starts;
	// sameNode says whether the page is under the node the person named
	// (only then may a browser be opened by itself).
	Shown func(s Start, sameNode bool)
	// Sleep waits d (nil: a timer honouring ctx). A seam for tests.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the clock (nil: time.Now).
	Now func() time.Time
}

// SlowDownStep is how much a slow_down answer adds to the interval (RFC
// 8628 section 3.5).
const SlowDownStep = 5 * time.Second

// defaultInterval and defaultExpiry apply when a node names none.
const (
	defaultInterval = 5 * time.Second
	defaultExpiry   = 10 * time.Minute
)

// Run asks for a code, shows it, and polls until a key is granted. It
// returns ErrDenied, ErrExpired, ErrNoBrowserSignIn, ErrBadPage, a node's
// refusal, or the context's error.
func (s *SignIn) Run(ctx context.Context) (Grant, error) {
	sleep, now := s.Sleep, s.Now
	if sleep == nil {
		sleep = sleepCtx
	}
	if now == nil {
		now = time.Now
	}
	c := *s.Client
	c.Key = "" // the tool-side endpoints take no credential
	var start Start
	err := c.Do(ctx, http.MethodPost, "/v1/device/code", map[string]string{"client_id": s.ClientID}, &start)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return Grant{}, ErrNoBrowserSignIn
	}
	if err != nil {
		return Grant{}, err
	}
	if start.DeviceCode == "" || start.UserCode == "" {
		return Grant{}, fmt.Errorf("%s answered the sign-in without a code", c.URL)
	}
	page := start.Complete
	if page == "" {
		page = start.URI
	}
	if !strings.HasPrefix(page, "https://") && !strings.HasPrefix(page, "http://") {
		return Grant{}, ErrBadPage
	}
	if s.Shown != nil {
		s.Shown(start, SameNode(c.URL, page))
	}
	interval := time.Duration(start.Interval) * time.Second
	if interval <= 0 {
		interval = defaultInterval
	}
	expiry := time.Duration(start.ExpiresIn) * time.Second
	if expiry <= 0 {
		expiry = defaultExpiry
	}
	deadline := now().Add(expiry)
	for {
		if err := sleep(ctx, interval); err != nil {
			return Grant{}, err
		}
		var g Grant
		err := c.Do(ctx, http.MethodPost, "/v1/device/token", map[string]string{"device_code": start.DeviceCode}, &g)
		var oe *OAuthError
		switch {
		case err == nil:
			if g.Key == "" {
				return Grant{}, fmt.Errorf("%s granted the sign-in without a key", c.URL)
			}
			g.Org, g.User, g.KeyID = Plain(g.Org), Plain(g.User), Plain(g.KeyID)
			return g, nil
		case errors.As(err, &oe) && oe.Code == "authorization_pending":
		case errors.As(err, &oe) && oe.Code == "slow_down":
			interval += SlowDownStep
		case errors.As(err, &oe) && oe.Code == "access_denied":
			return Grant{}, fmt.Errorf("%w: %s", ErrDenied, oe.Msg)
		case errors.As(err, &oe) && (oe.Code == "expired_token" || oe.Code == "invalid_grant"):
			return Grant{}, fmt.Errorf("%w: %s", ErrExpired, oe.Msg)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return Grant{}, err
		case errors.Is(err, ErrUnreachable):
			// A network blip: keep waiting until the code expires.
			if now().After(deadline) {
				return Grant{}, err
			}
		default:
			return Grant{}, err
		}
		if now().After(deadline) {
			return Grant{}, ErrExpired
		}
	}
}

// SameNode reports whether page is under node (the address the person
// named), so a browser may be opened on it without asking.
func SameNode(node, page string) bool {
	return strings.HasPrefix(page, node+"/")
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
