package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// THE SIGNED-RAIL 404 CONTRACT (D-016, Sol A5-01). Every org-to-node rail
// that caches a signed document must treat an unsigned 404 as "no answer",
// never as "withdrawn": a reverse-proxy error, a route mismatch, a downgraded
// server or anyone able to answer on the path can produce one. Only a SIGNED
// document (an explicit none, an empty retire version, a newer version) may
// empty a node's cache.
//
// One row per rail, all against the same gated fake: each row seeds a
// verified document through a real 200, flips the server to answer 404 for
// everything, fetches again, and asserts the node still holds what it
// verified. The policy-resource row covers the client half; the daemon half
// (publishPolicyResourceResult's PRNone dispatch) is pinned by
// cmd/observer's TestPolicyResourceDispositionTable.

// gatedRailServer serves one JSON document per path until down is set, then
// answers every request 404.
type gatedRailServer struct {
	srv    *httptest.Server
	down   atomic.Bool
	mu     sync.Mutex
	routes map[string]any
}

func newGatedRailServer(t *testing.T) *gatedRailServer {
	t.Helper()
	gs := &gatedRailServer{routes: map[string]any{}}
	gs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gs.down.Load() {
			writeTestJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		gs.mu.Lock()
		doc, ok := gs.routes[r.URL.Path]
		gs.mu.Unlock()
		if !ok {
			writeTestJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		writeTestJSON(w, http.StatusOK, doc)
	}))
	t.Cleanup(gs.srv.Close)
	return gs
}

func (gs *gatedRailServer) serve(path string, doc any) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.routes[path] = doc
}

func TestEverySignedRail404NeverClears(t *testing.T) {
	ctx := context.Background()
	rails := []struct {
		name string
		run  func(t *testing.T, gs *gatedRailServer)
	}{
		{"budget", func(t *testing.T, gs *gatedRailServer) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			c, s, _ := enrolledClient(t, gs.srv.URL)
			pinRoutingKey(t, s, encodeStdKey(pub))
			doc, _ := orgcontract.SignBudgetPolicy(priv, "org-1", "scim-42", budgetBody(41, 40_000_000))
			gs.serve("/api/agent/budget", doc)
			if out, err := c.FetchBudgetPolicy(ctx); err != nil || !out.HaveBody {
				t.Fatalf("seed: %+v err=%v", out, err)
			}
			gs.down.Store(true)
			out, _ := c.FetchBudgetPolicy(ctx)
			if !out.HaveBody || out.Body.CapTokens != 40_000_000 {
				t.Fatalf("live budget after 404 = %+v, want the verified cap", out)
			}
			persisted, err := s.LoadOrgBudget(ctx)
			if err != nil || !persisted.Have || persisted.Body.CapTokens != 40_000_000 {
				t.Fatalf("durable budget after 404 = %+v err=%v", persisted, err)
			}
		}},
		{"pricing", func(t *testing.T, gs *gatedRailServer) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			c, s, _ := enrolledClient(t, gs.srv.URL)
			enabledPricing(c)
			pinRoutingKey(t, s, encodeStdKey(pub))
			doc, _ := orgcontract.SignPricingPolicy(priv, "org-1", pricingBody(7, "m", 4, 20))
			gs.serve("/api/agent/pricing", doc)
			if out, err := c.FetchPricingPolicy(ctx); err != nil || !out.HaveBody {
				t.Fatalf("seed: %+v err=%v", out, err)
			}
			gs.down.Store(true)
			out, _ := c.FetchPricingPolicy(ctx)
			if !out.HaveBody {
				t.Fatalf("live pricing after 404 = %+v, want the verified body", out)
			}
			cached, err := s.LoadOrgPricing(ctx)
			if err != nil || !cached.Have || cached.Version != 7 {
				t.Fatalf("durable pricing after 404 = %+v err=%v", cached, err)
			}
		}},
		{"display", func(t *testing.T, gs *gatedRailServer) {
			_, priv, _ := ed25519.GenerateKey(rand.Reader)
			c, s, _ := enrolledClient(t, gs.srv.URL)
			gs.serve("/api/agent/display-policy", mustSignDisplay(t, priv, "org-1", 1, "tokens"))
			if changed, err := c.FetchOrgDisplayPolicy(ctx); err != nil || !changed {
				t.Fatalf("seed: changed=%v err=%v", changed, err)
			}
			gs.down.Store(true)
			_, _ = c.FetchOrgDisplayPolicy(ctx)
			row, ok, err := s.GetOrgDisplayPolicy(ctx)
			if err != nil || !ok || row.Version != 1 || row.DeveloperCostDisplay != "tokens" {
				t.Fatalf("display after 404 = %+v ok=%v err=%v", row, ok, err)
			}
		}},
		{"routing", func(t *testing.T, gs *gatedRailServer) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			c, s, _ := enrolledClient(t, gs.srv.URL)
			gs.serve("/api/agent/routing-policy", signedRoutingDoc(priv, pub, 3, "[routing]\n"))
			if changed, _, err := c.FetchRoutingPolicy(ctx); err != nil || !changed {
				t.Fatalf("seed: changed=%v err=%v", changed, err)
			}
			gs.down.Store(true)
			_, _, _ = c.FetchRoutingPolicy(ctx)
			row, ok, err := s.GetOrgRoutingPolicy(ctx)
			if err != nil || !ok || row.Version != 3 {
				t.Fatalf("routing after 404 = %+v ok=%v err=%v", row, ok, err)
			}
		}},
		{"announcement", func(t *testing.T, gs *gatedRailServer) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			c, s, _ := enrolledClient(t, gs.srv.URL)
			gs.serve("/api/agent/announcement", signAnnouncementDoc(2, testAnnBody, priv, pub))
			if changed, err := c.FetchOrgAnnouncement(ctx); err != nil || !changed {
				t.Fatalf("seed: changed=%v err=%v", changed, err)
			}
			gs.down.Store(true)
			_, _ = c.FetchOrgAnnouncement(ctx)
			row, ok, err := s.GetOrgAnnouncement(ctx)
			if err != nil || !ok || row.Version != 2 {
				t.Fatalf("announcement after 404 = %+v ok=%v err=%v", row, ok, err)
			}
		}},
		{"guard bundle", func(t *testing.T, gs *gatedRailServer) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			c, s, cachePath := enrolledClient(t, gs.srv.URL)
			gs.serve("/api/v1/policy-bundle", signedTestBundle(t, priv, pub, 3, escalatingTOML))
			if res, err := c.FetchPolicyBundle(ctx, cachePath); err != nil || res.Status != PolicyApplied {
				t.Fatalf("seed: %+v err=%v", res, err)
			}
			gs.down.Store(true)
			if res, err := c.FetchPolicyBundle(ctx, cachePath); err != nil || res.Status != PolicyNone {
				t.Fatalf("404 poll: %+v err=%v", res, err)
			}
			if _, err := os.Stat(cachePath); err != nil {
				t.Fatalf("a 404 removed the verified bundle cache: %v", err)
			}
			if row := orgStateRow(t, s, policyBundleStatePath(gs.srv.URL)); row == nil || row.Version != "3" {
				t.Fatalf("guard policy state after 404 = %+v, want v3", row)
			}
		}},
		{"policy resource", func(t *testing.T, gs *gatedRailServer) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			c, _, cacheDir := enrolledPRClient(t, gs.srv.URL)
			r1 := signedTestResource(t, priv, pub, policyfamAdmissionInput, 1, observeAdmissionBody, nil)
			gs.serve("/api/agent/policy/"+policyfamAdmissionInput, r1)
			opts := PolicyResourceOptions{CacheDir: cacheDir, AcceptFamilies: []string{policyfamAdmissionInput}}
			seeded, err := c.FetchAndAcceptPolicyResource(ctx, policyfamAdmissionInput, opts)
			if err != nil || seeded.Status != PRApplied {
				t.Fatalf("seed: %+v err=%v", seeded, err)
			}
			gs.down.Store(true)
			res, err := c.FetchAndAcceptPolicyResource(ctx, policyfamAdmissionInput, opts)
			if err != nil || res.Status != PRNone {
				t.Fatalf("404 poll: %+v err=%v", res, err)
			}
			if _, err := os.Stat(seeded.CachePath); err != nil {
				t.Fatalf("a 404 removed the verified resource cache: %v", err)
			}
		}},
		{"intelligence results", func(t *testing.T, gs *gatedRailServer) {
			c, s := enrolledIntelClient(t, gs.srv.URL)
			if err := s.UpsertOrgIntelResult(ctx, store.OrgIntelResult{
				OrgID: "org-1", SessionID: "s9", JobID: "j9", Title: "kept", SchemaVersion: "v2",
			}); err != nil {
				t.Fatalf("seed: %v", err)
			}
			gs.down.Store(true)
			_, _ = c.FetchIntelResults(ctx)
			rows, err := s.OrgIntelResultsForSession(ctx, "s9")
			if err != nil || len(rows) != 1 || rows[0].Title != "kept" {
				t.Fatalf("intel cache after 404 = %+v err=%v", rows, err)
			}
		}},
	}
	for _, rail := range rails {
		t.Run(rail.name, func(t *testing.T) {
			rail.run(t, newGatedRailServer(t))
		})
	}
}
