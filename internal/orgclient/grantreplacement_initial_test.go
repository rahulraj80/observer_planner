package orgclient

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestAcceptGrantReplacementInitialComponents is T-707's dual-signature
// rail, both compat directions on the node:
//
//   - a new server's replacement with a set: stored with the set and the
//     signature that covers it, and the stored row re-verifies;
//   - an old server's replacement without a set: accepted, keeping a set an
//     earlier grant carried together with that grant as its proof, and
//     re-verifies; an edit of the kept set does not (REVIEW-4 P4-10);
//   - an OLD node reading a new server's replacement: the primary signature
//     alone still verifies over the bytes an old node computes (the decoded
//     struct without the two new fields);
//   - a set without its signature, a signature without its set, or a set
//     edited after signing: refused whole.
func TestAcceptGrantReplacementInitialComponents(t *testing.T) {
	ctx := context.Background()
	const orgURL = "https://org.example.com"
	const orgID = "org-1"
	set := &orgcontract.InitialComponentSet{CatalogVersion: 3, IDs: []string{"codeintel", "guard"}}

	type fixture struct {
		r    orgcontract.GrantReplacement
		c    *Client
		priv ed25519.PrivateKey
		load func() orgcontract.StoredGrantDocument
	}
	withSet := func(t *testing.T) fixture {
		t.Helper()
		s := newAgentStore(t)
		c := newTestClient(t, s, &memBearerStore{})
		pub, priv := seedEnrolledNode(t, s, orgURL, orgID)
		r := validReplacementWithAuthority(t, priv, pub, orgURL, orgID, 1, []string{"components.govern"})
		r.InitialComponents = set
		r.InitialSignature = orgcontract.SignGrantReplacementInitial(priv, r)
		load := func() orgcontract.StoredGrantDocument {
			t.Helper()
			g, ok, err := s.LoadEnrolmentGrant(ctx, OrgKey(orgURL, orgID))
			if err != nil || !ok {
				t.Fatalf("LoadEnrolmentGrant: ok=%v err=%v", ok, err)
			}
			doc := orgcontract.StoredGrantDocument{
				OrgID: g.OrgID, OrgServerURL: g.OrgServerURL, KeyPinSHA256: g.KeyPinSHA256,
				Authority:         g.Authority,
				GrantedAt:         g.GrantedAt.UTC().Format(time.RFC3339),
				ExpiresAt:         g.SignedExpiresAt.UTC().Format(time.RFC3339),
				InitialComponents: g.InitialComponents, Signature: g.Signature,
				ReplacementGeneration: g.ReplacementGeneration,
				InitialProof:          g.InitialComponentsProof,
			}
			if err := orgcontract.VerifyStoredGrant(doc, pub); err != nil {
				t.Fatalf("stored replacement does not re-verify: %v", err)
			}
			return doc
		}
		return fixture{r: r, c: c, priv: priv, load: load}
	}
	// keptSetRow accepts f.r (with its set) and then a set-less generation 2,
	// and returns the stored row of the second, whose set was kept.
	keptSetRow := func(t *testing.T, f fixture) (orgcontract.StoredGrantDocument, orgcontract.GrantReplacement) {
		t.Helper()
		if ok, err := f.c.AcceptGrantReplacement(ctx, f.r); err != nil || !ok {
			t.Fatalf("accept with set: ok=%v err=%v", ok, err)
		}
		old := f.r
		old.Generation = 2
		old.InitialComponents, old.InitialSignature = nil, ""
		old.Signature = orgcontract.SignGrantReplacement(f.priv, old)
		if ok, err := f.c.AcceptGrantReplacement(ctx, old); err != nil || !ok {
			t.Fatalf("accept set-less: ok=%v err=%v", ok, err)
		}
		return f.load(), old
	}
	widened := &orgcontract.InitialComponentSet{CatalogVersion: 3, IDs: []string{"codeintel", "guard", "remote"}}

	t.Run("new server, new node: set stored and re-verifies", func(t *testing.T) {
		f := withSet(t)
		if ok, err := f.c.AcceptGrantReplacement(ctx, f.r); err != nil || !ok {
			t.Fatalf("accept: ok=%v err=%v", ok, err)
		}
		doc := f.load()
		if doc.InitialComponents == nil || len(doc.InitialComponents.IDs) != 2 || doc.InitialComponents.CatalogVersion != 3 {
			t.Fatalf("stored set = %+v", doc.InitialComponents)
		}
	})

	t.Run("old server, new node: no set, an earlier set is kept and still verifies", func(t *testing.T) {
		f := withSet(t)
		// Clearing the set would widen the node to its whole compiled set,
		// so it is kept with the earlier grant that signed it, and the row
		// re-verifies (f.load checks it).
		doc, _ := keptSetRow(t, f)
		if doc.InitialComponents == nil || len(doc.InitialComponents.IDs) != 2 {
			t.Fatalf("a set-less replacement dropped the earlier set: %+v", doc.InitialComponents)
		}
		if doc.InitialProof == nil || doc.InitialProof.ReplacementGeneration != 1 {
			t.Fatalf("the kept set carries no proof of the grant that signed it: %+v", doc.InitialProof)
		}
		// A third, set-less generation keeps the SAME proof (never a chain).
		third := f.r
		third.Generation = 3
		third.InitialComponents, third.InitialSignature = nil, ""
		third.Signature = orgcontract.SignGrantReplacement(f.priv, third)
		if ok, err := f.c.AcceptGrantReplacement(ctx, third); err != nil || !ok {
			t.Fatalf("accept third: ok=%v err=%v", ok, err)
		}
		if doc := f.load(); doc.InitialProof == nil || doc.InitialProof.ReplacementGeneration != 1 {
			t.Fatalf("third generation proof = %+v, want generation 1's", doc.InitialProof)
		}
	})

	// REVIEW-4 P4-10 / MODPKG-17 (1), red before the fix: the set-less
	// fallback accepted ANY set beside a signature over the set-less message.
	t.Run("a kept set edited on disk fails re-verification", func(t *testing.T) {
		f := withSet(t)
		doc, _ := keptSetRow(t, f)
		pub, err := orgcontract.VerifyGrantReplacement(f.r)
		if err != nil {
			t.Fatal(err)
		}
		edited := doc
		edited.InitialComponents = widened
		if err := orgcontract.VerifyStoredGrant(edited, pub); err == nil {
			t.Fatal("a widened kept set still verifies")
		}
		// Dropping the proof does not help: the set-less signature never
		// covered a set.
		edited.InitialProof = nil
		if err := orgcontract.VerifyStoredGrant(edited, pub); err == nil {
			t.Fatal("a widened kept set with its proof removed still verifies")
		}
	})

	t.Run("the primary signature swapped in beside an edited set fails", func(t *testing.T) {
		f := withSet(t)
		if ok, err := f.c.AcceptGrantReplacement(ctx, f.r); err != nil || !ok {
			t.Fatalf("accept: ok=%v err=%v", ok, err)
		}
		doc := f.load()
		pub, err := orgcontract.VerifyGrantReplacement(f.r)
		if err != nil {
			t.Fatal(err)
		}
		// The node holds the replacement's primary signature (its own push
		// response carries it); swapping it in with any set must not verify.
		doc.Signature = f.r.Signature
		doc.InitialComponents = widened
		if err := orgcontract.VerifyStoredGrant(doc, pub); err == nil {
			t.Fatal("primary signature beside an edited set still verifies")
		}
	})

	t.Run("an edited set stored with its initial signature fails re-verification", func(t *testing.T) {
		f := withSet(t)
		if ok, err := f.c.AcceptGrantReplacement(ctx, f.r); err != nil || !ok {
			t.Fatalf("accept: ok=%v err=%v", ok, err)
		}
		doc := f.load()
		doc.InitialComponents = widened
		pub, err := orgcontract.VerifyGrantReplacement(f.r)
		if err != nil {
			t.Fatal(err)
		}
		if err := orgcontract.VerifyStoredGrant(doc, pub); err == nil {
			t.Fatal("a widened stored set still verifies")
		}
	})

	t.Run("new server, old node: the primary signature alone verifies", func(t *testing.T) {
		f := withSet(t)
		raw, err := json.Marshal(f.r)
		if err != nil {
			t.Fatal(err)
		}
		// An old node's struct has neither field: decoding drops them, and it
		// verifies exactly the bytes it computes from what is left.
		var legacy orgcontract.GrantReplacement
		if err := json.Unmarshal(raw, &legacy); err != nil {
			t.Fatal(err)
		}
		legacy.InitialComponents, legacy.InitialSignature = nil, ""
		if _, err := orgcontract.VerifyGrantReplacement(legacy); err != nil {
			t.Fatalf("an old node would refuse a new server's replacement: %v", err)
		}
	})

	refusals := map[string]func(r *orgcontract.GrantReplacement){
		"set without its signature": func(r *orgcontract.GrantReplacement) { r.InitialSignature = "" },
		"signature without its set": func(r *orgcontract.GrantReplacement) { r.InitialComponents = nil },
		"set widened after signing": func(r *orgcontract.GrantReplacement) {
			r.InitialComponents = &orgcontract.InitialComponentSet{CatalogVersion: 3, IDs: []string{"codeintel", "guard", "remote"}}
		},
		"primary signature reused as the initial one": func(r *orgcontract.GrantReplacement) { r.InitialSignature = r.Signature },
	}
	for name, tamper := range refusals {
		t.Run("refused: "+name, func(t *testing.T) {
			f := withSet(t)
			tamper(&f.r)
			ok, err := f.c.AcceptGrantReplacement(ctx, f.r)
			if err != nil || ok {
				t.Fatalf("tampered replacement: ok=%v err=%v, want refused", ok, err)
			}
		})
	}
}
