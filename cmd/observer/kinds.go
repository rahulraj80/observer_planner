package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/turnkind"
)

// newKindsCmd is the parent for turn-kind backfill and management.
func newKindsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kinds",
		Short: "Turn-kind classification (backfill, stats)",
		Long: "Manage the turn_kind column on api_turns (§5.1, api-intelligence.md).\n" +
			"The live router classifies routed sessions; this command backfills\n" +
			"the rest using the deterministic offline classifier (internal/turnkind).",
	}
	cmd.AddCommand(newKindsBackfillCmd())
	cmd.AddCommand(newKindsStatsCmd())
	return cmd
}

func newKindsBackfillCmd() *cobra.Command {
	var (
		configPath string
		sinceStr   string
		batchSize  int
		dryRun     bool
		jsonOut    bool
	)
	cmd := &cobra.Command{
		Use:   "backfill [--since=RFC3339] [--batch=N]",
		Short: "Backfill NULL turn_kind on api_turns using the offline classifier",
		Long: "Scans api_turns rows where turn_kind IS NULL (or empty) and classifies\n" +
			"each using the deterministic offline heuristic (internal/turnkind).\n\n" +
			"Idempotent: only fills NULL/empty rows. Re-running is safe.\n\n" +
			"--since limits to turns with timestamp >= given time (RFC3339).\n" +
			"--batch controls how many rows to process per transaction (default 1000).\n" +
			"--dry-run prints what would be updated without writing.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			var since *time.Time
			if sinceStr != "" {
				t, err := time.Parse(time.RFC3339, sinceStr)
				if err != nil {
					return fmt.Errorf("parse --since: %w", err)
				}
				since = &t
			}

			_, database, cleanup, err := loadConfigAndDB(ctx, configPath)
			if err != nil {
				return err
			}
			defer cleanup()

			var totalUpdated int64
			var totalScanned int64
			var lastID int64

			for {
				// Select a batch of rows with NULL/empty turn_kind
				var q string
				var args []any
				if since != nil {
					q = `
						SELECT id, model, provider, input_tokens, output_tokens,
						       cache_read_tokens, cache_creation_tokens,
						       tool_use_count, message_count, session_id,
						       timestamp
						FROM api_turns
						WHERE (turn_kind IS NULL OR turn_kind = '')
						  AND id > ?
						  AND timestamp >= ?
						ORDER BY id ASC
						LIMIT ?
					`
					args = []any{lastID, since.Format(time.RFC3339Nano), batchSize}
				} else {
					q = `
						SELECT id, model, provider, input_tokens, output_tokens,
						       cache_read_tokens, cache_creation_tokens,
						       tool_use_count, message_count, session_id,
						       timestamp
						FROM api_turns
						WHERE (turn_kind IS NULL OR turn_kind = '')
						  AND id > ?
						ORDER BY id ASC
						LIMIT ?
					`
					args = []any{lastID, batchSize}
				}
				rows, err := database.QueryContext(ctx, q, args...)
				if err != nil {
					return fmt.Errorf("select batch: %w", err)
				}

				type turnRow struct {
					id                  int64
					model               string
					provider            string
					inputTokens         sql.NullInt64
					outputTokens        sql.NullInt64
					cacheReadTokens     sql.NullInt64
					cacheCreationTokens sql.NullInt64
					toolUseCount        sql.NullInt64
					messageCount        sql.NullInt64
					sessionID           string
					timestampStr        string
				}

				var batch []turnRow
				for rows.Next() {
					var r turnRow
					if err := rows.Scan(&r.id, &r.model, &r.provider, &r.inputTokens,
						&r.outputTokens, &r.cacheReadTokens, &r.cacheCreationTokens,
						&r.toolUseCount, &r.messageCount, &r.sessionID, &r.timestampStr); err != nil {
						_ = rows.Close()
						return fmt.Errorf("scan: %w", err)
					}
					batch = append(batch, r)
					lastID = r.id
				}
				if err := rows.Close(); err != nil {
					return fmt.Errorf("close rows: %w", err)
				}

				if len(batch) == 0 {
					break
				}

				totalScanned += int64(len(batch))

				// Classify each row
				type updateRow struct {
					id   int64
					kind string
					rule string
				}
				var toUpdate []updateRow
				for _, r := range batch {
					in := turnkind.ClassifierInput{
						PromptTokens:     r.inputTokens.Int64,
						ResponseTokens:   r.outputTokens.Int64,
						HasToolUse:       r.toolUseCount.Int64 > 0,
						IsStream:         false, // not stored per-turn currently
						SystemPromptHash: "",    // not stored per-turn currently
						MessageCount:     int(r.messageCount.Int64),
						SessionTurnIndex: 0,  // would need session-level scan
						WasRoutedKind:    "", // not available here
					}
					kind, rule := turnkind.Classify(in)
					if kind != turnkind.KindUnknown && kind != "" {
						toUpdate = append(toUpdate, updateRow{id: r.id, kind: string(kind), rule: rule})
					}
				}

				if len(toUpdate) > 0 && !dryRun {
					// Update in a transaction
					tx, err := database.BeginTx(ctx, nil)
					if err != nil {
						return fmt.Errorf("begin tx: %w", err)
					}
					stmt, err := tx.PrepareContext(ctx, `UPDATE api_turns SET turn_kind = ? WHERE id = ?`)
					if err != nil {
						_ = tx.Rollback()
						return fmt.Errorf("prepare: %w", err)
					}
					for _, u := range toUpdate {
						if _, err := stmt.ExecContext(ctx, u.kind, u.id); err != nil {
							_ = stmt.Close()
							_ = tx.Rollback()
							return fmt.Errorf("exec update: %w", err)
						}
					}
					_ = stmt.Close()
					if err := tx.Commit(); err != nil {
						return fmt.Errorf("commit: %w", err)
					}
				}

				totalUpdated += int64(len(toUpdate))

				if jsonOut {
					for _, u := range toUpdate {
						fmt.Printf("%d %s %s\n", u.id, u.kind, u.rule)
					}
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "\rProcessed %d, updated %d, last id %d...", totalScanned, totalUpdated, lastID)
				}

				if len(batch) < batchSize {
					break
				}
			}

			if !jsonOut {
				fmt.Fprintf(cmd.OutOrStdout(), "\nDone: scanned %d, updated %d\n", totalScanned, totalUpdated)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config.toml (defaults to ~/.observer/config.toml)")
	cmd.Flags().StringVar(&sinceStr, "since", "", "Only process turns with timestamp >= this RFC3339 time")
	cmd.Flags().IntVar(&batchSize, "batch", 1000, "Rows per batch")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be updated without writing")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit JSON lines (id kind rule)")
	return cmd
}

func newKindsStatsCmd() *cobra.Command {
	var (
		configPath string
		jsonOut    bool
	)
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show turn_kind distribution on api_turns",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			_, database, cleanup, err := loadConfigAndDB(ctx, configPath)
			if err != nil {
				return err
			}
			defer cleanup()

			rows, err := database.QueryContext(ctx, `
				SELECT COALESCE(NULLIF(turn_kind, ''), 'NULL/empty') AS kind, COUNT(*)
				FROM api_turns
				GROUP BY kind
				ORDER BY COUNT(*) DESC
			`)
			if err != nil {
				return fmt.Errorf("query: %w", err)
			}
			defer rows.Close()

			type stat struct {
				Kind  string `json:"kind"`
				Count int64  `json:"count"`
			}
			var stats []stat
			var total int64
			for rows.Next() {
				var s stat
				if err := rows.Scan(&s.Kind, &s.Count); err != nil {
					return fmt.Errorf("scan: %w", err)
				}
				stats = append(stats, s)
				total += s.Count
			}

			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{
					"total":   total,
					"by_kind": stats,
				})
			}

			fmt.Fprintln(cmd.OutOrStdout(), "turn_kind distribution:")
			for _, s := range stats {
				pct := float64(s.Count) * 100 / float64(total)
				fmt.Fprintf(cmd.OutOrStdout(), "  %-18s %10d  (%5.1f%%)\n", s.Kind, s.Count, pct)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  %-18s %10d  (100%%)\n", "TOTAL", total)
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config.toml (defaults to ~/.observer/config.toml)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit JSON")
	return cmd
}
