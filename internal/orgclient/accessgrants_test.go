package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/accessgrant"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

type storeState = store.AccessGrantState

// grantRailServer serves GET /api/agent/grants and POST /api/agent/requests
// with scripted answers.
type grantRailServer struct {
	srv       *httptest.Server
	mu        sync.Mutex
	status    int
	doc       any
	reqStatus int
	posted    []orgcontract.AccessRequestCreate
	machine   string
	verified  string
}

func newGrantRailServer(t *testing.T) *grantRailServer {
	t.Helper()
	gs := &grantRailServer{status: http.StatusNotFound, reqStatus: http.StatusCreated}
	gs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gs.mu.Lock()
		defer gs.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == orgcontract.AccessGrantsPath:
			gs.machine = r.Header.Get(orgcontract.HeaderMachineIdentity)
			gs.verified = r.Header.Get(orgcontract.HeaderGrantsVerified)
			if gs.status != http.StatusOK {
				writeTestJSON(w, gs.status, map[string]string{"error": "scripted"})
				return
			}
			writeTestJSON(w, http.StatusOK, gs.doc)
		case r.Method == http.MethodPost && r.URL.Path == "/api/agent/requests":
			var in orgcontract.AccessRequestCreate
			_ = json.NewDecoder(r.Body).Decode(&in)
			if gs.reqStatus != http.StatusCreated {
				writeTestJSON(w, gs.reqStatus, map[string]string{"error": "scripted"})
				return
			}
			gs.posted = append(gs.posted, in)
			writeTestJSON(w, http.StatusCreated, map[string]any{"id": len(gs.posted), "kind": "other", "message": in.Message, "status": "open", "request_type": in.RequestType})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(gs.srv.Close)
	return gs
}

func (gs *grantRailServer) lastVerified() string {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	return gs.verified
}

func (gs *grantRailServer) set(status int, doc any) {
	gs.mu.Lock()
	gs.status, gs.doc = status, doc
	gs.mu.Unlock()
}

const testGrantSubject = "scim-42|"

func signGrants(t *testing.T, priv ed25519.PrivateKey, org, subject string, version int64, issued time.Time, grants ...orgcontract.AccessGrantWire) orgcontract.AccessGrantDoc {
	t.Helper()
	d, err := orgcontract.SignAccessGrants(priv, org, subject, version, orgcontract.AccessGrantBody{
		Grants: grants, ServerNowNs: issued.UnixNano(), MaxOfflineS: 86400, IssuedAt: issued.UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func exemptGrant(id int64, now time.Time, ttl time.Duration) orgcontract.AccessGrantWire {
	return orgcontract.AccessGrantWire{
		ID: id, Type: accessgrant.TypeGuardExemption, Effect: orgcontract.AccessGrantEffectExempt,
		SubjectKind: orgcontract.AccessGrantSubjectMember, RuleID: "R-171",
		StartsAtNs: now.Add(-time.Minute).UnixNano(), ExpiresAtNs: now.Add(ttl).UnixNano(),
	}
}

// TestFetchAccessGrantsLadder walks D-016 against one node: 404, 5xx, auth,
// unsigned, foreign org, foreign subject, foreign key, stale issued_at and an
// older version never change the cache; only a newer signed document does, and
// only a signed EMPTY list empties it.
func TestFetchAccessGrantsLadder(t *testing.T) {
	ctx := context.Background()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	gs := newGrantRailServer(t)
	c, s, _ := enrolledClient(t, gs.srv.URL)
	now := time.Now()

	v1 := signGrants(t, priv, "org-1", testGrantSubject, 1, now, exemptGrant(7, now, time.Hour))
	unsigned := signGrants(t, priv, "org-1", testGrantSubject, 2, now)
	unsigned.Signature = ""
	steps := []struct {
		name      string
		status    int
		doc       any
		wantState string
		wantErr   bool
		wantCount int
		wantVer   int64
	}{
		{"older server (404), nothing cached", 404, nil, GrantFetchNotSupported, false, 0, 0},
		{"signed v1 cached", 200, v1, GrantFetchApplied, false, 1, 1},
		{"same version re-verified", 200, v1, GrantFetchCurrent, false, 1, 1},
		{"404 keeps the grants", 404, nil, GrantFetchNotSupported, false, 1, 1},
		{"5xx keeps the grants", 503, nil, GrantFetchUnreachable, true, 1, 1},
		{"401 keeps the grants", 401, nil, GrantFetchAuth, true, 1, 1},
		{"409 keeps the grants", 409, nil, GrantFetchConflict, true, 1, 1},
		{"unsigned refused", 200, unsigned, GrantFetchUnverified, true, 1, 1},
		{"another org refused", 200, signGrants(t, priv, "org-2", testGrantSubject, 2, now), GrantFetchUnverified, true, 1, 1},
		{"another subject refused", 200, signGrants(t, priv, "org-1", "someone|", 2, now), GrantFetchUnverified, true, 1, 1},
		{"a key that is not the org's refused", 200, signGrants(t, otherPriv, "org-1", testGrantSubject, 2, now), GrantFetchUnverified, true, 1, 1},
		{"a captured stale document refused", 200, signGrants(t, priv, "org-1", testGrantSubject, 2, now.Add(-2*time.Hour)), GrantFetchUnverified, true, 1, 1},
		{"signed v3 with two grants applies", 200, signGrants(t, priv, "org-1", testGrantSubject, 3, now, exemptGrant(7, now, time.Hour), exemptGrant(8, now, 2*time.Hour)), GrantFetchApplied, false, 2, 3},
		{"an older genuine document refused", 200, v1, GrantFetchUnverified, true, 2, 3},
		{"signed EMPTY v4 empties the cache", 200, signGrants(t, priv, "org-1", testGrantSubject, 4, now), GrantFetchApplied, false, 0, 4},
	}
	for _, st := range steps {
		gs.set(st.status, st.doc)
		out, err := c.FetchAccessGrants(ctx)
		if (err != nil) != st.wantErr {
			t.Fatalf("%s: err = %v, wantErr %v", st.name, err, st.wantErr)
		}
		if out.State != st.wantState {
			t.Errorf("%s: state = %q, want %q", st.name, out.State, st.wantState)
		}
		grants, _ := s.CachedAccessGrants(ctx)
		state, ok, _ := s.GetAccessGrantState(ctx)
		if len(grants) != st.wantCount || (ok && state.Version != st.wantVer) || (!ok && st.wantVer != 0) {
			t.Errorf("%s: node holds %d grants at v%d (ok=%v), want %d at v%d", st.name, len(grants), state.Version, ok, st.wantCount, st.wantVer)
		}
	}
}

// TestAccessGrantLookupHonoursExpiryAndOfflineWindow: a cached grant covers
// the query while it is live and verified; past its expiry, or past the
// offline window since the last verified sync, it covers nothing.
func TestAccessGrantLookupHonoursExpiryAndOfflineWindow(t *testing.T) {
	ctx := context.Background()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	gs := newGrantRailServer(t)
	c, s, _ := enrolledClient(t, gs.srv.URL)
	now := time.Now()
	g := exemptGrant(7, now, 2*time.Hour)
	g.MaxUses = 2
	gs.set(200, signGrants(t, priv, "org-1", testGrantSubject, 1, now, g))
	if _, err := c.FetchAccessGrants(ctx); err != nil {
		t.Fatal(err)
	}
	q := accessgrant.Query{RuleID: "R-171", OperandHash: "anything", Decision: accessgrant.DecisionDeny}
	if hit, ok := c.AccessGrantLookup(ctx, q); !ok || hit.GrantID != 7 || hit.UsesLeft != 2 {
		t.Fatalf("live grant: %+v %v", hit, ok)
	}
	if _, ok := c.AccessGrantLookup(ctx, accessgrant.Query{RuleID: "R-999", Decision: accessgrant.DecisionDeny}); ok {
		t.Fatal("another rule matched")
	}
	// Uses: two consumptions exhaust it.
	for i := 0; i < 2; i++ {
		if ok, err := s.ConsumeAccessGrantUse(ctx, 7, 2); err != nil || !ok {
			t.Fatalf("consume %d: %v %v", i, ok, err)
		}
	}
	if ok, _ := s.ConsumeAccessGrantUse(ctx, 7, 2); ok {
		t.Fatal("a third use was counted past max_uses")
	}
	if _, ok := c.AccessGrantLookup(ctx, q); ok {
		t.Fatal("exhausted grant still covers")
	}
	if err := s.SaveAccessGrants(ctx, mustState(t, s), []orgcontract.AccessGrantWire{exemptGrant(9, now, 2*time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// Past its expiry it covers nothing (checked last: a lookup advances the
	// node's clock high-water mark).
	if hit, ok := LookupAccessGrant(ctx, s, accessgrant.Query{RuleID: "R-171", Decision: accessgrant.DecisionDeny}, now.Add(3*time.Hour)); ok {
		t.Fatalf("expired grant still covers: %+v", hit)
	}
}

// TestAccessGrantLookupGoesStaleOffline: with no verified sync for longer than
// the org's offline window, a grant that has not expired still covers nothing.
func TestAccessGrantLookupGoesStaleOffline(t *testing.T) {
	ctx := context.Background()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	gs := newGrantRailServer(t)
	c, s, _ := enrolledClient(t, gs.srv.URL)
	now := time.Now()
	gs.set(200, signGrants(t, priv, "org-1", testGrantSubject, 1, now, exemptGrant(7, now, 72*time.Hour)))
	if _, err := c.FetchAccessGrants(ctx); err != nil {
		t.Fatal(err)
	}
	q := accessgrant.Query{RuleID: "R-171", Decision: accessgrant.DecisionDeny}
	if _, ok := LookupAccessGrant(ctx, s, q, now.Add(23*time.Hour)); !ok {
		t.Fatal("inside the offline window the grant should hold")
	}
	if _, ok := LookupAccessGrant(ctx, s, q, now.Add(25*time.Hour)); ok {
		t.Fatal("past the offline window the grant still covers")
	}
	// The clock is now read as at least +25h (high-water): setting it back
	// does not revive the grant.
	if _, ok := LookupAccessGrant(ctx, s, q, now.Add(time.Hour)); ok {
		t.Fatal("a clock set back revived a stale grant")
	}
}

func TestAccessRequestOutboxFlushes(t *testing.T) {
	ctx := context.Background()
	gs := newGrantRailServer(t)
	c, s, _ := enrolledClient(t, gs.srv.URL)
	in := orgcontract.AccessRequestCreate{
		Message: "need curl upload", RequestType: accessgrant.TypeGuardExemption,
		Payload: &orgcontract.AccessRequestPayload{RuleID: "R-171", Breadth: accessgrant.BreadthRule},
	}
	if _, err := c.QueueAccessRequest(ctx, in); err != nil {
		t.Fatal(err)
	}
	gs.mu.Lock()
	gs.reqStatus = http.StatusServiceUnavailable
	gs.mu.Unlock()
	if n, err := c.FlushAccessRequestOutbox(ctx); err == nil || n != 0 {
		t.Fatalf("flush against a down server: n=%d err=%v", n, err)
	}
	q, _ := s.ListAccessRequestOutbox(ctx)
	if len(q) != 1 || q[0].Attempts != 1 || q[0].LastError == "" {
		t.Fatalf("queue after failure: %+v", q)
	}
	gs.mu.Lock()
	gs.reqStatus = http.StatusCreated
	gs.mu.Unlock()
	if n, err := c.FlushAccessRequestOutbox(ctx); err != nil || n != 1 {
		t.Fatalf("flush: n=%d err=%v", n, err)
	}
	if q, _ := s.ListAccessRequestOutbox(ctx); len(q) != 0 {
		t.Fatalf("queue not drained: %+v", q)
	}
	if len(gs.posted) != 1 || gs.posted[0].RequestType != accessgrant.TypeGuardExemption {
		t.Fatalf("posted: %+v", gs.posted)
	}
}

// TestFetchAccessGrantsReportsTheVerifiedVersion pins M4-01's node half: the
// next fetch tells the org which document version this node holds VERIFIED;
// nothing before the first verified document, and nothing for a cache that
// no longer verifies.
func TestFetchAccessGrantsReportsTheVerifiedVersion(t *testing.T) {
	ctx := context.Background()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	gs := newGrantRailServer(t)
	c, s, _ := enrolledClient(t, gs.srv.URL)
	now := time.Now()
	gs.set(200, signGrants(t, priv, "org-1", testGrantSubject, 3, now, exemptGrant(7, now, time.Hour)))
	if _, err := c.FetchAccessGrants(ctx); err != nil {
		t.Fatal(err)
	}
	if gs.lastVerified() != "" {
		t.Fatalf("first fetch reported %q before anything was verified", gs.lastVerified())
	}
	if _, err := c.FetchAccessGrants(ctx); err != nil {
		t.Fatal(err)
	}
	if gs.lastVerified() != "3" {
		t.Fatalf("reported %q, want the verified version 3", gs.lastVerified())
	}
	st, _, _ := s.GetAccessGrantState(ctx)
	st.Signature = ""
	if err := s.SaveAccessGrants(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	_, _ = c.FetchAccessGrants(ctx)
	if gs.lastVerified() != "" {
		t.Fatalf("an unverified cache was reported as verified (%q)", gs.lastVerified())
	}
}

// TestAccessRequestRefusalIsNeverQueued pins C156-38: a 400 the server
// answers WITHOUT a per-field problems list is still a refusal on the merits.
// It is returned, never queued, and a refused item in the outbox never blocks
// the requests queued behind it.
func TestAccessRequestRefusalIsNeverQueued(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var posted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in orgcontract.AccessRequestCreate
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Message == "bad" {
			writeTestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "message": "unknown field"})
			return
		}
		mu.Lock()
		posted = append(posted, in.Message)
		mu.Unlock()
		writeTestJSON(w, http.StatusCreated, map[string]any{"id": 1, "kind": "other", "message": in.Message, "status": "open"})
	}))
	t.Cleanup(srv.Close)
	c, s, _ := enrolledClient(t, srv.URL)
	_, id, err := c.SendOrQueueAccessRequest(ctx, orgcontract.AccessRequestCreate{Message: "bad"})
	var refused *RequestRefusedError
	if !errors.As(err, &refused) || id != 0 || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("a problem-less 400 was not a refusal: id=%d err=%v", id, err)
	}
	if q, _ := s.ListAccessRequestOutbox(ctx); len(q) != 0 {
		t.Fatalf("a refused request was queued: %+v", q)
	}
	// A refused item at the head of the outbox does not block the next one.
	for _, m := range []string{"bad", "good"} {
		if _, err := c.QueueAccessRequest(ctx, orgcontract.AccessRequestCreate{Message: m}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := c.FlushAccessRequestOutbox(ctx); err != nil || n != 1 {
		t.Fatalf("flush: n=%d err=%v", n, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posted) != 1 || posted[0] != "good" {
		t.Fatalf("posted %v", posted)
	}
}

// TestPrepareAccessRequestWithoutMachineIdentity pins the live finding from
// the G6/G7 walk: a node with no machine identity (an individual enrolment)
// must not stamp an explicit member subject, which the validator refuses as
// an approver-only widening. It leaves the subject unset so the request is
// accepted, with the "all of your machines" warning.
func TestPrepareAccessRequestWithoutMachineIdentity(t *testing.T) {
	ctx := context.Background()
	gs := newGrantRailServer(t)
	c, _, _ := enrolledClient(t, gs.srv.URL)
	out, err := c.PrepareAccessRequest(ctx, orgcontract.AccessRequestCreate{
		Message: "need the migration script", RequestType: accessgrant.TypeGuardExemption,
		Payload: &orgcontract.AccessRequestPayload{RuleID: "R-171", Breadth: accessgrant.BreadthRule},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.MachineIdentity != "" || out.SubjectKind != "" {
		t.Fatalf("subject = %q machine = %q, want both empty on an individual node", out.SubjectKind, out.MachineIdentity)
	}
	norm, probs := accessgrant.Validate(accessgrant.Request{
		Type: out.RequestType, Payload: *out.Payload, SubjectKind: out.SubjectKind,
		Machine: out.MachineIdentity, Justification: out.Message,
	}, accessgrant.Settings{})
	if len(probs) != 0 {
		t.Fatalf("problems: %+v", probs)
	}
	if norm.SubjectKind != orgcontract.AccessGrantSubjectMember || len(norm.Warnings) == 0 {
		t.Fatalf("normalized subject %q warnings %v, want member with a warning", norm.SubjectKind, norm.Warnings)
	}
}

func TestAccessGrantsClearedOnUnenrol(t *testing.T) {
	ctx := context.Background()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	gs := newGrantRailServer(t)
	c, s, _ := enrolledClient(t, gs.srv.URL)
	now := time.Now()
	gs.set(200, signGrants(t, priv, "org-1", testGrantSubject, 1, now, exemptGrant(7, now, time.Hour)))
	if _, err := c.FetchAccessGrants(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.QueueAccessRequest(ctx, orgcontract.AccessRequestCreate{Message: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEnrolment(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetAccessGrantState(ctx); ok {
		t.Fatal("grant state survived unenrolment")
	}
	if g, _ := s.CachedAccessGrants(ctx); len(g) != 0 {
		t.Fatal("grants survived unenrolment")
	}
	if q, _ := s.ListAccessRequestOutbox(ctx); len(q) != 0 {
		t.Fatal("outbox survived unenrolment")
	}
}

func mustState(t *testing.T, s interface {
	GetAccessGrantState(context.Context) (storeState, bool, error)
},
) storeState {
	t.Helper()
	st, ok, err := s.GetAccessGrantState(context.Background())
	if err != nil || !ok {
		t.Fatalf("state: %v %v", ok, err)
	}
	return st
}
