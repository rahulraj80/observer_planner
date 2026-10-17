package orgcontract

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

// TestIntelTombstoneSignVerify pins the purge-tombstone set's acceptance
// rules (parity follow-up ruling 1, the D-192 precedent): a node accepts only
// a set signed by its pinned org key, for its own org, addressed to its own
// subject, with well-formed entries. Each refusal row is one way a set must
// clear nothing.
func TestIntelTombstoneSignVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherPub, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	pin := base64.StdEncoding.EncodeToString(pub)
	body := IntelTombstoneBody{Subject: "u-1", IssuedAt: "2026-10-04T10:00:00Z", Entries: []IntelTombstone{
		{SessionID: "s-1", JobID: "14", PurgedAt: "2026-10-04T10:00:00Z"},
	}}
	good, err := SignIntelTombstones(priv, "org-a", body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyIntelTombstones(good, "org-a", "u-1", pin)
	if err != nil || len(got.Entries) != 1 || got.Entries[0].SessionID != "s-1" || got.Entries[0].JobID != "14" {
		t.Fatalf("verify good = %+v, %v", got, err)
	}

	foreignKey, _ := SignIntelTombstones(otherPriv, "org-a", body)
	badEntry, _ := SignIntelTombstones(priv, "org-a", IntelTombstoneBody{Subject: "u-1", Entries: []IntelTombstone{{SessionID: "s-1", JobID: "x"}}})
	noSession, _ := SignIntelTombstones(priv, "org-a", IntelTombstoneBody{Subject: "u-1", Entries: []IntelTombstone{{JobID: "3"}}})
	tampered := good
	tampered.Body = `{"subject":"u-1","issued_at":"","entries":[{"session_id":"s-9","job_id":"99","purged_at":""}]}`
	rehashed := tampered
	sum := sha256.Sum256([]byte(rehashed.Body))
	rehashed.BodyHash = hex.EncodeToString(sum[:])
	unsigned := good
	unsigned.Signature = ""

	for name, c := range map[string]struct {
		doc            IntelTombstoneDoc
		org, subj, pin string
	}{
		"another org":      {good, "org-b", "u-1", pin},
		"another subject":  {good, "org-a", "u-2", pin},
		"empty subject":    {good, "org-a", "", pin},
		"no pinned key":    {good, "org-a", "u-1", ""},
		"foreign key":      {foreignKey, "org-a", "u-1", pin},
		"pinned other key": {good, "org-a", "u-1", base64.StdEncoding.EncodeToString(otherPub)},
		"unsigned":         {unsigned, "org-a", "u-1", pin},
		"body hash":        {tampered, "org-a", "u-1", pin},
		"rehashed tamper":  {rehashed, "org-a", "u-1", pin},
		"bad job id":       {badEntry, "org-a", "u-1", pin},
		"missing session":  {noSession, "org-a", "u-1", pin},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyIntelTombstones(c.doc, c.org, c.subj, c.pin); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	if _, err := SignIntelTombstones(priv, "org-a", IntelTombstoneBody{}); err == nil {
		t.Fatal("signed a set with no subject")
	}
}
