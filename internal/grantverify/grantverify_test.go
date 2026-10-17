package grantverify

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/govern"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// TestVerifyAcrossConsentRails is the one verifier's table: per rail, the
// integrity verdict and what the signature covers, plus the tampers that
// must still fail.
func TestVerifyAcrossConsentRails(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	granted := time.Date(2026, 10, 3, 4, 0, 0, 0, time.UTC).Format(time.RFC3339)
	expires := time.Date(2026, 11, 2, 4, 0, 0, 0, time.UTC).Format(time.RFC3339)
	doc := func(mode, actor, signMode, signActor string) orgcontract.StoredGrantDocument {
		sig := orgcontract.SignEnrolmentGrant(priv, orgcontract.EnrolmentGrant{
			OrgID: "org", OrgServerURL: "http://org", KeyPinSHA256: "pin",
			Authority: []string{"settings.pin"}, GrantedAt: granted, ExpiresAt: expires,
			ConsentMode: signMode, ConsentActor: signActor,
		})
		return orgcontract.StoredGrantDocument{
			OrgID: "org", OrgServerURL: "http://org", KeyPinSHA256: "pin",
			Authority: []string{"settings.pin"}, GrantedAt: granted, ExpiresAt: expires,
			ConsentMode: mode, ConsentActor: actor, Signature: sig,
		}
	}
	cases := []struct {
		name     string
		doc      orgcontract.StoredGrantDocument
		tenancy  string
		pub      ed25519.PublicKey
		want     govern.GrantIntegrity
		evidence govern.ConsentEvidence
	}{
		{"idp: mode and actor signed", doc(govern.ConsentIdP, "a@acme", govern.ConsentIdP, "a@acme"), orgcontract.TenancyManaged, pub, govern.GrantIntegrityValid, govern.ConsentEvidenceModeAndActor},
		{"enterprise posture: mode signed", doc(govern.ConsentManaged, "dev", govern.ConsentManaged, ""), orgcontract.TenancyManaged, pub, govern.GrantIntegrityValid, govern.ConsentEvidenceMode},
		{"managed token rail: nothing signed", doc(govern.ConsentManaged, "dev", "", ""), orgcontract.TenancyManaged, pub, govern.GrantIntegrityValid, govern.ConsentEvidenceNone},
		{"individual token rail: nothing signed", doc(govern.ConsentInteractive, "dev", "", ""), orgcontract.TenancyIndividual, pub, govern.GrantIntegrityValid, govern.ConsentEvidenceNone},
		{"tamper: individual flipped to managed", doc(govern.ConsentManaged, "dev", "", ""), orgcontract.TenancyIndividual, pub, govern.GrantIntegrityInvalid, govern.ConsentEvidenceUnverified},
		{"tamper: idp actor rewritten", doc(govern.ConsentIdP, "mallory", govern.ConsentIdP, "a@acme"), orgcontract.TenancyManaged, pub, govern.GrantIntegrityInvalid, govern.ConsentEvidenceUnverified},
		{"no key material: unchecked", doc(govern.ConsentManaged, "dev", "", ""), orgcontract.TenancyManaged, nil, govern.GrantIntegrityUnchecked, govern.ConsentEvidenceUnverified},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ev := Verify(c.doc, c.tenancy, c.pub)
			if got != c.want || ev != c.evidence {
				t.Fatalf("Verify = (%q, %q), want (%q, %q)", got, ev, c.want, c.evidence)
			}
		})
	}
}

func TestConsentModeForTenancy(t *testing.T) {
	for in, want := range map[string]string{
		orgcontract.TenancyManaged:    govern.ConsentManaged,
		orgcontract.TenancyIndividual: govern.ConsentInteractive,
		"":                            govern.ConsentInteractive,
	} {
		if got := ConsentModeForTenancy(in); got != want {
			t.Errorf("ConsentModeForTenancy(%q) = %q, want %q", in, got, want)
		}
	}
}
