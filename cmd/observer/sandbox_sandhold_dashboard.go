//go:build !sbo_omit_terminals_sandbox_sandhold && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote/sandhold"
)

// sandbox_sandhold_dashboard.go is the SandHold row on the dashboard's
// Terminals > Sandboxes tab: where runs can go on the connected node, and the
// node's advisor inside Observer.
//
//   - GET  /api/terminal/sandbox/sandhold reads only local facts (the
//     connection record and the environment), like the row's probe: never
//     the key, never the node.
//   - GET  /api/terminal/sandbox/sandhold/advise asks the node for its three
//     questions, and POST answers them and returns the node's advice. Both
//     run only when the person presses the panel's button, and send the key
//     only to the node that issued it, from this daemon (the browser never
//     sees it: every answer is scrubbed of it before it is written).
//
// All three are owner-local (a paired remote viewer reaches none), and both
// advisor calls need the hosted panel's confirm token (a header another
// site cannot send). Nothing here is logged.

// sandholdAdviseTimeout bounds one call to the node.
const sandholdAdviseTimeout = 20 * time.Second

// sandholdConnectCommand is the exact command the panel shows.
const sandholdConnectCommand = "observer sandbox connect <node-url>"

// The bounds of a set of answers the daemon forwards.
const (
	sandholdAnswerMax   = 64
	sandholdReachMax    = 32
	sandholdHostNameMax = 253
)

// sandholdOfferView is one isolation the node offered at connect, in its
// words: the tier ("shielded"), its title ("Shielded"), what it means, and
// the networks it can give ("none", "limited", "open").
type sandholdOfferView struct {
	Tier     string   `json:"tier"`
	Title    string   `json:"title"`
	Means    string   `json:"means,omitempty"`
	Networks []string `json:"networks"`
	Default  bool     `json:"default,omitempty"`
}

// sandholdPlaceView is the answer of GET /api/terminal/sandbox/sandhold.
type sandholdPlaceView struct {
	// Connected is true when a run could name a node: a saved sign-in, or
	// SANDHOLD_URL with SANDHOLD_API_KEY in the daemon's environment.
	Connected bool   `json:"connected"`
	Node      string `json:"node,omitempty"`
	User      string `json:"user,omitempty"`
	Org       string `json:"org,omitempty"`
	// ExpiresAt is when the saved key stops working; Expired once past.
	ExpiresAt string `json:"expires_at,omitempty"`
	Expired   bool   `json:"expired,omitempty"`
	// KeyStore says where the key is kept, in words (never the key).
	KeyStore string `json:"key_store,omitempty"`
	// FromEnv: SANDHOLD_API_KEY is set and wins over a saved sign-in.
	FromEnv      bool                `json:"from_env,omitempty"`
	Offers       []sandholdOfferView `json:"offers"`
	TargetsError string              `json:"targets_error,omitempty"`
	// Problem is why the connection record could not be read.
	Problem string `json:"problem,omitempty"`
	// ComponentActive: the SandHold sandboxes component is turned on, so a
	// sandboxed terminal can run on the node (it must also be in the
	// enabled backends).
	ComponentActive bool   `json:"component_active"`
	ConnectCommand  string `json:"connect_command"`
	ConfirmToken    string `json:"confirm_token"`
}

// sandholdDashboardRoutes are the row's routes on the dashboard. The
// /api/terminal/sandbox prefix puts them under the terminals.sandbox
// component's guard, and the terminals section's governance.
func sandholdDashboardRoutes(configPath string) []dashapi.ExtraRoute {
	sec := dashapi.Section("terminals")
	return []dashapi.ExtraRoute{
		{Pattern: "GET /api/terminal/sandbox/sandhold", Capability: dashapi.CapabilityLocal, Section: sec, Handler: func(w http.ResponseWriter, r *http.Request) {
			sandholdPlaceHandler(w, r, configPath)
		}},
		{Pattern: "GET /api/terminal/sandbox/sandhold/advise", Capability: dashapi.CapabilityLocal, Section: sec, Handler: sandholdAdviseQuestionsHandler},
		{Pattern: "POST /api/terminal/sandbox/sandhold/advise", Capability: dashapi.CapabilityLocal, Section: sec, Handler: sandholdAdviseHandler},
	}
}

// sandholdComponentActive reports whether the SandHold sandboxes component
// is selected under the config at cfgPath (a variable so tests set it).
var sandholdComponentActive = func(cfgPath string) bool {
	cfg, _, err := config.LoadGovernance(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		return false
	}
	return sandboxDriverSelected(componentSnapshotForConfig(cfg), sandholdComponent)
}

// sandholdPlace is the row's view over local facts only.
func sandholdPlace(observerDir string, now time.Time) sandholdPlaceView {
	v := sandholdPlaceView{Offers: []sandholdOfferView{}, ConnectCommand: sandholdConnectCommand}
	conn, err := sandholdLoadConnection(observerDir)
	v.FromEnv = sandholdEnvKeySet()
	switch {
	case err != nil:
		v.Problem = "the SandHold connection record could not be read (" + sandhold.PlainLine(err.Error()) + "); connect again"
	case conn != nil:
		v.Connected = true
		v.Node, v.User, v.Org = conn.URL, sandhold.PlainLine(conn.User), sandhold.PlainLine(conn.Org)
		v.KeyStore = sandhold.PlainLine(conn.KeyStore)
		v.TargetsError = sandhold.PlainLine(conn.TargetsError)
		if !conn.ExpiresAt.IsZero() {
			v.ExpiresAt = conn.ExpiresAt.UTC().Format(time.RFC3339)
			v.Expired = conn.Expired(now)
		}
		for _, o := range conn.Offers {
			v.Offers = append(v.Offers, sandholdOfferView{
				Tier: sandhold.PlainLine(o.Tier), Title: sandhold.TierTitle(sandhold.PlainLine(o.Tier)),
				Means: sandhold.PlainLine(o.Means), Networks: o.Networks, Default: o.Default,
			})
		}
	}
	if v.FromEnv {
		if node := sandholdNode(observerDir); node != "" {
			v.Connected, v.Node = true, node
		}
	}
	return v
}

func sandholdPlaceHandler(w http.ResponseWriter, r *http.Request, configPath string) {
	tok := hostedConfirmToken(w, r)
	obs, err := hostedObserverDir()
	if err != nil {
		sandholdDashJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()}, "")
		return
	}
	v := sandholdPlace(obs, time.Now())
	v.ComponentActive = sandholdComponentActive(configPath)
	v.ConfirmToken = tok
	sandholdDashJSON(w, http.StatusOK, v, "")
}

// sandholdAdviseClient is a client of the connected node with its key, or
// the refusal to write when there is none. The key is returned so every
// answer can be scrubbed of it.
func sandholdAdviseClient(w http.ResponseWriter) (*sandhold.Client, string, bool) {
	obs, err := hostedObserverDir()
	if err != nil {
		sandholdDashJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()}, "")
		return nil, "", false
	}
	cred, err := sandholdHostedCredential(obs)
	key := cred.Get(sandhold.EnvKey)
	if err != nil || key == "" || cred.Endpoint == "" {
		sandholdDashJSON(w, http.StatusConflict, map[string]string{
			"error": "this machine is not connected to a SandHold node",
			"fix":   "run " + sandholdConnectCommand,
		}, key)
		return nil, "", false
	}
	return sandholdNewClient(cred.Endpoint, key), key, true
}

// sandholdNewClient builds the advisor's client (a variable so tests point
// it at a fake node).
var sandholdNewClient = sandhold.NewClient

// sandholdAdviseQuestionsHandler serves GET /api/terminal/sandbox/sandhold/advise.
func sandholdAdviseQuestionsHandler(w http.ResponseWriter, r *http.Request) {
	// A GET that calls the node needs the confirm token too: a page on
	// another site could otherwise make the browser send it (an image, a
	// link) and so make this daemon call the node. The token's header is
	// one no other site can set.
	h := strings.TrimSpace(r.Header.Get(hostedConfirmHeader))
	ck, err := r.Cookie(hostedConfirmCookie)
	if h == "" || err != nil || subtle.ConstantTimeCompare([]byte(h), []byte(strings.TrimSpace(ck.Value))) != 1 {
		sandholdDashJSON(w, http.StatusForbidden, map[string]string{"error": "missing or mismatched confirm token", "fix": "reload the page"}, "")
		return
	}
	c, key, ok := sandholdAdviseClient(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), sandholdAdviseTimeout)
	defer cancel()
	qs, err := c.AdviseQuestions(ctx)
	if err != nil {
		sandholdWriteNodeError(w, err, key)
		return
	}
	sandholdDashJSON(w, http.StatusOK, map[string]any{"node": c.URL, "questions": qs}, key)
}

// sandholdAdviseHandler serves POST /api/terminal/sandbox/sandhold/advise:
// {"runs", "trust", "reach": {"packages", "hosts"}}.
func sandholdAdviseHandler(w http.ResponseWriter, r *http.Request) {
	if !hostedConfirmed(w, r) {
		return
	}
	var in sandhold.AdviseRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		sandholdDashJSON(w, http.StatusBadRequest, map[string]string{"error": "the answers are not the JSON expected"}, "")
		return
	}
	if msg := sandholdAnswersProblem(in); msg != "" {
		sandholdDashJSON(w, http.StatusBadRequest, map[string]string{"error": msg}, "")
		return
	}
	c, key, ok := sandholdAdviseClient(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), sandholdAdviseTimeout)
	defer cancel()
	a, err := c.Advise(ctx, in)
	if err != nil {
		sandholdWriteNodeError(w, err, key)
		return
	}
	sandholdDashJSON(w, http.StatusOK, map[string]any{"node": c.URL, "advice": a}, key)
}

// sandholdAnswersProblem checks a set of answers before it goes to the
// node: short, plain words, a bounded reach. The node judges what they
// mean.
func sandholdAnswersProblem(in sandhold.AdviseRequest) string {
	bad := func(s string, max int) bool {
		return len(s) > max || strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
	}
	switch {
	case bad(in.Runs, sandholdAnswerMax) || bad(in.Trust, sandholdAnswerMax):
		return "an answer is too long or holds a control character"
	case len(in.Reach.Packages) > sandholdReachMax || len(in.Reach.Hosts) > sandholdReachMax:
		return "too many places to reach"
	}
	for _, p := range in.Reach.Packages {
		if p == "" || bad(p, sandholdAnswerMax) {
			return "a package source is empty, too long or holds a control character"
		}
	}
	for _, h := range in.Reach.Hosts {
		if h == "" || bad(h, sandholdHostNameMax) || strings.ContainsAny(h, " /") {
			return "a host name is empty, too long or not a host name"
		}
	}
	return ""
}

// sandholdWriteNodeError writes a failed call: the node's refusal in its
// own words (why and the fix) as 422, anything else as 502.
func sandholdWriteNodeError(w http.ResponseWriter, err error, key string) {
	var ae *sandhold.APIError
	if errors.As(err, &ae) {
		sandholdDashJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": sandhold.PlainLine(ae.Msg), "fix": sandhold.PlainLine(ae.Fix), "node_status": ae.Status}, key)
		return
	}
	sandholdDashJSON(w, http.StatusBadGateway, map[string]string{"error": sandhold.PlainLine(err.Error())}, key)
}

// sandholdDashJSON writes v with every occurrence of key replaced, so a
// node that echoes the key back (or an error that carries it) never puts
// it in the browser.
func sandholdDashJSON(w http.ResponseWriter, status int, v any, key string) {
	// Not HTML-escaped: the command shown reads "<node-url>", and the page
	// renders text, never markup.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		http.Error(w, "the answer could not be encoded", http.StatusInternalServerError)
		return
	}
	b := buf.Bytes()
	if key != "" {
		b = bytes.ReplaceAll(b, []byte(key), []byte("[the key]"))
		// The key as JSON writes it, should it hold a character JSON
		// escapes.
		if q, err := json.Marshal(key); err == nil {
			b = bytes.ReplaceAll(b, bytes.Trim(q, `"`), []byte("[the key]"))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
