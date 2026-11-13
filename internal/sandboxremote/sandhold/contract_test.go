package sandhold

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// contract_test.go checks this client against SandHold's customer API
// contract (SandHold note 41 D2: SandHold owns the contract, every client
// built elsewhere tests against it). testdata/contract/customer is a
// byte-identical copy of SandHold's contract/customer: one file per call,
// written there by the node's real handlers. The tests replay each answer
// the client depends on and check the fields it reads and the words it
// shows. TestContractCopyIsSandHolds compares the copy with a SandHold
// checkout when one is beside this repository.

const contractDir = "testdata/contract/customer"

// vector is one contract file.
type vector struct {
	Name     string          `json:"name"`
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Request  json.RawMessage `json:"request"`
	Status   int             `json:"status"`
	Response json.RawMessage `json:"response"`
	// Accept and Stream are an event-stream call's (exec-stream).
	Accept string `json:"accept"`
	Stream []struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	} `json:"stream"`
	// Frames are a terminal's WebSocket frames in order (terminal,
	// terminal-pipe): from "node" or "client", binary (base64), json or a
	// close.
	Frames []struct {
		From   string          `json:"from"`
		Binary []byte          `json:"binary"`
		JSON   json.RawMessage `json:"json"`
		Close  int             `json:"close"`
		Reason string          `json:"reason"`
	} `json:"frames"`
}

// The values a contract file's placeholders stand for in these tests.
const (
	contractTime = "2026-12-11T00:00:00Z"
	contractID   = "0b9d3c1e-6a52-4c57-9a55-0d8c3f6f2a11"
)

func loadVector(t *testing.T, name string) vector {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(contractDir, name+".json"))
	if err != nil {
		t.Fatalf("contract vector %s: %v", name, err)
	}
	var v vector
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("contract vector %s: %v", name, err)
	}
	if v.Name != name || v.Method == "" || v.Path == "" || v.Status == 0 {
		t.Fatalf("contract vector %s: not a contract file (name %q, %s %s, status %d)", name, v.Name, v.Method, v.Path, v.Status)
	}
	return v
}

// fill replaces the placeholders that must parse (a time, an id) with real
// values; the others (<api-key>, <key-id>, the codes) are ordinary strings.
func fill(s string) string {
	return strings.NewReplacer("<time>", contractTime, "<id>", contractID).Replace(s)
}

// contractNode answers the given vectors in order for their method and
// path, the last one repeating, and fails the test on any other call.
func contractNode(t *testing.T, vs ...vector) *Client {
	t.Helper()
	next := map[string][]vector{}
	for _, v := range vs {
		// A contract path may carry a query; calls are matched on the path
		// and the query is checked when the call arrives.
		path, _, _ := strings.Cut(fill(v.Path), "?")
		k := v.Method + " " + path
		next[k] = append(next[k], v)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k := r.Method + " " + r.URL.Path
		q := next[k]
		if len(q) == 0 {
			t.Errorf("the client called %s, which no replayed contract file answers", k)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		v := q[0]
		if len(q) > 1 {
			next[k] = q[1:]
		}
		if _, wantQ, ok := strings.Cut(fill(v.Path), "?"); ok {
			want, _ := url.ParseQuery(wantQ)
			if got := r.URL.Query(); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: the client sent the query %v, the contract's call sends %v", v.Name, got, want)
			}
		}
		if len(v.Request) > 0 {
			// The client must send every field the contract's request has.
			var want, got map[string]any
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(v.Request, &want)
			if err := json.Unmarshal(body, &got); err != nil {
				t.Errorf("%s: the client's request is not JSON: %v", v.Name, err)
			}
			for f := range want {
				if _, ok := got[f]; !ok {
					t.Errorf("%s: the client's request lacks %q, which the contract's request has", v.Name, f)
				}
			}
		}
		if len(v.Stream) > 0 {
			if v.Accept != "" && r.Header.Get("Accept") != v.Accept {
				t.Errorf("%s: the client asked for %q, the contract's call sends Accept %q", v.Name, r.Header.Get("Accept"), v.Accept)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(v.Status)
			for _, ev := range v.Stream {
				// One data line per event, as the node's handler writes it.
				var data bytes.Buffer
				if err := json.Compact(&data, []byte(fill(string(ev.Data)))); err != nil {
					t.Errorf("%s: event data: %v", v.Name, err)
				}
				_, _ = io.WriteString(w, "event: "+ev.Event+"\ndata: "+data.String()+"\n\n")
			}
			return
		}
		if len(v.Response) == 0 {
			w.WriteHeader(v.Status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if v.Status == http.StatusServiceUnavailable {
			// A full node's 503 carries Retry-After in seconds (contract
			// README, sandbox-create-full); the file holds only the body.
			w.Header().Set("Retry-After", "30")
		}
		w.WriteHeader(v.Status)
		_, _ = io.WriteString(w, fill(string(v.Response)))
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "")
}

func TestContractSignIn(t *testing.T) {
	code := loadVector(t, "device-code")
	pending := loadVector(t, "device-token-pending")
	approved := loadVector(t, "device-token-approved")
	// The contract's page is a placeholder, so give the start a real one.
	code.Response = json.RawMessage(strings.Replace(string(code.Response), "<verification-uri-with-code>", "https://node.example:48000/app/#/device?code=BCDF-GHJK", 1))

	var shown Start
	var waits []time.Duration
	si := &SignIn{
		Client: contractNode(t, code, pending, approved), ClientID: "Observer on laptop",
		Shown: func(s Start, _ bool) { shown = s },
		Sleep: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil },
	}
	g, err := si.Run(context.Background())
	if err != nil {
		t.Fatalf("sign-in over the contract's answers: %v", err)
	}
	if shown.DeviceCode != "<device-code>" || shown.UserCode != "<user-code>" || shown.URI != "https://node.example:48000/app/#/device" || shown.Interval != 3 {
		t.Errorf("the start was not read as the contract gives it: %+v", shown)
	}
	if len(waits) != 2 || waits[0] != 3*time.Second {
		t.Errorf("polled at %v, want the contract's interval of 3s, twice (pending, then approved)", waits)
	}
	want, _ := time.Parse(time.RFC3339, contractTime)
	if g.Key != "<api-key>" || g.KeyID != "<key-id>" || g.Org != "org_acme" || g.User != "alice" || !g.ExpiresAt.Equal(want) {
		t.Errorf("the grant was not read as the contract gives it: key id %q org %q user %q expires %v", g.KeyID, g.Org, g.User, g.ExpiresAt)
	}
}

func TestContractSignInRefusals(t *testing.T) {
	for _, tc := range []struct {
		vector string
		code   string
		is     error
	}{
		{"device-token-pending", "authorization_pending", nil},
		{"device-token-denied", "access_denied", ErrDenied},
		{"device-token-used", "invalid_grant", ErrExpired},
	} {
		v := loadVector(t, tc.vector)
		c := contractNode(t, v)
		err := c.Do(context.Background(), v.Method, v.Path, map[string]string{"device_code": "x"}, nil)
		var oe *OAuthError
		if !errors.As(err, &oe) || oe.Code != tc.code {
			t.Errorf("%s: got %v, want the OAuth code %q", tc.vector, err, tc.code)
			continue
		}
		var words struct {
			Desc string `json:"error_description"`
		}
		_ = json.Unmarshal(v.Response, &words)
		if words.Desc == "" || oe.Msg != words.Desc {
			t.Errorf("%s: the person would read %q, want the node's own words %q", tc.vector, oe.Msg, words.Desc)
		}
		if tc.is == nil {
			continue
		}
		// The whole sign-in ends with the matching refusal.
		code := loadVector(t, "device-code")
		code.Response = json.RawMessage(strings.Replace(string(code.Response), "<verification-uri-with-code>", "https://node.example:48000/app/#/device?code=BCDF-GHJK", 1))
		si := &SignIn{Client: contractNode(t, code, v), Sleep: func(context.Context, time.Duration) error { return nil }}
		if _, err := si.Run(context.Background()); !errors.Is(err, tc.is) {
			t.Errorf("%s: the sign-in ended with %v, want %v", tc.vector, err, tc.is)
		}
	}
}

func TestContractWhoamiAndRefusal(t *testing.T) {
	c := contractNode(t, loadVector(t, "whoami"))
	who, err := c.Whoami(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if who.Org != "org_acme" || who.User != "alice" || who.KeyID != "<key-id>" || who.Via != "apikey" || who.Admin {
		t.Errorf("whoami was not read as the contract gives it: %+v", who)
	}

	v := loadVector(t, "unauthenticated")
	_, err = contractNode(t, v).Whoami(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != v.Status {
		t.Fatalf("got %v, want the node's refusal with status %d", err, v.Status)
	}
	var words struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(v.Response, &words)
	if words.Error == "" || ae.Msg != words.Error {
		t.Errorf("the person would read %q, want the node's own words %q", ae.Msg, words.Error)
	}
}

func TestContractRevokeKey(t *testing.T) {
	v := loadVector(t, "key-revoke")
	c := contractNode(t, v)
	c.Key = "k"
	if err := c.RevokeKey(context.Background(), contractID); err != nil {
		t.Errorf("revoking over the contract's answer (%d, no body): %v", v.Status, err)
	}
}

// TestContractTargets: every target the contract lists reads into one of
// Observer's classes, and where a target names both a tier and a level the
// two agree (the one-to-one mapping this client relies on).
func TestContractTargets(t *testing.T) {
	c := contractNode(t, loadVector(t, "targets"))
	ts, err := c.Targets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) == 0 {
		t.Fatal("the contract lists no target")
	}
	for _, tg := range ts {
		if tg.ID == "" || tg.Tier == "" || tg.TierMeans == "" || tg.TierRank == 0 || tg.State == "" {
			t.Errorf("target %q lacks a field this client shows: tier %q means %q rank %d state %q", tg.ID, tg.Tier, tg.TierMeans, tg.TierRank, tg.State)
		}
		byTier, okTier := ClassForTier(tg.Tier)
		byLevel, okLevel := ClassForLevel(tg.Capabilities.Isolation)
		if !okTier || !okLevel {
			t.Errorf("target %q: tier %q or level %q is a word this client does not know", tg.ID, tg.Tier, tg.Capabilities.Isolation)
			continue
		}
		if byTier != byLevel {
			t.Errorf("target %q: tier %q reads as %s but level %q reads as %s", tg.ID, tg.Tier, byTier, tg.Capabilities.Isolation, byLevel)
		}
	}
	offers := Offers(ts)
	if len(offers) == 0 {
		t.Fatal("no offer was folded from the contract's targets")
	}
	var last vocab.IsoClass
	for i, o := range offers {
		if o.Class == "" || len(o.Networks) == 0 || len(o.Targets) == 0 {
			t.Errorf("offer %q is incomplete: %+v", o.Tier, o)
		}
		if i > 0 && o.Class.Rank() < last.Rank() {
			t.Errorf("offers are not weakest first: %s after %s", o.Class, last)
		}
		last = o.Class
	}
}

// TestContractCopyIsSandHolds compares the copy with SandHold's own files
// when a SandHold checkout is beside this repository (or SANDHOLD_REPO
// names one). A difference means the contract changed there: copy the
// files (cp <SandHold>/contract/customer/*.json here), then make these
// tests pass.
func TestContractCopyIsSandHolds(t *testing.T) {
	repo := os.Getenv("SANDHOLD_REPO")
	if repo == "" {
		wd, err := os.Getwd()
		if err != nil {
			t.Skip(err)
		}
		// internal/sandboxremote/sandhold -> the repository -> its parent.
		repo = filepath.Join(wd, "..", "..", "..", "..", "SandHold")
	}
	theirs := filepath.Join(repo, "contract", "customer")
	names, err := filepath.Glob(filepath.Join(theirs, "*.json"))
	if err != nil || len(names) == 0 {
		t.Skipf("no SandHold checkout with contract/customer at %s (set SANDHOLD_REPO): the copy was not compared", repo)
	}
	seen := map[string]bool{}
	for _, f := range names {
		n := filepath.Base(f)
		seen[n] = true
		want, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(contractDir, n))
		switch {
		case errors.Is(err, os.ErrNotExist):
			t.Errorf("%s is in SandHold's contract but not in %s", n, contractDir)
		case err != nil:
			t.Fatal(err)
		case !bytes.Equal(got, want):
			t.Errorf("%s differs from SandHold's contract", n)
		}
	}
	ours, _ := filepath.Glob(filepath.Join(contractDir, "*.json"))
	for _, f := range ours {
		if n := filepath.Base(f); !seen[n] {
			t.Errorf("%s is in %s but not in SandHold's contract", n, contractDir)
		}
	}
}

// TestContractWhoamiInWords: a self-serve account's whoami reads as the
// person ("Ada (ada@example.com), your own account", no org id), an org
// member's as the member in the org's name, and a node that sends no names
// as the ids.
func TestContractWhoamiInWords(t *testing.T) {
	who, err := contractNode(t, loadVector(t, "whoami-self")).Whoami(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if who.Account != AccountSelf || who.Name != "Ada" || who.Email != "ada@example.com" {
		t.Fatalf("whoami-self was not read as the contract gives it: %+v", who)
	}
	conn := Connection{URL: "https://node.example", Org: who.Org, User: who.User, Account: who.Account, Name: who.Name, Email: who.Email, OrgName: who.OrgName}
	if w, in := conn.Who(); w != "Ada (ada@example.com), your own account" || in != "" {
		t.Errorf("self: %q in %q", w, in)
	}
	if s := conn.Summary(time.Now()); !strings.Contains(s, "as Ada (ada@example.com), your own account") || strings.Contains(s, "org_solo") {
		t.Errorf("summary: %s", s)
	}
	org, err := contractNode(t, loadVector(t, "whoami")).Whoami(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	conn = Connection{Org: org.Org, User: org.User, Account: org.Account, OrgName: org.OrgName}
	if w, in := conn.Who(); w != "alice" || in != "Acme Robotics (org_acme)" {
		t.Errorf("org: %q in %q", w, in)
	}
	if w, in := (&Connection{Org: "org_old", User: "bob"}).Who(); w != "bob" || in != "org_old" {
		t.Errorf("older node: %q in %q", w, in)
	}
}

// TestContractTargetsStarter: a starter account's targets carry its
// limits, and the offers kept within them have no tier below the starter
// one and no open network; with no limits nothing is dropped.
func TestContractTargetsStarter(t *testing.T) {
	ts, lim, err := contractNode(t, loadVector(t, "targets-starter")).TargetsAndLimits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lim == nil || lim.Isolation != "sealed" || lim.OpenNetwork {
		t.Fatalf("starter limits: %+v", lim)
	}
	offers := OffersWithin(ts, lim)
	if len(offers) == 0 {
		t.Fatal("no offer within the starter limits")
	}
	for _, o := range offers {
		if tierRankOf(o.Tier) < 3 || slices.Contains(o.Networks, NetworkOpen) {
			t.Errorf("offered beyond the starter limits: %+v", o)
		}
	}
	// The node's limits are applied even to targets it still listed: a
	// Shielded target with an allow list is dropped, Vault keeps none and
	// limited.
	mixed := []Target{{ID: "runc", Tier: "shielded", TierRank: 2, State: "active"}, {ID: "kata", Tier: "vault", TierRank: 4, State: "active"}}
	mixed[0].Capabilities.EgressAllowlist, mixed[1].Capabilities.EgressAllowlist = true, true
	got := OffersWithin(mixed, lim)
	if len(got) != 1 || got[0].Tier != "vault" || !slices.Equal(got[0].Networks, []string{NetworkNone, NetworkLimited}) {
		t.Errorf("mixed: %+v", got)
	}
	if all := OffersWithin(mixed, nil); len(all) != 2 || !slices.Contains(all[0].Networks, NetworkOpen) {
		t.Errorf("no limits: %+v", all)
	}
}
