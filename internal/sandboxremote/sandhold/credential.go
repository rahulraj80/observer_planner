package sandhold

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// credential.go is what a connect keeps, in two halves: the Credential (the
// key, a secret, kept in Observer's keystore) and the Connection (who and
// which node and what it offers, no secret, kept as an owner-only state file
// so the backend row can be described without opening the keychain or
// calling the node).

// KeystoreService is the keychain service the key is stored under.
const KeystoreService = "sbo-sandbox-sandhold-v1"

// KeystoreRecord is the record name under KeystoreService.
const KeystoreRecord = "credential"

// The environment variables that win over a saved sign-in (note 41 D3: an
// API key in the environment always wins, for CI).
const (
	EnvURL = "SANDHOLD_URL"
	EnvKey = "SANDHOLD_API_KEY" //nolint:gosec // G101: an environment variable name, not a credential
)

// Credential is a saved key and the node it was issued by. It never prints
// its key: String, GoString and every fmt verb name the node only.
type Credential struct {
	URL   string
	Key   string
	KeyID string
}

// String names the credential without its key.
func (c Credential) String() string { return "sandhold credential for " + c.URL }

// GoString is String.
func (c Credential) GoString() string { return c.String() }

// Format prints String for every verb.
func (c Credential) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(c.String())) }

type storedCredential struct {
	Version int    `json:"version"`
	URL     string `json:"url"`
	Key     string `json:"key"`
	KeyID   string `json:"key_id,omitempty"`
}

// EncodeCredential is the keystore value.
func EncodeCredential(c Credential) ([]byte, error) {
	if c.URL == "" || c.Key == "" {
		return nil, errors.New("sandhold: a credential needs a node and a key")
	}
	return json.Marshal(storedCredential{Version: 1, URL: c.URL, Key: c.Key, KeyID: c.KeyID})
}

// DecodeCredential parses a keystore value; it never echoes the value.
func DecodeCredential(b []byte) (Credential, error) {
	var s storedCredential
	if err := json.Unmarshal(b, &s); err != nil || s.Version != 1 || s.URL == "" || s.Key == "" {
		return Credential{}, errors.New("sandhold: the saved credential does not parse; connect again")
	}
	return Credential{URL: s.URL, Key: s.Key, KeyID: s.KeyID}, nil
}

// Connection is the non-secret record of a connect.
type Connection struct {
	Version     int       `json:"version"`
	URL         string    `json:"url"`
	KeyID       string    `json:"key_id,omitempty"`
	Org         string    `json:"org,omitempty"`
	User        string    `json:"user,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitzero"`
	ConnectedAt time.Time `json:"connected_at"`
	// Offers is what the node offered this key at connect (GET
	// /v1/targets), folded per isolation; empty when that call failed.
	Offers []Offer `json:"offers,omitempty"`
	// TargetsError is why the targets could not be read at connect.
	TargetsError string `json:"targets_error,omitempty"`
	// KeyStore says where the key is kept ("the OS keychain" or the file
	// fallback's directory), for people.
	KeyStore string `json:"key_store,omitempty"`
	// OrgToken, when set, says this machine reaches the node as an enrolled
	// member of an Observer org: no key is kept, each call carries a
	// short-lived DPoP-bound token from the org's token service (auth.go).
	OrgToken *OrgToken `json:"org_token,omitempty"`
	// Plan is the account's first-month offer as the node last told it
	// (GET /v1/offer, SandHold note 44): nil for an org without one.
	Plan *Plan `json:"plan,omitempty"`
}

// OrgToken is where an org member's tokens for the node come from. No
// secret: the token service is reached with the machine's org enrolment.
type OrgToken struct {
	// Issuer is the org's token service; TokenURL, when set, is where its
	// token endpoint is dialled (default <issuer>/oauth2/token).
	Issuer   string `json:"issuer"`
	TokenURL string `json:"token_url,omitempty"`
	// Config is the Observer config file the enrolment is read with ("":
	// the standard location).
	Config string `json:"config,omitempty"`
}

// Summary is the connection in one line, for the backend row and status.
func (c *Connection) Summary(now time.Time) string {
	if c == nil || c.URL == "" {
		return NotConnected
	}
	var b strings.Builder
	b.WriteString("connected to " + c.URL)
	if c.User != "" {
		b.WriteString(" as " + c.User)
		if c.Org != "" {
			b.WriteString(" in " + c.Org)
		}
	}
	if c.OrgToken != nil {
		b.WriteString(" as a member of your Observer org (short-lived tokens from " + c.OrgToken.Issuer + ")")
	}
	if !c.ExpiresAt.IsZero() {
		if now.After(c.ExpiresAt) {
			b.WriteString("; the key expired on " + c.ExpiresAt.UTC().Format("2 Jan 2006") + ", connect again")
		} else {
			b.WriteString("; key works until " + c.ExpiresAt.UTC().Format("2 Jan 2006"))
		}
	}
	switch {
	case len(c.Offers) > 0:
		parts := make([]string, 0, len(c.Offers))
		for _, o := range c.Offers {
			p := TierTitle(o.Tier)
			if o.Class != "" {
				p += " = " + string(o.Class)
			}
			parts = append(parts, p)
		}
		b.WriteString("; offers " + strings.Join(parts, ", "))
	case c.TargetsError != "":
		b.WriteString("; its isolation could not be read at connect (" + c.TargetsError + ")")
	}
	return PlainLine(b.String())
}

// Expired reports whether the key's expiry has passed.
func (c *Connection) Expired(now time.Time) bool {
	return c != nil && !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt)
}

// NotConnected is the row's state with no connection.
const NotConnected = "not connected: run `observer sandbox connect` (SandHold's public node; first month free) or `observer sandbox connect <node-url>`"

// Settings are what a call to the node would use: the environment wins over
// the saved sign-in, and a saved key is never sent to another node.
type Settings struct {
	URL     string
	Key     string
	FromEnv bool
}

// Resolve picks the node and key: SANDHOLD_URL and SANDHOLD_API_KEY win;
// a saved key is dropped when SANDHOLD_URL names another node. saved may be
// nil.
func Resolve(saved *Credential, lookup func(string) (string, bool)) (Settings, error) {
	var s Settings
	if saved != nil {
		s.URL, s.Key = saved.URL, saved.Key
	}
	if v, ok := lookup(EnvURL); ok && strings.TrimSpace(v) != "" {
		u, err := NodeURL(v)
		if err != nil {
			return Settings{}, fmt.Errorf("%s: %w", EnvURL, err)
		}
		if saved == nil || u != saved.URL {
			s.Key = "" // the saved key belongs to the saved node only
		}
		s.URL = u
	}
	if v, ok := lookup(EnvKey); ok && strings.TrimSpace(v) != "" {
		if s.URL == "" {
			return Settings{}, fmt.Errorf("%s is set but no node is: set %s too, or connect", EnvKey, EnvURL)
		}
		s.Key, s.FromEnv = strings.TrimSpace(v), true
	}
	return s, nil
}
