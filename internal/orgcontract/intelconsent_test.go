package orgcontract

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
)

// TestIntelRequirementSignVerify pins the requirement document's binding: it
// verifies for its org and version under the signing key, and refuses another
// org, a changed version, a tampered body and an unsigned document.
func TestIntelRequirementSignVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	doc, err := SignIntelRequirement(priv, "org-a", "u-1", 4, IntelRequirementBody{OrgName: "Acme", Enabled: true, EvidenceLevel: "titles"})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyIntelRequirement(doc, "org-a", "u-1", pubB64); err != nil {
		t.Fatalf("verify: %v", err)
	}
	cases := map[string]func(d IntelRequirementDoc) (IntelRequirementDoc, string){
		"other_org":  func(d IntelRequirementDoc) (IntelRequirementDoc, string) { return d, "org-b" },
		"bumped_ver": func(d IntelRequirementDoc) (IntelRequirementDoc, string) { d.Version = 5; return d, "org-a" },
		"tampered": func(d IntelRequirementDoc) (IntelRequirementDoc, string) {
			d.Body = strings.Replace(d.Body, `"enabled":true`, `"enabled":false`, 1)
			return d, "org-a"
		},
		"unsigned": func(d IntelRequirementDoc) (IntelRequirementDoc, string) { d.Signature = ""; return d, "org-a" },
	}
	for name, mut := range cases {
		d, org := mut(doc)
		if err := VerifyIntelRequirement(d, org, "u-1", pubB64); err == nil {
			t.Errorf("%s: verified", name)
		}
	}
	// C156-16: the document is one member's effective settings, so it is bound
	// to that member; another member's node refuses it, and a node with no
	// enrolment subject binds nothing.
	for _, sub := range []string{"u-2", ""} {
		if err := VerifyIntelRequirement(doc, "org-a", sub, pubB64); err == nil {
			t.Errorf("u-1's document verified for subject %q", sub)
		}
	}
}

// TestIntelDisclosure pins the one disclosure: it names the org, never uses an
// em dash, changes its digest when what it covers changes, and switches the
// way out to leaving the org when the consent is compulsory.
func TestIntelDisclosure(t *testing.T) {
	b := IntelRequirementBody{OrgName: "Acme", ProviderLabel: "Azure", Model: "gpt-x", EvidenceLevel: "titles", Purposes: []string{"structural_activity_insights"}}
	text, d1 := IntelDisclosure(b)
	if !strings.Contains(text, "Acme") || strings.Contains(text, "—") || !strings.Contains(text, "observer intel disable") {
		t.Fatalf("disclosure = %q", text)
	}
	// Ruling Q4: the friendly label leads and the id follows it.
	if !strings.Contains(text, "Purposes: Title only (structural_activity_insights).") {
		t.Fatalf("purpose line = %q", text)
	}
	b.EvidenceLevel = "excerpts"
	_, d2 := IntelDisclosure(b)
	if d1 == d2 {
		t.Fatal("a wider level produced the same digest")
	}
	b.RequireIndividualConsent = true
	text, _ = IntelDisclosure(b)
	if !strings.Contains(text, "leave the organization") {
		t.Fatalf("compulsory disclosure = %q", text)
	}
	// An unknown level reads as the narrowest description.
	b.EvidenceLevel = "everything"
	text, _ = IntelDisclosure(b)
	if !strings.Contains(text, "metadata only") {
		t.Fatalf("unknown level disclosure = %q", text)
	}
}
