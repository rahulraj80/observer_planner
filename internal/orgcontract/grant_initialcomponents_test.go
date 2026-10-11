package orgcontract

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// Modular packaging plan 5.6.1 (lane MP-GOVF): the additive
// initial_components grant field. Like the ACP-P6c consent evidence it is
// bound into the signature and PRESENCE-VERSIONED, so a grant without it
// signs exactly as before; unlike consent it can be minted on the token
// rail, so the server only offers it to a node that advertised the
// capability (GrantForNode).

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// TestSigningMessageByteIdenticalWithoutInitialComponents is the compat
// golden: no initial set means the pre-field bytes, for a token-rail grant
// (the existing golden constant) and for an idp grant with consent evidence.
func TestSigningMessageByteIdenticalWithoutInitialComponents(t *testing.T) {
	g := goldenGrant()
	if got := hex.EncodeToString(EnrolmentGrantSigningMessage(g)); got != goldenConsentFreeSigningMessage {
		t.Fatalf("token-rail grant without initial_components = %s, want golden %s", got, goldenConsentFreeSigningMessage)
	}
	g.ConsentMode, g.ConsentActor = "idp", "dev@example.com"
	withConsent := EnrolmentGrantSigningMessage(g)
	g.InitialComponents = &InitialComponentSet{CatalogVersion: 1, IDs: []string{"codeintel"}}
	if hex.EncodeToString(EnrolmentGrantSigningMessage(g)) == hex.EncodeToString(withConsent) {
		t.Fatal("the initial set is not bound into the signing message")
	}
	g.InitialComponents = nil
	if hex.EncodeToString(EnrolmentGrantSigningMessage(g)) != hex.EncodeToString(withConsent) {
		t.Fatal("removing the initial set does not restore the consent-only bytes")
	}
}

func TestInitialComponentsBinding(t *testing.T) {
	pub, priv := newKey(t)
	base := goldenGrant()
	base.InitialComponents = &InitialComponentSet{CatalogVersion: 1, IDs: []string{"codeintel", "adapter.codex"}}
	base.Signature = SignEnrolmentGrant(priv, base)
	if err := VerifyEnrolmentGrant(base, pub); err != nil {
		t.Fatalf("signed grant does not verify: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(g *EnrolmentGrant)
		ok     bool
	}{
		{"reordered and duplicated ids are the same set", func(g *EnrolmentGrant) {
			g.InitialComponents = &InitialComponentSet{CatalogVersion: 1, IDs: []string{"adapter.codex", "codeintel", "codeintel"}}
		}, true},
		{"an added id is tampering", func(g *EnrolmentGrant) {
			g.InitialComponents = &InitialComponentSet{CatalogVersion: 1, IDs: []string{"codeintel", "adapter.codex", "remote"}}
		}, false},
		{"a changed catalog version is tampering", func(g *EnrolmentGrant) {
			g.InitialComponents = &InitialComponentSet{CatalogVersion: 2, IDs: []string{"codeintel", "adapter.codex"}}
		}, false},
		{"dropping the set is tampering", func(g *EnrolmentGrant) { g.InitialComponents = nil }, false},
		{"an empty set is not an absent set", func(g *EnrolmentGrant) {
			g.InitialComponents = &InitialComponentSet{CatalogVersion: 1}
		}, false},
	}
	for _, tc := range cases {
		g := base
		tc.mutate(&g)
		err := VerifyEnrolmentGrant(g, pub)
		if (err == nil) != tc.ok {
			t.Errorf("%s: verify err = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

// legacyGrant is the EnrolmentGrant shape a node built before the field
// decodes into: the JSON decoder drops initial_components.
type legacyGrant struct {
	OrgID        string   `json:"org_id"`
	OrgServerURL string   `json:"org_server_url"`
	KeyPinSHA256 string   `json:"key_pin_sha256"`
	Authority    []string `json:"authority"`
	GrantedAt    string   `json:"granted_at"`
	ExpiresAt    string   `json:"expires_at"`
	Signature    string   `json:"signature"`
}

// legacyVerify is what such a node computes (the pre-field, consent-free
// algorithm reproduced in grant_consent_test.go).
func legacyVerify(t *testing.T, wire []byte, pub ed25519.PublicKey) bool {
	t.Helper()
	var lg legacyGrant
	if err := json.Unmarshal(wire, &lg); err != nil {
		t.Fatal(err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(lg.Signature)
	if err != nil {
		t.Fatal(err)
	}
	msg := legacySigningMessage(EnrolmentGrant{
		OrgID: lg.OrgID, OrgServerURL: lg.OrgServerURL, KeyPinSHA256: lg.KeyPinSHA256,
		Authority: lg.Authority, GrantedAt: lg.GrantedAt, ExpiresAt: lg.ExpiresAt,
	})
	return ed25519.Verify(pub, msg, sig)
}

// TestInitialComponentsCompatMatrix covers the four node/server cells.
func TestInitialComponentsCompatMatrix(t *testing.T) {
	pub, priv := newKey(t)
	minted := goldenGrant()
	minted.InitialComponents = &InitialComponentSet{CatalogVersion: 1, IDs: []string{"codeintel"}}
	sign := func(g EnrolmentGrant) []byte {
		g.Signature = SignEnrolmentGrant(priv, g)
		raw, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	t.Run("old node, new server: the field is withheld and the grant verifies", func(t *testing.T) {
		wire := sign(GrantForNode(minted, nil))
		if !legacyVerify(t, wire, pub) {
			t.Fatal("an old node refuses a grant from a new server")
		}
	})
	t.Run("old node, new server that skipped GrantForNode: refused (the hazard GrantForNode exists for)", func(t *testing.T) {
		if legacyVerify(t, sign(minted), pub) {
			t.Fatal("expected an old node to refuse a grant carrying initial_components")
		}
	})
	t.Run("new node, old server: no field, verifies, no initial set", func(t *testing.T) {
		old := goldenGrant()
		old.Signature = SignEnrolmentGrant(priv, old)
		raw, _ := json.Marshal(old)
		var g EnrolmentGrant
		if err := json.Unmarshal(raw, &g); err != nil {
			t.Fatal(err)
		}
		if g.InitialComponents != nil || VerifyEnrolmentGrant(g, pub) != nil {
			t.Fatalf("new node on an old server's grant: set=%v", g.InitialComponents)
		}
	})
	t.Run("new node, new server: field delivered and verified", func(t *testing.T) {
		wire := sign(GrantForNode(minted, []string{"other", EnrolCapabilityInitialComponents}))
		var g EnrolmentGrant
		if err := json.Unmarshal(wire, &g); err != nil {
			t.Fatal(err)
		}
		if g.InitialComponents == nil || VerifyEnrolmentGrant(g, pub) != nil {
			t.Fatal("new node lost or failed the initial set")
		}
		// The stored-row re-verification must carry it too.
		doc := StoredGrantDocument{
			OrgID: g.OrgID, OrgServerURL: g.OrgServerURL, KeyPinSHA256: g.KeyPinSHA256,
			Authority: g.Authority, GrantedAt: g.GrantedAt, ExpiresAt: g.ExpiresAt,
			InitialComponents: g.InitialComponents, Signature: g.Signature,
		}
		if err := VerifyStoredGrant(doc, pub); err != nil {
			t.Fatalf("stored grant with the set: %v", err)
		}
		doc.InitialComponents = nil
		if VerifyStoredGrant(doc, pub) == nil {
			t.Fatal("a stored row that dropped the set still verifies")
		}
	})
}

func TestEnrollRequestCapabilitiesCompat(t *testing.T) {
	// A new node's request decodes on an old server (no
	// DisallowUnknownFields on the enrol handler) and vice versa.
	raw, err := json.Marshal(EnrollRequest{OneTimeToken: "t.s", AgentPublicKey: "k", Capabilities: []string{EnrolCapabilityInitialComponents}})
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		OneTimeToken   string `json:"one_time_token"`
		AgentPublicKey string `json:"agent_public_key"`
	}
	if err := json.Unmarshal(raw, &old); err != nil || old.OneTimeToken != "t.s" {
		t.Fatalf("old server decode: %v %+v", err, old)
	}
	var fresh EnrollRequest
	if err := json.Unmarshal([]byte(`{"one_time_token":"t.s","agent_public_key":"k"}`), &fresh); err != nil || fresh.Capabilities != nil {
		t.Fatalf("old node request on a new server: %v %+v", err, fresh)
	}
	if b, _ := json.Marshal(EnrollRequest{OneTimeToken: "t.s", AgentPublicKey: "k"}); string(b) != `{"one_time_token":"t.s","agent_public_key":"k"}` {
		t.Fatalf("a request without capabilities changed shape: %s", b)
	}
}
