package orgcontract

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func bindingTestResource(t *testing.T) (SignedPolicyResource, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"schema":1}`
	sum := sha256.Sum256([]byte(body))
	r := SignedPolicyResource{
		ID: "default", Version: 7, Family: "node.governance", CompilerVersion: "v1",
		Body: body, BodyHash: hex.EncodeToString(sum[:]), SelectorsJSON: "{}",
		PublicKey: base64.RawURLEncoding.EncodeToString(pub),
	}
	return r, priv
}

// TestPolicyResourceBindingSignVerify pins the v2 message (C156-14): a bound
// envelope verifies self-contained, a v1 signature never verifies once a
// binding is attached, every bound field is covered, and the CONTENT digest
// the replay floor and the withdrawal key on ignores the binding.
func TestPolicyResourceBindingSignVerify(t *testing.T) {
	r, priv := bindingTestResource(t)
	v1sig, err := SignPolicyResource(priv, r)
	if err != nil {
		t.Fatal(err)
	}
	r.Signature = v1sig
	if _, err := VerifyPolicyResource(r); err != nil {
		t.Fatalf("v1 envelope: %v", err)
	}
	content, _ := PolicyResourceMessageDigest(r)

	b := PolicyBinding{OrgID: "org-1", Subject: "dana", Target: "nodegov-vocab-6"}
	bound := r
	bound.Binding = &b
	if _, err := VerifyPolicyResource(bound); err == nil {
		t.Fatal("a v1 signature verified with a binding attached (the binding would be unsigned)")
	}
	sig, err := SignPolicyResource(priv, bound)
	if err != nil {
		t.Fatal(err)
	}
	bound.Signature = sig
	if _, err := VerifyPolicyResource(bound); err != nil {
		t.Fatalf("bound envelope: %v", err)
	}
	for name, mut := range map[string]func(*PolicyBinding){
		"org":     func(p *PolicyBinding) { p.OrgID = "org-2" },
		"subject": func(p *PolicyBinding) { p.Subject = "eli" },
		"target":  func(p *PolicyBinding) { p.Target = "nodegov-vocab-1" },
	} {
		forged := bound
		bb := b
		mut(&bb)
		forged.Binding = &bb
		if _, err := VerifyPolicyResource(forged); err == nil {
			t.Errorf("rebinding the %s kept the signature valid", name)
		}
	}
	stripped := bound
	stripped.Binding = nil
	if _, err := VerifyPolicyResource(stripped); err == nil {
		t.Error("stripping the binding kept the signature valid")
	}
	if c, _ := PolicyResourceMessageDigest(bound); c != content {
		t.Errorf("content digest moved with the binding: %s vs %s", c, content)
	}
	if d1, _ := PolicyResourceDeliveryDigest(r); d1 != content {
		t.Errorf("an unbound delivery digest %s differs from its content digest %s", d1, content)
	}
	if d2, _ := PolicyResourceDeliveryDigest(bound); d2 == content {
		t.Error("a bound delivery digest equals the content digest")
	}
}

// TestPolicyResourceETagShapes pins the validator: the unbound ETag keeps the
// released shape byte for byte, the bound one adds a suffix, and the content
// digest is recoverable from both.
func TestPolicyResourceETagShapes(t *testing.T) {
	r, _ := bindingTestResource(t)
	content, _ := PolicyResourceMessageDigest(r)
	unbound, err := PolicyResourceETag(r)
	if err != nil || unbound != `"pr-`+content+`"` {
		t.Fatalf("unbound etag %q (%v), want the released shape", unbound, err)
	}
	r.Binding = &PolicyBinding{OrgID: "org-1", Subject: "dana"}
	bound, _ := PolicyResourceETag(r)
	if !strings.HasPrefix(bound, `"pr-`+content+".") || bound == unbound {
		t.Fatalf("bound etag %q", bound)
	}
	for _, etag := range []string{unbound, bound} {
		if d, ok := PolicyResourceETagContentDigest(etag); !ok || d != content {
			t.Errorf("content digest of %q = %q %v", etag, d, ok)
		}
	}
	for _, bad := range []string{"", `"pr-"`, `pr-abc`, `"xx-abc"`} {
		if _, ok := PolicyResourceETagContentDigest(bad); ok {
			t.Errorf("%q parsed as an etag", bad)
		}
	}
}

// TestPolicyBundleBindingSignVerify pins the guard bundle's v2 message
// (C156-15): VerifyPolicyBundle dispatches on the binding, a v1 signature
// never verifies bound, and another member's binding fails.
func TestPolicyBundleBindingSignVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	b := PolicyBundle{Version: 3, BundleTOML: "[[rule]]\n", PublicKey: base64.RawURLEncoding.EncodeToString(pub)}
	b.Signature = SignPolicyBundle(priv, b.Version, []byte(b.BundleTOML))
	if _, err := VerifyPolicyBundle(b); err != nil {
		t.Fatalf("v1 bundle: %v", err)
	}
	bound := b
	bound.Binding = &PolicyBinding{OrgID: "org-1", Subject: "dana"}
	if _, err := VerifyPolicyBundle(bound); err == nil {
		t.Fatal("a v1 bundle signature verified with a binding attached")
	}
	bound.Signature = SignPolicyBundleEnvelope(priv, bound)
	if _, err := VerifyPolicyBundle(bound); err != nil {
		t.Fatalf("bound bundle: %v", err)
	}
	other := bound
	other.Binding = &PolicyBinding{OrgID: "org-1", Subject: "eli"}
	if _, err := VerifyPolicyBundle(other); err == nil {
		t.Fatal("dana's bound bundle verified as eli's")
	}
	if SignPolicyBundleEnvelope(priv, b) != b.Signature {
		t.Fatal("SignPolicyBundleEnvelope on an unbound bundle differs from the v1 signature")
	}
}
