package sandhold

import (
	"context"
	"fmt"
	"net/http"
)

// Whoami is GET /v1/whoami: who the key belongs to.
type Whoami struct {
	Org   string `json:"org"`
	User  string `json:"user"`
	Admin bool   `json:"admin"`
	Via   string `json:"via"`
	KeyID string `json:"keyId"`
}

// Target is one entry of GET /v1/targets, in the node's own words.
type Target struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Tier is the member-facing isolation: shared, shielded, sealed or vault.
	Tier string `json:"tier"`
	// TierMeans is the node's one phrase for the tier.
	TierMeans string `json:"tierMeans"`
	TierRank  int    `json:"tierRank"`
	State     string `json:"state"`
	Default   bool   `json:"default"`
	Foreign   bool   `json:"foreign"`
	About     struct {
		Title    string   `json:"title"`
		Benefits []string `json:"benefits"`
		Limits   []string `json:"limits"`
	} `json:"about"`
	Capabilities struct {
		// Isolation is the measured or configured level: process,
		// container, user-kernel or microvm. More exact than the tier.
		Isolation       string `json:"isolation"`
		SeparateKernel  bool   `json:"separateKernel"`
		EgressAllowlist bool   `json:"egressAllowlist"`
		PTY             bool   `json:"pty"`
	} `json:"capabilities"`
}

// Whoami asks the node who the client's key belongs to.
func (c *Client) Whoami(ctx context.Context) (Whoami, error) {
	var w Whoami
	if err := c.Do(ctx, http.MethodGet, "/v1/whoami", nil, &w); err != nil {
		return Whoami{}, err
	}
	w.Org, w.User, w.Via, w.KeyID = Plain(w.Org), Plain(w.User), Plain(w.Via), Plain(w.KeyID)
	return w, nil
}

// Targets lists where sandboxes can run on the node for this key.
func (c *Client) Targets(ctx context.Context) ([]Target, error) {
	var out struct {
		Targets []Target `json:"targets"`
	}
	if err := c.Do(ctx, http.MethodGet, "/v1/targets", nil, &out); err != nil {
		return nil, err
	}
	for i := range out.Targets {
		t := &out.Targets[i]
		t.ID, t.Name, t.Tier, t.TierMeans, t.State = PlainLine(t.ID), PlainLine(t.Name), PlainLine(t.Tier), PlainLine(t.TierMeans), PlainLine(t.State)
		t.Capabilities.Isolation = PlainLine(t.Capabilities.Isolation)
		t.About.Title = PlainLine(t.About.Title)
		for j := range t.About.Benefits {
			t.About.Benefits[j] = PlainLine(t.About.Benefits[j])
		}
		for j := range t.About.Limits {
			t.About.Limits[j] = PlainLine(t.About.Limits[j])
		}
	}
	return out.Targets, nil
}

// RevokeKey revokes key id at the node (DELETE /v1/keys/{id}). The id is
// checked before it becomes part of the address.
func (c *Client) RevokeKey(ctx context.Context, id string) error {
	if !ValidKeyID(id) {
		return fmt.Errorf("%q is not a key id", Plain(id))
	}
	return c.Do(ctx, http.MethodDelete, "/v1/keys/"+id, nil, nil)
}
