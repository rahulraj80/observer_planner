package agentconfigorg

import (
	"encoding/json"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

func TestAccessFor(t *testing.T) {
	enforced := orgcontract.AgentConfigDirective{Setting: "codex/model", Value: json.RawMessage(`"o3"`), Mode: "enforced", Policy: "Baseline"}
	def := orgcontract.AgentConfigDirective{Setting: "claude-code/model", Value: json.RawMessage(`"sonnet"`), Mode: "default", Policy: "Baseline"}
	cases := []struct {
		name                   string
		body                   orgcontract.AgentConfigPolicyBody
		readOnly, grantsOnly   bool
		lockedCodex            bool
		claudeModel, codexOpts bool // Granted("claude-code/model"), Granted("codex/approval_policy")
		posture                agentconfig.SecretsPosture
	}{
		{"no grants: read-only", orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{enforced}}, true, false, true, true, true, agentconfig.SecretsWarn},
		{
			"a setting grant",
			orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{enforced, def}, Grants: []orgcontract.AgentConfigGrant{{Setting: "claude-code/model"}}, SecretsPosture: "refuse"},
			false, true, true, true, false, agentconfig.SecretsRefuse,
		},
		{
			"a whole-tool grant",
			orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{enforced}, Grants: []orgcontract.AgentConfigGrant{{Setting: "codex/*"}}},
			false, true, true, false, true, agentconfig.SecretsWarn,
		},
		{
			"editable opens all but the enforced",
			orgcontract.AgentConfigPolicyBody{Directives: []orgcontract.AgentConfigDirective{enforced}, DeveloperAccess: "editable", SecretsPosture: "allow"},
			false, false, true, true, true, agentconfig.SecretsAllow,
		},
		{"an unknown access value is read-only", orgcontract.AgentConfigPolicyBody{DeveloperAccess: "everything"}, true, false, false, true, true, agentconfig.SecretsWarn},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := AccessFor(c.body)
			if a.ReadOnly != c.readOnly || a.GrantsOnly != c.grantsOnly || (a.Locked["codex/model"] != "") != c.lockedCodex || a.SecretsPosture != c.posture {
				t.Fatalf("access = %+v", a)
			}
			if a.Granted("claude-code/model") != c.claudeModel || a.Granted("codex/approval_policy") != c.codexOpts {
				t.Fatalf("grants: claude=%v codex=%v (%+v)", a.Granted("claude-code/model"), a.Granted("codex/approval_policy"), a)
			}
		})
	}
}

func TestNodeAccess(t *testing.T) {
	body := &orgcontract.AgentConfigPolicyBody{
		DeveloperAccess: "editable",
		Directives:      []orgcontract.AgentConfigDirective{{Setting: "codex/model", Mode: "enforced", Policy: "Baseline"}},
	}
	cases := []struct {
		name     string
		in       NodeAccessInput
		readOnly bool
		reason   string
		locked   bool
	}{
		{"individual edits freely", NodeAccessInput{}, false, "", false},
		{"individual ignores a stray cached body", NodeAccessInput{Body: body}, false, "", false},
		{"managed with no policy is read-only", NodeAccessInput{Managed: true}, true, ManagedReadOnlyReason, false},
		{"managed with a policy locks the enforced setting", NodeAccessInput{Managed: true, Body: body}, false, "", true},
		{"an org-pinned page is read-only", NodeAccessInput{PageReadOnly: true}, true, PagePinnedReason, false},
		{"an org-pinned page keeps the locks", NodeAccessInput{Managed: true, Body: body, PageReadOnly: true}, true, PagePinnedReason, true},
		{"the managed default yields to a held policy", NodeAccessInput{Managed: true, Body: body, PageReadOnly: true, PageFromDefault: true}, false, "", true},
		{"the managed default holds with no policy", NodeAccessInput{Managed: true, PageReadOnly: true, PageFromDefault: true}, true, ManagedReadOnlyReason, false},
		// T-436: an AI tool setting edit grant opens what it names.
		{"an edit grant lifts the enforced lock", NodeAccessInput{Managed: true, Body: body, EditGrants: []string{"codex/model"}}, false, "", false},
		{"a tool-wide edit grant lifts it too", NodeAccessInput{Managed: true, Body: body, EditGrants: []string{"codex/*"}}, false, "", false},
		{"an edit grant for another setting keeps the lock", NodeAccessInput{Managed: true, Body: body, EditGrants: []string{"codex/approval_policy"}}, false, "", true},
		{"an edit grant opens a managed machine with no policy", NodeAccessInput{Managed: true, EditGrants: []string{"codex/model"}, PageReadOnly: true, PageFromDefault: true}, false, "", false},
		{"an org-pinned page stays read-only under an edit grant", NodeAccessInput{Managed: true, Body: body, EditGrants: []string{"codex/model"}, PageReadOnly: true}, true, PagePinnedReason, false},
		{"an individual node has no use for an edit grant", NodeAccessInput{Body: body, EditGrants: []string{"codex/model"}}, false, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := NodeAccess(c.in)
			if a.ReadOnly != c.readOnly || a.Reason != c.reason || (a.Locked["codex/model"] == "Baseline") != c.locked {
				t.Fatalf("%+v", a)
			}
		})
	}
}

// TestWithEditGrants (T-436): an edit grant adds to a grants-only policy's
// grants, opens a read-only-until-granted machine to exactly what it names,
// and never lifts any other read-only.
func TestWithEditGrants(t *testing.T) {
	grantsOnly := AccessFor(orgcontract.AgentConfigPolicyBody{Grants: []orgcontract.AgentConfigGrant{{Setting: "claude-code/model"}}})
	got := WithEditGrants(grantsOnly, []string{"codex/model"})
	if !got.GrantsOnly || !got.Granted("codex/model") || !got.Granted("claude-code/model") || got.Granted("codex/approval_policy") {
		t.Fatalf("grants-only + edit grant: %+v", got)
	}
	if len(grantsOnly.Grants) != 1 {
		t.Fatalf("the input access was modified: %+v", grantsOnly)
	}
	none := AccessFor(orgcontract.AgentConfigPolicyBody{})
	got = WithEditGrants(none, []string{"codex/*"})
	if got.ReadOnly || !got.GrantsOnly || !got.Granted("codex/model") || got.Granted("claude-code/model") {
		t.Fatalf("read-only until granted + edit grant: %+v", got)
	}
	pinned := agentconfig.Access{ReadOnly: true, Reason: PagePinnedReason}
	if got := WithEditGrants(pinned, []string{"codex/*"}); !got.ReadOnly {
		t.Fatalf("an org-pinned page was opened: %+v", got)
	}
	if got := WithEditGrants(none, nil); !got.ReadOnly {
		t.Fatalf("no grant changed the access: %+v", got)
	}
}
