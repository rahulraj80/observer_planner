package sandhold

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
)

// auth_test.go: the org member's credential (a DPoP-bound token per call)
// drives the same Box as a key: every REST call and the terminal handshake
// carry "Authorization: DPoP" and a proof bound to the call's method and
// address without its query, never X-API-Key, never a token in an address,
// and no redirect is followed with the token.

const orgTok = "org-at-jwt-0123456789abcdef"

// fakeSigner records the (htm, htu) of every proof it signed.
type fakeSigner struct {
	mu     sync.Mutex
	signed []string
	err    error
}

func (s *fakeSigner) SignCall(_ context.Context, htm, htu string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", "", s.err
	}
	s.signed = append(s.signed, htm+" "+htu)
	return orgTok, "proof(" + htm + " " + htu + ")", nil
}

// dpopFront fronts a boxNode: it checks the DPoP headers against the
// request it arrived with, then hands the request on as the key's.
type dpopFront struct {
	srv     *httptest.Server
	mu      sync.Mutex
	bad     []string
	apiKeys int
}

func newDPoPFront(t *testing.T, f *boxNode) *dpopFront {
	d := &dpopFront{}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "proof(" + r.Method + " " + d.srv.URL + r.URL.Path + ")"
		d.mu.Lock()
		if r.Header.Get("X-API-Key") != "" {
			d.apiKeys++
		}
		if r.Header.Get("Authorization") != "DPoP "+orgTok || r.Header.Get("DPoP") != want {
			d.bad = append(d.bad, fmt.Sprintf("%s %s: Authorization %q DPoP %q", r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("DPoP")))
		}
		d.mu.Unlock()
		r.Header.Del("Authorization")
		r.Header.Del("DPoP")
		r.Header.Set("X-API-Key", boxKey)
		f.serve(w, r)
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func TestDPoPBoxSignsEveryCall(t *testing.T) {
	f := newBoxNode(t)
	d := newDPoPFront(t, f)
	s := &fakeSigner{}
	b, err := NewBoxAuth(d.srv.URL, DPoP(s), d.srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Files (a query in the address), a streamed exec, and the terminal.
	if err := b.WriteFiles(ctx, "sb1", []sandboxremote.UploadFile{{Path: "/workspace/a.txt", Mode: 0o644, Size: 2, Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("hi")), nil }}}); err != nil {
		t.Fatal(err)
	}
	p, err := b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"echo", "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(p)
	if code, err := p.Wait(ctx); code != 0 || err != nil {
		t.Fatalf("exec: %d %v", code, err)
	}
	p, err = b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"claude"}, Env: map[string]string{"K": "v"}, PTY: true, Stdin: true})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Write([]byte("x"))
	_, _ = io.ReadAll(p)
	if code, err := p.Wait(ctx); code != 3 || err != nil {
		t.Fatalf("terminal: %d %v", code, err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.bad) > 0 || d.apiKeys > 0 {
		t.Fatalf("calls without a matching DPoP credential: %v (X-API-Key sent %d times)", d.bad, d.apiKeys)
	}
	sawPTY := false
	for _, sg := range s.signed {
		if strings.ContainsAny(sg, "?#") {
			t.Errorf("a proof is bound to an address with a query: %s", sg)
		}
		if sg == "GET "+d.srv.URL+"/v1/sandboxes/sb1/pty" {
			sawPTY = true
		}
	}
	if !sawPTY {
		t.Errorf("the terminal handshake was not signed: %v", s.signed)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.ptyURLs {
		if strings.Contains(u, orgTok) || strings.Contains(u, "claude") || strings.Contains(u, "proof") {
			t.Errorf("the terminal's address carries what it must not: %s", u)
		}
	}
}

func TestDPoPNeverFollowsARedirect(t *testing.T) {
	hit := false
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer elsewhere.Close()
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/steal", http.StatusTemporaryRedirect)
	}))
	defer node.Close()
	// The caller's client follows redirects; the copy refuses them.
	c := NewClientAuth(node.URL, DPoP(&fakeSigner{}), &http.Client{})
	_, err := c.Whoami(context.Background())
	if !errors.Is(err, ErrRedirect) || hit {
		t.Fatalf("got %v (elsewhere reached: %v), want ErrRedirect and no call elsewhere", err, hit)
	}
	if strings.Contains(err.Error(), orgTok) {
		t.Error("the refusal carries the token")
	}
	b, err := NewBoxAuth(node.URL, DPoP(&fakeSigner{}), &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Exec(context.Background(), "sb1", sandboxremote.ExecSpec{Argv: []string{"sh"}, PTY: true}); !errors.Is(err, ErrRedirect) || hit {
		t.Fatalf("terminal: %v (elsewhere reached: %v)", err, hit)
	}
}

func TestDPoPAddressAndSignerRules(t *testing.T) {
	if _, err := NewBoxAuth("http://sandhold.example.com", DPoP(&fakeSigner{}), nil); err == nil {
		t.Error("plain http to another machine was accepted for a token")
	}
	if _, err := NewBoxAuth("https://sandhold.example.com", nil, nil); err == nil {
		t.Error("a box without a credential was built")
	}
	// A signer that fails sends nothing.
	sent := false
	node := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sent = true }))
	defer node.Close()
	c := NewClientAuth(node.URL, DPoP(&fakeSigner{err: errors.New("invalid_target")}), nil)
	if _, err := c.Whoami(context.Background()); err == nil || sent || !strings.Contains(err.Error(), "invalid_target") {
		t.Fatalf("a failed signer: %v (sent %v)", err, sent)
	}
	// A plain key still travels as X-API-Key, and only there.
	h := http.Header{}
	if err := APIKey(boxKey).Authorize(context.Background(), "GET", "https://n/v1/whoami", h); err != nil || h.Get("X-API-Key") != boxKey || h.Get("Authorization") != "" {
		t.Fatalf("APIKey: %v %v", h, err)
	}
	if s := fmt.Sprintf("%v %s %+v", APIKey(boxKey), APIKey(boxKey), APIKey(boxKey)); strings.Contains(s, boxKey) {
		t.Errorf("an API key printed: %s", s)
	}
}
