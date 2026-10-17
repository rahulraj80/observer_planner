package orgcontract

import (
	"testing"
)

// TestVerifyKeptInitialSet is the table for a replacement row whose initial
// set was KEPT from an earlier grant (REVIEW-4 P4-10, ledger MODPKG-17 (1)):
// the row verifies over the set-less replacement message, and the set only
// under the earlier grant's own signature, carried as InitialProof. One row
// per binding a swap could try to break.
func TestVerifyKeptInitialSet(t *testing.T) {
	pub, priv, g, _ := storedGrantFixture(t)
	set := &InitialComponentSet{CatalogVersion: 2, IDs: []string{"codeintel", "guard"}}

	// The earlier grant: an enrolment grant that signed the set.
	g.InitialComponents = set
	g.Signature = SignEnrolmentGrant(priv, g)
	proof := StoredGrantDocument{
		OrgID: g.OrgID, OrgServerURL: g.OrgServerURL, KeyPinSHA256: g.KeyPinSHA256,
		Authority: g.Authority, GrantedAt: g.GrantedAt, ExpiresAt: g.ExpiresAt,
		InitialComponents: set, Signature: g.Signature,
	}

	// The set-less replacement that kept it.
	repl := GrantReplacement{
		OrgID: g.OrgID, OrgServerURL: g.OrgServerURL, KeyPinSHA256: g.KeyPinSHA256,
		Generation: 3, Authority: g.Authority, GrantedAt: g.GrantedAt, ExpiresAt: g.ExpiresAt,
	}
	repl.Signature = SignGrantReplacement(priv, repl)
	kept := StoredGrantDocument{
		OrgID: repl.OrgID, OrgServerURL: repl.OrgServerURL, KeyPinSHA256: repl.KeyPinSHA256,
		Authority: repl.Authority, GrantedAt: repl.GrantedAt, ExpiresAt: repl.ExpiresAt,
		Signature: repl.Signature, ReplacementGeneration: repl.Generation,
		InitialComponents: set, InitialProof: &proof,
	}

	with := func(mut func(d *StoredGrantDocument)) StoredGrantDocument {
		d := kept
		p := proof
		d.InitialProof = &p
		mut(&d)
		return d
	}
	widened := &InitialComponentSet{CatalogVersion: 2, IDs: []string{"codeintel", "guard", "remote"}}

	cases := []struct {
		name    string
		doc     StoredGrantDocument
		wantErr bool
	}{
		{name: "kept set with its proof verifies", doc: kept},
		{name: "same set in another order verifies", doc: with(func(d *StoredGrantDocument) {
			d.InitialComponents = &InitialComponentSet{CatalogVersion: 2, IDs: []string{"guard", "codeintel"}}
		})},
		{name: "proof with an unsigned local consent actor still verifies", doc: with(func(d *StoredGrantDocument) {
			d.InitialProof.ConsentMode, d.InitialProof.ConsentActor = "managed", "alice"
		})},
		{name: "row set widened", doc: with(func(d *StoredGrantDocument) { d.InitialComponents = widened }), wantErr: true},
		{name: "row and proof set widened together", doc: with(func(d *StoredGrantDocument) {
			d.InitialComponents = widened
			d.InitialProof.InitialComponents = widened
		}), wantErr: true},
		{name: "catalog version changed", doc: with(func(d *StoredGrantDocument) {
			d.InitialComponents = &InitialComponentSet{CatalogVersion: 9, IDs: set.IDs}
		}), wantErr: true},
		{name: "row authority widened", doc: with(func(d *StoredGrantDocument) {
			d.Authority = CanonicalAuthority(append(append([]string{}, d.Authority...), "enforce.budget"))
		}), wantErr: true},
		{name: "proof for another server", doc: with(func(d *StoredGrantDocument) { d.InitialProof.OrgServerURL = "https://attacker.example" }), wantErr: true},
		{name: "proof not older than the row", doc: with(func(d *StoredGrantDocument) { d.InitialProof.ReplacementGeneration = 3 }), wantErr: true},
		{name: "proof without a set", doc: with(func(d *StoredGrantDocument) { d.InitialProof.InitialComponents = nil }), wantErr: true},
		{name: "proof that is itself kept", doc: with(func(d *StoredGrantDocument) {
			inner := proof
			d.InitialProof.InitialProof = &inner
		}), wantErr: true},
		{name: "proof on an enrolment row", doc: with(func(d *StoredGrantDocument) { d.ReplacementGeneration = 0 }), wantErr: true},
		{name: "proof signature swapped for the row's", doc: with(func(d *StoredGrantDocument) { d.InitialProof.Signature = repl.Signature }), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyStoredGrant(tc.doc, pub)
			if tc.wantErr && err == nil {
				t.Fatal("VerifyStoredGrant accepted a document it must refuse")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("VerifyStoredGrant refused a good document: %v", err)
			}
		})
	}

	// The column codec round-trips the proof, and an older reader that
	// decodes only the set still gets the set.
	raw, ok, err := EncodeStoredInitialComponents(set, &proof)
	if err != nil || !ok {
		t.Fatalf("encode: ok=%v err=%v", ok, err)
	}
	gotSet, gotProof := DecodeStoredInitialComponents(raw)
	if !SameInitialComponentSet(gotSet, set) || gotProof == nil || gotProof.Signature != proof.Signature {
		t.Fatalf("round trip lost the set or proof: %+v %+v", gotSet, gotProof)
	}
	if err := VerifyStoredGrant(func() StoredGrantDocument {
		d := kept
		d.InitialComponents, d.InitialProof = gotSet, gotProof
		return d
	}(), pub); err != nil {
		t.Fatalf("decoded kept row does not verify: %v", err)
	}
}
