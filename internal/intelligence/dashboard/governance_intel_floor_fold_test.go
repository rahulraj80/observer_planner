package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/govern"
	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

// governedProviderAs is governedProvider for a node of chosen tenancy: managed
// resolves under a managed-class consent (so the managed defaults apply too),
// otherwise an individual consent.
func governedProviderAs(t *testing.T, managed bool, settingsHidden, settingsReadOnly []string) GovernanceProvider {
	t.Helper()
	spec, err := nodegov.Compile(nodegov.PolicyInput{
		HiddenSettings: settingsHidden, ReadOnlySettings: settingsReadOnly,
		Notice: nodegov.Notice{OrgDisplayName: "Acme Platform Engineering"},
	})
	if err != nil {
		t.Fatalf("nodegov.Compile: %v", err)
	}
	now := time.Now().UTC()
	grant := &govern.Grant{
		OrgKey: "ok", Generation: 1, OrgName: "Acme", KeyPinSHA256: "pin",
		Authority: []string{govern.AuthorityDashboardVisibility}, ExpiresAt: now.Add(24 * time.Hour),
	}
	if managed {
		grant.ConsentMode = govern.ConsentManaged
	}
	live := govern.LiveIdentity{Enrolled: true, OrgKey: "ok", Generation: 1, KeyPinSHA256: "pin"}
	eff := govern.Resolve(govern.Delivered{Present: true, Version: 14, BodyHash: "bh", Spec: spec}, grant, live, now)
	if !eff.Active || eff.State != govern.StateApplied || eff.Managed != managed {
		t.Fatalf("test fixture did not resolve to an applied posture of the asked tenancy: %+v", eff)
	}
	return func(context.Context) govern.Effective { return eff }
}

// TestIntelConsentStatusNeverHiddenOnIndividualNode pins T-379 / review
// M4-07 for the banner's read: GET /api/intel/status passes the guard on an
// individual node whose org hides `intel`, and is refused on a managed one.
func TestIntelConsentStatusNeverHiddenOnIndividualNode(t *testing.T) {
	for _, tc := range []struct {
		managed bool
		refused bool
	}{{false, false}, {true, true}} {
		s := newRemoteTestServer(t, Options{Governance: governedProviderAs(t, tc.managed, []string{"intel"}, nil)})
		rec := httptest.NewRecorder()
		s.guardedHandler("127.0.0.1:8081").ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/intel/status"))
		if got := strings.Contains(rec.Body.String(), "governance_hidden"); got != tc.refused {
			t.Fatalf("managed=%v: GET /api/intel/status = %d %s, want refused=%v", tc.managed, rec.Code, rec.Body.String(), tc.refused)
		}
	}
}
