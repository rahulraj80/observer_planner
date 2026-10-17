package orgclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// signedTestTombstone is the org's signed withdrawal of family from subject.
func signedTestTombstone(t *testing.T, priv ed25519.PrivateKey, pub ed25519.PublicKey, family, subject string, version int64) orgcontract.SignedPolicyResource {
	t.Helper()
	r := orgcontract.NewPolicyResourceTombstone("default", family, subject, version)
	r.PublicKey = base64.RawURLEncoding.EncodeToString(pub)
	r.SignedAt = "2026-10-03T00:00:00Z"
	sig, err := orgcontract.SignPolicyResource(priv, r)
	if err != nil {
		t.Fatalf("SignPolicyResource: %v", err)
	}
	r.Signature = sig
	return r
}

// TestFetchAndAcceptPolicyResource_SignedWithdrawal pins the node half of
// withdrawal by targeting (D-016 follow-up to Sol A5-01): only a SIGNED
// tombstone, under the pinned key, addressed to THIS node's subject, at or
// above the durable floor, withdraws an installed version. A 404, an
// unsigned or foreign-key tombstone, another node's tombstone and a stale
// one all keep the cached version.
func TestFetchAndAcceptPolicyResource_SignedWithdrawal(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	const subject = "scim-42" // enrolledPRClient's UserID
	family := policyfamAdmissionInput

	cases := []struct {
		name       string
		after      func() *orgcontract.SignedPolicyResource // nil = 404
		wantStatus PolicyResourceStatus
		wantCode   PolicyResourceRejectCode
	}{
		{"signed tombstone at the held version", func() *orgcontract.SignedPolicyResource {
			r := signedTestTombstone(t, priv, pub, family, subject, 3)
			return &r
		}, PRWithdrawn, PRRejectNone},
		{"signed tombstone at a newer version", func() *orgcontract.SignedPolicyResource {
			r := signedTestTombstone(t, priv, pub, family, subject, 5)
			return &r
		}, PRWithdrawn, PRRejectNone},
		{"plain 404", func() *orgcontract.SignedPolicyResource { return nil }, PRNone, PRRejectNone},
		{"unsigned tombstone", func() *orgcontract.SignedPolicyResource {
			r := orgcontract.NewPolicyResourceTombstone("default", family, subject, 5)
			r.PublicKey = base64.RawURLEncoding.EncodeToString(pub)
			return &r
		}, PRRejected, PRRejectSigInvalid},
		{"tombstone signed by a key the node never pinned", func() *orgcontract.SignedPolicyResource {
			r := signedTestTombstone(t, otherPriv, otherPub, family, subject, 5)
			return &r
		}, PRRejected, PRRejectKeyPinMismatch},
		{"another node's tombstone", func() *orgcontract.SignedPolicyResource {
			r := signedTestTombstone(t, priv, pub, family, "scim-99", 5)
			return &r
		}, PRRejected, PRRejectClosedEnvelope},
		{"a stale tombstone below the floor", func() *orgcontract.SignedPolicyResource {
			r := signedTestTombstone(t, priv, pub, family, subject, 2)
			return &r
		}, PRRejected, PRRejectVersionDowngrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ps := newPolicyResourceServer(t)
			held := signedTestResource(t, priv, pub, family, 3, observeAdmissionBody, nil)
			ps.resources[family] = &held
			c, s, cacheDir := enrolledPRClient(t, ps.srv.URL)
			opts := PolicyResourceOptions{CacheDir: cacheDir, AcceptFamilies: []string{family}}
			ctx := context.Background()
			if res, err := c.FetchAndAcceptPolicyResource(ctx, family, opts); err != nil || res.Status != PRApplied {
				t.Fatalf("seed v3: %+v err=%v", res, err)
			}

			ps.resources[family] = tc.after()
			res, err := c.FetchAndAcceptPolicyResource(ctx, family, opts)
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if res.Status != tc.wantStatus || res.RejectCode != tc.wantCode {
				t.Fatalf("result = %+v, want %s/%q", res, tc.wantStatus, tc.wantCode)
			}

			cachePath := filepath.Join(cacheDir, orgKeyForTest(t, c), "1", family+".json")
			raw, rerr := os.ReadFile(cachePath)
			if rerr != nil {
				t.Fatalf("read cache: %v", rerr)
			}
			var cached orgcontract.SignedPolicyResource
			if err := json.Unmarshal(raw, &cached); err != nil {
				t.Fatalf("decode cache: %v", err)
			}
			enr, _ := s.LoadEnrolment(ctx)
			st, _, _ := s.LoadPolicyResourceState(ctx, OrgKey(enr.OrgServerURL, enr.OrgID), family)
			if tc.wantStatus != PRWithdrawn {
				if cached.Body != observeAdmissionBody || cached.Version != 3 || st.BodyHash != held.BodyHash {
					t.Fatalf("the held v3 did not survive: cache %+v state %+v", cached, st)
				}
				return
			}
			if !orgcontract.IsPolicyResourceTombstone(cached) || st.FloorVersion != res.Version {
				t.Fatalf("withdrawal not durable: cache %+v state %+v", cached, st)
			}
			// The next poll rides the tombstone's ETag into a 304 and stays
			// withdrawn.
			again, err := c.FetchAndAcceptPolicyResource(ctx, family, opts)
			if err != nil || again.Status != PRWithdrawn {
				t.Fatalf("repeat poll = %+v err=%v, want withdrawn", again, err)
			}
			// A newer real publish addressed to the node installs again.
			next := signedTestResource(t, priv, pub, family, res.Version+1, enforceAdmissionBody, nil)
			ps.resources[family] = &next
			back, err := c.FetchAndAcceptPolicyResource(ctx, family, PolicyResourceOptions{
				CacheDir: cacheDir, AcceptFamilies: []string{family}, PreauthorizeEnforce: []string{family},
			})
			if err != nil || back.Status != PRApplied || back.Version != res.Version+1 {
				t.Fatalf("re-inclusion = %+v err=%v, want applied v%d", back, err, res.Version+1)
			}
		})
	}
}
