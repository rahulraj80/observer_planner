package main

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/toolresolve"
)

// liveToolGateBinding returns tool's first live blocking tool-gate binding.
func liveToolGateBinding(t *testing.T, tool string) (integration.Capability, integration.HookBinding) {
	t.Helper()
	c, ok := integration.For(tool)
	if !ok {
		t.Fatalf("%s is not a registry row", tool)
	}
	bs := liveBlockingToolGateBindings(c)
	if len(bs) == 0 {
		t.Fatalf("%s has no live blocking tool-gate binding; pick another row", tool)
	}
	return c, bs[0]
}

// TestBindingRegistrationSkipKinds pins review M2-05: only a skip that is
// COVERED elsewhere (the Claude Code plugin) reads as registered. A
// not-applicable skip (Cline on Windows, a suppressed cross-OS target) has
// no Observer hook at all, so the node tool gate must not read it as current.
func TestBindingRegistrationSkipKinds(t *testing.T) {
	c, b := liveToolGateBinding(t, "claude-code")
	cases := []struct {
		name string
		home hookHomeEvents
		want string
	}{
		{"covered by the plugin", hookHomeEvents{home: hookHomeNative, present: map[string]bool{}, skipped: "plugin", covered: true}, "yes"},
		{"not applicable on this OS", hookHomeEvents{home: hookHomeNative, present: map[string]bool{}, skipped: "macOS / Linux only"}, "n/a"},
		{"registered", hookHomeEvents{home: hookHomeNative, present: map[string]bool{b.NativeEvent: true}}, "yes"},
		{"missing", hookHomeEvents{home: hookHomeNative, present: map[string]bool{}}, "no"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := bindingRegistration(c, b, []hookHomeEvents{tc.home})
			if got != tc.want {
				t.Fatalf("bindingRegistration = %q, want %q", got, tc.want)
			}
		})
	}
	if _, ok := toolGateRegReason["n/a"]; !ok {
		t.Error("the n/a verdict has no node tool-gate reason")
	}
}

// TestDroidPromptSubmitOwesLiveTest pins review M2-10: `observer start`
// does not write droid's never-fired UserPromptSubmit (the grandfather
// basis does not hold for a hook that never ran), while it still writes the
// live PreToolUse gate.
func TestDroidPromptSubmitOwesLiveTest(t *testing.T) {
	if !autoRegisterSkipEvent("droid", "UserPromptSubmit") || !autoRegisterSkipEvent("droid-windows", "UserPromptSubmit") {
		t.Fatal("observer start would write droid UserPromptSubmit, which never fired live")
	}
	if autoRegisterSkipEvent("droid", "PreToolUse") {
		t.Fatal("observer start skips droid's live PreToolUse gate")
	}
	if !autoRegisterDecision("droid", true) {
		t.Fatal("droid is no longer auto-registered for its live tool gate")
	}
}

// TestLaunchHookHomes pins review M2-06: with a native and a Windows-bridge
// home, only the home of the binary the launch resolves decides the gate,
// so a current bridge hook never stands in for a missing native one.
func TestLaunchHookHomes(t *testing.T) {
	c, b := liveToolGateBinding(t, "claude-code")
	native := hookHomeEvents{home: hookHomeNative, present: map[string]bool{}}
	bridge := hookHomeEvents{home: hookHomeBridge, present: map[string]bool{b.NativeEvent: true}}
	prev := toolGateLaunchHome
	t.Cleanup(func() { toolGateLaunchHome = prev })

	toolGateLaunchHome = func(integration.Capability) string { return hookHomeNative }
	homes := launchHookHomes(c, []hookHomeEvents{native, bridge})
	if got, _ := bindingRegistration(c, b, homes); got != "no" {
		t.Fatalf("native launch with only a bridge hook = %q, want no", got)
	}
	toolGateLaunchHome = func(integration.Capability) string { return hookHomeBridge }
	homes = launchHookHomes(c, []hookHomeEvents{native, bridge})
	if got, _ := bindingRegistration(c, b, homes); got != "yes" {
		t.Fatalf("interop launch with a current bridge hook = %q, want yes", got)
	}
	// One home: nothing to choose, and the launch binary is not resolved.
	toolGateLaunchHome = func(integration.Capability) string { t.Fatal("resolved with a single home"); return "" }
	if got := launchHookHomes(c, []hookHomeEvents{native}); len(got) != 1 {
		t.Fatalf("single home = %+v", got)
	}

	// The resolution mapping is the launcher's own: only a foreign-only
	// interop launch is the bridge home.
	foreign := toolresolve.Resolution{Verdict: toolresolve.VerdictForeignOnly, Considered: []toolresolve.Candidate{{Path: "/mnt/c/x/claude.exe", Foreign: true}}}
	if got := launchHomeForResolution(foreign); got != hookHomeBridge {
		t.Errorf("foreign-only interop resolution = %q, want bridge", got)
	}
	nativeRes := toolresolve.Resolution{Verdict: toolresolve.VerdictOK, Bin: "/usr/bin/claude"}
	if got := launchHomeForResolution(nativeRes); got != hookHomeNative {
		t.Errorf("native resolution = %q, want native", got)
	}
	if got := launchHomeForResolution(toolresolve.Resolution{Verdict: toolresolve.VerdictNotFound}); !strings.EqualFold(got, hookHomeNative) {
		t.Errorf("not found = %q, want native", got)
	}
}
