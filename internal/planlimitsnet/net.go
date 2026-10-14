package planlimitsnet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/planlimits"
)

// MaxBody caps a response body or a CLI's output.
const MaxBody = 1 << 20

// defaultTimeout bounds one HTTP call.
const defaultTimeout = 20 * time.Second

// Options configure a Client. The zero value is usable.
type Options struct {
	// HTTP overrides the HTTP client (tests). Its CheckRedirect is replaced
	// so redirects are never followed.
	HTTP *http.Client
	// Version is sent in the User-Agent and to the codex app-server.
	Version string
	// Getenv reads the environment (tests). Defaults to os.Getenv.
	Getenv func(string) string
	// ExcludeDirs are PATH directories never used to resolve a vendor
	// command (Observer's own shim directory).
	ExcludeDirs []string
	// Resolve overrides command resolution (tests).
	Resolve func(cmd string) (string, error)
}

// Client performs the requests drivers plan.
type Client struct {
	http    *http.Client
	version string
	getenv  func(string) string
	exclude map[string]bool
	resolve func(string) (string, error)
}

// New returns a Client.
func New(o Options) *Client {
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	} else {
		cp := *hc
		hc = &cp
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c := &Client{http: hc, version: o.Version, getenv: o.Getenv, exclude: map[string]bool{}, resolve: o.Resolve}
	if c.getenv == nil {
		c.getenv = os.Getenv
	}
	for _, d := range o.ExcludeDirs {
		if d != "" {
			c.exclude[filepath.Clean(d)] = true
		}
	}
	if c.resolve == nil {
		c.resolve = c.lookPath
	}
	return c
}

// ErrHostNotAllowed is returned for a URL outside the source's host table.
var ErrHostNotAllowed = errors.New("planlimitsnet: host not allowed for this source")

// CheckURL enforces the URL policy for a source: HTTPS, no user info, port
// 443 or none, no fragment, and an exact host from the source row.
func CheckURL(src planlimits.SourceDef, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, err.Error())
	}
	if u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return ErrHostNotAllowed
	}
	if p := u.Port(); p != "" && p != "443" {
		return ErrHostNotAllowed
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range src.Hosts {
		if host == strings.ToLower(h) {
			return nil
		}
	}
	return ErrHostNotAllowed
}

// Do performs every request of one refresh, sequentially, and returns the
// responses by key. A refused request gets a Response with Err set; nothing
// is sent for it.
func (c *Client) Do(ctx context.Context, src planlimits.SourceDef, reqs []planlimits.Request) map[string]planlimits.Response {
	out := make(map[string]planlimits.Response, len(reqs))
	for _, r := range reqs {
		switch r.Kind {
		case planlimits.ReqHTTP:
			out[r.Key] = c.doHTTP(ctx, src, r)
		case planlimits.ReqExec:
			out[r.Key] = c.doExec(ctx, src, r)
		default:
			out[r.Key] = planlimits.Response{Key: r.Key, Err: fmt.Errorf("%w: unknown request kind", planlimits.ErrShape)}
		}
	}
	return out
}

func (c *Client) doHTTP(ctx context.Context, src planlimits.SourceDef, r planlimits.Request) planlimits.Response {
	resp := planlimits.Response{Key: r.Key}
	if err := CheckURL(src, r.URL); err != nil {
		resp.Err = err
		return resp
	}
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if len(r.Body) > 0 {
		body = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.URL, body)
	if err != nil {
		resp.Err = fmt.Errorf("%w: %s", planlimits.ErrTransient, err.Error())
		return resp
	}
	for k, v := range r.Header {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "superbased-observer-plans/"+strings.TrimSpace(c.version))
	for k, v := range r.SecretHeaders {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		// The error text can echo the URL; never a header.
		resp.Err = fmt.Errorf("%w: %s", planlimits.ErrTransient, errClass(err))
		return resp
	}
	defer res.Body.Close()
	resp.Status = res.StatusCode
	resp.RetryAfter = ParseRetryAfter(res.Header.Get("Retry-After"), time.Now())
	b, err := io.ReadAll(io.LimitReader(res.Body, MaxBody+1))
	if err != nil {
		resp.Err = fmt.Errorf("%w: read body", planlimits.ErrTransient)
		return resp
	}
	if len(b) > MaxBody {
		resp.Err = fmt.Errorf("%w: body too large", planlimits.ErrShape)
		return resp
	}
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		resp.Err = fmt.Errorf("%w: redirect refused", planlimits.ErrShape)
		return resp
	}
	resp.Body = b
	return resp
}

// errClass reduces a transport error to a short class, never a URL with a
// query or a header.
func errClass(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return "timeout"
		}
		return "network error"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "network error"
}

// ParseRetryAfter reads a Retry-After header in seconds or as an HTTP date.
func ParseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

// envAllow are the variables a vendor command inherits.
var envAllow = []string{
	"PATH", "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME",
	"XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "CODEX_HOME", "LANG", "LC_ALL", "SYSTEMROOT", "TMPDIR", "TEMP", "TMP",
}

func (c *Client) env(extra []string) []string {
	var out []string
	for _, k := range envAllow {
		if v := c.getenv(k); v != "" {
			out = append(out, k+"="+v)
		}
	}
	return append(out, extra...)
}

// lookPath resolves a command on PATH, skipping excluded directories.
func (c *Client) lookPath(name string) (string, error) {
	for _, dir := range filepath.SplitList(c.getenv("PATH")) {
		if dir == "" || c.exclude[filepath.Clean(dir)] {
			continue
		}
		if p, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: %s is not installed", planlimits.ErrUnavailable, name)
}

// command checks that argv runs the source's own command and resolves it.
func (c *Client) command(src planlimits.SourceDef, argv []string) (string, error) {
	if len(argv) == 0 || src.Command == "" || argv[0] != src.Command {
		return "", fmt.Errorf("%w: command not allowed for this source", planlimits.ErrShape)
	}
	return c.resolve(argv[0])
}

func (c *Client) doExec(ctx context.Context, src planlimits.SourceDef, r planlimits.Request) planlimits.Response {
	resp := planlimits.Response{Key: r.Key}
	bin, err := c.command(src, r.Argv)
	if err != nil {
		resp.Err = err
		return resp
	}
	timeout := r.Timeout
	if timeout <= 0 || timeout > time.Minute {
		timeout = 20 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, r.Argv[1:]...) //nolint:gosec // G204: argv is the registry's fixed vector, checked against the source row
	cmd.Env = c.env(r.Env)
	cmd.Stdin = bytes.NewReader(nil)
	var out capBuffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	resp.Body = out.Bytes()
	var ee *exec.ExitError
	switch {
	case cctx.Err() != nil:
		resp.Err = fmt.Errorf("%w: %s timed out", planlimits.ErrTransient, src.Command)
	case errors.As(err, &ee):
		resp.Status = ee.ExitCode() // the parser still reads the output (a sign-in prompt)
	case err != nil:
		resp.Err = fmt.Errorf("%w: %s did not start", planlimits.ErrUnavailable, src.Command)
	}
	return resp
}

// Converse runs a driver's process session (the codex app-server) over the
// process's stdin and stdout, with a timeout.
func (c *Client) Converse(ctx context.Context, src planlimits.SourceDef, d planlimits.Driver, in planlimits.PlanInput) (planlimits.Snapshot, error) {
	bin, err := c.command(src, d.Argv)
	if err != nil {
		return planlimits.Snapshot{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, d.Argv[1:]...) //nolint:gosec // G204: argv is the registry's fixed vector, checked against the source row
	cmd.Env = c.env(nil)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return planlimits.Snapshot{}, fmt.Errorf("%w: %s", planlimits.ErrTransient, err.Error())
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return planlimits.Snapshot{}, fmt.Errorf("%w: %s", planlimits.ErrTransient, err.Error())
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return planlimits.Snapshot{}, fmt.Errorf("%w: %s did not start", planlimits.ErrUnavailable, src.Command)
	}
	// A copy: the caller's parameter map (an account's stored settings) is
	// never written to.
	params := make(map[string]string, len(in.Params)+1)
	for k, v := range in.Params {
		params[k] = v
	}
	params["observer_version"] = c.version
	in.Params = params
	snap, cerr := d.Converse(stdin, io.LimitReader(stdout, 8*MaxBody), in)
	_ = stdin.Close()
	cancel()
	_ = cmd.Wait()
	if cerr != nil && cctx.Err() != nil && !errors.Is(cerr, planlimits.ErrNotApplicable) {
		return snap, fmt.Errorf("%w: %s timed out", planlimits.ErrTransient, src.Command)
	}
	return snap, cerr
}

// capBuffer keeps at most MaxBody bytes.
type capBuffer struct{ b bytes.Buffer }

func (c *capBuffer) Write(p []byte) (int, error) {
	if room := MaxBody - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

// Bytes returns the captured output.
func (c *capBuffer) Bytes() []byte { return c.b.Bytes() }
