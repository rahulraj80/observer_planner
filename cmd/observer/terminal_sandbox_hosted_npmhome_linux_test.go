//go:build linux && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"context"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
)

// Fork change (upstream's installTool on a NoRootExec vendor): SandHold runs
// every exec as the box's non-root user, so upstream's `npm install -g` into
// the image's global prefix fails with EACCES. The npm way installs under
// the box user's home instead, its cache is removed afterwards, and the
// prefix's bin is on PATH; any other vendor keeps upstream's flow.

// hostedInstallSpecs runs installTool with choice on vendor id and returns
// the exec specs the box saw.
func hostedInstallSpecs(t *testing.T, id string, choice sandboxremote.InstallChoice) (*hostedRun, []sandboxremote.ExecSpec) {
	t.Helper()
	hh := newHostedHarness(t, sandboxremote.VendorE2B)
	v, ok := sandboxremote.VendorFor(id)
	if !ok {
		t.Fatalf("no vendor %s", id)
	}
	var specs []sandboxremote.ExecSpec
	hh.client.execs = func(_ *fakeBox, spec sandboxremote.ExecSpec) (sandboxremote.Process, error) {
		specs = append(specs, spec)
		return newFakeProcess("", 0), nil
	}
	info, err := hh.client.Create(context.Background(), sandboxremote.BoxSpec{})
	if err != nil {
		t.Fatal(err)
	}
	r := hh.run
	r.vendor, r.client, r.rec.VendorID = v, hh.client, info.ID
	r.layout = v.Layout(r.plan.Root)
	r.plan.ToolInstalls = []sandboxremote.InstallChoice{choice}
	r.install = &choice
	if err := r.installTool(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r, specs
}

func TestHostedNPMInstallUnderHomeOnNoRootExec(t *testing.T) {
	npm := sandboxremote.InstallChoice{Channel: "npm", Argv: []string{"npm", "install", "-g", "@anthropic-ai/claude-code"}, Needs: []string{"npm"}}
	r, specs := hostedInstallSpecs(t, sandboxremote.VendorSandHold, npm)
	if !r.vendor.NoRootExec {
		t.Fatal("the SandHold row is no longer NoRootExec; this test no longer covers it")
	}
	home := r.layout.Home
	if len(specs) != 2 || !slices.Equal(specs[0].Argv, npm.Argv) {
		t.Fatalf("execs %+v", specs)
	}
	env := specs[0].Env
	if env["NPM_CONFIG_PREFIX"] != path.Join(home, ".local") {
		t.Errorf("npm prefix %q, want under the box home %s", env["NPM_CONFIG_PREFIX"], home)
	}
	cache := env["NPM_CONFIG_CACHE"]
	if !strings.HasPrefix(cache, home+"/") {
		t.Errorf("npm cache %q is not under the box home %s", cache, home)
	}
	if !slices.Equal(specs[1].Argv, []string{"rm", "-rf", "--", cache}) {
		t.Errorf("the npm cache is not removed after the install: %v", specs[1].Argv)
	}
	bin := path.Join(home, ".local", "bin")
	for what, p := range map[string]string{"install": env["PATH"], "agent": r.agentEnv()["PATH"]} {
		if !slices.Contains(strings.Split(p, ":"), bin) {
			t.Errorf("%s PATH %q lacks %s", what, p, bin)
		}
	}
}

func TestHostedNPMInstallUpstreamElsewhere(t *testing.T) {
	npm := sandboxremote.InstallChoice{Channel: "npm", Argv: []string{"npm", "install", "-g", "x"}, Needs: []string{"npm"}}
	script := sandboxremote.InstallChoice{Channel: "script", Argv: []string{"bash", "-c", "curl -fsSL https://example.invalid/install | bash"}, Needs: []string{"bash", "curl"}}
	// A vendor with a root exec keeps upstream's global install.
	r, specs := hostedInstallSpecs(t, sandboxremote.VendorE2B, npm)
	if len(specs) != 1 || specs[0].Env["NPM_CONFIG_PREFIX"] != "" || strings.Contains(r.boxEnv()["PATH"], ".local/bin") {
		t.Errorf("a root-exec vendor's npm install changed: %+v", specs)
	}
	// A NoRootExec vendor's script install is upstream's too.
	r, specs = hostedInstallSpecs(t, sandboxremote.VendorSandHold, script)
	if len(specs) != 1 || specs[0].Env["NPM_CONFIG_PREFIX"] != "" || strings.Contains(r.boxEnv()["PATH"], ".local/bin") {
		t.Errorf("a script install changed: %+v", specs)
	}
}
