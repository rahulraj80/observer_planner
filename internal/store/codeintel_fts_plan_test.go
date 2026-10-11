//go:build !sbo_omit_codeintel

package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/codeintel"
)

func codeIntelExplain(t *testing.T, d *sql.DB, q string, args ...any) []string {
	t.Helper()
	rows, err := d.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+q, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		out = append(out, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestCodeIntelFTSDeletesSeekByRowid pins T-415 for codeintel_fts: the
// rebuild and file-save clears must reach the fts5 table by rowid (idxStr
// "=") from codeintel_nodes' index. The pre-fix
// `DELETE FROM codeintel_fts WHERE project = ?` planned as
// `SCAN codeintel_fts VIRTUAL TABLE INDEX 0:`, a read of the whole table.
func TestCodeIntelFTSDeletesSeekByRowid(t *testing.T) {
	_, database := newTestStore(t)
	for _, tc := range []struct {
		name string
		q    string
		args []any
	}{
		{"project", codeIntelFTSDeleteProjectSQL, []any{"/p"}},
		{"file", codeIntelFTSDeleteFileSQL, []any{"/p", int64(1)}},
	} {
		plan := codeIntelExplain(t, database, tc.q, tc.args...)
		joined := strings.Join(plan, "\n")
		sawVTab, sawNodes := false, false
		for _, line := range plan {
			if strings.Contains(line, "codeintel_fts VIRTUAL TABLE") {
				sawVTab = true
				if !strings.Contains(line, "INDEX 0:=") {
					t.Errorf("%s: fts5 table not reached by rowid: %q\n%s", tc.name, line, joined)
				}
			}
			if strings.Contains(line, "codeintel_nodes") {
				sawNodes = true
				if !strings.Contains(line, "project=?") {
					t.Errorf("%s: codeintel_nodes not seeked by project: %q\n%s", tc.name, line, joined)
				}
			}
		}
		if !sawVTab || !sawNodes {
			t.Errorf("%s: plan missing the node-id path (vtab=%v nodes=%v):\n%s", tc.name, sawVTab, sawNodes, joined)
		}
	}
}

// TestCodeIntelFTSStaysOnLiveNodes pins the invariant the by-node-id clear
// relies on: re-saving a file and rebuilding leaves no FTS row for a node
// that no longer exists, an orphan an older binary left is hidden from
// search, and the project delete still sweeps it.
func TestCodeIntelFTSStaysOnLiveNodes(t *testing.T) {
	s, database := newTestStore(t)
	ctx := context.Background()
	const project = "/p/live"
	save := func(name string) {
		t.Helper()
		if err := s.CodeIntelSaveFile(ctx, codeintel.FileResult{
			Project: project, Path: project + "/a.go", Lang: "go", Parser: "test",
			ContentHash: name, MTime: 1,
			Nodes: []codeintel.Node{{Kind: "function", Name: name, FQN: "pkg." + name, StartLine: 1, EndLine: 2, Signature: "func " + name + "()"}},
		}); err != nil {
			t.Fatalf("CodeIntelSaveFile: %v", err)
		}
	}
	orphans := func() int {
		t.Helper()
		var n int
		if err := database.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM codeintel_fts_content c
			  WHERE NOT EXISTS (SELECT 1 FROM codeintel_nodes n WHERE n.id = c.id)`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	save("Alpha")
	if err := s.CodeIntelBuildDerived(ctx, project); err != nil {
		t.Fatal(err)
	}
	save("Bravo") // replaces Alpha's node with a new id
	if n := orphans(); n != 0 {
		t.Errorf("after re-save: %d FTS rows point at a deleted node", n)
	}
	if err := s.CodeIntelBuildDerived(ctx, project); err != nil {
		t.Fatal(err)
	}
	if n := orphans(); n != 0 {
		t.Errorf("after rebuild: %d FTS rows point at a deleted node", n)
	}
	if got, _ := s.CodeIntelSearch(ctx, project, "Bravo", 10); len(got) != 1 {
		t.Errorf("search Bravo = %+v, want one hit", got)
	}
	if got, _ := s.CodeIntelSearch(ctx, project, "Alpha", 10); len(got) != 0 {
		t.Errorf("replaced symbol still searchable: %+v", got)
	}

	// A legacy orphan (an older binary's interrupted pass) is hidden from
	// search, survives a by-node-id rebuild, and is swept by the project
	// delete.
	if _, err := database.ExecContext(ctx,
		`INSERT INTO codeintel_fts(rowid, tokens, node_id, project, name, fqn, kind, lang, file, start_line, end_line)
		 VALUES(987654, 'ghost', 987654, ?, 'Ghost', 'pkg.Ghost', 'function', 'go', 'a.go', 1, 2)`, project); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.CodeIntelSearch(ctx, project, "Ghost", 10); len(got) != 0 {
		t.Errorf("orphan FTS row surfaced in search: %+v", got)
	}
	if err := s.CodeIntelDeleteProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM codeintel_fts`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("project delete left %d FTS rows", left)
	}
}
