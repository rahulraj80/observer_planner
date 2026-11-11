package sandhold

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// box.go is the SandHold row's wire client: sandboxremote.Client over one
// node and one API key, the hosted core's only seam to the node (create,
// get, delete, files, exec and the terminal). It never prints the key, sends
// it only to the node it was given for, over https (http for this machine
// only), and follows no redirect with it; every word the node sends is
// stripped of control characters before anyone sees it, except the raw
// output of the user's own command.

// The bounds of the box calls (variables so tests shorten them).
var (
	// createWait bounds a create: the node answers once the sandbox's
	// command service answers, a cold start included.
	createWait = 3 * time.Minute
	// callWait bounds a small JSON call (get, list, delete).
	callWait = time.Minute
	// fileWait bounds one file upload or download.
	fileWait = 5 * time.Minute
	// deleteTries bounds the deletes of a box the node gave less than asked.
	deleteTries = 3
)

// maxUploadFile is the most one PUT carries (the node's own limit is
// 100 MiB; a tar batch stays under it).
const maxUploadFile = 64 << 20

// MetadataLabel is the create metadata key the run id is sent under.
const MetadataLabel = sandboxremote.LabelKey

// Box is the wire client of one node.
type Box struct {
	api *Client
	// tar caches, per sandbox, whether the box has tar (a batch upload).
	tar sync.Map
}

// NewBox returns the wire client of node (normalised by NodeURL) with key.
// The HTTP client has no overall timeout, since an exec stream and a
// terminal run as long as their command; every call is bounded by its
// context.
func NewBox(node, key string) (*Box, error) {
	u, err := NodeURL(node)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("sandhold: no API key for " + u)
	}
	return &Box{api: &Client{URL: u, Key: key, HTTP: streamHTTPClient()}}, nil
}

// streamHTTPClient follows no redirect and has no overall timeout.
func streamHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// String names the client without its key.
func (b *Box) String() string { return "sandhold box client for " + b.api.URL }

var _ sandboxremote.Client = (*Box)(nil)

// netWords are the node network words a box of each tier may carry: none
// and proxy_only block the box's own egress (the relay carries the model
// proxy), so only none will do; on internet any answer is accepted and said.
var netWords = map[vocab.Tier][]string{
	vocab.TierNone:      {NetworkNone},
	vocab.TierProxyOnly: {NetworkNone},
	vocab.TierInternet:  {NetworkNone, NetworkLimited, NetworkOpen},
}

// createRequest maps a box spec onto the node's create.
func createRequest(spec sandboxremote.BoxSpec) (CreateRequest, error) {
	req := CreateRequest{Template: spec.Template, TimeoutSeconds: spec.TimeoutSeconds, Env: spec.Env, Metadata: spec.Labels}
	if spec.Isolation != "" {
		w, ok := TierForClass(spec.Isolation)
		if !ok {
			return CreateRequest{}, fmt.Errorf("sandhold: no SandHold isolation gives class %s", spec.Isolation)
		}
		req.Isolation = w
	}
	if _, ok := netWords[spec.Tier]; !ok {
		return CreateRequest{}, fmt.Errorf("sandhold: no %s network tier on a SandHold node", spec.Tier)
	}
	if spec.Tier == vocab.TierInternet {
		req.Network = &CreateNetwork{Open: true}
	}
	return req, nil
}

// checkGot refuses a box the node says got less than the spec asked: an
// isolation below the class asked for (or none reported when one was
// asked), or a network more open than the tier allows (or none reported).
// It never claims more than the node said.
func checkGot(spec sandboxremote.BoxSpec, sb Sandbox) error {
	if spec.Isolation != "" {
		got, ok := ClassForTier(sb.Isolation)
		switch {
		case !ok:
			return fmt.Errorf("the node did not say which isolation sandbox %s got (asked for %s)", sb.ID, TierTitle(classTier[spec.Isolation]))
		case got.Rank() < spec.Isolation.Rank():
			return fmt.Errorf("the node gave sandbox %s %s isolation, below the %s asked for", sb.ID, TierTitle(sb.Isolation), TierTitle(classTier[spec.Isolation]))
		}
	}
	allowed := netWords[spec.Tier]
	for _, w := range allowed {
		if strings.EqualFold(sb.Network, w) {
			return nil
		}
	}
	if sb.Network == "" {
		return fmt.Errorf("the node did not say which network sandbox %s got (the %s tier needs network none)", sb.ID, spec.Tier)
	}
	return fmt.Errorf("the node gave sandbox %s network %s, more open than the %s tier allows (network none)", sb.ID, sb.Network, spec.Tier)
}

// info is a sandbox as the hosted core records it.
func info(sb Sandbox) sandboxremote.BoxInfo {
	extra := map[string]string{}
	for k, v := range map[string]string{"isolation": sb.Isolation, "network": sb.Network, "runs_on": sb.Where()} {
		if v != "" {
			extra[k] = v
		}
	}
	return sandboxremote.BoxInfo{
		ID: sb.ID, Running: strings.EqualFold(sb.State, "Running"),
		Extra: extra, Deadline: sb.ExpiresAt, Described: sb.Words(),
	}
}

// Create starts a sandbox with the isolation and network the spec asks for,
// and refuses (deleting it) a sandbox the node says got less.
func (b *Box) Create(ctx context.Context, spec sandboxremote.BoxSpec) (sandboxremote.BoxInfo, error) {
	req, err := createRequest(spec)
	if err != nil {
		return sandboxremote.BoxInfo{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, createWait)
	sb, err := b.api.CreateSandbox(cctx, req)
	cancel()
	if err != nil {
		var ae *APIError
		switch {
		case errors.As(err, &ae) && ae.Status == http.StatusNotFound && spec.Template != "":
			return sandboxremote.BoxInfo{}, fmt.Errorf("%w: %w", sandboxremote.ErrTemplateUnavailable, err)
		case errors.As(err, &ae) && ae.Status == http.StatusTooManyRequests:
			return sandboxremote.BoxInfo{}, fmt.Errorf("%w: %w", sandboxremote.ErrRateLimited, err)
		case errors.As(err, &ae), errors.Is(err, ErrNotSent), errors.Is(err, ErrRedirect):
			return sandboxremote.BoxInfo{}, fmt.Errorf("the node refused the sandbox: %w", err)
		}
		// The request went out and no answer came back: the node may have
		// made the sandbox.
		return sandboxremote.BoxInfo{}, fmt.Errorf("%w: %w", sandboxremote.ErrCreateUnknown, err)
	}
	if cerr := checkGot(spec, sb); cerr != nil {
		var derr error
		for i := 0; i < deleteTries; i++ {
			dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callWait)
			derr = b.Delete(dctx, sb.ID)
			cancel()
			if derr == nil || errors.Is(derr, sandboxremote.ErrNotFound) {
				return sandboxremote.BoxInfo{}, fmt.Errorf("%w: %s; the sandbox was deleted", sandboxremote.ErrRefused, cerr.Error())
			}
		}
		return sandboxremote.BoxInfo{}, fmt.Errorf("%w: %s, and it could not be deleted (%s): remove it with `sandhold rm %s` or in the node's portal", sandboxremote.ErrRefused, cerr.Error(), PlainLine(derr.Error()), sb.ID)
	}
	return info(sb), nil
}

// notFound maps the node's 404 to ErrNotFound.
func notFound(err error) error {
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return fmt.Errorf("%w: %w", sandboxremote.ErrNotFound, err)
	}
	return err
}

// Get reads a sandbox.
func (b *Box) Get(ctx context.Context, id string) (sandboxremote.BoxInfo, error) {
	cctx, cancel := context.WithTimeout(ctx, callWait)
	defer cancel()
	sb, err := b.api.GetSandbox(cctx, id)
	if err != nil {
		return sandboxremote.BoxInfo{}, notFound(err)
	}
	return info(sb), nil
}

// ListByLabel cannot find a box by its run label: the node's list carries
// no metadata (a gap reported to SandHold). It says so instead of guessing.
func (b *Box) ListByLabel(context.Context, string, string) ([]sandboxremote.BoxInfo, error) {
	return nil, fmt.Errorf("%w: the node's sandbox list carries no metadata, so a box cannot be found by its run label; look for it with `sandhold ls` or in the node's portal", sandboxremote.ErrNotSupported)
}

// Delete removes a sandbox (ErrNotFound when the node no longer has it).
func (b *Box) Delete(ctx context.Context, id string) error {
	cctx, cancel := context.WithTimeout(ctx, callWait)
	defer cancel()
	return notFound(b.api.DeleteSandbox(cctx, id))
}

// Reattach is not offered: the node ends a terminal session when its
// socket goes.
func (b *Box) Reattach(context.Context, string, string) (sandboxremote.Process, error) {
	return nil, fmt.Errorf("sandhold: a terminal re-attach: %w", sandboxremote.ErrNotSupported)
}

// ---- files -----------------------------------------------------------------

// putFile writes one file (PUT /v1/sandboxes/{id}/files?path=&mode=, a raw
// body), as the node's sandbox user.
func (b *Box) putFile(ctx context.Context, id, p string, mode uint32, size int64, body io.Reader) error {
	sp, err := sandboxPath(id)
	if err != nil {
		return err
	}
	if !path.IsAbs(p) || path.Clean(p) != p {
		return fmt.Errorf("sandhold: upload path %q is not a clean absolute path", p)
	}
	q := url.Values{"path": {p}, "mode": {strconv.FormatUint(uint64(mode&0o7777), 8)}}
	cctx, cancel := context.WithTimeout(ctx, fileWait)
	defer cancel()
	res, err := b.api.send(cctx, request{method: http.MethodPut, path: sp + "/files", query: q, body: body, size: size, contentType: "application/octet-stream"})
	if err != nil {
		return notFound(err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxReply))
	return res.Body.Close()
}

// WriteFiles writes files into the box. One file (a secret, an upload part)
// is one PUT; several are packed into tar batches, each one PUT and one
// unpack, when the box has tar, so a workspace of thousands of files is not
// thousands of calls.
func (b *Box) WriteFiles(ctx context.Context, id string, files []sandboxremote.UploadFile) error {
	if len(files) > 1 && b.hasTar(ctx, id) {
		return b.writeTar(ctx, id, files)
	}
	for _, f := range files {
		if f.Size > maxUploadFile {
			return fmt.Errorf("sandhold: %s is %d bytes, over the %d a single upload carries", f.Path, f.Size, maxUploadFile)
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = b.putFile(ctx, id, f.Path, f.Mode, f.Size, rc)
		_ = rc.Close()
		if err != nil {
			return fmt.Errorf("sandhold: upload %s: %w", f.Path, err)
		}
	}
	return nil
}

// hasTar reports (once per box) whether the box has tar.
func (b *Box) hasTar(ctx context.Context, id string) bool {
	if v, ok := b.tar.Load(id); ok {
		return v.(bool)
	}
	out, code, err := b.run(ctx, id, []string{"sh", "-c", "command -v tar >/dev/null 2>&1 && echo yes"}, 256)
	has := err == nil && code == 0 && strings.TrimSpace(string(out)) == "yes"
	b.tar.Store(id, has)
	return has
}

// unpackScript unpacks a batch at the root as the box user, then removes
// it. Argument: the batch's path.
const unpackScript = `tar -xf "$1" -C / --no-same-owner 2>&1; c=$?; rm -f -- "$1"; exit $c`

// writeTar packs files into batches under maxUploadFile and unpacks each.
func (b *Box) writeTar(ctx context.Context, id string, files []sandboxremote.UploadFile) error {
	for len(files) > 0 {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		n := 0
		for n < len(files) {
			f := files[n]
			if n > 0 && int64(buf.Len())+f.Size+1024 > maxUploadFile {
				break
			}
			if f.Size > maxUploadFile-1024 {
				return fmt.Errorf("sandhold: %s is %d bytes, over the %d a single upload carries", f.Path, f.Size, maxUploadFile)
			}
			if !path.IsAbs(f.Path) || path.Clean(f.Path) != f.Path {
				return fmt.Errorf("sandhold: upload path %q is not a clean absolute path", f.Path)
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(rc, f.Size+1))
			_ = rc.Close()
			if err != nil {
				return err
			}
			if int64(len(data)) != f.Size {
				return fmt.Errorf("sandhold: %s changed size while it was read", f.Path)
			}
			if err := tw.WriteHeader(&tar.Header{Name: strings.TrimPrefix(f.Path, "/"), Mode: int64(f.Mode & 0o7777), Size: f.Size, Typeflag: tar.TypeReg, ModTime: time.Now()}); err != nil {
				return err
			}
			if _, err := tw.Write(data); err != nil {
				return err
			}
			n++
		}
		if err := tw.Close(); err != nil {
			return err
		}
		batch := "/tmp/.observer-upload-" + nonce() + ".tar"
		if err := b.putFile(ctx, id, batch, 0o600, int64(buf.Len()), bytes.NewReader(buf.Bytes())); err != nil {
			return fmt.Errorf("sandhold: upload a batch of %d files: %w", n, err)
		}
		out, code, err := b.run(ctx, id, []string{"sh", "-c", unpackScript, "sh", batch}, 4096)
		if err != nil {
			return fmt.Errorf("sandhold: unpack a batch of %d files: %w", n, err)
		}
		if code != 0 {
			return fmt.Errorf("sandhold: unpacking a batch of %d files exited %d: %s", n, code, PlainLine(string(out)))
		}
		files = files[n:]
	}
	return nil
}

// ReadFile reads one file (GET /v1/sandboxes/{id}/files?path=), at most max
// bytes.
func (b *Box) ReadFile(ctx context.Context, id, p string, max int64) ([]byte, error) {
	sp, err := sandboxPath(id)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, fileWait)
	defer cancel()
	res, err := b.api.send(cctx, request{method: http.MethodGet, path: sp + "/files", query: url.Values{"path": {p}}, size: -1, accept: "application/octet-stream"})
	if err != nil {
		return nil, notFound(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %s", ErrUnreachable, b.api.URL, Plain(err.Error()))
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("sandhold: %s is over %d bytes", p, max)
	}
	return data, nil
}

// run is a one-shot command's output (stdout and stderr together, capped)
// and exit code.
func (b *Box) run(ctx context.Context, id string, argv []string, limit int64) ([]byte, int, error) {
	p, err := b.Exec(ctx, id, sandboxremote.ExecSpec{Argv: argv})
	if err != nil {
		return nil, 0, err
	}
	defer p.Close()
	out, _ := io.ReadAll(io.LimitReader(p, limit))
	code, err := p.Wait(ctx)
	return out, code, err
}

// nonce is a fresh random name part.
func nonce() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
