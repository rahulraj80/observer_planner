package diag

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"

	orgdb "github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// seedGovernedNode writes the two rows doctor's grant re-verification reads:
// a signed grant and the org key-material row enrolment records beside it.
func seedGovernedNode(t *testing.T) (*sql.DB, ed25519.PrivateKey) {
	t.Helper()
	ctx := context.Background()
	d, err := dbtemplate.Open(ctx, orgdb.Options{Path: filepath.Join(t.TempDir(), "observer.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const orgURL = "https://org.example.test"
	pin := orgcontract.PublicKeyPinHash(pub)
	now := time.Now().UTC().Truncate(time.Second)
	g := orgcontract.EnrolmentGrant{
		OrgID:        "org-1",
		OrgServerURL: orgURL,
		KeyPinSHA256: pin,
		Authority:    orgcontract.CanonicalAuthority([]string{"settings.pin"}),
		GrantedAt:    now.Format(time.RFC3339),
		ExpiresAt:    now.Add(30 * 24 * time.Hour).Format(time.RFC3339),
	}
	g.Signature = orgcontract.SignEnrolmentGrant(priv, g)
	authority, _ := json.Marshal(g.Authority)

	if _, err := d.ExecContext(ctx, `
INSERT INTO org_enrolment_grant
  (org_key, generation, org_id, org_name, org_server_url, key_pin_sha256, authority_json,
   consent_mode, consent_actor, granted_at, expires_at, signed_expires_at, last_renewed_at,
   signature, receipt_hash, replacement_generation)
VALUES (?, 1, ?, 'Acme', ?, ?, ?, '', '', ?, ?, ?, '', ?, ?, 0)`,
		orgURL+"|org-1", g.OrgID, orgURL, pin, string(authority),
		g.GrantedAt, g.ExpiresAt, g.ExpiresAt, g.Signature,
		orgcontract.EnrolmentGrantReceiptHash(g)); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
INSERT INTO guard_policy_state (layer, path, version, content_hash, signature, loaded_at)
VALUES ('org', ?, ?, ?, NULL, ?)`,
		orgURL+orgKeyMaterialSuffix, base64.StdEncoding.EncodeToString(pub), pin,
		now.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed key material: %v", err)
	}
	return d, priv
}

// TestCheckStoredGrantIntegrity is doctor's half of Track C item 1: the same
// three answers the daemon resolves, from the same two rows.
func TestCheckStoredGrantIntegrity(t *testing.T) {
	ctx := context.Background()

	t.Run("untouched grant verifies", func(t *testing.T) {
		d, _ := seedGovernedNode(t)
		got, detail := checkStoredGrantIntegrity(ctx, d)
		if got != grantIntegrityValid {
			t.Fatalf("integrity = %q (%s), want valid", got, detail)
		}
	})

	t.Run("rewritten authority list is INVALID", func(t *testing.T) {
		d, _ := seedGovernedNode(t)
		widened, _ := json.Marshal([]string{"settings.pin", "enforce.budget"})
		if _, err := d.ExecContext(ctx,
			`UPDATE org_enrolment_grant SET authority_json = ?`, string(widened)); err != nil {
			t.Fatalf("tamper: %v", err)
		}
		got, detail := checkStoredGrantIntegrity(ctx, d)
		if got != grantIntegrityInvalid {
			t.Fatalf("integrity = %q (%s), want invalid", got, detail)
		}
	})

	t.Run("no key material is UNCHECKED, never invalid", func(t *testing.T) {
		d, _ := seedGovernedNode(t)
		if _, err := d.ExecContext(ctx,
			`DELETE FROM guard_policy_state WHERE path LIKE '%'||?`, orgKeyMaterialSuffix); err != nil {
			t.Fatalf("remove key material: %v", err)
		}
		got, _ := checkStoredGrantIntegrity(ctx, d)
		if got != grantIntegrityUnchecked {
			t.Fatalf("integrity = %q, want unchecked", got)
		}
	})

	t.Run("no grant at all is UNCHECKED and silent", func(t *testing.T) {
		d, _ := seedGovernedNode(t)
		if _, err := d.ExecContext(ctx, `DELETE FROM org_enrolment_grant`); err != nil {
			t.Fatalf("remove grant: %v", err)
		}
		got, detail := checkStoredGrantIntegrity(ctx, d)
		if got != grantIntegrityUnchecked || detail != "" {
			t.Fatalf("integrity = %q detail = %q, want unchecked and silent", got, detail)
		}
	})
}

// TestCheckStoredGrantIntegrityConsentRails (Sol A3-03 class): doctor must
// reach the SAME verdict as the daemon, through the same verifier. A managed
// token-rail grant is signed with no consent fields while the node records
// its resolved mode and local actor; doctor used to rebuild the message from
// the stored fields only and report every such grant as altered (FAIL).
func TestCheckStoredGrantIntegrityConsentRails(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T, tenancy string) *sql.DB {
		t.Helper()
		d, _ := seedGovernedNode(t)
		if _, err := d.ExecContext(ctx,
			`UPDATE org_enrolment_grant SET consent_mode = 'managed', consent_actor = 'dev'`); err != nil {
			t.Fatalf("record resolved consent: %v", err)
		}
		if _, err := d.ExecContext(ctx, `
INSERT INTO org_enrolment (id, org_id, org_name, org_server_url, user_id, user_email, enrolled_at, bearer_key_id, tenancy)
VALUES (1, 'org-1', 'Acme', 'https://org.example.test', 'u-1', 'dev@acme.example', '2026-10-03T00:00:00Z', 'k', ?)`, tenancy); err != nil {
			t.Fatalf("seed enrolment: %v", err)
		}
		return d
	}

	t.Run("managed token rail verifies and says the actor is local", func(t *testing.T) {
		got, detail := checkStoredGrantIntegrity(ctx, seed(t, orgcontract.TenancyManaged))
		if got != grantIntegrityValid {
			t.Fatalf("integrity = %q (%s), want valid", got, detail)
		}
		if !strings.Contains(detail, "not signed by the organization") {
			t.Fatalf("detail does not report the consent evidence: %q", detail)
		}
	})

	t.Run("an individual grant flipped to managed is INVALID", func(t *testing.T) {
		got, detail := checkStoredGrantIntegrity(ctx, seed(t, orgcontract.TenancyIndividual))
		if got != grantIntegrityInvalid {
			t.Fatalf("integrity = %q (%s), want invalid", got, detail)
		}
	})
}
