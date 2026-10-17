package orgcontract

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func acTestKey(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv, base64.StdEncoding.EncodeToString(pub)
}

func acBody() AgentConfigPolicyBody {
	return AgentConfigPolicyBody{
		SecretsPosture: "refuse", OnDrift: "report", DeveloperAccess: "read_only",
		Directives: []AgentConfigDirective{{
			Setting: "claude-code/model", Value: json.RawMessage(`  "claude-sonnet-4-5" `), Mode: AgentConfigModeEnforced,
			Layer: AgentConfigLayerTeam, Policy: "Apps baseline",
		}},
	}
}

func TestAgentConfigPolicySignVerify(t *testing.T) {
	priv, pub := acTestKey(t)
	doc, err := SignAgentConfigPolicy(priv, "org-1", "user-1", 7, acBody())
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAgentConfigPolicy(doc, "org-1", "user-1", pub); err != nil {
		t.Fatalf("verify: %v", err)
	}
	b, err := DecodeAgentConfigPolicyBody(doc.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(b.Directives[0].Value) != `"claude-sonnet-4-5"` {
		t.Fatalf("value not compacted: %s", b.Directives[0].Value)
	}
	if b.Schema != AgentConfigPolicySchema || b.SecretsPosture != "refuse" || b.OnDrift != "report" {
		t.Fatalf("body = %+v", b)
	}
}

func TestAgentConfigPolicyRefusals(t *testing.T) {
	priv, pub := acTestKey(t)
	_, otherPub := acTestKey(t)
	doc, err := SignAgentConfigPolicy(priv, "org-1", "user-1", 7, acBody())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name            string
		mutate          func(d *AgentConfigPolicyDoc)
		org, sub, pubKy string
		want            string
	}{
		{"other org", nil, "org-2", "user-1", pub, "for org"},
		{"other member", nil, "org-1", "user-2", pub, "for member"},
		{"other key", nil, "org-1", "user-1", otherPub, "signature invalid"},
		{"inflated version", func(d *AgentConfigPolicyDoc) { d.Version = 99 }, "org-1", "user-1", pub, "signature invalid"},
		{"body tampered", func(d *AgentConfigPolicyDoc) {
			d.Body = strings.Replace(d.Body, "sonnet", "opus", 1)
		}, "org-1", "user-1", pub, "body hash"},
		{"rehashed tamper", func(d *AgentConfigPolicyDoc) {
			d.Body = strings.Replace(d.Body, "sonnet", "opus", 1)
			d.BodyHash = acHashHex(d.Body)
		}, "org-1", "user-1", pub, "signature invalid"},
		{"unsigned", func(d *AgentConfigPolicyDoc) { d.Signature = "" }, "org-1", "user-1", pub, "unsigned"},
		{"negative version", func(d *AgentConfigPolicyDoc) { d.Version = -1 }, "org-1", "user-1", pub, "negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := doc
			if tc.mutate != nil {
				tc.mutate(&d)
			}
			err := VerifyAgentConfigPolicy(d, tc.org, tc.sub, tc.pubKy)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// A body signed by a newer server with an extra field still verifies (the
// signature covers the bytes) and decodes leniently.
func TestAgentConfigPolicyForwardCompatibleBody(t *testing.T) {
	priv, pub := acTestKey(t)
	body := `{"schema":1,"secrets_posture":"paranoid","on_drift":"later","developer_access":"maybe","future":{"x":1},"directives":[]}`
	doc := AgentConfigPolicyDoc{OrgID: "o", Subject: "s", Version: 3, Body: body, BodyHash: acHashHex(body)}
	doc.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, AgentConfigPolicySigningMessage("o", "s", 3, body)))
	if err := VerifyAgentConfigPolicy(doc, "o", "s", pub); err != nil {
		t.Fatal(err)
	}
	b, err := DecodeAgentConfigPolicyBody(body)
	if err != nil {
		t.Fatal(err)
	}
	// Unknown closed values never widen: warn, reassert, read_only.
	if b.SecretsPosture != "warn" || b.OnDrift != "reassert" || b.DeveloperAccess != "read_only" {
		t.Fatalf("normalized = %+v", b)
	}
	if _, err := DecodeAgentConfigPolicyBody(`{"schema":2}`); err == nil {
		t.Fatal("a newer schema must be refused")
	}
}

func TestAgentConfigPolicyEmpty(t *testing.T) {
	if !(AgentConfigPolicyBody{SecretsPosture: "refuse"}).Empty() {
		t.Fatal("no directives and no grants is empty")
	}
	if (AgentConfigPolicyBody{Grants: []AgentConfigGrant{{Setting: "codex/*"}}}).Empty() {
		t.Fatal("a grant is not empty")
	}
}

func TestAgentConfigPolicyRefusesEmptySubject(t *testing.T) {
	priv, _ := acTestKey(t)
	if _, err := SignAgentConfigPolicy(priv, "o", " ", 1, AgentConfigPolicyBody{}); err == nil {
		t.Fatal("empty subject must be refused")
	}
}

func acHashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
