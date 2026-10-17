package orgclient

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// intelDigestsRail names the developer digest rail for the not-supported log.
const intelDigestsRail = "intel-digests"

// intelDigestsMaxBodyBytes bounds the digest body (a member is matched to a
// bounded number of projects and each digest is short prose).
const intelDigestsMaxBodyBytes = 2 << 20

// DigestFetch is one developer-digest pull (T-490). State uses the result
// rail's IntelFetch* vocabulary so the dashboard renders both the same way.
type DigestFetch struct {
	State string `json:"state"`
	// Projects is how many projects the org matched this member to.
	Projects int                          `json:"projects"`
	Digests  []orgcontract.IntelDigestRow `json:"digests"`
	// Withheld carries the server's reason when nothing was looked up.
	Withheld string `json:"withheld,omitempty"`
}

// FetchProjectDigests reads the weekly org digests of the projects this
// member works in (T-490, D-328 item 3) on demand. It is a read of what the
// org already built: nothing is cached on the node and nothing leaves it
// beyond the enrolment's own proof. The request is the result rail's shape
// (bearer plus the per-request Ed25519 proof over the constant path with an
// empty query, bound to the org id), so a proof captured for another rail
// never replays here. A server that predates the rail, or an org with digests
// off, answers not_supported.
func (c *Client) FetchProjectDigests(ctx context.Context) (DigestFetch, error) {
	ident, err := c.resolveIntelRailIdentity(ctx)
	if err != nil {
		return DigestFetch{State: IntelFetchNotEnrolled}, err
	}
	ts := time.Now().Unix()
	sig := ed25519.Sign(ident.SignKey, orgcontract.IntelRailSigningMessage(
		ts, http.MethodGet, orgcontract.IntelDigestsPath, "", ident.OrgID))
	reqURL := strings.TrimRight(ident.ServerURL, "/") + orgcontract.IntelDigestsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return DigestFetch{State: IntelFetchUnreachable}, fmt.Errorf("orgclient.FetchProjectDigests: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+ident.Bearer)
	req.Header.Set(orgcontract.HeaderTimestamp, strconv.FormatInt(ts, 10))
	req.Header.Set(orgcontract.HeaderAgentSignature, base64.RawURLEncoding.EncodeToString(sig))

	resp, err := c.httpClient.Do(req)
	c.noteRenewalFromResponse(RenewalPathOther, resp, err)
	if err != nil {
		return DigestFetch{State: IntelFetchUnreachable}, fmt.Errorf("orgclient.FetchProjectDigests: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	status := c.railStatus(intelDigestsRail, resp)
	if state := classifyIntelStatus(status); state != IntelFetchOK {
		return DigestFetch{State: state}, nil
	}
	var body orgcontract.IntelDigestsResponse
	if derr := json.NewDecoder(io.LimitReader(resp.Body, intelDigestsMaxBodyBytes)).Decode(&body); derr != nil {
		return DigestFetch{State: IntelFetchMalformed}, fmt.Errorf("orgclient.FetchProjectDigests: decode: %w", derr)
	}
	if body.Digests == nil {
		body.Digests = []orgcontract.IntelDigestRow{}
	}
	return DigestFetch{State: IntelFetchOK, Projects: body.Projects, Digests: body.Digests, Withheld: body.Withheld}, nil
}
