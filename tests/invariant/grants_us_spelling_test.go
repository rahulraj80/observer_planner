package invariant

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// grantsCopyFiles are the request/grant flow's sources (backlog item 5,
// docs/plans/request-grants-design-2026-10-03.md): the CLI, the node and org
// handlers, the pure core and the three dashboard pages. Their user-facing
// copy follows the T-160 ruling (US "organization"); this list is the lane's
// own files, so a later lane's British spelling elsewhere is not policed here.
var grantsCopyFiles = []string{
	"cmd/observer/request.go",
	"internal/accessgrant",
	"internal/intelligence/dashboard/requests.go",
	"internal/orgclient/accessgrants.go",
	"internal/orgclient/accessrequest_prepare.go",
	"internal/orgclient/requests.go",
	"internal/orgserver/accessgrant_handlers.go",
	"internal/orgserver/grantstore",
	"web/src/pages/Requests.tsx",
	"web2/src/components/GrantDialog.tsx",
	"web2/src/pages/Grants.tsx",
	"web2/src/pages/Requests.tsx",
}

// TestGrantsCopyUsesUSOrganization fails on any "organisation" (or
// organise / organised) in the request/grant flow's files, code comments
// included: the spelling ruling is one word everywhere, and a comment is
// where copy gets drafted before it moves into a string.
func TestGrantsCopyUsesUSOrganization(t *testing.T) {
	root := repoRoot(t)
	british := regexp.MustCompile(`(?i)organis(ation|ations|ed|e|ing)\b`)
	for _, rel := range grantsCopyFiles {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil && os.IsNotExist(err) && inPublicTree(root) {
			continue // a private-only path (internal/orgserver, web2) the public tree strips
		}
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		files := []string{path}
		if info.IsDir() {
			entries, err := os.ReadDir(path)
			if err != nil {
				t.Fatal(err)
			}
			files = files[:0]
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
					files = append(files, filepath.Join(path, e.Name()))
				}
			}
		}
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(string(raw), "\n") {
				if british.MatchString(line) {
					rp, _ := filepath.Rel(root, f)
					t.Errorf("%s:%d uses the British spelling (T-160 standardized on \"organization\"): %s", filepath.ToSlash(rp), i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}
