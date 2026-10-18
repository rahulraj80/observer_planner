package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// DeriveResult carries what one Derive pass produced.
type DeriveResult struct {
	// Staged is how many events were admitted and staged.
	Staged int
	// Skipped is how many candidate rows were filtered by the tier gate
	// or vocabulary bucketing (tests/diagnostics only).
	Skipped int
}

// lookbackWindow bounds each derive pass: long enough to catch sessions
// closed while the daemon was down, short enough that re-derivation after
// a crash stays cheap. Dedup discipline: every derive query filters on a
// NOT-IN marker of already-staged seqs via the staged UNIQUE key — the
// watermark below is the simple, robust form.
const lookbackWindow = 2 * time.Hour

// Derive performs one pull-model pass: it derives contract events from the
// node's local capture tables (sessions/actions/token_usage/failure_context)
// within the lookback window, gates each through the CURRENT consent tier
// (contract.TierAllows), and stages the survivors. Dedup: a candidate whose
// (event, dedup_key) pair was already staged in the window is skipped, so
// repeated passes are idempotent and a crash never double-counts.
//
// All timestamps leave as client-UTC RFC 3339 (contract tsRe); the server
// never trusts arrival order (plan §D3).
func (c *Collector) Derive(ctx context.Context) (DeriveResult, error) {
	if !contract.ValidTier(c.cfg.Tier) || c.cfg.Tier == contract.TierOff {
		return DeriveResult{}, nil
	}
	// Identity FIRST: staging requires an install id so the seq space is
	// install-scoped from the first row (mints lazily on first use).
	if _, err := c.InstallID(ctx); err != nil {
		return DeriveResult{}, err
	}
	since := c.now().UTC().Add(-lookbackWindow).Format(time.RFC3339)
	nowTS := c.now().UTC().Format(time.RFC3339)

	var res DeriveResult
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("telemetry.Derive: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Already-staged dedup keys within the window: derived rows carry a
	// deterministic key (event + ':' + local identity) persisted in the
	// telemetry_staged.dedup_key column (migration 116); loading it here
	// keeps repeated passes idempotent WITHOUT relying on the wire payload,
	// which never contains the key (stageEvent strips it).
	staged := map[string]bool{}
	rows, err := tx.QueryContext(ctx,
		`SELECT event, dedup_key FROM telemetry_staged WHERE created_at >= ?`, since)
	if err != nil {
		return res, fmt.Errorf("telemetry.Derive: staged list: %w", err)
	}
	for rows.Next() {
		var event, dedupKey string
		if err := rows.Scan(&event, &dedupKey); err != nil {
			_ = rows.Close()
			return res, fmt.Errorf("telemetry.Derive: staged scan: %w", err)
		}
		if dedupKey != "" {
			staged[event+":"+dedupKey] = true
		}
	}
	_ = rows.Close()

	stage := func(event string, fields map[string]any) error {
		key := ""
		if k, ok := fields["k"].(string); ok {
			key = k
		}
		if key != "" && staged[event+":"+key] {
			res.Skipped++
			return nil
		}
		if !contract.TierAllows(c.cfg.Tier, event) {
			res.Skipped++
			return nil
		}
		if key != "" {
			staged[event+":"+key] = true
		}
		if err := c.stageEvent(ctx, tx, event, nowTS, fields); err != nil {
			return err
		}
		res.Staged++
		return nil
	}

	inApp, err := inAppSessions(ctx, tx)
	if err != nil {
		return res, err
	}
	c.inApp = inApp
	if err := c.deriveSessions(ctx, tx, since, stage); err != nil {
		return res, err
	}
	if err := c.deriveToolUse(ctx, tx, since, stage); err != nil {
		return res, err
	}
	if err := c.deriveTokenUse(ctx, tx, since, stage); err != nil {
		return res, err
	}
	if err := c.deriveErrors(ctx, tx, since, stage); err != nil {
		return res, err
	}
	if err := c.deriveDayActive(ctx, tx, stage); err != nil {
		return res, err
	}
	if err := c.derivePromptCycles(ctx, tx, since, stage); err != nil {
		return res, err
	}
	// Honest gaps: if the staging bounds evicted anything, ship the count
	// (plan §D2) — batch_meta re-ships on every pass while drops persist.
	if err := c.stageBatchMeta(ctx, tx, stage); err != nil {
		return res, err
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("telemetry.Derive: commit: %w", err)
	}
	return res, nil
}

// deriveSessions emits session_start/session_end pairs from the sessions
// table. Sessions with ended_at set get start+end (duration, outcome); open
// sessions get a start only. The local `tool` name maps onto the contract's
// closed adapter vocabulary (unknown → other, plan §D5).
func (c *Collector) deriveSessions(ctx context.Context, tx sqlTx, since string, stage func(string, map[string]any) error) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT s.id, COALESCE(s.tool, ''), COALESCE(s.started_at, ''),
		       COALESCE(s.ended_at, ''), COALESCE(s.error_rate, 0)
		FROM sessions s
		WHERE COALESCE(s.started_at, '') >= ? OR COALESCE(s.ended_at, '') >= ?
		ORDER BY s.started_at`, since, since)
	if err != nil {
		return fmt.Errorf("telemetry.deriveSessions: %w", err)
	}
	defer rows.Close()
	type sess struct {
		id, tool, started, ended string
		errRate                  float64
	}
	var list []sess
	for rows.Next() {
		var s sess
		if err := rows.Scan(&s.id, &s.tool, &s.started, &s.ended, &s.errRate); err != nil {
			return fmt.Errorf("telemetry.deriveSessions: scan: %w", err)
		}
		list = append(list, s)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("telemetry.deriveSessions: rows: %w", err)
	}
	for _, s := range list {
		adapter := adapterName(s.tool)
		if err := stage(contract.EventSessionStart, map[string]any{
			"k": s.id, "adapter": adapter, "launch": launchTag(c.inApp, s.id),
		}); err != nil {
			return err
		}
		if s.ended == "" {
			continue
		}
		outcome := "ok"
		if s.errRate > 0.5 {
			outcome = "error"
		}
		dur, err := durationRFC(s.started, s.ended)
		if err != nil {
			// Unparseable local timestamps are skipped honestly, not
			// guessed around.
			continue
		}
		if err := stage(contract.EventSessionEnd, map[string]any{
			"k": s.id, "adapter": adapter, "duration_ms": dur, "outcome": outcome,
			"launch": launchTag(c.inApp, s.id),
		}); err != nil {
			return err
		}
	}
	return nil
}

// deriveToolUse emits per-(session, tool) tool_use counters. The local
// `actions.tool`/`raw_tool_name` vocabulary maps onto the contract's closed
// tool_name list (unknown → other); count is the aggregate, never a
// per-call event.
func (c *Collector) deriveToolUse(ctx context.Context, tx sqlTx, since string, stage func(string, map[string]any) error) error {
	// actions.tool is the AI tool (claude-code, codex, ...); the called tool is
	// the action type. Count tool-call action types only, mapped onto the
	// contract's closed tool_name list (actionToolName).
	rows, err := tx.QueryContext(ctx, `
		SELECT COALESCE(session_id, ''), COALESCE(action_type, ''), COUNT(*)
		FROM actions
		WHERE timestamp >= ?
		GROUP BY session_id, action_type`, since)
	if err != nil {
		return fmt.Errorf("telemetry.deriveToolUse: %w", err)
	}
	defer rows.Close()
	type key struct{ session, tool string }
	counts := map[key]int{}
	var order []key
	for rows.Next() {
		var session, actionType string
		var n int
		if err := rows.Scan(&session, &actionType, &n); err != nil {
			return fmt.Errorf("telemetry.deriveToolUse: scan: %w", err)
		}
		name, isTool := actionToolName[actionType]
		if !isTool {
			continue
		}
		k := key{session, name}
		if _, seen := counts[k]; !seen {
			order = append(order, k)
		}
		counts[k] += n
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("telemetry.deriveToolUse: rows: %w", err)
	}
	for _, k := range order {
		if err := stage(contract.EventToolUse, map[string]any{
			"k": k.session + "/" + k.tool, "tool_name": k.tool, "count": counts[k],
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Collector) deriveTokenUse(ctx context.Context, tx sqlTx, since string, stage func(string, map[string]any) error) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT session_id, model,
		       SUM(COALESCE(input_tokens, 0)), SUM(COALESCE(output_tokens, 0)),
		       SUM(COALESCE(cache_read_tokens, 0))
		FROM token_usage
		WHERE timestamp >= ?
		GROUP BY session_id, model`, since)
	if err != nil {
		return fmt.Errorf("telemetry.deriveTokenUse: %w", err)
	}
	defer rows.Close()
	type agg struct {
		session, role, tier string
		in, out, cache      int64
	}
	var list []agg
	index := map[[3]string]int{}
	for rows.Next() {
		var session, model string
		var in, out, cache int64
		if err := rows.Scan(&session, &model, &in, &out, &cache); err != nil {
			return fmt.Errorf("telemetry.deriveTokenUse: scan: %w", err)
		}
		key := [3]string{session, "assistant", modelTier(model)}
		if i, ok := index[key]; ok {
			list[i].in += in
			list[i].out += out
			list[i].cache += cache
			continue
		}
		index[key] = len(list)
		list = append(list, agg{session: session, role: key[1], tier: key[2], in: in, out: out, cache: cache})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("telemetry.deriveTokenUse: rows: %w", err)
	}
	for _, v := range list {
		if err := stage(contract.EventTokenUse, map[string]any{
			"k":          v.session + "/" + v.role + "/" + v.tier,
			"model_tier": v.tier, "role": v.role, "launch": launchTag(c.inApp, v.session),
			"tokens_in": v.in, "tokens_out": v.out, "cache_read": v.cache,
		}); err != nil {
			return err
		}
	}
	return nil
}

// deriveErrors emits error-class counters from failure_context. Only the
// closed error_class vocabulary leaves — command summaries, hashes and
// messages never do (ruling A2: error CLASSES only).
func (c *Collector) deriveErrors(ctx context.Context, tx sqlTx, since string, stage func(string, map[string]any) error) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT session_id, COALESCE(error_category, ''), COUNT(*)
		FROM failure_context
		WHERE timestamp >= ?
		GROUP BY session_id, error_category`, since)
	if err != nil {
		return fmt.Errorf("telemetry.deriveErrors: %w", err)
	}
	defer rows.Close()
	type ecnt struct {
		session, category string
		n                 int
	}
	var list []ecnt
	for rows.Next() {
		var v ecnt
		if err := rows.Scan(&v.session, &v.category, &v.n); err != nil {
			return fmt.Errorf("telemetry.deriveErrors: scan: %w", err)
		}
		list = append(list, v)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("telemetry.deriveErrors: rows: %w", err)
	}
	for _, v := range list {
		class, surface := errorClass(v.category)
		if err := stage(contract.EventError, map[string]any{
			"k": v.session + "/" + v.category, "error_class": class, "surface": surface,
		}); err != nil {
			return err
		}
	}
	return nil
}

// deriveDayActive emits one day_active heartbeat per local day with any
// activity — the DAU signal (plan §D2). Idempotent via the day-bucketed
// dedup key.
func (c *Collector) deriveDayActive(ctx context.Context, tx sqlTx, stage func(string, map[string]any) error) error {
	day := c.now().UTC().Format("2006-01-02")
	return stage(contract.EventDayActive, map[string]any{"k": "day/" + day})
}

// stageBatchMeta ships the honest-gap counter when it moved since the last
// batch (plan §D2: dropped events are reported, never silently lost).
func (c *Collector) stageBatchMeta(ctx context.Context, tx sqlTx, stage func(string, map[string]any) error) error {
	var dropped int64
	if err := tx.QueryRowContext(ctx,
		`SELECT dropped_count FROM telemetry_state WHERE id = 1`).Scan(&dropped); err != nil {
		return fmt.Errorf("telemetry.stageBatchMeta: %w", err)
	}
	if dropped == 0 {
		return nil
	}
	// The counter NEVER resets: batch_meta re-ships with the current
	// cumulative dropped_count on every pass while drops persist (same
	// honesty model as bounds enforcement — drops stay visible until the
	// staged rows age out). The drop-sized dedup key keeps the pass
	// idempotent within a window; a later, larger drop stages a new row.
	return stage(contract.EventBatchMeta, map[string]any{
		"k": fmt.Sprintf("drop/%d", dropped), "dropped_count": dropped, "retry_count": 0,
	})
}

// stageEvent assigns the monotonic seq + install id and writes one staged
// row with the bounds enforcement. fields["k"] (the dedup key) is dropped
// from the wire payload — it is local identity, not contract vocabulary.
func (c *Collector) stageEvent(ctx context.Context, tx sqlTx, event, nowTS string, fields map[string]any) error {
	// The dedup key (fields["k"]) is local identity: persisted to its own
	// column below, stripped from the wire payload.
	key, _ := fields["k"].(string)
	wire := make(map[string]any, len(fields))
	for k, v := range fields {
		if k != "k" {
			wire[k] = v
		}
	}
	payload, err := json.Marshal(wire)
	if err != nil {
		return fmt.Errorf("telemetry.stageEvent: marshal: %w", err)
	}
	var iid string
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(install_id, '') FROM telemetry_state WHERE id = 1`).Scan(&iid); err != nil {
		return fmt.Errorf("telemetry.stageEvent: state: %w", err)
	}
	if iid == "" {
		return fmt.Errorf("telemetry.stageEvent: no install identity (Derive mints it first)")
	}
	var nextSeq int64
	if err := tx.QueryRowContext(ctx,
		`UPDATE telemetry_state SET next_seq = next_seq + 1 WHERE id = 1 RETURNING next_seq - 1`).Scan(&nextSeq); err != nil {
		return fmt.Errorf("telemetry.stageEvent: seq: %w", err)
	}
	// The dedup key persists in its own column (migration 116): it is local
	// identity, stripped from the wire payload, but loadable cheaply for the
	// next pass's dedup map — without it, re-derives re-stage everything
	// (the W2.1 bug the loop test caught).
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO telemetry_staged (install_id, seq, event, payload, bytes, created_at, dedup_key)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		iid, nextSeq, event, string(payload), len(payload), nowTS, key); err != nil {
		return fmt.Errorf("telemetry.stageEvent: insert: %w", err)
	}
	return c.enforceBounds(ctx, tx)
}
