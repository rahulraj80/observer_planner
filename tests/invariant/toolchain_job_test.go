package invariant

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// toolchainListFile is the one owner of the list of toolchain tests.
const toolchainListFile = "scripts/ci/toolchain-tests.txt"

var (
	toolchainOnce  sync.Once
	toolchainNames map[string]bool
	toolchainErr   error
)

// toolchainTest marks a test (or a helper of one) that execs the Go
// toolchain. Its top-level test must be listed in scripts/ci/toolchain-tests.txt
// (the test fails otherwise, so the ci `go` job never silently misses one).
// With SBO_SKIP_TOOLCHAIN_TESTS=1, which the race shards set, it skips: the
// non-race `go` job runs the listed tests instead (D-609). Locally, with the
// variable unset, every test runs as before.
func toolchainTest(t *testing.T) {
	t.Helper()
	toolchainOnce.Do(func() {
		b, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(toolchainListFile)))
		if err != nil {
			toolchainErr = err
			return
		}
		toolchainNames = map[string]bool{}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				toolchainNames[line] = true
			}
		}
	})
	if toolchainErr != nil {
		t.Fatalf("read %s: %v", toolchainListFile, toolchainErr)
	}
	top := strings.SplitN(t.Name(), "/", 2)[0]
	if !toolchainNames[top] {
		t.Fatalf("%s execs the Go toolchain but is not listed in %s; add it so the ci go job runs it", top, toolchainListFile)
	}
	if os.Getenv("SBO_SKIP_TOOLCHAIN_TESTS") == "1" {
		t.Skipf("execs the Go toolchain: runs in the ci go job without -race (%s)", toolchainListFile)
	}
}

// TestToolchainListNamesExist pins the other direction: every name in
// scripts/ci/toolchain-tests.txt is a test in this package that calls
// toolchainTest, so the go job's -run filter never names a test that was
// renamed or deleted (go test -run with an unknown name passes silently).
func TestToolchainListNamesExist(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "tests", "invariant")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var src strings.Builder
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_test.go") || e.Name() == "toolchain_job_test.go" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	all := src.String()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(toolchainListFile)))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		if !strings.Contains(all, "func "+name+"(t *testing.T)") {
			t.Errorf("%s lists %s, but no test of that name exists in tests/invariant", toolchainListFile, name)
		}
	}
	if !strings.Contains(all, "toolchainTest(t)") {
		t.Error("no test calls toolchainTest; the list and the go job step are dead")
	}
}
