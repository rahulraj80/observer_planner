package orgclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrServerUnreachable reports that nothing answered at the org server URL
// (DNS failure, connection refused, TLS failure, timeout).
var ErrServerUnreachable = errors.New("orgclient: organization server unreachable")

// ErrNotOrgServer reports that something answered at the URL, but not an
// Observer organization server (its unauthenticated /healthz did not answer
// the way one does).
var ErrNotOrgServer = errors.New("orgclient: not an Observer organization server")

// probeTimeout bounds the reachability probe: it is a pre-flight sanity check
// in front of a human, not a long-running call.
const probeTimeout = 8 * time.Second

// ProbeServer checks, before an enrolment code is spent, that orgURL is an
// Observer organization server this machine can reach: one GET of the
// server's unauthenticated /healthz, which every org-server role serves and
// which answers {"status":"ok", ...}. It sends no credential and changes no
// state. A nil error means "reachable and looks like an org server".
//
// It uses the client's own HTTP client, so any transport configuration the
// enrolment exchange would use applies to the probe too.
//
// /healthz alone is not enough (T-718, the 2026-10-09 dashboard re-enrol
// failure): the shipped HAProxy front end answers /healthz ITSELF
// (monitor-uri, deploy/modules/_shared/azure/cloudinit/haproxy.yaml.tftpl
// D27), so on such an estate the public /healthz is HAProxy's HTML, the probe
// called a real org server "not an Observer organization server", and the
// dashboard refused a link the CLI (which never probes) enrolled with. The
// probe therefore asks /readyz, which no front end shadows, when /healthz does
// not answer like an org server; either one answering the org's JSON shape is
// an org server. The error names what each path answered.
func (c *Client) ProbeServer(ctx context.Context, orgURL string) error {
	base := strings.TrimRight(strings.TrimSpace(orgURL), "/")
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	hzStatus, hzOK, err := c.probePath(ctx, base, "/healthz", func(status int, s string) bool {
		return status == http.StatusOK && s == "ok"
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrServerUnreachable, err)
	}
	if hzOK {
		return nil
	}
	// /readyz answers {"status":"ok"} or, while the org is still starting,
	// 503 {"status":"not-ready"}: both are an Observer organization server.
	rzStatus, rzOK, err := c.probePath(ctx, base, "/readyz", func(status int, s string) bool {
		return (status == http.StatusOK && s == "ok") || (status == http.StatusServiceUnavailable && s == "not-ready")
	})
	if err == nil && rzOK {
		return nil
	}
	return fmt.Errorf("%w: %s/healthz answered %s and /readyz answered %s, neither like an Observer organization server",
		ErrNotOrgServer, base, hzStatus, rzStatus)
}

// probePath GETs one unauthenticated path and reports a short description of
// the answer, whether it matched, and a transport error.
func (c *Client) probePath(ctx context.Context, base, path string, match func(status int, jsonStatus string) bool) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10)) //nolint:errcheck // a short read just fails the shape check below
	var hz struct {
		Status string `json:"status"`
	}
	if jerr := json.Unmarshal(body, &hz); jerr != nil {
		return fmt.Sprintf("HTTP %d without JSON", resp.StatusCode), false, nil
	}
	return fmt.Sprintf("HTTP %d status=%q", resp.StatusCode, hz.Status), match(resp.StatusCode, hz.Status), nil
}
