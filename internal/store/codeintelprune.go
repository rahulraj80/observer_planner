package store

import (
	"context"
	"database/sql"
	"fmt"
)

// codeintelprune.go holds the plain-SQL per-project delete and the
// [codeintel].retention_days prune, split out of codeintel.go (lane MP-L1H,
// T-715) so the retention pass and the archive hot seam keep them in a build
// without the codeintel component (-tags sbo_omit_codeintel): the codeintel_*
// tables stay in the schema either way.

// CodeIntelDeleteProject removes every codeintel_* row for a project.
//
// It deletes each table explicitly by `project` rather than leaning on
// ON DELETE CASCADE from codeintel_files. The cascade seeks child rows by
// BARE file_id, but every file_id index is (project, file_id, ...) -
// project-first - so SQLite can't use it for the cascade and full-scans
// codeintel_{nodes,edges,sites} once PER deleted file: O(files × rows),
// measured at 20+ minutes / 100% CPU on a ~40k-file project. The
// per-`project` deletes here each ride a project-leading index instead, and
// FK enforcement is disabled for the operation so the (now-redundant)
// cascade machinery doesn't re-scan. Correctness is preserved because every
// child table is deleted explicitly - including codeintel_fts, a virtual
// table with NO foreign key that the old cascade-only delete leaked entirely.
//
// foreign_keys is toggled on a single pinned connection (the pragma is a
// no-op inside a transaction and is per-connection) and always restored
// before that connection returns to the pool.
func (s *Store) CodeIntelDeleteProject(ctx context.Context, project string) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("store.CodeIntelDeleteProject: conn: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return fmt.Errorf("store.CodeIntelDeleteProject: disable fk: %w", err)
	}
	// Restore FK enforcement before the connection goes back to the pool,
	// using a background context so a cancelled ctx can't skip the restore.
	defer func() { _, _ = conn.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`) }()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store.CodeIntelDeleteProject: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := codeIntelDeleteProjectTx(ctx, tx, project); err != nil {
		return fmt.Errorf("store.CodeIntelDeleteProject: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store.CodeIntelDeleteProject: commit: %w", err)
	}
	return nil
}

// codeIntelProjectDeletes is the per-project delete fan-out, child-to-parent.
// It is a package-level list with exactly TWO callers - CodeIntelDeleteProject
// and the archive-completion path in archive.go - so "one owner per table"
// survives the arrival of cold storage: the archival move must not grow a
// second, quietly divergent copy of this statement list. In particular
// codeintel_fts is a virtual table with no foreign key, and a copy that
// forgets it leaks the whole FTS index (the bug this list's explicitness was
// introduced to fix).
//
// The codeintel_fts row deliberately keeps the by-`project` predicate, a
// scan of the whole fts5 table (T-415): deleting a project is rare
// (operator delete, stale-project prune, archive move), and the scan is the
// one complete sweep, removing any orphan FTS row an older binary left for
// a node that no longer exists. The frequent rebuild path clears by node id
// instead (codeIntelFTSDeleteProjectSQL).
var codeIntelProjectDeletes = []string{
	`DELETE FROM codeintel_fts WHERE project = ?`,
	`DELETE FROM codeintel_embeddings WHERE project = ?`,
	`DELETE FROM codeintel_minhash WHERE project = ?`,
	`DELETE FROM codeintel_sites WHERE project = ?`,
	`DELETE FROM codeintel_edges WHERE project = ?`,
	`DELETE FROM codeintel_nodes WHERE project = ?`,
	`DELETE FROM codeintel_files WHERE project = ?`,
}

// codeIntelDeleteProjectTx runs the fan-out inside a caller-owned transaction.
// The caller is responsible for the foreign_keys=OFF pinning documented on
// CodeIntelDeleteProject - without it, the cascade re-scan makes this O(files
// × rows).
func codeIntelDeleteProjectTx(ctx context.Context, tx *sql.Tx, project string) error {
	for _, del := range codeIntelProjectDeletes {
		if _, err := tx.ExecContext(ctx, del, project); err != nil {
			return err
		}
	}
	return nil
}

// CodeIntelPruneStaleProjects deletes every codeintel_* row for projects
// whose most recent index pass (MAX(codeintel_files.indexed_at), unix
// seconds) is older than retentionDays, returning the project keys it
// deleted. This is the [codeintel].retention_days sweep (Ticket B,
// docs/plans/claude-code-hook-stall-ticket-and-db-prune-plan-2026-07-12.md):
// the index is rebuildable from the repo, so age-pruning is safe, and each
// project deletes through the SAME CodeIntelDeleteProject path `observer
// index delete -r` uses (one owner per table - no second delete SQL).
//
// Conservative by construction:
//   - retentionDays ≤ 0 disables the sweep entirely (returns nil, nil).
//   - A project with NO successful index pass yet (MAX(indexed_at) = 0 -
//     e.g. freshly registered files still 'pending') is NEVER pruned; its
//     zero watermark says "not indexed", not "indexed long ago".
//   - An actively indexed project always carries a fresh indexed_at and
//     is untouched.
//
// Idempotent: a second run within the same horizon finds no stale
// projects and is a no-op.
func (s *Store) CodeIntelPruneStaleProjects(ctx context.Context, retentionDays int) ([]string, error) {
	candidates, err := s.CodeIntelStaleProjects(ctx, retentionDays)
	if err != nil {
		return nil, fmt.Errorf("store.CodeIntelPruneStaleProjects: %w", err)
	}
	stale := make([]string, 0, len(candidates))
	for _, c := range candidates {
		stale = append(stale, c.Project)
	}
	for _, p := range stale {
		if err := s.CodeIntelDeleteProject(ctx, p); err != nil {
			return nil, fmt.Errorf("store.CodeIntelPruneStaleProjects: %w", err)
		}
	}
	if len(stale) == 0 {
		return nil, nil
	}
	return stale, nil
}
