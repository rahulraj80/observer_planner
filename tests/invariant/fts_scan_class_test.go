package invariant

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The FTS full-scan class (T-412 FTSSTALL, T-415): both FTS5 tables on the
// node keep their non-text columns UNINDEXED (action_excerpts.action_id,
// codeintel_fts.project / node_id / ...), so a WHERE / JOIN predicate or a
// bare COUNT(*) on the virtual table is planned as a scan of EVERY stored
// row. On a large node that held the write lock for about a second per call
// and stalled ingestion for days. The cheap shapes are a MATCH, a rowid
// equality, or a seek on the shadow table (action_excerpts_content.c0,
// indexed by migration 147) that resolves to rowids.
//
// This guard fails when production Go source reintroduces one of the scan
// shapes. Each per-site fix also carries an EXPLAIN QUERY PLAN test in its
// own package; this one catches a NEW site. Patterns are assembled from
// fragments so this file never matches itself.
var ftsScanPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{
		name: "predicate on an UNINDEXED action_excerpts column",
		re:   regexp.MustCompile(`(?is)FROM\s+action_` + `excerpts\s+WHERE\s+(?:action_id|tool_name|target|error_message)\b`),
	},
	{
		name: "join on action_excerpts.action_id",
		re:   regexp.MustCompile(`(?is)JOIN\s+action_` + `excerpts\s+(?:AS\s+)?\w+\s+ON\s+[^\n]*\.action_id\b`),
	},
	{
		name: "COUNT(*) on the action_excerpts virtual table",
		re:   regexp.MustCompile(`(?is)COUNT\(\*\)\s+FROM\s+action_` + `excerpts\b`),
	},
	{
		name: "predicate on an UNINDEXED codeintel_fts column",
		re:   regexp.MustCompile(`(?is)FROM\s+codeintel_` + `fts\s+WHERE\s+(?:node_id|project|name|fqn|kind|lang|file|start_line|end_line)\b`),
	},
}

// ftsScanAllowed lists the deliberate scans, each with its reason. The key
// is the slash-separated repo-relative path; the value is a substring of
// the matched text.
var ftsScanAllowed = map[string][]string{
	// The rare project delete fan-out keeps its by-project scan as the one
	// complete sweep (it also removes orphans an older binary left); the
	// frequent rebuild clears by node id. See codeIntelProjectDeletes, which
	// moved from codeintel.go to codeintelprune.go when codeintel became a
	// removable component (MP-L1H, D-512); the scan itself is unchanged.
	"internal/store/codeintelprune.go": {"WHERE project"},
	// Generator of the historical, already-applied migration 079.
	"internal/db/reasoninggen/main.go": {""},
}

func TestNoFTSFullScanShapes(t *testing.T) {
	root := repoRoot(t)
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			body = stripLineComments(body)
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, p := range ftsScanPatterns {
				for _, m := range p.re.FindAllString(string(body), -1) {
					if ftsScanIsAllowed(rel, m) {
						continue
					}
					t.Errorf("%s: %s: %q (resolve ids through the shadow table or rowid; see TestNoFTSFullScanShapes)", rel, p.name, m)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// stripLineComments blanks every line whose first non-space text is `//`,
// so a comment that quotes the old scan shape (as the fixes' own
// explanations do) is not mistaken for a query.
func stripLineComments(body []byte) []byte {
	lines := strings.Split(string(body), "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "//") {
			lines[i] = ""
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func ftsScanIsAllowed(rel, match string) bool {
	for _, sub := range ftsScanAllowed[rel] {
		if strings.Contains(match, sub) {
			return true
		}
	}
	return false
}
