package planlimitsnet

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/planlimits"
)

func TestCheckURLTable(t *testing.T) {
	src, _ := planlimits.SourceByID(planlimits.SourceOpenRouter)
	cases := map[string]bool{
		"https://openrouter.ai/api/v1/key":       true,
		"https://OpenRouter.ai/api/v1/key":       true,
		"https://openrouter.ai:443/api/v1/key":   true,
		"http://openrouter.ai/api/v1/key":        false,
		"https://openrouter.ai:8443/api":         false,
		"https://user:pw@openrouter.ai/api":      false,
		"https://openrouter.ai.evil.test/api":    false,
		"https://evil.test/?h=openrouter.ai":     false,
		"https://openrouter.ai/api#frag":         false,
		"https://openrouter.ai%2F@evil.test/api": false,
	}
	for u, ok := range cases {
		if err := CheckURL(src, u); (err == nil) != ok {
			t.Errorf("%s: err=%v want ok=%v", u, err, ok)
		}
	}
}

// rewrite sends every request to the test server, keeping the original host
// in the URL the client checked.
type rewrite struct {
	base *url.URL
	seen []*http.Request
}

func (r *rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	r.seen = append(r.seen, req.Clone(req.Context()))
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host = r.base.Scheme, r.base.Host
	return http.DefaultTransport.RoundTrip(out)
}

func TestDoHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/key":
			if r.Header.Get("Authorization") != "Bearer K" {
				w.WriteHeader(401)
				return
			}
			_, _ = io.WriteString(w, `{"data":{}}`)
		case "/api/v1/credits":
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(429)
		case "/redirect":
			http.Redirect(w, r, "https://evil.test/", http.StatusFound)
		}
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	rt := &rewrite{base: base}
	c := New(Options{HTTP: &http.Client{Transport: rt}, Version: "test"})
	src, _ := planlimits.SourceByID(planlimits.SourceOpenRouter)
	res := c.Do(context.Background(), src, []planlimits.Request{
		{Key: "key", Kind: planlimits.ReqHTTP, URL: "https://openrouter.ai/api/v1/key", SecretHeaders: map[string]string{"Authorization": "Bearer K"}},
		{Key: "credits", Kind: planlimits.ReqHTTP, URL: "https://openrouter.ai/api/v1/credits"},
		{Key: "redir", Kind: planlimits.ReqHTTP, URL: "https://openrouter.ai/redirect"},
		{Key: "evil", Kind: planlimits.ReqHTTP, URL: "https://evil.test/x", SecretHeaders: map[string]string{"Authorization": "Bearer K"}},
	})
	if res["key"].Status != 200 || string(res["key"].Body) != `{"data":{}}` {
		t.Errorf("key %+v", res["key"])
	}
	if res["credits"].Status != 429 || res["credits"].RetryAfter != 7*time.Second {
		t.Errorf("credits %+v", res["credits"])
	}
	if res["redir"].Err == nil {
		t.Error("a redirect must never be followed")
	}
	if !errors.Is(res["evil"].Err, ErrHostNotAllowed) {
		t.Errorf("evil host %+v", res["evil"])
	}
	for _, r := range rt.seen {
		if r.URL.Host == "evil.test" {
			t.Fatal("a refused host must never be contacted")
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "superbased-observer-plans/") {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
	}
	if len(rt.seen) != 3 {
		t.Errorf("sent %d requests, want 3", len(rt.seen))
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	if ParseRetryAfter("30", now) != 30*time.Second || ParseRetryAfter("", now) != 0 || ParseRetryAfter("soon", now) != 0 {
		t.Error("seconds form")
	}
	if got := ParseRetryAfter(now.Add(time.Minute).Format(http.TimeFormat), now); got != time.Minute {
		t.Errorf("date form %s", got)
	}
}

func TestExecPolicy(t *testing.T) {
	src, _ := planlimits.SourceByID(planlimits.SourceKiroCLI)
	c := New(Options{Resolve: func(string) (string, error) { return "", errors.New("never resolved") }})
	res := c.Do(context.Background(), src, []planlimits.Request{{Key: "out", Kind: planlimits.ReqExec, Argv: []string{"sh", "-c", "id"}}})
	if !errors.Is(res["out"].Err, planlimits.ErrShape) {
		t.Errorf("a command other than the source's must be refused: %+v", res["out"])
	}
	c = New(Options{Getenv: func(k string) string {
		if k == "PATH" {
			return t.TempDir()
		}
		return ""
	}})
	res = c.Do(context.Background(), src, []planlimits.Request{{Key: "out", Kind: planlimits.ReqExec, Argv: []string{"kiro-cli", "chat"}}})
	if !errors.Is(res["out"].Err, planlimits.ErrUnavailable) {
		t.Errorf("a missing CLI is unavailable: %+v", res["out"])
	}
}

func TestExecSkipsShimDirAndRunsFixedArgv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture")
	}
	shim, real := t.TempDir(), t.TempDir()
	write := func(dir, body string) {
		if err := os.WriteFile(filepath.Join(dir, "amp"), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write(shim, "echo SHIM")
	write(real, `echo "args:$*"; echo "secret:${OPENAI_API_KEY:-none}"`)
	env := map[string]string{"PATH": shim + string(os.PathListSeparator) + real, "OPENAI_API_KEY": "leak"}
	c := New(Options{ExcludeDirs: []string{shim}, Getenv: func(k string) string { return env[k] }})
	src, _ := planlimits.SourceByID(planlimits.SourceAmpCLI)
	reqs, _ := planlimits.Drivers[planlimits.SourceAmpCLI].Plan(planlimits.PlanInput{})
	res := c.Do(context.Background(), src, reqs)
	out := string(res["out"].Body)
	if strings.Contains(out, "SHIM") || !strings.Contains(out, "args:usage") {
		t.Errorf("resolved the shim or wrong argv: %q", out)
	}
	if !strings.Contains(out, "secret:none") {
		t.Errorf("only allow-listed variables reach a vendor command: %q", out)
	}
}
