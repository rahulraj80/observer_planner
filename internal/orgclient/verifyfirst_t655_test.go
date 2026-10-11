package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// verifyFirstServer serves one scripted document per rail path.
type verifyFirstServer struct {
	mu   sync.Mutex
	docs map[string]any
	srv  *httptest.Server
}

func newVerifyFirstServer(t *testing.T) *verifyFirstServer {
	t.Helper()
	vs := &verifyFirstServer{docs: map[string]any{}}
	vs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vs.mu.Lock()
		doc, ok := vs.docs[r.URL.Path]
		vs.mu.Unlock()
		if !ok {
			writeTestJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		writeTestJSON(w, http.StatusOK, doc)
	}))
	t.Cleanup(vs.srv.Close)
	return vs
}

func (vs *verifyFirstServer) set(path string, doc any) {
	vs.mu.Lock()
	vs.docs[path] = doc
	vs.mu.Unlock()
}

// zeroSig is a well-formed signature that verifies against no key.
var zeroSig = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))

// TestRailsVerifyBeforeVersionCompare pins T-655 (the T-390 shape on four
// more rails): the display-policy, announcement, routing and agent-config
// rails verify an offered document BEFORE they compare its version with the
// cached one. A document at or below the cached version that does not verify
// (a corrupted signature, or one signed for another member) is REFUSED with
// an error, never read as "unchanged", and the cache keeps its verified row.
// The verified document at the cached version stays the quiet no-op.
func TestRailsVerifyBeforeVersionCompare(t *testing.T) {
	ctx := context.Background()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	type rail struct {
		name string
		path string
		good func(version int64) any
		bad  []struct {
			name string
			doc  func(version int64) any
		}
		fetch  func(c *Client) (bool, error)
		cached func(s *store.Store) int64
	}
	display := func(v int64) orgcontract.DisplayPolicyDoc {
		return mustSignDisplay(t, priv, "org-1", v, "tokens")
	}
	routing := func(v int64) orgcontract.RoutingPolicyDoc {
		d := signedRoutingDoc(priv, pub, v, "[routing]\n")
		return d
	}
	agentCfg := func(sub string, v int64) orgcontract.AgentConfigPolicyDoc {
		return mustSignAgentConfig(t, priv, "org-1", sub, v, orgcontract.AgentConfigDirective{
			Setting: "codex/model", Value: json.RawMessage(`"gpt-5-codex"`), Mode: "enforced", Layer: "o1_org", Policy: "Baseline",
		})
	}
	rails := []rail{
		{
			name: "display-policy", path: "/api/agent/display-policy",
			good: func(v int64) any { return display(v) },
			bad: []struct {
				name string
				doc  func(version int64) any
			}{
				{"corrupt signature", func(v int64) any { d := display(v); d.Signature = zeroSig; return d }},
			},
			fetch: func(c *Client) (bool, error) { return c.FetchOrgDisplayPolicy(ctx) },
			cached: func(s *store.Store) int64 {
				r, _, _ := s.GetOrgDisplayPolicy(ctx)
				return r.Version
			},
		},
		{
			name: "announcement", path: "/api/agent/announcement",
			good: func(v int64) any { return signAnnouncementDoc(v, testAnnBody, priv, pub) },
			bad: []struct {
				name string
				doc  func(version int64) any
			}{
				{"corrupt signature", func(v int64) any {
					d := signAnnouncementDoc(v, testAnnBody, priv, pub)
					d.Signature = zeroSig
					return d
				}},
			},
			fetch: func(c *Client) (bool, error) { return c.FetchOrgAnnouncement(ctx) },
			cached: func(s *store.Store) int64 {
				r, _, _ := s.GetOrgAnnouncement(ctx)
				return r.Version
			},
		},
		{
			name: "routing", path: "/api/agent/routing-policy",
			good: func(v int64) any { return routing(v) },
			bad: []struct {
				name string
				doc  func(version int64) any
			}{
				{"corrupt v1 signature", func(v int64) any { d := routing(v); d.Signature = zeroSig; return d }},
				{"corrupt v2 signature", func(v int64) any { d := routing(v); d.SignatureV2 = zeroSig; return d }},
			},
			fetch: func(c *Client) (bool, error) {
				changed, _, err := c.FetchRoutingPolicy(ctx)
				return changed, err
			},
			cached: func(s *store.Store) int64 {
				r, _, _ := s.GetOrgRoutingPolicy(ctx)
				return r.Version
			},
		},
		{
			name: "agent-config", path: orgcontract.AgentConfigPolicyPath,
			good: func(v int64) any { return agentCfg("scim-42", v) },
			bad: []struct {
				name string
				doc  func(version int64) any
			}{
				{"corrupt signature", func(v int64) any { d := agentCfg("scim-42", v); d.Signature = zeroSig; return d }},
				{"another member's signature relabelled", func(v int64) any {
					d := agentCfg("someone-else", v)
					d.Subject = "scim-42"
					return d
				}},
			},
			fetch: func(c *Client) (bool, error) { return c.FetchAgentConfigPolicy(ctx) },
			cached: func(s *store.Store) int64 {
				r, _, _ := s.GetOrgAgentConfigPolicy(ctx)
				return r.Version
			},
		},
	}

	for _, rl := range rails {
		t.Run(rl.name, func(t *testing.T) {
			vs := newVerifyFirstServer(t)
			s := newAgentStore(t)
			if err := s.WriteEnrolment(ctx, store.Enrolment{
				OrgID: "org-1", OrgName: "Acme", OrgServerURL: vs.srv.URL, UserID: "scim-42", UserEmail: "dev@acme.example",
				EnrolledAt: time.Now().UTC().Format(time.RFC3339), BearerKeyID: "test", Tenancy: orgcontract.TenancyManaged,
			}); err != nil {
				t.Fatal(err)
			}
			c := newTestClient(t, s, &memBearerStore{bearer: "bearer-xyz"})
			c.SetAgentConfigPolicyGate(func(context.Context) bool { return true })

			vs.set(rl.path, rl.good(2))
			if changed, err := rl.fetch(c); err != nil || !changed {
				t.Fatalf("first fetch: changed=%v err=%v", changed, err)
			}
			for _, b := range rl.bad {
				for _, v := range []int64{2, 1} {
					vs.set(rl.path, b.doc(v))
					changed, err := rl.fetch(c)
					if err == nil || changed {
						t.Errorf("%s at v%d: changed=%v err=%v; want a refusal", b.name, v, changed, err)
					}
					if got := rl.cached(s); got != 2 {
						t.Errorf("%s at v%d: cache now v%d", b.name, v, got)
					}
				}
			}
			vs.set(rl.path, rl.good(2))
			if changed, err := rl.fetch(c); err != nil || changed {
				t.Fatalf("verified same version: changed=%v err=%v; want the quiet no-op", changed, err)
			}
		})
	}
}
