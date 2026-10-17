package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

func mustSignAgentConfig(t *testing.T, priv ed25519.PrivateKey, org, sub string, version int64, dirs ...orgcontract.AgentConfigDirective) orgcontract.AgentConfigPolicyDoc {
	t.Helper()
	d, err := orgcontract.SignAgentConfigPolicy(priv, org, sub, version, orgcontract.AgentConfigPolicyBody{Directives: dirs})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestFetchAgentConfigPolicy walks the D-016 state rules against one managed
// node, and pins that the rail is never called on an individual node or
// with the gate off.
func TestFetchAgentConfigPolicy(t *testing.T) {
	ctx := context.Background()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	var status atomic.Int32
	var doc atomic.Value
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != orgcontract.AgentConfigPolicyPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		if st := int(status.Load()); st != http.StatusOK {
			writeTestJSON(w, st, map[string]string{"error": "scripted"})
			return
		}
		writeTestJSON(w, http.StatusOK, doc.Load())
	}))
	t.Cleanup(srv.Close)

	s := newAgentStore(t)
	enr := store.Enrolment{
		OrgID: "org-1", OrgName: "Acme", OrgServerURL: srv.URL, UserID: "scim-42", UserEmail: "dev@acme.example",
		EnrolledAt: time.Now().UTC().Format(time.RFC3339), BearerKeyID: "test", Tenancy: orgcontract.TenancyIndividual,
	}
	if err := s.WriteEnrolment(ctx, enr); err != nil {
		t.Fatal(err)
	}
	c := newTestClient(t, s, &memBearerStore{bearer: "bearer-xyz"})

	model := orgcontract.AgentConfigDirective{Setting: "codex/model", Value: json.RawMessage(`"gpt-5-codex"`), Mode: "enforced", Layer: "o1_org", Policy: "Baseline"}
	v2 := mustSignAgentConfig(t, priv, "org-1", "scim-42", 2, model)
	doc.Store(v2)
	status.Store(http.StatusOK)

	// Gate off: no request.
	if changed, err := c.FetchAgentConfigPolicy(ctx); changed || err != nil || calls.Load() != 0 {
		t.Fatalf("gate off: changed=%v err=%v calls=%d", changed, err, calls.Load())
	}
	// Gate on but the enrolment is individual: still no request.
	c.SetAgentConfigPolicyGate(func(context.Context) bool { return true })
	if changed, err := c.FetchAgentConfigPolicy(ctx); changed || err != nil || calls.Load() != 0 {
		t.Fatalf("individual: changed=%v err=%v calls=%d", changed, err, calls.Load())
	}
	enr.Tenancy = orgcontract.TenancyManaged
	if err := s.WriteEnrolment(ctx, enr); err != nil {
		t.Fatal(err)
	}

	inflated := v2
	inflated.Version = 90
	empty := mustSignAgentConfig(t, priv, "org-1", "scim-42", 5)
	steps := []struct {
		name        string
		status      int
		doc         any
		wantChanged bool
		wantErr     bool
		wantVersion int64
		wantDirs    int
	}{
		{"signed v2 is cached", 200, v2, true, false, 2, 1},
		{"same version is a no-op", 200, v2, false, false, 2, 1},
		{"404 keeps the cache", 404, nil, false, false, 2, 1},
		{"5xx keeps the cache", 503, nil, false, true, 2, 1},
		{"another member's body refused", 200, mustSignAgentConfig(t, priv, "org-1", "scim-7", 3, model), false, true, 2, 1},
		{"another org's body refused", 200, mustSignAgentConfig(t, priv, "org-2", "scim-42", 3, model), false, true, 2, 1},
		{"a foreign key refused", 200, mustSignAgentConfig(t, otherPriv, "org-1", "scim-42", 3, model), false, true, 2, 1},
		{"a replay at an inflated version refused", 200, inflated, false, true, 2, 1},
		{"a signed EMPTY body withdraws", 200, empty, true, false, 5, 0},
		{"an older genuine body is not re-applied", 200, v2, false, false, 5, 0},
	}
	for _, st := range steps {
		status.Store(int32(st.status))
		if st.doc != nil {
			doc.Store(st.doc)
		}
		changed, err := c.FetchAgentConfigPolicy(ctx)
		if (err != nil) != st.wantErr || changed != st.wantChanged {
			t.Fatalf("%s: changed=%v err=%v", st.name, changed, err)
		}
		row, ok, err := c.AgentConfigPolicy(ctx)
		if err != nil || !ok || row.Version != st.wantVersion {
			t.Fatalf("%s: cache = %+v ok=%v err=%v", st.name, row, ok, err)
		}
		body, _ := orgcontract.DecodeAgentConfigPolicyBody(row.Body)
		if len(body.Directives) != st.wantDirs {
			t.Fatalf("%s: directives = %d, want %d", st.name, len(body.Directives), st.wantDirs)
		}
	}
}

// TestAgentConfigRailDispatch pins the two halves of the rail tables: the
// server's rail key maps onto this rail's fetch, and the push cycle counts it.
func TestAgentConfigRailDispatch(t *testing.T) {
	if !pushCycleRails[orgcontract.RailAgentConfig] {
		t.Fatal("agent_config is not a push-cycle rail")
	}
	found := false
	for _, row := range railFetchers {
		for _, k := range row.keys {
			if k == orgcontract.RailAgentConfig {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no railFetchers row answers agent_config")
	}
}
