package orgclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// requests.go — W6 dev->org REQUEST/MESSAGE system, node-side client half
// (see docs/plans/org-parity-full-depth-plan-2026-08-24.md §0.4 + §4 "W6",
// internal/orgserver/api/orgrequests.go for the agent-facing server handlers,
// internal/orgserver/orgrequests_handlers.go for the admin-facing half). This
// is the node's ONLY ask channel to the org: it posts a short typed request
// and can read back its own requests + statuses. The admin never grants
// anything through this path directly — they respond by editing policy on
// the existing policy rail; PostOrgRequest / ListMyOrgRequests only carry the
// ask and its recorded status, never a grant.

// ErrOrgRequestCapReached is returned when this user already has the
// server's maximum number of OPEN requests outstanding. It is not
// retryable until an existing request is resolved or declined.
var ErrOrgRequestCapReached = errors.New("orgclient: too many open requests to the organization")

// maxOrgRequestResponseBytes caps a single request-response document /
// list-of-requests document.
const maxOrgRequestResponseBytes = 256 << 10

// OrgRequest is one dev->org request/message, as the node sees it (its own
// requests only — the server never lets a node read another user's).
type OrgRequest struct {
	ID             int64  `json:"id"`
	Kind           string `json:"kind"`
	Target         string `json:"target,omitempty"`
	Message        string `json:"message"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	ResolvedAt     string `json:"resolved_at,omitempty"`
	ResolvedBy     string `json:"resolved_by,omitempty"`
	ResolutionNote string `json:"resolution_note,omitempty"`

	// Structured request-grant fields (request-grants design §1.4); empty on
	// a legacy free-text request and on a server that predates the design.
	RequestType        string                                 `json:"request_type,omitempty"`
	SubjectKind        string                                 `json:"subject_kind,omitempty"`
	Payload            *orgcontract.AccessRequestPayload      `json:"payload,omitempty"`
	RequestedDurationS int64                                  `json:"requested_duration_s,omitempty"`
	RequestedUses      int64                                  `json:"requested_uses,omitempty"`
	Grant              *orgcontract.AccessRequestGrantSummary `json:"grant,omitempty"`
	// Duplicate is true when the server matched an identical open request
	// and returned it instead of opening a second (GRANT-7).
	Duplicate bool     `json:"duplicate,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

// RequestProblem is one field the server refused a structured request for.
type RequestProblem struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RequestRefusedError carries the server's refusal of a request on its
// merits (a 4xx the request itself caused). Problems names each field when
// the server said which; Message is the server's text otherwise. A refusal
// is never retried: retrying the same bytes gets the same answer.
type RequestRefusedError struct {
	Problems []RequestProblem
	// Status is the HTTP status the server refused with.
	Status int
	// Message is the server's own message when it named no field.
	Message string
}

func (e *RequestRefusedError) Error() string {
	parts := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		parts = append(parts, p.Field+": "+p.Message)
	}
	if len(parts) == 0 {
		if msg := strings.TrimSpace(e.Message); msg != "" {
			return "orgclient: the organization refused the request: " + msg
		}
		return "orgclient: the organization refused the request"
	}
	return "orgclient: the organization refused the request: " + strings.Join(parts, "; ")
}

// refusedStatuses are the answers that refuse a request ON ITS MERITS (review
// C156-38): the same bytes would be refused again, so the request is never
// queued or retried, with or without a per-field problems list. Everything
// else that is not a success, an auth failure or the open-request cap (a 5xx,
// an unexpected status, a transport error) is "the org is unreachable" and is
// queued for a later cycle.
var refusedStatuses = map[int]bool{
	http.StatusBadRequest:            true,
	http.StatusNotAcceptable:         true,
	http.StatusConflict:              true,
	http.StatusGone:                  true,
	http.StatusRequestEntityTooLarge: true,
	http.StatusUnsupportedMediaType:  true,
	http.StatusUnprocessableEntity:   true,
}

// PostOrgRequest sends a short typed ask to the org server (POST
// /api/agent/requests). kind is a closed vocabulary the server validates
// (enable_feature | raise_budget | allow_tool | other); an empty kind is
// server-normalised to "other", but any other unrecognised value is
// rejected -- pass "" or "other" rather than guessing at future kinds.
// target is the optional feature/tool/etc. the ask concerns; message is the
// free-form body the operator typed. Identity is stamped by the server from
// the bearer credential this node already presents on every push -- nothing
// here can spoof who is asking.
func (c *Client) PostOrgRequest(ctx context.Context, kind, target, message string) (OrgRequest, error) {
	return c.PostAccessRequest(ctx, orgcontract.AccessRequestCreate{Kind: kind, Target: target, Message: message})
}

// PostAccessRequest sends a request to the org server (POST
// /api/agent/requests): the legacy free-text shape when RequestType is empty,
// or a structured access request (request-grants design §1.4). A structured
// request the server already holds open comes back with Duplicate set; a
// refused one returns *RequestRefusedError naming each field. Identity is
// stamped by the server from this node's bearer; the machine identity is the
// org-salted fingerprint a managed enrolment already reports.
func (c *Client) PostAccessRequest(ctx context.Context, in orgcontract.AccessRequestCreate) (OrgRequest, error) {
	in.Message = strings.TrimSpace(in.Message)
	in.Kind = strings.TrimSpace(in.Kind)
	in.Target = strings.TrimSpace(in.Target)
	if in.Message == "" {
		return OrgRequest{}, errors.New("orgclient.PostOrgRequest: message is required")
	}
	enr, err := c.store.LoadEnrolment(ctx)
	if err != nil {
		return OrgRequest{}, fmt.Errorf("orgclient.PostOrgRequest: load enrolment: %w", err)
	}
	if enr == nil {
		return OrgRequest{}, ErrNotEnrolled
	}
	bearer, err := c.bearers.LoadBearer()
	if err != nil {
		return OrgRequest{}, fmt.Errorf("orgclient.PostOrgRequest: load bearer: %w", err)
	}
	if in.RequestType != "" && in.MachineIdentity == "" {
		in.MachineIdentity = orgcontract.SanitizeMachineHeader(c.ManagedMachineIdentity(ctx))
	}
	body, err := json.Marshal(in)
	if err != nil {
		return OrgRequest{}, err
	}
	url := strings.TrimRight(enr.OrgServerURL, "/") + "/api/agent/requests"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return OrgRequest{}, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return OrgRequest{}, fmt.Errorf("orgclient.PostOrgRequest: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusTooManyRequests:
		return OrgRequest{}, ErrOrgRequestCapReached
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return OrgRequest{}, ErrAuthFailed
	case refusedStatuses[resp.StatusCode]:
		// A loose decode: the body may be any JSON error shape (or none).
		var refused struct {
			Problems []RequestProblem `json:"problems"`
			Message  string           `json:"message"`
			Error    string           `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, maxOrgRequestResponseBytes)).Decode(&refused)
		return OrgRequest{}, &RequestRefusedError{
			Problems: refused.Problems, Status: resp.StatusCode,
			Message: firstNonBlank(refused.Message, refused.Error, fmt.Sprintf("server returned %d", resp.StatusCode)),
		}
	default:
		return OrgRequest{}, fmt.Errorf("orgclient.PostOrgRequest: server returned %d", resp.StatusCode)
	}

	var out OrgRequest
	if err := orgcontract.DecodeCapped(resp.Body, maxOrgRequestResponseBytes, &out); err != nil {
		return OrgRequest{}, fmt.Errorf("orgclient.PostOrgRequest: decode response: %w", err)
	}
	c.logger.Info("org request posted", "id", out.ID, "kind", out.Kind, "request_type", out.RequestType, "duplicate", out.Duplicate)
	return out, nil
}

// firstNonBlank returns the first value that is not blank.
func firstNonBlank(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// ErrRequestNotOpen is returned when withdrawing a request that is no longer
// open (already granted, declined or withdrawn).
var ErrRequestNotOpen = errors.New("orgclient: the request is no longer open")

// ErrRequestNotFound is returned when the request does not exist or is not
// this member's.
var ErrRequestNotFound = errors.New("orgclient: no such request")

// WithdrawOrgRequest withdraws one of this member's OPEN requests (POST
// /api/agent/requests/{id}/withdraw).
func (c *Client) WithdrawOrgRequest(ctx context.Context, id int64) error {
	enr, err := c.store.LoadEnrolment(ctx)
	if err != nil {
		return fmt.Errorf("orgclient.WithdrawOrgRequest: load enrolment: %w", err)
	}
	if enr == nil {
		return ErrNotEnrolled
	}
	bearer, err := c.bearers.LoadBearer()
	if err != nil {
		return fmt.Errorf("orgclient.WithdrawOrgRequest: load bearer: %w", err)
	}
	url := fmt.Sprintf("%s/api/agent/requests/%d/withdraw", strings.TrimRight(enr.OrgServerURL, "/"), id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("orgclient.WithdrawOrgRequest: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusNotFound:
		return ErrRequestNotFound
	case http.StatusConflict:
		return ErrRequestNotOpen
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrAuthFailed
	default:
		return fmt.Errorf("orgclient.WithdrawOrgRequest: server returned %d", resp.StatusCode)
	}
}

// ListMyOrgRequests reads back this node's own requests and their current
// statuses (GET /api/agent/requests). The server scopes the list to the
// bearer's identity — there is no way to ask for anyone else's.
func (c *Client) ListMyOrgRequests(ctx context.Context) ([]OrgRequest, error) {
	enr, err := c.store.LoadEnrolment(ctx)
	if err != nil {
		return nil, fmt.Errorf("orgclient.ListMyOrgRequests: load enrolment: %w", err)
	}
	if enr == nil {
		return nil, ErrNotEnrolled
	}
	bearer, err := c.bearers.LoadBearer()
	if err != nil {
		return nil, fmt.Errorf("orgclient.ListMyOrgRequests: load bearer: %w", err)
	}

	url := strings.TrimRight(enr.OrgServerURL, "/") + "/api/agent/requests"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("orgclient.ListMyOrgRequests: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrAuthFailed
	default:
		return nil, fmt.Errorf("orgclient.ListMyOrgRequests: server returned %d", resp.StatusCode)
	}

	var out struct {
		Requests []OrgRequest `json:"requests"`
	}
	if err := orgcontract.DecodeCapped(resp.Body, maxOrgRequestResponseBytes, &out); err != nil {
		return nil, fmt.Errorf("orgclient.ListMyOrgRequests: decode response: %w", err)
	}
	return out.Requests, nil
}
