package sandhold

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// sandbox.go is the node's sandbox lifecycle as its customer API answers it
// (contract vectors sandbox-create, sandbox-create-refused, sandbox-get,
// sandbox-list, sandbox-delete, sandbox-missing): create, get, list and
// delete, each sandbox in the node's own words (note 41 D4).

// Sandbox is one sandbox as the node describes it to its member.
type Sandbox struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Image string `json:"image"`
	Owner string `json:"owner"`
	// Isolation is the node's tier word (shared, shielded, sealed, vault),
	// IsolationMeans its one phrase for it.
	Isolation      string `json:"isolation"`
	IsolationMeans string `json:"isolationMeans"`
	// Network is the node's network word (none, limited, open).
	Network string `json:"network"`
	// RunsOn is the target; Engine and Target are its older names.
	RunsOn string `json:"runsOn"`
	Engine string `json:"engine"`
	Target string `json:"target"`
	// Metadata is the caller's own labels from the create (a node older
	// than 2026-11-11 sends none).
	Metadata  map[string]string `json:"metadata"`
	CreatedAt time.Time         `json:"createdAt"`
	ExpiresAt time.Time         `json:"expiresAt"`
	// NetworkNote, on a create's answer only, says the sandbox did not get
	// the network asked for (contract sandbox-create-open-not-granted).
	NetworkNote *NetworkNote `json:"networkNote,omitempty"`
}

// NetworkNote is the node's coded word that a create got a different
// network than it asked for: what was asked and got, why, and what would
// give it.
type NetworkNote struct {
	Code  string `json:"code"`
	Asked string `json:"asked"`
	Got   string `json:"got"`
	Why   string `json:"why"`
	Fix   string `json:"fix"`
}

// NetworkOpenNotGranted is the note's code for an open network asked for
// and not given.
const NetworkOpenNotGranted = "network_open_not_granted"

// clean removes control characters from every word the node sent.
func (s *Sandbox) clean() {
	for _, p := range []*string{&s.ID, &s.State, &s.Image, &s.Owner, &s.Isolation, &s.IsolationMeans, &s.Network, &s.RunsOn, &s.Engine, &s.Target} {
		*p = PlainLine(*p)
	}
	if len(s.Metadata) > 0 {
		md := make(map[string]string, len(s.Metadata))
		for k, v := range s.Metadata {
			md[PlainLine(k)] = PlainLine(v)
		}
		s.Metadata = md
	}
	if n := s.NetworkNote; n != nil {
		for _, p := range []*string{&n.Code, &n.Asked, &n.Got, &n.Why, &n.Fix} {
			*p = PlainLine(*p)
		}
	}
}

// Where is the target the sandbox runs on ("" when the node names none).
func (s Sandbox) Where() string {
	for _, w := range []string{s.RunsOn, s.Target, s.Engine} {
		if w != "" {
			return w
		}
	}
	return ""
}

// Words says how the sandbox runs, in the node's words only: "Shielded (a
// hardened container on the host's kernel), network none, runs on runc".
func (s Sandbox) Words() string {
	var parts []string
	if s.Isolation != "" {
		p := TierTitle(s.Isolation)
		if s.IsolationMeans != "" {
			p += " (" + s.IsolationMeans + ")"
		}
		parts = append(parts, p)
	} else {
		parts = append(parts, "isolation not reported")
	}
	if s.Network != "" {
		parts = append(parts, "network "+s.Network)
	} else {
		parts = append(parts, "network not reported")
	}
	if w := s.Where(); w != "" {
		parts = append(parts, "runs on "+w)
	}
	out := strings.Join(parts, ", ")
	if n := s.NetworkNote; n != nil && n.Code != "" {
		// The node's coded reason the network differs from the one asked
		// for, so the run says why (an open network not granted).
		out += "; " + n.Words()
	}
	return PlainLine(out)
}

// Words is the note for people: "network open was asked for, got none
// (network_open_not_granted): <the node's why>".
func (n NetworkNote) Words() string {
	w := "network"
	if n.Asked != "" {
		w += " " + n.Asked + " was asked for"
	} else {
		w += " differs from the one asked for"
	}
	if n.Got != "" {
		w += ", got " + n.Got
	}
	w += " (" + n.Code + ")"
	if n.Why != "" {
		w += ": " + n.Why
	}
	return PlainLine(w)
}

// CreateRequest is what POST /v1/sandboxes is sent. A template names a
// saved setup; what the request itself says wins field by field (note 43).
type CreateRequest struct {
	Template       string            `json:"template,omitempty"`
	Image          string            `json:"image,omitempty"`
	TimeoutSeconds int               `json:"timeoutSeconds,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	// Isolation is the node's tier word ("" lets the node place it).
	Isolation string `json:"isolation,omitempty"`
	// Target names one of the node's targets (`observer sandbox targets`);
	// empty lets the node place the sandbox.
	Target string `json:"target,omitempty"`
	// Placement is the long form of Isolation: a measured isolation level
	// (container, user-kernel, microvm) the node must meet.
	Placement *CreatePlacement `json:"placement,omitempty"`
	Network   *CreateNetwork   `json:"network,omitempty"`
}

// CreatePlacement is a create's placement member.
type CreatePlacement struct {
	Require struct {
		Isolation string `json:"isolation,omitempty"`
	} `json:"require"`
}

// CreateNetwork is a create's network member. No allow list keeps the
// node's deny-by-default egress; Open accepts an open network on a target
// that cannot enforce one.
type CreateNetwork struct {
	Allow []string `json:"allow,omitempty"`
	Open  bool     `json:"open,omitempty"`
}

// sandboxIDPattern is the shape of a sandbox id before it becomes part of an
// address.
var sandboxIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidSandboxID reports whether id may be put in a path.
func ValidSandboxID(id string) bool {
	return sandboxIDPattern.MatchString(id) && !strings.Contains(id, "..")
}

func sandboxPath(id string) (string, error) {
	if !ValidSandboxID(id) {
		return "", fmt.Errorf("%q is not a sandbox id", PlainLine(id))
	}
	return "/v1/sandboxes/" + id, nil
}

// CreateSandbox starts a sandbox. The node answers once the sandbox's
// command service answers, so ctx should allow for a cold start.
func (c *Client) CreateSandbox(ctx context.Context, req CreateRequest) (Sandbox, error) {
	var sb Sandbox
	if err := c.Do(ctx, http.MethodPost, "/v1/sandboxes", req, &sb); err != nil {
		return Sandbox{}, err
	}
	sb.clean()
	if !ValidSandboxID(sb.ID) {
		return Sandbox{}, fmt.Errorf("%s answered the create with no usable sandbox id", c.URL)
	}
	return sb, nil
}

// GetSandbox reads one sandbox.
func (c *Client) GetSandbox(ctx context.Context, id string) (Sandbox, error) {
	p, err := sandboxPath(id)
	if err != nil {
		return Sandbox{}, err
	}
	var sb Sandbox
	if err := c.Do(ctx, http.MethodGet, p, nil, &sb); err != nil {
		return Sandbox{}, err
	}
	sb.clean()
	return sb, nil
}

// ListSandboxes lists the member's own sandboxes.
func (c *Client) ListSandboxes(ctx context.Context) ([]Sandbox, error) {
	return c.listSandboxes(ctx, "")
}

// ListSandboxesBy lists the member's sandboxes that carry the metadata
// label key = value (GET /v1/sandboxes?metadata=key=value). A node older
// than the filter ignores it and lists them all; the caller tells by the
// labels on what comes back.
func (c *Client) ListSandboxesBy(ctx context.Context, key, value string) ([]Sandbox, error) {
	return c.listSandboxes(ctx, "?"+url.Values{"metadata": {key + "=" + value}}.Encode())
}

func (c *Client) listSandboxes(ctx context.Context, query string) ([]Sandbox, error) {
	var out struct {
		Sandboxes []Sandbox `json:"sandboxes"`
	}
	if err := c.Do(ctx, http.MethodGet, "/v1/sandboxes"+query, nil, &out); err != nil {
		return nil, err
	}
	for i := range out.Sandboxes {
		out.Sandboxes[i].clean()
	}
	return out.Sandboxes, nil
}

// DeleteSandbox removes a sandbox.
func (c *Client) DeleteSandbox(ctx context.Context, id string) error {
	p, err := sandboxPath(id)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil)
}
