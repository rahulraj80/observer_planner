//go:build !sbo_omit_adapter_antigravity && !sbo_omit_adapter_antigravity_cli

package main

// backfill_antigravity.go is adapter.antigravity's backfill pass
// (--antigravity-project-root), built unless the adapter is compiled out
// (lane MP-L1D, T-598); backfill_antigravity_omit.go refuses the flag.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/adapter/antigravity"
	"github.com/marmutapp/superbased-observer/internal/git"
	"github.com/marmutapp/superbased-observer/internal/platform/crossmount"
	"github.com/marmutapp/superbased-observer/internal/scrub"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// antigravityProjectRootBackfillDirs returns the set of antigravity
// conversations directories to scan: every crossmount-resolved home's
// `.gemini/antigravity/conversations`. Mirrors the codex helper so
// the dispatcher and tests can compute / inject the same set.
func antigravityProjectRootBackfillDirs() []string {
	var homes []string
	for _, h := range crossmount.AllHomes() {
		homes = append(homes, h.Path)
	}
	return antigravity.ConversationsDirs(homes)
}

// backfillAntigravityProjectRoot re-attributes antigravity action /
// session rows to the correct project, refreshes session.model +
// session.started_at, AND lifts per-turn token rows + the actual
// model name into the DB via the language_server's structured
// trajectory endpoint (Path B, MVP scope).
//
// Targets data ingested before the statedb deep-parser fix landed
// (every session pinned to project_id=[antigravity] with model=""
// and started_at = recovery-run wall-clock) and before structured
// recovery shipped (zero token_usage rows).
//
// Walks every .pb file under the supplied conversations directories.
// For each file:
//  1. Extract conversation UUID from filename, look up the index
//     entry, resolve project root via antigravity.ResolveIndexEntry,
//     upsert the project, and UPDATE the session + cascaded action
//     rows when project_id differs.
//  2. Best-effort: fetch GetCascadeTrajectory via the bridge,
//     parse the structured trajectory for model + per-turn tokens
//     + real conversation start time, INSERT token_usage rows, and
//     prefer the structured model over the state.vscdb hint when
//     refreshing session.model.
//  3. Refresh session.model and session.started_at - only when
//     the existing value is empty / placeholder, so a real value
//     is never overwritten.
//
// The structured fetch is best-effort: ~85% hit rate on the user's
// host (the bridge can only reach conversations loaded by a running
// language_server). Misses are logged and skipped.
func backfillAntigravityProjectRoot(ctx context.Context, db *sql.DB, conversationsDirs []string, fileLimit int) (AntigravityProjectRootBackfill, error) {
	res := AntigravityProjectRootBackfill{}
	st := store.New(db)

	updateSessionPID, err := db.PrepareContext(ctx,
		`UPDATE sessions SET project_id = ? WHERE id = ? AND project_id != ?`)
	if err != nil {
		return res, fmt.Errorf("backfill antigravity-project-root: prepare session pid: %w", err)
	}
	defer updateSessionPID.Close()

	updateActionsPID, err := db.PrepareContext(ctx,
		`UPDATE actions SET project_id = ? WHERE session_id = ? AND project_id != ?`)
	if err != nil {
		return res, fmt.Errorf("backfill antigravity-project-root: prepare actions pid: %w", err)
	}
	defer updateActionsPID.Close()

	// session.model + session.started_at - refresh only when the
	// existing value is empty / placeholder, so a real value is never
	// overwritten by an older index snapshot.
	refreshSessionMeta, err := db.PrepareContext(ctx,
		`UPDATE sessions
		    SET model      = COALESCE(NULLIF(?, ''), model),
		        started_at = CASE WHEN ?<>'' THEN ? ELSE started_at END
		  WHERE id = ?
		    AND tool = 'antigravity'`)
	if err != nil {
		return res, fmt.Errorf("backfill antigravity-project-root: prepare session meta: %w", err)
	}
	defer refreshSessionMeta.Close()

	// Refresh existing token_usage rows' timestamps. The first Path B
	// release synthesised per-turn ts as StartedAt + i*1s; the parser
	// now spreads them across [StartedAt, EndedAt] so the dashboard's
	// chronological view spans the actual conversation duration.
	updateTokenTimestamp, err := db.PrepareContext(ctx,
		`UPDATE token_usage
		    SET timestamp = ?
		  WHERE source_file = ?
		    AND source_event_id = ?
		    AND timestamp != ?`)
	if err != nil {
		return res, fmt.Errorf("backfill antigravity-project-root: prepare token ts: %w", err)
	}
	defer updateTokenTimestamp.Close()

	// Refresh existing token_usage rows' message_id. The original Path
	// B commit (5af3771) used `antigravity-struct:<uuid>:turn:N`; the
	// shared scheme adopted in the bug-fix session uses
	// `antigravity:<uuid>:turn:N` so tokens and structured ToolEvents
	// join on the dashboard. Token rows already in the DB carry the
	// stale prefix and are orphaned from every joined row until this
	// UPDATE runs. Keyed by (source_file, source_event_id) so re-runs
	// are no-ops once corrected.
	updateTokenMessageID, err := db.PrepareContext(ctx,
		`UPDATE token_usage
		    SET message_id = ?
		  WHERE source_file = ?
		    AND source_event_id = ?
		    AND message_id != ?`)
	if err != nil {
		return res, fmt.Errorf("backfill antigravity-project-root: prepare token msg id: %w", err)
	}
	defer updateTokenMessageID.Close()

	// Refresh existing structured action rows' message_id when
	// ParseStructuredTrajectory's assignment scheme changes between
	// observer versions (e.g. Option 1 flipped run_command from
	// step-fraction to time-nearest-token). InsertActions' ON CONFLICT
	// only refreshes duration_ms, so without this UPDATE existing rows
	// keep stale ids and stay orphaned from the dashboard's per-turn
	// grouping. Keyed on (source_file, source_event_id) so re-runs
	// no-op once corrected.
	updateActionMessageID, err := db.PrepareContext(ctx,
		`UPDATE actions
		    SET message_id = ?
		  WHERE source_file = ?
		    AND source_event_id = ?
		    AND message_id != ?`)
	if err != nil {
		return res, fmt.Errorf("backfill antigravity-project-root: prepare action msg id: %w", err)
	}
	defer updateActionMessageID.Close()

	// Realign markdown action timestamps onto the real conversation
	// timeline. Pre-Path-B markdown recovery used the recovery
	// wall-clock as the base ts (so all markdown rows clustered at
	// the time observer last ran the recovery, sometimes 2+ months
	// after the actual conversation). The realignment spreads them
	// evenly across [StartedAt, EndedAt] in their original order.
	selectMarkdownActions, err := db.PrepareContext(ctx,
		`SELECT id, timestamp FROM actions
		  WHERE session_id = ?
		    AND tool = 'antigravity'
		    AND raw_tool_name LIKE 'markdown.%'
		  ORDER BY timestamp ASC, id ASC`)
	if err != nil {
		return res, fmt.Errorf("backfill antigravity-project-root: prepare select markdown actions: %w", err)
	}
	defer selectMarkdownActions.Close()
	updateActionTimestamp, err := db.PrepareContext(ctx,
		`UPDATE actions SET timestamp = ? WHERE id = ?`)
	if err != nil {
		return res, fmt.Errorf("backfill antigravity-project-root: prepare action ts: %w", err)
	}
	defer updateActionTimestamp.Close()

	// Tier 1 dedup: when structured trajectory surfaces authoritative
	// ToolEvents (artifact edits, file views), delete the markdown
	// `*Edited file…*` / `*Viewed…*` inline-tool rows that recovered
	// the same edits with less detail. Run only for sessions where
	// structured emitted tools; markdown user/assistant text rows are
	// untouched (assistant text only lives in markdown - see Tier 2
	// probe notes in docs/handovers/antigravity-tier-1-2-3-handoff-2026-05-04.md).
	deleteMarkdownInlineTools, err := db.PrepareContext(ctx,
		`DELETE FROM actions
		  WHERE session_id = ?
		    AND tool = 'antigravity'
		    AND raw_tool_name LIKE 'markdown.inline.%'`)
	if err != nil {
		return res, fmt.Errorf("backfill antigravity-project-root: prepare delete inline tools: %w", err)
	}
	defer deleteMarkdownInlineTools.Close()

	// Tier 2 dedup: when structured surfaces user_prompt rows from
	// 1.2.19.2, delete markdown.user_input rows for the same session.
	// Markdown's user_input was synthesized from `### User Input`
	// blocks in the converted trajectory; structured carries the same
	// text with deterministic SourceEventIDs and real per-step ts.
	deleteMarkdownUserInputs, err := db.PrepareContext(ctx,
		`DELETE FROM actions
		  WHERE session_id = ?
		    AND tool = 'antigravity'
		    AND raw_tool_name = 'markdown.user_input'`)
	if err != nil {
		return res, fmt.Errorf("backfill antigravity-project-root: prepare delete user inputs: %w", err)
	}
	defer deleteMarkdownUserInputs.Close()

	// Tier 3 dedup: when structured surfaces assistant_text rows
	// from 1.2.20.1, delete markdown.planner_response rows for the
	// same session.
	deleteMarkdownPlanner, err := db.PrepareContext(ctx,
		`DELETE FROM actions
		  WHERE session_id = ?
		    AND tool = 'antigravity'
		    AND raw_tool_name = 'markdown.planner_response'`)
	if err != nil {
		return res, fmt.Errorf("backfill antigravity-project-root: prepare delete planner: %w", err)
	}
	defer deleteMarkdownPlanner.Close()

	scrubber := scrub.New()

	for _, dir := range conversationsDirs {
		walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".pb" {
				return nil
			}
			if fileLimit > 0 && res.FilesScanned >= fileLimit {
				return filepath.SkipAll
			}
			res.FilesScanned++

			entry, ok := antigravity.ResolveIndexEntry(path)
			if !ok {
				return nil
			}
			res.IndexHits++

			sessionID := strings.TrimSuffix(filepath.Base(path), ".pb")

			if entry.ProjectRoot != "" && entry.ProjectRoot != "[antigravity]" {
				info, gerr := git.Resolve(entry.ProjectRoot)
				newRoot := entry.ProjectRoot
				remote := ""
				if gerr == nil && info.Root != "" {
					newRoot = info.Root
					remote = git.NormalizeRemote(info.Remote)
				}
				pid, err := st.UpsertProject(ctx, newRoot, remote)
				if err == nil {
					if r, err := updateSessionPID.ExecContext(ctx, pid, sessionID, pid); err == nil {
						if n, _ := r.RowsAffected(); n > 0 {
							res.SessionsReattributed += int(n)
						}
					}
					if r, err := updateActionsPID.ExecContext(ctx, pid, sessionID, pid); err == nil {
						if n, _ := r.RowsAffected(); n > 0 {
							res.ActionsUpdated += int(n)
						}
					}
				}
			}

			// Best-effort structured-trajectory enrichment.
			// Re-fetching via the bridge is ~250ms per conversation;
			// for ~100 sessions the total is ~30s. Failure (no
			// language_server has the conversation loaded) is the
			// common case for the ~15% miss rate - log and continue.
			modelHint := entry.ModelHint
			startedAt := ""
			if !entry.Created.IsZero() {
				startedAt = entry.Created.UTC().Format(time.RFC3339Nano)
			}
			structured, sErr := antigravity.FetchStructuredTrajectory(ctx, sessionID, entry.ProjectRoot, path, 30*time.Second, scrubber)
			if sErr != nil {
				res.StructuredFetchFailed++
			} else {
				res.StructuredFetched++
				if structured.Model != "" {
					modelHint = structured.Model
				}
				if !structured.StartedAt.IsZero() {
					startedAt = structured.StartedAt.UTC().Format(time.RFC3339Nano)
				}
				// Insert tokens + new structured tool events in one
				// idempotent Ingest call. Existing rows are
				// no-op'd via the (source_file, source_event_id)
				// UNIQUE constraint.
				if len(structured.TokenEvents) > 0 || len(structured.ToolEvents) > 0 {
					ir, ierr := st.Ingest(ctx, structured.ToolEvents, structured.TokenEvents, store.IngestOptions{})
					if ierr == nil {
						res.StructuredTokensInserted += ir.TokensInserted
						res.StructuredToolsInserted += ir.ActionsInserted
					}
				}
				// Tier 1 dedup: now that authoritative structured tool
				// events are in the DB, drop the legacy markdown.inline.*
				// rows for this session. Idempotent - re-runs find no
				// rows to delete.
				if len(structured.ToolEvents) > 0 {
					if r, derr := deleteMarkdownInlineTools.ExecContext(ctx, sessionID); derr == nil {
						if n, _ := r.RowsAffected(); n > 0 {
							res.MarkdownInlineToolsDedup += int(n)
						}
					}
				}
				// Tier 2 dedup: when structured surfaces user_prompt
				// rows from 1.2.19.2, drop the markdown.user_input
				// rows for this session.
				var sawUserPrompt bool
				assistantTextCount := 0
				for _, ev := range structured.ToolEvents {
					switch ev.RawToolName {
					case "structured.user_prompt":
						sawUserPrompt = true
					case "structured.assistant_text":
						assistantTextCount++
					}
				}
				if sawUserPrompt {
					if r, derr := deleteMarkdownUserInputs.ExecContext(ctx, sessionID); derr == nil {
						if n, _ := r.RowsAffected(); n > 0 {
							res.MarkdownInlineToolsDedup += int(n)
						}
					}
				}
				// Tier 3 dedup is coverage-conditional. Only suppress
				// markdown.planner_response when structured carries
				// the assistant text for at least the threshold
				// fraction of LLM calls. Below threshold (gemini
				// sessions; 803add5e is 3/130 = 2%), markdown carries
				// the missing narrative and must stay.
				coverage := 0.0
				if len(structured.TokenEvents) > 0 {
					coverage = float64(assistantTextCount) / float64(len(structured.TokenEvents))
				}
				if coverage >= antigravity.AssistantTextCoverageThreshold {
					if r, derr := deleteMarkdownPlanner.ExecContext(ctx, sessionID); derr == nil {
						if n, _ := r.RowsAffected(); n > 0 {
							res.MarkdownInlineToolsDedup += int(n)
						}
					}
				} else if coverage < antigravity.AssistantTextCoverageThreshold {
					// Recover markdown.planner_response rows that may
					// have been deleted by a prior over-aggressive
					// dedup pass. Re-fetch markdown via the bridge,
					// extract planner-response-only events, and
					// idempotently INSERT (UNIQUE on
					// (source_file, source_event_id) collapses re-runs).
					md, mdErr := antigravity.FetchMarkdownTrajectory(ctx, sessionID, 30*time.Second)
					if mdErr == nil && md != "" {
						baseTs := structured.StartedAt
						if baseTs.IsZero() && !entry.Created.IsZero() {
							baseTs = entry.Created
						}
						if baseTs.IsZero() {
							baseTs = time.Now().UTC()
						}
						plannerEvents := antigravity.ParseMarkdownPlannerResponses(
							path, sessionID, entry.ProjectRoot, baseTs, scrubber, md,
						)
						if len(plannerEvents) > 0 {
							ir, ierr := st.Ingest(ctx, plannerEvents, nil, store.IngestOptions{})
							if ierr == nil {
								res.MarkdownPlannerRecovered += ir.ActionsInserted
							}
						}
					}
				}
				// Refresh existing token_usage timestamps so old rows
				// (synthesised at 1s-apart from StartedAt by the
				// first Path B release) get the new spread values.
				// Also refresh stale message_id prefixes so the
				// dashboard's per-message join lines up structured
				// ToolEvents under their owning token row.
				for _, te := range structured.TokenEvents {
					newTs := te.Timestamp.UTC().Format(time.RFC3339Nano)
					if r, uerr := updateTokenTimestamp.ExecContext(ctx,
						newTs, te.SourceFile, te.SourceEventID, newTs); uerr == nil {
						if n, _ := r.RowsAffected(); n > 0 {
							res.TokenTimestampsRefreshed += int(n)
						}
					}
					if r, uerr := updateTokenMessageID.ExecContext(ctx,
						te.MessageID, te.SourceFile, te.SourceEventID, te.MessageID); uerr == nil {
						if n, _ := r.RowsAffected(); n > 0 {
							res.TokenMessageIDsRefreshed += int(n)
						}
					}
				}
				// Refresh existing structured action rows whose
				// MessageID assignment changed (e.g. Option 1 moved
				// run_command from step-fraction to nearest-token).
				for _, ev := range structured.ToolEvents {
					if r, uerr := updateActionMessageID.ExecContext(ctx,
						ev.MessageID, ev.SourceFile, ev.SourceEventID, ev.MessageID); uerr == nil {
						if n, _ := r.RowsAffected(); n > 0 {
							res.ActionMessageIDsRefreshed += int(n)
						}
					}
				}
				// Realign markdown actions if we know the real
				// conversation window AND the existing rows look
				// like recovery wall-clock (i.e. they're more than
				// a minute off the structured StartedAt).
				if !structured.StartedAt.IsZero() {
					n, _ := realignMarkdownActions(ctx,
						selectMarkdownActions, updateActionTimestamp,
						sessionID, structured.StartedAt, structured.EndedAt)
					res.MarkdownActionsRealigned += n
				}
			}

			if modelHint != "" || startedAt != "" {
				if r, err := refreshSessionMeta.ExecContext(ctx, modelHint, startedAt, startedAt, sessionID); err == nil {
					if n, _ := r.RowsAffected(); n > 0 {
						res.SessionsRefreshed += int(n)
						if structured.Model != "" {
							res.StructuredModelLifted += int(n)
						}
					}
				}
			}
			return nil
		})
		if walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
			continue
		}
	}
	return res, nil
}

// realignMarkdownActions spreads existing markdown-derived action
// rows for a session evenly across [startedAt, endedAt]. Pre-Path-B
// markdown recovery used the recovery wall-clock as the base ts, so
// all markdown rows clustered at the time observer last ran the
// recovery - sometimes months after the actual conversation. This
// realignment puts them back on the real timeline.
//
// No-op when:
//   - no markdown actions exist for the session,
//   - the existing min(timestamp) is already within 60s of startedAt
//     (already realigned on a prior run, idempotent),
//   - endedAt is zero or doesn't exceed startedAt (no spread window).
//
// Returns the count of rows whose timestamp changed.
func realignMarkdownActions(ctx context.Context,
	selectStmt, updateStmt *sql.Stmt,
	sessionID string, startedAt, endedAt time.Time,
) (int, error) {
	rows, err := selectStmt.QueryContext(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	type row struct {
		id int64
		ts time.Time
	}
	var batch []row
	for rows.Next() {
		var r row
		var tsStr string
		if err := rows.Scan(&r.id, &tsStr); err != nil {
			rows.Close()
			return 0, err
		}
		if t, perr := time.Parse(time.RFC3339Nano, tsStr); perr == nil {
			r.ts = t
		} else if t, perr := time.Parse("2006-01-02T15:04:05Z", tsStr); perr == nil {
			r.ts = t
		}
		batch = append(batch, r)
	}
	rows.Close()
	if len(batch) == 0 {
		return 0, nil
	}
	// Idempotency guard: if the earliest stored ts is already within
	// 60s of startedAt, the realignment already ran. Skip.
	if !batch[0].ts.IsZero() && batch[0].ts.Sub(startedAt).Abs() < time.Minute {
		return 0, nil
	}
	// Determine the spread window. If endedAt is missing, fall back
	// to a synthetic 1s-apart window starting at startedAt - still
	// a huge improvement over the recovery wall-clock since at
	// least the absolute time anchor is correct.
	dur := time.Duration(0)
	if !endedAt.IsZero() && endedAt.After(startedAt) {
		dur = endedAt.Sub(startedAt)
	}
	updated := 0
	for i, r := range batch {
		var newTs time.Time
		if dur > 0 && len(batch) > 1 {
			off := time.Duration(int64(dur) * int64(i) / int64(len(batch)-1))
			newTs = startedAt.Add(off)
		} else {
			newTs = startedAt.Add(time.Duration(i) * time.Second)
		}
		if !newTs.Equal(r.ts) {
			if _, err := updateStmt.ExecContext(ctx, newTs.UTC().Format(time.RFC3339Nano), r.id); err == nil {
				updated++
			}
		}
	}
	return updated, nil
}
