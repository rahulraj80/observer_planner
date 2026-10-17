package invariant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOrgSkipIntegrityCheckIsTestOnly pins that orgdb.Options.SkipIntegrityCheck
// (the advisory quick_check off switch, D-609) is set only by test code: the
// dbtest fixtures and _test.go files. A production open must keep the probe
// (it is size-gated and fail-open already).
func TestOrgSkipIntegrityCheckIsTestOnly(t *testing.T) {
	root := repoRoot(t)
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", ".claude", "website", "marketing", "model-pricing":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if rel == "internal/orgserver/db/db.go" || strings.HasPrefix(rel, "internal/orgserver/db/dbtest/") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// Only the ORG Options field is pinned: the agent store's
		// internal/db Options has its own SkipIntegrityCheck, which the
		// daemon sets by design (it runs quick_check once in the background
		// after the listeners bind). A file is in scope when it imports the
		// org db package.
		src := string(b)
		if strings.Contains(src, `"github.com/marmutapp/superbased-observer/internal/orgserver/db"`) &&
			strings.Contains(src, "SkipIntegrityCheck") {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("SkipIntegrityCheck is for test fixtures only; production code sets it in: %v", offenders)
	}
}
