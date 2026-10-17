package orgcontract

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func displayTestKey(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv, base64.StdEncoding.EncodeToString(pub)
}

// TestVerifyDisplayPolicy walks every refusal the node relies on, one row
// each, against a document genuinely signed for org-1 at version 3.
func TestVerifyDisplayPolicy(t *testing.T) {
	priv, pub := displayTestKey(t)
	_, otherPub := displayTestKey(t)
	good, err := SignDisplayPolicy(priv, "org-1", 3, DisplayPolicyBody{DeveloperCostDisplay: DisplayCostTokens})
	if err != nil {
		t.Fatal(err)
	}
	announcementSig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, AnnouncementSigningMessage(good.Version, good.Body)))

	cases := []struct {
		name    string
		mutate  func(d *DisplayPolicyDoc)
		org     string
		key     string
		wantErr string
	}{
		{name: "genuine document verifies", org: "org-1", key: pub},
		{name: "replayed at an inflated version", mutate: func(d *DisplayPolicyDoc) { d.Version = 99 }, org: "org-1", key: pub, wantErr: "signature invalid"},
		{name: "presented to a node of another org", org: "org-2", key: pub, wantErr: "is for org"},
		{name: "relabelled to another org", mutate: func(d *DisplayPolicyDoc) { d.OrgID = "org-2" }, org: "org-2", key: pub, wantErr: "signature invalid"},
		{name: "unsigned", mutate: func(d *DisplayPolicyDoc) { d.Signature = "" }, org: "org-1", key: pub, wantErr: "unsigned"},
		{name: "signed by a key that is not pinned", org: "org-1", key: otherPub, wantErr: "signature invalid"},
		{name: "body swapped (hash kept)", mutate: func(d *DisplayPolicyDoc) { d.Body = `{"developer_cost_display":"usd"}` }, org: "org-1", key: pub, wantErr: "body hash mismatch"},
		{name: "another rail's signature by the same key", mutate: func(d *DisplayPolicyDoc) { d.Signature = announcementSig }, org: "org-1", key: pub, wantErr: "signature invalid"},
		{name: "empty expected org", org: "", key: pub, wantErr: "is for org"},
		{name: "negative version", mutate: func(d *DisplayPolicyDoc) { d.Version = -1 }, org: "org-1", key: pub, wantErr: "negative version"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := good
			if c.mutate != nil {
				c.mutate(&d)
			}
			err := VerifyDisplayPolicy(d, c.org, c.key)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

// TestDisplayPolicyBodyUnknownIsTokens (Sol A4-03 class): only an explicit
// "usd" shows dollars. An empty, unknown or future value resolves to token
// usage on both sides of the rail (D-013 fail-safe): failing toward dollars
// would expose costs an org may have chosen to hide.
func TestDisplayPolicyBodyUnknownIsTokens(t *testing.T) {
	for _, c := range []struct {
		in, want string
	}{
		{"tokens", DisplayCostTokens},
		{"usd", DisplayCostUSD},
		{"", DisplayCostTokens},
		{"USD", DisplayCostTokens},
		{"tokens_percent", DisplayCostTokens},
		{"percent_of_budget", DisplayCostTokens}, // a future value this build does not know
	} {
		enc, err := EncodeDisplayPolicyBody(DisplayPolicyBody{DeveloperCostDisplay: c.in})
		if err != nil {
			t.Fatal(err)
		}
		dec, err := DecodeDisplayPolicyBody(enc)
		if err != nil {
			t.Fatal(err)
		}
		if dec.DeveloperCostDisplay != c.want {
			t.Errorf("round-trip %q = %q, want %q", c.in, dec.DeveloperCostDisplay, c.want)
		}
		raw, err := DecodeDisplayPolicyBody(`{"developer_cost_display":"` + c.in + `"}`)
		if err != nil {
			t.Fatal(err)
		}
		if raw.DeveloperCostDisplay != c.want {
			t.Errorf("decode raw %q = %q, want %q", c.in, raw.DeveloperCostDisplay, c.want)
		}
	}
	if _, err := DecodeDisplayPolicyBody("not json"); err == nil {
		t.Error("garbage body decoded")
	}
}
