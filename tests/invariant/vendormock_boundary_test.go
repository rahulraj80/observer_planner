package invariant

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// vendormock_boundary_test.go pins plan 12A.8 of the enterprise vendor API
// plan (docs/plans/enterprise-vendor-api-plan-2026-10-07.md): the vendor mock
// servers and conformance suite under internal/testsupport never reach a
// production binary. No build tag is relied on: the guard is the import graph.

const testSupportPkg = "internal/testsupport"

func isTestSupport(rel string) bool {
	return rel == testSupportPkg || strings.HasPrefix(rel, testSupportPkg+"/")
}

// TestNoMockPackageInProductionDeps: the full dependency closure of every
// command under cmd/ (observer and observer-org included) contains no
// internal/testsupport package. go list -deps excludes test files, so a
// _test.go import is allowed and a production import is not.
func TestNoMockPackageInProductionDeps(t *testing.T) {
	root := filepath.Join("..", "..")
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		t.Fatalf("read cmd/: %v", err)
	}
	var cmds []string
	for _, e := range entries {
		if e.IsDir() {
			cmds = append(cmds, "./cmd/"+e.Name())
		}
	}
	required := map[string]bool{"./cmd/observer": false, "./cmd/observer-org": false}
	if inPublicTree(root) {
		delete(required, "./cmd/observer-org") // stripped from the public tree; cmd/observer is still guarded
	}
	for _, c := range cmds {
		if _, ok := required[c]; ok {
			required[c] = true
		}
	}
	for c, seen := range required {
		if !seen {
			t.Fatalf("%s not found under cmd/: the guard would be vacuous", c)
		}
	}
	args := append([]string{"list", "-deps"}, cmds...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %v failed: %v\n%s", cmds, err, out)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg := strings.TrimSpace(line)
		if !strings.HasPrefix(pkg, modulePath) {
			continue
		}
		count++
		if rel := strings.TrimPrefix(pkg, modulePath); isTestSupport(rel) {
			t.Errorf("a production command links %s: mock servers and the conformance suite must never "+
				"reach a release binary (plan 12A.8)", rel)
		}
	}
	if count < 50 {
		t.Fatalf("go list -deps returned only %d module packages: the guard would be vacuous", count)
	}
}

// TestTestSupportImportedOnlyByTests: no non-test package in the module may
// import internal/testsupport except testsupport itself (.Imports excludes
// test-only imports).
func TestTestSupportImportedOnlyByTests(t *testing.T) {
	cmd := exec.Command("go", "list", "-e", "-f", "{{.ImportPath}}{{range .Imports}} {{.}}{{end}}", "./...")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list ./... failed: %v\n%s", err, out)
	}
	sawMock := false
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], modulePath) {
			continue
		}
		importer := strings.TrimPrefix(fields[0], modulePath)
		if isTestSupport(importer) {
			sawMock = true
			continue
		}
		for _, imp := range fields[1:] {
			if rel := strings.TrimPrefix(imp, modulePath); strings.HasPrefix(imp, modulePath) && isTestSupport(rel) {
				t.Errorf("non-test package %s imports %s (plan 12A.8: test support only)", importer, rel)
			}
		}
	}
	if !sawMock {
		t.Fatal("internal/testsupport/vendormock not found in ./...: the guard would be vacuous")
	}
}
