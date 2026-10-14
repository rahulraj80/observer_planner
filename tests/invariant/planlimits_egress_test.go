package invariant

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// planlimits_egress_test.go pins the Plans & limits module's egress posture
// (docs/plans-and-limits.md, D-640; ledger rows SUBS-2 / SUBS-4):
//
//   - internal/planlimitsnet is the ONLY package of the module that imports a
//     network or process package (net, net/http, os/exec);
//   - the always-on daemon paths never link the module's I/O packages at
//     all, so a daemon with no account added cannot call out;
//   - only the polling service and the observer command import the I/O
//     packages.

// planModulePrefix matches every package of the module.
const planModulePrefix = "internal/planlimits"

// planNetPkg is the one package allowed network and process imports.
const planNetPkg = "internal/planlimitsnet"

// planIOPkgs are the module's I/O packages (network, and vendor-local reads).
var planIOPkgs = []string{"internal/planlimitsnet", "internal/planlimitslocal"}

// planIOImporters may import planIOPkgs.
var planIOImporters = []string{"internal/planlimitssvc", "cmd/observer"}

// planNetworkImports are the imports reserved to planNetPkg.
var planNetworkImports = map[string]bool{"net": true, "net/http": true, "os/exec": true}

func planGoList(t *testing.T, args ...string) []string {
	t.Helper()
	toolchainTest(t)
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func TestPlanLimitsNetworkOnlyInOnePackage(t *testing.T) {
	saw := 0
	for _, line := range planGoList(t, "list", "-e", "-f", "{{.ImportPath}}{{range .Imports}} {{.}}{{end}}", "./internal/...") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], modulePath) {
			continue
		}
		rel := strings.TrimPrefix(fields[0], modulePath)
		if !strings.HasPrefix(rel, planModulePrefix) {
			continue
		}
		saw++
		if rel == planNetPkg {
			continue
		}
		for _, imp := range fields[1:] {
			if planNetworkImports[imp] {
				t.Errorf("%s imports %s: only %s may make network calls or run processes for this module", rel, imp, planNetPkg)
			}
		}
	}
	if saw < 4 {
		t.Fatalf("found %d module packages, want at least 4: the guard would be vacuous", saw)
	}
}

func TestPlanLimitsIOIsolated(t *testing.T) {
	for _, line := range planGoList(t, "list", "-e", "-f", "{{.ImportPath}}{{range .Imports}} {{.}}{{end}}", "./...") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], modulePath) {
			continue
		}
		importer := strings.TrimPrefix(fields[0], modulePath)
		for _, imp := range fields[1:] {
			target := strings.TrimPrefix(imp, modulePath)
			if _, hit := matchesCloudPkg(target, planIOPkgs); !hit {
				continue
			}
			if _, ok := matchesCloudPkg(importer, planIOImporters); ok {
				continue
			}
			if _, ok := matchesCloudPkg(importer, planIOPkgs); ok {
				continue
			}
			t.Errorf("%s imports %s: only the Plans & limits service and the observer command may", importer, target)
		}
	}
}

func TestPlanLimitsDaemonPathsCannotReachIO(t *testing.T) {
	for _, entry := range []string{"internal/watcher", "internal/proxy", "internal/store", "internal/hook", "internal/adapter/codex", "internal/adapter/claudecode"} {
		deps := planGoList(t, "list", "-deps", "./"+entry)
		sawSelf := false
		for _, pkg := range deps {
			rel := strings.TrimPrefix(strings.TrimSpace(pkg), modulePath)
			if rel == entry {
				sawSelf = true
			}
			if bad, hit := matchesCloudPkg(rel, append(append([]string{}, planIOPkgs...), "internal/planlimitssvc")); hit {
				t.Errorf("%s transitively imports %s: the always-on daemon paths must never link the Plans & limits I/O", entry, bad)
			}
		}
		if !sawSelf {
			t.Fatalf("go list -deps ./%s did not include itself: the guard would be vacuous", entry)
		}
	}
}
