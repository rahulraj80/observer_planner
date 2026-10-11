package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestEnrolmentGrantInitialComponentsRoundTrip pins agent migration 159
// (modular packaging P4, lane MP-GOV): a grant's signed initial component
// set is stored with the grant and read back exactly (it is bound into the
// signature, so a lost or reordered set would stop the row verifying); no
// set reads back nil; an empty set reads back empty (core only); a grant
// replacement without a set keeps the stored set, one with a set (T-707)
// replaces it.
func TestEnrolmentGrantInitialComponentsRoundTrip(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s := New(database)
	base := EnrolmentGrant{OrgKey: "k", OrgID: "o", GrantedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}

	cases := []struct {
		name string
		set  *orgcontract.InitialComponentSet
	}{
		{"no set", nil},
		{"empty set is core only", &orgcontract.InitialComponentSet{CatalogVersion: 1, IDs: []string{}}},
		{"a set", &orgcontract.InitialComponentSet{CatalogVersion: 1, IDs: []string{"codeintel", "org.client"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := base
			g.InitialComponents = tc.set
			if err := s.WriteEnrolmentGrant(ctx, g); err != nil {
				t.Fatal(err)
			}
			got, ok, err := s.LoadEnrolmentGrant(ctx, "k")
			if err != nil || !ok {
				t.Fatalf("load: %v %v", ok, err)
			}
			switch {
			case tc.set == nil && got.InitialComponents != nil:
				t.Fatalf("no set read back as %+v", got.InitialComponents)
			case tc.set != nil && got.InitialComponents == nil:
				t.Fatal("set lost")
			case tc.set != nil && (got.InitialComponents.CatalogVersion != tc.set.CatalogVersion || len(got.InitialComponents.IDs) != len(tc.set.IDs)):
				t.Fatalf("set = %+v, want %+v", got.InitialComponents, tc.set)
			}
		})
	}
	// A replacement keeps the stored set (clearing it would widen the node
	// to its whole compiled set).
	repl := base
	repl.ReplacementGeneration = 1
	if applied, err := s.ReplaceEnrolmentGrant(ctx, repl); err != nil || !applied {
		t.Fatalf("replace: %v %v", applied, err)
	}
	got, _, _ := s.LoadEnrolmentGrant(ctx, "k")
	if got.InitialComponents == nil || len(got.InitialComponents.IDs) != 2 {
		t.Fatalf("replacement dropped the set: %+v", got.InitialComponents)
	}
	// ...WITH the earlier grant that signed it (MODPKG-17 (1)): its own
	// signature never covered the set.
	if p := got.InitialComponentsProof; p == nil || p.ReplacementGeneration != 0 ||
		!orgcontract.SameInitialComponentSet(p.InitialComponents, got.InitialComponents) {
		t.Fatalf("kept set carries no proof of the enrolment grant: %+v", got.InitialComponentsProof)
	}
	// A replacement carrying its own signed set (T-707) replaces it.
	repl.ReplacementGeneration = 2
	repl.InitialComponents = &orgcontract.InitialComponentSet{CatalogVersion: 5, IDs: []string{"guard"}}
	if applied, err := s.ReplaceEnrolmentGrant(ctx, repl); err != nil || !applied {
		t.Fatalf("replace with set: %v %v", applied, err)
	}
	got, _, _ = s.LoadEnrolmentGrant(ctx, "k")
	if got.InitialComponents == nil || got.InitialComponents.CatalogVersion != 5 || len(got.InitialComponents.IDs) != 1 {
		t.Fatalf("replacement set not stored: %+v", got.InitialComponents)
	}
	if got.InitialComponentsProof != nil {
		t.Fatalf("a set the replacement signed itself kept a stale proof: %+v", got.InitialComponentsProof)
	}
}
