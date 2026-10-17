package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestCostDisplayResolverFailsToTokens (Sol A4-03 class): the resolver
// behind the budget-warning messages and the guard's budget verdict text
// must not read dollars on an ENROLLED node whose cached signed policy
// cannot be read. A solo install and an enrolled node whose org never chose
// stay dollars; a verified org choice is honoured.
func TestCostDisplayResolverFailsToTokens(t *testing.T) {
	ctx := context.Background()
	open := func(t *testing.T) (*store.Store, func(string)) {
		t.Helper()
		database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "observer.db")})
		if err != nil {
			t.Fatalf("db.Open: %v", err)
		}
		t.Cleanup(func() { _ = database.Close() })
		exec := func(q string) {
			if _, err := database.ExecContext(ctx, q); err != nil {
				t.Fatalf("exec %q: %v", q, err)
			}
		}
		return store.New(database), exec
	}
	enrol := func(t *testing.T, st *store.Store) {
		t.Helper()
		if err := st.WriteEnrolment(ctx, store.Enrolment{
			OrgID: "org-1", OrgName: "Acme", OrgServerURL: "https://org.example.test",
			UserID: "member-1", Tenancy: orgcontract.TenancyManaged,
		}); err != nil {
			t.Fatalf("WriteEnrolment: %v", err)
		}
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, st *store.Store, exec func(string))
		want  string
	}{
		{"solo install: dollars", func(*testing.T, *store.Store, func(string)) {}, orgcontract.DisplayCostUSD},
		{"enrolled, org never chose: dollars", func(t *testing.T, st *store.Store, _ func(string)) { enrol(t, st) }, orgcontract.DisplayCostUSD},
		{"enrolled, org chose tokens", func(t *testing.T, st *store.Store, _ func(string)) {
			enrol(t, st)
			if err := st.UpsertOrgDisplayPolicy(ctx, store.OrgDisplayPolicyRow{
				OrgID: "org-1", Version: 1, Body: "{}", BodyHash: "h", Signature: "s", ServerPubkey: "k",
				DeveloperCostDisplay: "tokens", ReceivedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatal(err)
			}
		}, orgcontract.DisplayCostTokens},
		{"enrolled, cached policy unreadable: tokens", func(t *testing.T, st *store.Store, exec func(string)) {
			enrol(t, st)
			exec(`DROP TABLE org_display_policy`)
		}, orgcontract.DisplayCostTokens},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, exec := open(t)
			c.setup(t, st, exec)
			if got := newCostDisplayResolver(st).Mode(); got != c.want {
				t.Fatalf("Mode() = %q, want %q", got, c.want)
			}
		})
	}
}
