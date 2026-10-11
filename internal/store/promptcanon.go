package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/marmutapp/superbased-observer/internal/hooklifecycle"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/sessionmsg"
)

// canonicalPromptsBySession loads the user_prompt actions matching where
// (an AND-able predicate over the actions alias a, values bound via args)
// and returns, per session, the prompts that remain after the prompt-shadow
// fold (T-570): a tool whose prompt-submit hook and transcript both record
// one prompt (integration Hook.PromptShadowsTranscript, Codex today) counts
// it once, through the ONE pairing table in internal/sessionmsg, never a
// second copy of it in SQL. Rows come back in timestamp order per session,
// each carrying its actions.id as ActionID. db is a pool, a connection or a
// transaction (the cloud evidence snapshot reads through its own conn).
func canonicalPromptsBySession(ctx context.Context, db promptQuerier, where string, args ...any) (map[string][]sessionmsg.ActionRow, error) {
	//nolint:gosec // G202: where is a closed constant predicate from this package; values bind via args.
	rows, err := db.QueryContext(ctx, `
		SELECT a.id, a.session_id, COALESCE(s.tool, ''), COALESCE(a.tool, ''), COALESCE(a.timestamp, ''),
		       COALESCE(a.message_id, ''), COALESCE(a.source_event_id, ''),
		       COALESCE(a.source_file, ''), COALESCE(a.source_file_hash, '')
		  FROM actions a
		  LEFT JOIN sessions s ON s.id = a.session_id
		 WHERE a.action_type = 'user_prompt' AND `+where+`
		 ORDER BY a.session_id, a.timestamp, a.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("canonical prompts: %w", err)
	}
	defer rows.Close()
	bySession := map[string][]sessionmsg.ActionRow{}
	tools := map[string]string{}
	for rows.Next() {
		var id int64
		var sid, tool, actionTool, ts, msgID, srcEvent, srcFile, srcFileHash string
		if err := rows.Scan(&id, &sid, &tool, &actionTool, &ts, &msgID, &srcEvent, &srcFile, &srcFileHash); err != nil {
			return nil, fmt.Errorf("canonical prompts: scan: %w", err)
		}
		tools[sid] = tool
		bySession[sid] = append(bySession[sid], sessionmsg.ActionRow{
			ActionID:      strconv.FormatInt(id, 10),
			ActionType:    "user_prompt",
			Timestamp:     ts,
			MessageID:     msgID,
			SourceEventID: srcEvent,
			// The same hook-row classification the Messages tab applies.
			HookCaptured: hooklifecycle.IsHookSource(actionTool, srcFile, srcFileHash),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("canonical prompts: rows: %w", err)
	}
	out := make(map[string][]sessionmsg.ActionRow, len(bySession))
	for sid, acts := range bySession {
		ic, _ := integration.For(tools[sid])
		out[sid] = sessionmsg.CanonicalPrompts(acts, ic.Hook.PromptShadowsTranscript)
	}
	return out, nil
}

// promptQuerier is the read surface canonicalPromptsBySession needs:
// *sql.DB, *sql.Conn and *sql.Tx all satisfy it.
type promptQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// canonicalPromptCounts returns, per session in sessionIDs, the number of
// prompts left after the prompt-shadow fold (canonicalPromptsBySession). A
// session with no user_prompt row is absent. The ids are bound in chunks so
// the statement stays under every SQLite variable limit.
func canonicalPromptCounts(ctx context.Context, db promptQuerier, sessionIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(sessionIDs))
	const chunk = 400
	for start := 0; start < len(sessionIDs); start += chunk {
		end := start + chunk
		if end > len(sessionIDs) {
			end = len(sessionIDs)
		}
		ids := sessionIDs[start:end]
		args := make([]any, len(ids))
		marks := make([]byte, 0, 2*len(ids))
		for i, id := range ids {
			args[i] = id
			if i > 0 {
				marks = append(marks, ',')
			}
			marks = append(marks, '?')
		}
		bySession, err := canonicalPromptsBySession(ctx, db, `a.session_id IN (`+string(marks)+`)`, args...)
		if err != nil {
			return nil, err
		}
		for sid, prompts := range bySession {
			out[sid] = len(prompts)
		}
	}
	return out, nil
}
