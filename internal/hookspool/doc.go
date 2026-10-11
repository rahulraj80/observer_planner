// Package hookspool is the DB-free hand-off between a lifecycle hook and the
// daemon (hook-parity Lane C1, docs/audits/hook-parity-audit-2026-10-02.md
// section 6, Lane C risk note).
//
// Some hosts give a session-end or turn-end hook a tiny budget (Claude Code
// about 1.5 s shared across every SessionEnd hook, Codex 3 s) and may kill
// the hook process when the CLI exits. Opening the SQLite database, running
// its migration check and ingesting a row does not reliably fit. The hook
// therefore appends ONE small JSON line to a spool file beside the global
// config and exits; the daemon (the one drainer) ingests it through the
// store's normal Ingest seam.
//
// Drain protocol (two phase, so a line is never lost to a race between an
// append and the drainer): a drain pass first PROCESSES every aside file
// that was set aside on an earlier pass (older than Grace), then sets the
// live spool aside by renaming it. A hook that opened the live file before
// the rename finishes its single append long before the next pass reads
// the aside file, and a hook that opens it after the rename creates a new
// live file. Nothing here opens the database; the caller does.
package hookspool
