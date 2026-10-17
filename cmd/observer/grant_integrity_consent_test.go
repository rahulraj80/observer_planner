package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/govern"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestStoredGrantIntegrityAcrossConsentRails pins the fix for every managed
// TOKEN-rail grant reading as tampered: the node records the consent it
// resolved, the organization may have signed less (or none), and the
// integrity check must verify against what was SIGNED.
func TestStoredGrantIntegrityAcrossConsentRails(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	granted := time.Date(2026, 10, 3, 4, 0, 0, 0, time.UTC)
	expires := granted.Add(30 * 24 * time.Hour)
	sign := func(mode, actor string) string {
		return orgcontract.SignEnrolmentGrant(priv, orgcontract.EnrolmentGrant{
			OrgID: "org", OrgServerURL: "http://org", KeyPinSHA256: "pin",
			Authority: []string{"settings.pin", "extract.managed"},
			GrantedAt: granted.Format(time.RFC3339), ExpiresAt: expires.Format(time.RFC3339),
			ConsentMode: mode, ConsentActor: actor,
		})
	}
	row := func(mode, actor, sig string) store.EnrolmentGrant {
		return store.EnrolmentGrant{
			OrgID: "org", OrgServerURL: "http://org", KeyPinSHA256: "pin",
			Authority: []string{"extract.managed", "settings.pin"},
			GrantedAt: granted, SignedExpiresAt: expires,
			ConsentMode: mode, ConsentActor: actor, Signature: sig,
		}
	}
	cases := []struct {
		name    string
		row     store.EnrolmentGrant
		tenancy string
		want    govern.GrantIntegrity
	}{
		{
			"managed token rail: nothing signed, node derived managed + local actor",
			row(govern.ConsentManaged, "dev", sign("", "")), orgcontract.TenancyManaged, govern.GrantIntegrityValid,
		},
		{
			"individual token rail: nothing signed, node derived interactive",
			row(govern.ConsentInteractive, "dev", sign("", "")), orgcontract.TenancyIndividual, govern.GrantIntegrityValid,
		},
		{
			"enterprise posture: managed signed with no actor, node recorded its own",
			row(govern.ConsentManaged, "dev", sign(govern.ConsentManaged, "")), orgcontract.TenancyManaged, govern.GrantIntegrityValid,
		},
		{
			"idp rail: mode and actor signed verbatim",
			row(govern.ConsentIdP, "a@acme", sign(govern.ConsentIdP, "a@acme")), orgcontract.TenancyManaged, govern.GrantIntegrityValid,
		},
		{
			"tamper: an individual grant's mode flipped to managed is still caught",
			row(govern.ConsentManaged, "dev", sign("", "")), orgcontract.TenancyIndividual, govern.GrantIntegrityInvalid,
		},
		{
			"tamper: authority widened",
			func() store.EnrolmentGrant {
				r := row(govern.ConsentManaged, "dev", sign("", ""))
				r.Authority = append(r.Authority, "enforce.budget")
				return r
			}(), orgcontract.TenancyManaged, govern.GrantIntegrityInvalid,
		},
		{
			"tamper: idp actor rewritten",
			row(govern.ConsentIdP, "mallory", sign(govern.ConsentIdP, "a@acme")), orgcontract.TenancyManaged, govern.GrantIntegrityInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := storedGrantIntegrity(tc.row, tc.tenancy, pub); got != tc.want {
				t.Fatalf("integrity = %v, want %v", got, tc.want)
			}
		})
	}
}
