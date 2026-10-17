//go:build !sbo_omit_archive

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/archive"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
)

// newArchiveReclaimCleanupCmd implements `observer archive reclaim-cleanup`
// (T-493): list, and with --delete remove, the original database a reclaim
// renamed aside (<db>.pre-reclaim-<stamp> plus its -wal / -shm).
//
// `reclaim --run` deliberately deletes nothing and tells the operator to
// remove the leftover once the daemon is back and healthy; on 2026-09-26 a
// forgotten 35.7 GB leftover filled the disk. This is that removal as one
// command (and the Settings > Storage card runs exactly this command as a
// job). It is operator-invoked only, never automatic, and the refusals live
// in the pure archive.PlanLeftoverCleanup table: nothing is deleted while a
// reclaim copy is still being written, when there is no database at
// db_path, or when the database in place does not pass PRAGMA quick_check,
// because the leftover may be the only good copy. It never opens the
// database through the migrating opener, so it cannot create one.
func newArchiveReclaimCleanupCmd() *cobra.Command {
	var (
		configPath string
		jsonOut    bool
		del        bool
	)
	cmd := &cobra.Command{
		Use:   "reclaim-cleanup",
		Short: "List, and with --delete remove, the original database a reclaim kept aside",
		Long: "`observer archive reclaim --run` renames the original database aside as\n" +
			"<db>.pre-reclaim-<stamp> (with its -wal / -shm) and never deletes it. That copy\n" +
			"is as large as the database was before the reclaim.\n\n" +
			"With no flags this only LISTS those leftovers and their size.\n\n" +
			"With --delete it first verifies the database now in place (it must exist and\n" +
			"pass PRAGMA quick_check, which reads the whole file and can take minutes on a\n" +
			"large database) and refuses while a reclaim copy is still being written; only\n" +
			"then are the leftovers deleted. Safe while the daemon runs: the leftovers are\n" +
			"not the files it has open.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(config.LoadOptions{GlobalPath: configPath})
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			rep, err := buildCleanupReport(cmd.Context(), cfg.Observer.DBPath, del)
			if err != nil {
				return err
			}
			if del && rep.Plan != nil && rep.Plan.OK() {
				deleteLeftovers(&rep)
			}
			if err := emitCleanupReport(cmd, rep, jsonOut); err != nil {
				return err
			}
			switch {
			case rep.DeleteError != "":
				return fmt.Errorf("archive reclaim-cleanup: %s", rep.DeleteError)
			case del && rep.Plan != nil && !rep.Plan.OK() && rep.Plan.Decision != archive.CleanupNothing:
				return fmt.Errorf("archive reclaim-cleanup: %s", rep.Plan.Reason)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to config.toml")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	cmd.Flags().BoolVar(&del, "delete", false,
		"verify the database in place (quick_check), then delete the leftovers. Without this the command only lists them")
	return cmd
}

// cleanupReport is the one shape the list view, the refusal and the
// post-delete summary render.
type cleanupReport struct {
	DBPath     string                    `json:"db_path"`
	Leftovers  []archive.ReclaimLeftover `json:"leftovers"`
	Bytes      int64                     `json:"bytes"`
	InProgress bool                      `json:"reclaim_in_progress"`
	// Plan is set only with --delete (the database is checked only then).
	Plan        *archive.CleanupPlan `json:"plan,omitempty"`
	Deleted     []string             `json:"deleted,omitempty"`
	FreedBytes  int64                `json:"freed_bytes"`
	DeleteError string               `json:"delete_error,omitempty"`
}

// buildCleanupReport lists the leftovers next to dbPath and, when deleting,
// measures the database in place and plans. It deletes nothing.
func buildCleanupReport(ctx context.Context, dbPath string, deleting bool) (cleanupReport, error) {
	rep := cleanupReport{DBPath: dbPath, Leftovers: []archive.ReclaimLeftover{}}
	entries, err := os.ReadDir(filepath.Dir(dbPath))
	if err != nil {
		return rep, fmt.Errorf("archive reclaim-cleanup: list %s: %w", filepath.Dir(dbPath), err)
	}
	var files []archive.LeftoverFile
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		files = append(files, archive.LeftoverFile{Name: e.Name(), Bytes: info.Size()})
	}
	leftovers, inProgress := archive.MatchLeftovers(filepath.Base(dbPath), files)
	if leftovers != nil {
		rep.Leftovers = leftovers
	}
	rep.InProgress = inProgress
	for _, l := range rep.Leftovers {
		rep.Bytes += l.Bytes
	}
	if !deleting {
		return rep, nil
	}
	in := archive.CleanupInput{Leftovers: rep.Leftovers, InProgress: inProgress}
	if fi, serr := os.Lstat(dbPath); serr == nil && fi.Mode().IsRegular() {
		in.LivePresent = true
	}
	// The whole-file check runs only when it can change the answer.
	if in.LivePresent && !in.InProgress && len(in.Leftovers) > 0 {
		in.LiveQuickCheck = liveQuickCheck(ctx, dbPath)
	}
	plan := archive.PlanLeftoverCleanup(in)
	rep.Plan = &plan
	return rep, nil
}

// liveQuickCheck runs PRAGMA quick_check(1) on the database in place through
// the plain (non-migrating) opener. "" when it could not be run.
func liveQuickCheck(ctx context.Context, dbPath string) string {
	database, err := db.OpenPlain(ctx, dbPath)
	if err != nil {
		return ""
	}
	defer database.Close()
	fp, err := db.ReadFingerprint(ctx, database)
	if err != nil {
		return ""
	}
	return fp.QuickCheck
}

// deleteLeftovers removes every leftover file, sidecars before the database
// they belong to, re-checking each is still a regular file. It stops at the
// first failure and records it.
func deleteLeftovers(rep *cleanupReport) {
	dir := filepath.Dir(rep.DBPath)
	for _, l := range rep.Leftovers {
		for i := len(l.Files) - 1; i >= 0; i-- {
			p := filepath.Join(dir, l.Files[i].Name)
			fi, err := os.Lstat(p)
			if err != nil || !fi.Mode().IsRegular() {
				rep.DeleteError = fmt.Sprintf("%s is no longer a regular file; stopped", p)
				return
			}
			if err := os.Remove(p); err != nil {
				rep.DeleteError = fmt.Sprintf("delete %s: %v", p, err)
				return
			}
			rep.Deleted = append(rep.Deleted, p)
			rep.FreedBytes += fi.Size()
		}
	}
}

func emitCleanupReport(cmd *cobra.Command, rep cleanupReport, jsonOut bool) error {
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	out := cmd.OutOrStdout()
	if len(rep.Leftovers) == 0 {
		fmt.Fprintf(out, "no reclaim leftovers next to %s\n", rep.DBPath)
		if rep.InProgress {
			fmt.Fprintln(out, "(a reclaim copy is being written there right now)")
		}
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, l := range rep.Leftovers {
		for _, f := range l.Files {
			fmt.Fprintf(w, "%s\t%s\n", filepath.Join(filepath.Dir(rep.DBPath), f.Name), humanBytes(f.Bytes))
		}
	}
	fmt.Fprintf(w, "total\t%s\n", humanBytes(rep.Bytes))
	if err := w.Flush(); err != nil {
		return err
	}
	switch {
	case rep.Plan == nil:
		fmt.Fprintln(out, "\nRun `observer archive reclaim-cleanup --delete` to verify the database in place and delete these.")
	case rep.DeleteError != "":
		fmt.Fprintf(out, "\ndeleted %d file(s), %s, then stopped: %s\n", len(rep.Deleted), humanBytes(rep.FreedBytes), rep.DeleteError)
	case rep.Plan.OK():
		fmt.Fprintf(out, "\nthe database in place passed quick_check; deleted %d file(s), freed %s\n", len(rep.Deleted), humanBytes(rep.FreedBytes))
	default:
		fmt.Fprintf(out, "\nNothing deleted: %s\n", rep.Plan.Reason)
	}
	return nil
}
