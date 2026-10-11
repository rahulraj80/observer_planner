package orgcontract

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestAccessGrantSignVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pinned := base64.StdEncoding.EncodeToString(pub)
	subject := AccessGrantSubject("alice", "laptop-7")
	body := AccessGrantBody{
		Grants:      []AccessGrantWire{{ID: 12, Type: "guard_exemption", Effect: AccessGrantEffectSoften, SubjectKind: AccessGrantSubjectMachine, MachineIdentity: "laptop-7", RuleID: "R-171", StartsAtNs: 1, ExpiresAtNs: 2}},
		ServerNowNs: 1, MaxOfflineS: 86400, IssuedAt: time.Now().UTC().Format(time.RFC3339),
	}
	doc, err := SignAccessGrants(priv, "org-1", subject, 7, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAccessGrants(doc, "org-1", subject, pinned); err != nil {
		t.Fatalf("verify: %v", err)
	}
	cases := []struct {
		name string
		mut  func(d *AccessGrantDoc) (org, subj string)
	}{
		{"other org", func(d *AccessGrantDoc) (string, string) { return "org-2", subject }},
		{"other subject (cross-caller replay)", func(d *AccessGrantDoc) (string, string) { return "org-1", AccessGrantSubject("bob", "laptop-7") }},
		{"other machine", func(d *AccessGrantDoc) (string, string) { return "org-1", AccessGrantSubject("alice", "devbox") }},
		{"inflated version", func(d *AccessGrantDoc) (string, string) { d.Version = 99; return "org-1", subject }},
		{"tampered body", func(d *AccessGrantDoc) (string, string) {
			d.Body = strings.Replace(d.Body, "soften", "exempt", 1)
			return "org-1", subject
		}},
		{"unsigned", func(d *AccessGrantDoc) (string, string) { d.Signature = ""; return "org-1", subject }},
	}
	for _, c := range cases {
		d := doc
		org, subj := c.mut(&d)
		if err := VerifyAccessGrants(d, org, subj, pinned); err == nil {
			t.Errorf("%s: verified", c.name)
		}
	}
	// A domain from another rail must not verify here.
	other, _ := SignDisplayPolicy(priv, "org-1", 7, DisplayPolicyBody{DeveloperCostDisplay: "usd"})
	forged := doc
	forged.Signature = other.Signature
	if err := VerifyAccessGrants(forged, "org-1", subject, pinned); err == nil {
		t.Fatal("cross-rail signature verified")
	}
}

func TestAccessGrantBodyRoundTrip(t *testing.T) {
	enc, err := EncodeAccessGrantBody(AccessGrantBody{})
	if err != nil || !strings.Contains(enc, `"grants":[]`) {
		t.Fatalf("empty body must encode an explicit empty list: %s %v", enc, err)
	}
	b, err := DecodeAccessGrantBody(`{"grants":[{"id":1,"type":"guard_exemption","subject_kind":"member","starts_at_ns":1},{"id":0,"subject_kind":"member"},{"id":2,"subject_kind":"team"},{"id":3,"subject_kind":"member","effect":"delete"}],"server_now_ns":5}`)
	if err != nil || len(b.Grants) != 1 || b.Grants[0].ID != 1 {
		t.Fatalf("decode kept unknown shapes: %+v %v", b, err)
	}
}

func TestAccessGrantFresh(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		at   string
		want bool
	}{
		{now.Format(time.RFC3339), true},
		{now.Add(-59 * time.Minute).Format(time.RFC3339), true},
		{now.Add(-61 * time.Minute).Format(time.RFC3339), false},
		{now.Add(10 * time.Minute).Format(time.RFC3339), false},
		{"", false},
		{"yesterday", false},
	}
	for _, c := range cases {
		if got, _ := AccessGrantFresh(c.at, now); got != c.want {
			t.Errorf("%q: %v", c.at, got)
		}
	}
}
