//go:build !sbo_omit_adapter_codex && !sbo_omit_adapter_open_interpreter

package main

// backfill_codex.go holds the backfill passes that parse with
// internal/adapter/codex (--codex-fork-dedup and the codex half of
// --codex-tool-input), built unless adapter.codex is compiled out (lane
// MP-L1D, T-598); backfill_codex_omit.go refuses them.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/marmutapp/superbased-observer/internal/adapter/codex"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/platform/crossmount"
	"github.com/marmutapp/superbased-observer/internal/scrub"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// backfillCodexForkDedup purges historical duplicate codex token_usage
// rows that a fork / subagent spawn created by replaying its parent
// rollout's token_count telemetry into the child's rollout file. The
// ingest fix (codex.ReplayedTokenLines) now suppresses these at parse
// time; this pass cleans up rows inserted before the fix shipped.
//
// It enumerates the DISTINCT source_file of codex token_usage rows,
// re-runs codex.ReplayedTokenLines over each file still on disk, and
// matches the replayed lines' synthetic source_event_ids
// ("tk:<basename>:L<line>") against the stored rows. DRY-RUN BY DEFAULT
// (apply=false): it only tallies matched rows / summed tokens / touched
// sessions and deletes nothing. With apply=true it DELETEs the matched
// rows in bounded transactions. Deletion is keyed by (source_file,
// source_event_id) so a second --apply is a natural no-op.
//
// As a cheap bonus it captures each file's session lineage
// (forked_from_id / parent_thread_id / thread_source) via
// codex.ScanForkLineage and persists it with Store.SetSessionLineage so
// pre-fix sessions gain the lineage the ingest path now records. Lineage
// is only written on apply; the dry run reports what it would set.
//
// Foreign-OS paths (a Windows-captured rollout read from a WSL2
// observer) are handled the way the sibling project-root backfills do:
// when the stored source_file no longer stats, a
// crossmount.TranslateForeignPath fallback is attempted before treating
// the file as missing. out receives per-file notes when non-nil (the
// caller passes nil under --json).
func backfillCodexForkDedup(ctx context.Context, database *sql.DB, apply bool, fileLimit int, out io.Writer) (CodexForkDedupBackfill, error) {
	res := CodexForkDedupBackfill{Applied: apply}
	st := store.New(database)

	rows, err := database.QueryContext(ctx, `
		SELECT DISTINCT source_file FROM token_usage
		 WHERE tool = 'codex'
		   AND source_file IS NOT NULL
		   AND source_file != ''
		 ORDER BY source_file`)
	if err != nil {
		return res, fmt.Errorf("backfill codex-fork-dedup: list source_files: %w", err)
	}
	var files []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			rows.Close()
			return res, fmt.Errorf("backfill codex-fork-dedup: scan source_file: %w", err)
		}
		files = append(files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, fmt.Errorf("backfill codex-fork-dedup: iterate source_files: %w", err)
	}

	sessionsSeen := map[string]bool{}

	for _, sourceFile := range files {
		if fileLimit > 0 && res.FilesScanned >= fileLimit {
			break
		}

		// Resolve the on-disk path: prefer the stored path; fall back
		// to a crossmount translation for foreign-OS (Windows) paths
		// captured before the observer moved to WSL, mirroring the
		// sibling project-root backfills.
		diskPath := sourceFile
		if _, statErr := os.Stat(diskPath); statErr != nil {
			if translated := crossmount.TranslateForeignPath(sourceFile); translated != sourceFile {
				if _, statErr2 := os.Stat(translated); statErr2 == nil {
					diskPath = translated
				}
			}
		}

		f, openErr := os.Open(diskPath)
		if openErr != nil {
			res.FilesMissing++
			if out != nil {
				fmt.Fprintf(out, "  skip %s (rollout no longer on disk)\n", sourceFile)
			}
			continue
		}
		res.FilesScanned++

		replayed, scanErr := codex.ReplayedTokenLines(f)
		f.Close()
		if scanErr != nil {
			if out != nil {
				fmt.Fprintf(out, "  warn %s: scan failed: %v\n", sourceFile, scanErr)
			}
			continue
		}

		// Bonus: retrofit the owning session's fork lineage. Cheap
		// second read of a small header; independent of the delete set.
		if lf, lopenErr := os.Open(diskPath); lopenErr == nil {
			lin, ok, lerr := codex.ScanForkLineage(lf)
			lf.Close()
			hasLineage := ok && lin.SessionID != "" &&
				(lin.ForkedFromID != "" || lin.ParentThreadID != "" || lin.ThreadSource != "")
			if lerr == nil && hasLineage {
				if apply {
					// Count only when the UPDATE actually changed a row -
					// a re-run over an already-stamped session returns
					// changed=false, so the counter stays honest.
					if changed, serr := st.SetSessionLineage(ctx, models.SessionLineage{
						SessionID:      lin.SessionID,
						ForkedFromID:   lin.ForkedFromID,
						ParentThreadID: lin.ParentThreadID,
						ThreadSource:   lin.ThreadSource,
					}); serr == nil && changed {
						res.LineageBackfilled++
					}
				} else {
					// Dry-run: count only what an --apply UPDATE would
					// actually change - the session must EXIST and at
					// least one supplied marker must differ from what's
					// stored (mirrors SetSessionLineage's WHERE guard).
					var curFork, curParent, curSource string
					qerr := database.QueryRowContext(ctx,
						`SELECT IFNULL(forked_from_id, ''), IFNULL(parent_thread_id, ''), IFNULL(thread_source, '')
						   FROM sessions WHERE id = ?`, lin.SessionID).
						Scan(&curFork, &curParent, &curSource)
					switch {
					case errors.Is(qerr, sql.ErrNoRows):
						// Session never ingested - nothing to backfill.
					case qerr != nil:
						return res, fmt.Errorf("backfill codex-fork-dedup: check lineage for %s: %w", lin.SessionID, qerr)
					case (lin.ForkedFromID != "" && lin.ForkedFromID != curFork) ||
						(lin.ParentThreadID != "" && lin.ParentThreadID != curParent) ||
						(lin.ThreadSource != "" && lin.ThreadSource != curSource):
						res.LineageBackfilled++
					}
				}
			}
		}

		if len(replayed) == 0 {
			continue
		}

		// Build the replayed source_event_id set the adapter would have
		// written for these lines. Use a separator-aware basename: the
		// adapter builds the id with filepath.Base at PARSE time on the
		// capturing host, so a Windows-captured rollout carries a
		// backslash-separated path in source_file. filepath.Base on a
		// Linux observer would return the whole "C:\\...\\rollout.jsonl"
		// string and never match "tk:rollout.jsonl:L<n>". lastPathSegment
		// strips after the last '/' OR '\\' regardless of host OS.
		base := lastPathSegment(sourceFile)
		wantIDs := make(map[string]bool, len(replayed))
		for ln := range replayed {
			wantIDs[fmt.Sprintf("tk:%s:L%d", base, ln)] = true
		}

		urows, qerr := database.QueryContext(ctx, `
			SELECT session_id, source_event_id,
			       COALESCE(input_tokens, 0), COALESCE(output_tokens, 0), COALESCE(cache_read_tokens, 0),
			       COALESCE(reasoning_tokens, 0), COALESCE(web_search_requests, 0), COALESCE(estimated_cost_usd, 0)
			  FROM token_usage
			 WHERE tool = 'codex' AND source_file = ?`, sourceFile)
		if qerr != nil {
			return res, fmt.Errorf("backfill codex-fork-dedup: select rows for %s: %w", sourceFile, qerr)
		}
		var matchedIDs []string
		var fileMatched int
		var fileInput, fileOutput, fileCacheRead, fileReasoning, fileWebSearch int64
		var fileCost float64
		fileSessions := map[string]bool{}
		for urows.Next() {
			var sid, eid string
			var in, outp, cr, reason, web int64
			var cost float64
			if err := urows.Scan(&sid, &eid, &in, &outp, &cr, &reason, &web, &cost); err != nil {
				urows.Close()
				return res, fmt.Errorf("backfill codex-fork-dedup: scan row: %w", err)
			}
			if !wantIDs[eid] {
				continue
			}
			matchedIDs = append(matchedIDs, eid)
			fileMatched++
			fileInput += in
			fileOutput += outp
			fileCacheRead += cr
			fileReasoning += reason
			fileWebSearch += web
			fileCost += cost
			fileSessions[sid] = true
			sessionsSeen[sid] = true
		}
		urows.Close()
		if err := urows.Err(); err != nil {
			return res, fmt.Errorf("backfill codex-fork-dedup: iterate rows: %w", err)
		}

		res.RowsMatched += fileMatched
		res.InputTokens += fileInput
		res.OutputTokens += fileOutput
		res.CacheReadTokens += fileCacheRead
		res.ReasoningTokens += fileReasoning
		res.WebSearchRequests += fileWebSearch
		res.EstimatedCostUSD += fileCost

		if fileMatched > 0 && out != nil {
			fmt.Fprintf(
				out,
				"  %s: %d duplicate token row(s) (input=%d output=%d cache_read=%d reasoning=%d web_search=%d est_cost=$%.4f) across %d session(s)\n",
				sourceFile, fileMatched, fileInput, fileOutput, fileCacheRead, fileReasoning, fileWebSearch, fileCost, len(fileSessions),
			)
		}

		if apply && len(matchedIDs) > 0 {
			deleted, derr := deleteCodexForkDedupRows(ctx, database, sourceFile, matchedIDs)
			if derr != nil {
				return res, derr
			}
			res.RowsDeleted += deleted
		}
	}

	res.SessionsTouched = len(sessionsSeen)
	return res, nil
}

// deriveCodexToolInputs re-reads one rollout file and returns the tool
// inputs it can ground, keyed by the SAME source_event_id the adapter
// writes onto the row.
//
// The adapter is the primary source: ParseSessionFile owns every
// decision about what a row's input is, and whatever it produces wins.
// scanPatchApplyChanges supplements it with the single field the
// standalone patch path provably discards, and can only ADD keys the
// adapter left absent - it can never override one.
func deriveCodexToolInputs(ctx context.Context, sc *scrub.Scrubber, tool, path string) (map[string]string, error) {
	ad := codex.NewWithOptions(sc, "")
	if tool == models.ToolOpenInterpreter {
		ad = codex.NewOpenInterpreterWithOptions(sc, "")
	}
	parsed, err := ad.ParseSessionFile(ctx, path, 0)
	if err != nil {
		return nil, fmt.Errorf("backfill codex-tool-input: parse %s: %w", path, err)
	}
	out := make(map[string]string, len(parsed.ToolEvents))
	for _, ev := range parsed.ToolEvents {
		if ev.SourceEventID != "" && ev.RawToolInput != "" {
			out[ev.SourceEventID] = ev.RawToolInput
		}
	}
	changes, err := scanPatchApplyChanges(ctx, sc, path)
	if err != nil {
		return nil, err
	}
	for id, v := range changes {
		if _, seen := out[id]; !seen {
			out[id] = v
		}
	}
	return out, nil
}

// scrubbedChanges renders a patch_apply_end `changes` object for storage,
// or "" when the record carries nothing. The rendering itself is owned by
// the adapter (codex.RenderPatchChangesInput) because the live parse path
// writes the same column from the same bytes - a recovered row and a
// live-captured row must be byte-identical.
func scrubbedChanges(sc *scrub.Scrubber, raw json.RawMessage) string {
	return codex.RenderPatchChangesInput(sc, raw)
}
