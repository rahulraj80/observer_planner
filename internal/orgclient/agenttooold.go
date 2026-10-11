package orgclient

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// agenttooold.go is the push rail's memory of the org's version floor (the
// 426 min_agent_version refusal, W5 skew enforcement). It moved out of
// update.go (lane MP-GOV4, T-711) because it is core: the refusal arrives on
// every push whether or not this binary carries the org.update component,
// and a node built without self-update must still tell its operator why the
// org refuses it. It imports nothing from internal/update.

// refusalReasonAgentTooOld is the machine-readable reason of a min-version
// refusal. It equals update.RefusalReasonAgentTooOld (pinned by
// TestRefusalReasonMatchesUpdate in an update-tagged test), repeated here so
// this core file needs no update import.
const refusalReasonAgentTooOld = "agent_too_old"

// AgentTooOldRefusal is the last 426 this client received, if any.
//
// IN-MEMORY on purpose, the updateCache posture: it describes a live condition
// (the org's floor versus the running binary), and a restart re-learns it on
// the very next push cycle. Persisting it would risk showing a stale "you are
// too old" banner on a node that has since been updated.
type AgentTooOldRefusal struct {
	// Message is the operator-facing line, naming both versions.
	Message string
	// MinVersion is what the org requires; YourVersion is what this node
	// reported. Both are echoed so a surface can render them separately.
	MinVersion  string
	YourVersion string
	At          time.Time
}

// agentTooOldState holds the last refusal, guarded by its own lock.
type agentTooOldState struct {
	mu      sync.Mutex
	refusal *AgentTooOldRefusal
}

// noteAgentTooOld records a 426 refusal.
func (c *Client) noteAgentTooOld(body []byte, status int) string {
	msg := agentTooOldMessage(body, status)
	var b orgcontract.AgentTooOldBody
	_ = json.Unmarshal(body, &b)
	c.tooOld.mu.Lock()
	c.tooOld.refusal = &AgentTooOldRefusal{
		Message:     msg,
		MinVersion:  strings.TrimSpace(b.MinVersion),
		YourVersion: strings.TrimSpace(b.YourVersion),
		At:          time.Now().UTC(),
	}
	c.tooOld.mu.Unlock()
	return msg
}

// clearAgentTooOld forgets a refusal. Called on every ACCEPTED push, so the
// banner disappears the moment the node is updated rather than needing a
// restart or a dismissal, and on an identity change at enrolment.
func (c *Client) clearAgentTooOld() {
	c.tooOld.mu.Lock()
	c.tooOld.refusal = nil
	c.tooOld.mu.Unlock()
}

// AgentTooOld reports the last min-version refusal, or nil.
func (c *Client) AgentTooOld() *AgentTooOldRefusal {
	if c == nil {
		return nil
	}
	c.tooOld.mu.Lock()
	defer c.tooOld.mu.Unlock()
	return c.tooOld.refusal
}

// agentTooOldMessage renders a 426 refusal for the node's own operator.
//
// The server sends orgcontract.AgentTooOldBody, a SUPERSET of the standard
// {error, message} shape, so this reads the typed fields when they are there
// and falls back to serverError's generic rendering when they are not: a
// server that answers 426 for some other reason still produces a usable line
// rather than an empty one.
//
// The message names BOTH versions on purpose: the developer reading it in
// `observer status` or on the dashboard banner is not the admin who set the
// floor, and "426" tells them nothing they can act on.
func agentTooOldMessage(body []byte, status int) string {
	var b orgcontract.AgentTooOldBody
	if err := json.Unmarshal(body, &b); err == nil && b.Reason == refusalReasonAgentTooOld {
		your := strings.TrimSpace(b.YourVersion)
		if your == "" {
			your = "unknown"
		}
		msg := fmt.Sprintf("your org requires observer %s or newer; this node reports %s",
			strings.TrimSpace(b.MinVersion), your)
		if m := strings.TrimSpace(b.Message); m != "" {
			return msg + " (" + m + ")"
		}
		return msg
	}
	return serverError(body, status)
}

// ShareOptions exposes the node's resolved push-share posture.
//
// Its caller is the update wiring, which needs to know whether this node is
// admin-managed or enterprise-granted in order to resolve
// [update].auto_apply's DEFAULT (section 3.9). Those two facts are not both in
// the TOML (EnterpriseGranted arrives on the runtime enrolment grant), so a
// caller that read the config alone would silently answer "BYO" for half the
// managed fleet. Resolving it through the SAME accessor the push seam uses is
// what keeps the two from disagreeing.
func (c *Client) ShareOptions() store.ShareOptions { return c.shareOptions() }
