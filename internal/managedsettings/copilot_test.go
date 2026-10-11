package managedsettings

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
)

// TestCopilotManagedArtifacts pins T-434: a locked org value for GitHub
// Copilot produces Copilot managed artifacts generated from the policy, in
// each product's own convention.
func TestCopilotManagedArtifacts(t *testing.T) {
	b := render(t,
		dir(`copilot/"chat.tools.global.autoApprove"`, enf, `false`),
		dir(`copilot/"extensions.allowed"`, enf, `{"github":true}`),
		dir("copilot-cli/permissions.disableBypassPermissionsMode", enf, `"disable"`),
		dir("copilot-cli/model", enf, `"gpt-5"`),
	)
	vs, ok := toolOf(b, "copilot")
	if !ok {
		t.Fatalf("no copilot bundle; unplaced %+v", b.Unplaced)
	}
	cases := []struct{ artifact, want string }{
		// VS Code policy names, not setting ids.
		{"copilot/policy.json", `"ChatToolsAutoApprove": false`},
		{"copilot/policy.json", `"AllowedExtensions": {`},
		{"copilot/policy.mobileconfig", "<string>com.microsoft.VSCode</string>"},
		// an object policy is JSON text in the profile; a bool stays native.
		{"copilot/policy.mobileconfig", "<key>AllowedExtensions</key>"},
		{"copilot/policy.mobileconfig", "<string>{&quot;github&quot;:true}</string>"},
		{"copilot/policy.reg", `"ChatToolsAutoApprove"=dword:00000000`},
		{"copilot/policy.reg", `[HKEY_LOCAL_MACHINE\SOFTWARE\Policies\Microsoft\VSCode]`},
	}
	for _, c := range cases {
		a, ok := artifact(vs, c.artifact)
		if !ok {
			t.Errorf("no %s", c.artifact)
			continue
		}
		if !strings.Contains(a.Content, c.want) {
			t.Errorf("%s lacks %q:\n%s", c.artifact, c.want, a.Content)
		}
	}
	if a, _ := artifact(vs, "copilot/policy.json"); strings.Contains(a.Content, "chat.tools.global.autoApprove") {
		t.Error("policy.json is keyed by policy name, never the setting id")
	}

	cli, ok := toolOf(b, "copilot-cli")
	if !ok {
		t.Fatalf("no copilot-cli bundle; unplaced %+v", b.Unplaced)
	}
	for _, c := range []struct{ artifact, want string }{
		{"copilot-cli/managed-settings.json", `"disableBypassPermissionsMode": "disable"`},
		// MDM: flat dotted names, string values.
		{"copilot-cli/managed-settings.mobileconfig", "<key>permissions.disableBypassPermissionsMode</key>"},
		{"copilot-cli/managed-settings.reg", `"permissions.disableBypassPermissionsMode"="disable"`},
		{"copilot-cli/managed-settings.reg", `[HKEY_LOCAL_MACHINE\SOFTWARE\Policies\GitHubCopilot]`},
	} {
		a, ok := artifact(cli, c.artifact)
		if !ok || !strings.Contains(a.Content, c.want) {
			t.Errorf("%s lacks %q:\n%s", c.artifact, c.want, a.Content)
		}
	}
	// Honest strengths: the bypass switch locks, a model is a baseline.
	got := map[string]Strength{}
	for _, p := range cli.Placed {
		got[p.Setting] = p.Strength
	}
	if got["copilot-cli/permissions.disableBypassPermissionsMode"] != StrengthTamperProof || got["copilot-cli/model"] != StrengthDefaultOnly {
		t.Errorf("copilot-cli strengths %+v", got)
	}
}

// TestCopilotPolicyNamesAreCatalogued keeps the rename table honest: every
// key names a catalogue setting the org policy can author, and no two
// settings share one policy name.
func TestCopilotPolicyNamesAreCatalogued(t *testing.T) {
	seen := map[string]string{}
	for id, name := range copilotPolicyNames {
		r, ok := agentconfig.Setting(id)
		if !ok {
			t.Errorf("%s is not in the catalogue", id)
			continue
		}
		if !Authorable(r) {
			t.Errorf("%s cannot be set by the org policy", id)
		}
		if prev, dup := seen[name]; dup {
			t.Errorf("%s and %s share policy %s", prev, id, name)
		}
		seen[name] = id
	}
}
