//go:build !sbo_omit_terminals

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/termsvc"
)

// firstTool returns the first registry row matching pred.
func firstTool(t *testing.T, pred func(integration.Capability) bool) integration.Capability {
	t.Helper()
	for _, c := range integration.Capabilities() {
		if c.Handoff.Launchable() && pred(c) {
			return c
		}
	}
	t.Fatal("no registry row matches")
	return integration.Capability{}
}

// TestCreateFreshSandboxFallback pins D-178 / D-221 at the node's launch
// decision: with the command guard enforcing and no sandbox available, a
// proxied sandbox_enforce tool is REFUSED on a managed node whose org chose
// org_disallow, and launched unsandboxed (recorded_acceptance) everywhere
// else: the default, an individual node, a tool with no proxy lever, a
// blocking-hook tool whose gate is current on this node, an observe-mode
// guard. A blocking-hook tool whose gate is NOT current here is judged like
// a tool with no hook (D-262), so it reaches the org's choice too.
func TestCreateFreshSandboxFallback(t *testing.T) {
	proxied := firstTool(t, func(c integration.Capability) bool {
		return c.EnforcementChannel() == integration.EnforceSandbox && c.Proxy != nil
	})
	unproxied := firstTool(t, func(c integration.Capability) bool {
		return c.EnforcementChannel() == integration.EnforceSandbox && c.Proxy == nil
	})
	hookBlock := firstTool(t, func(c integration.Capability) bool {
		return c.EnforcementChannel() == integration.EnforceHookBlock
	})
	const disallow = "[fallbacks]\nsandbox_unavailable = \"org_disallow\"\n"
	cases := []struct {
		name    string
		tool    string
		mode    string
		bundle  string
		managed bool
		gate    bool // the tool-gate hook is registered and current here
		refused bool
	}{
		{"managed + org_disallow + proxied tool: refused", proxied.Tool, "enforce", disallow, true, false, true},
		{"managed + default: recorded acceptance", proxied.Tool, "enforce", "", true, false, false},
		{"individual + org_disallow: recorded acceptance", proxied.Tool, "enforce", disallow, false, false, false},
		{"managed + org_disallow + unproxied tool: recorded acceptance", unproxied.Tool, "enforce", disallow, true, false, false},
		{"managed + org_disallow + blocking-hook tool, gate current: unaffected", hookBlock.Tool, "enforce", disallow, true, true, false},
		{"managed + org_disallow + blocking-hook tool, gate not current here: refused", hookBlock.Tool, "enforce", disallow, true, false, hookBlock.Proxy != nil},
		{"managed + org_disallow, guard observes: unaffected", proxied.Tool, "observe", disallow, true, false, false},
		// Review M2-13: a per-rule enforced org rule makes the tool family
		// enforcing under a global observe mode (GateEnforcement().Tool), so
		// the launch decision is the enforcing one, as the hook posture is.
		{"managed + org_disallow + per-rule enforce, guard observes: refused", proxied.Tool, "observe", disallow + "\n[[override]]\nrule = \"R-110\"\ndecision = \"deny\"\nenforce = true\n", true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.Default().Guard
			cfg.Enabled, cfg.Mode = true, tc.mode
			cfg.Rules.OrgBundle = filepath.Join(dir, "org-policy-bundle.json")
			cfg.Rules.UserPolicy = ""
			pin := ""
			if tc.bundle != "" {
				pub, priv, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				writeFileT(t, cfg.Rules.OrgBundle, signedGuardBundleJSON(t, priv, pub, 3, tc.bundle))
				pin = pinOf(pub)
			}
			managed := tc.managed
			gd, err := guard.New(guard.Options{Config: cfg, Home: dir, OrgKeyPinHash: pin, ManagedTenancy: func() bool { return managed }})
			if err != nil {
				t.Fatal(err)
			}
			lau := &recordingLauncher{}
			svc := termsvc.New(termsvc.Options{
				Recorder: assembledRecorder{},
				Launcher: lau,
				Policy:   termsvc.Policy{AllowFresh: true, AllowedTools: []string{tc.tool}, AllowedProjectRoots: []string{dir}},
			})
			gateCurrent := tc.gate
			a := &launchManagerAdapter{svc: svc, guard: gd, // no sandboxProber: no sandbox on this machine
				toolGate: func(string) nodeToolGateState { return nodeToolGateState{Current: gateCurrent} }}
			_, err = a.CreateFresh(dashapi.FreshLaunchSpec{Tool: tc.tool, Subcommand: tc.tool, ProjectRoot: dir})
			if tc.refused {
				if !errors.Is(err, dashapi.ErrLaunchSandboxRequired) {
					t.Fatalf("err = %v, want ErrLaunchSandboxRequired", err)
				}
				if lau.last.Dir != "" {
					t.Fatal("a refused launch reached the spawner")
				}
				// The dashboard's API client shows the first 200 bytes of an
				// error body (http.Error adds a newline): the whole reason,
				// fix included, must fit for the longest proxied tool name.
				longest := ""
				for _, c := range integration.Capabilities() {
					if c.Handoff.Launchable() && c.Proxy != nil && len(c.Tool) > len(longest) {
						longest = c.Tool
					}
				}
				if n := len(err.Error()) - len(tc.tool) + len(longest) + 1; n > 200 {
					t.Errorf("refusal is %d bytes for %q, over the dashboard's 200-byte error display: %v", n, longest, err)
				}
				return
			}
			// No sandboxer is wired, so a launch that asked for one would fail
			// (termsvc.ErrSandboxUnavailable): a clean spawn proves the honest
			// unsandboxed recorded_acceptance launch.
			if err != nil {
				t.Fatalf("launch = %v, want an unsandboxed spawn", err)
			}
			if lau.last.Dir == "" {
				t.Fatal("the launch never reached the spawner")
			}
		})
	}
}
