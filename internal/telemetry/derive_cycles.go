package telemetry

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// Prompt cycles (contract event prompt_cycle; telemetry plan Step 5.7): one
// event per user prompt, from the prompt to the last model activity before the
// session's next prompt. Sourced from the transcript-derived tables every
// adapter fills (actions.user_prompt, token_usage), so it works with or
// without the proxy. Content-free: only the model/provider FAMILY, token
// sizes, the cache share and the end-to-end time leave; no prompt text is read.

const (
	// promptDedupGap collapses the same prompt recorded twice (hook and
	// transcript) a moment apart into one cycle start.
	promptDedupGap = 2 * time.Second
	// cycleQuiet: the last cycle of a session counts as complete once the
	// session has been quiet this long; earlier it may still be running.
	cycleQuiet = 10 * time.Minute
)

type cyclePrompt struct {
	id int64
	ts time.Time
}

type cycleTokens struct {
	ts                         time.Time
	model                      string
	in, out, cacheRead, create int64
}

// derivePromptCycles stages one prompt_cycle per completed cycle whose prompt
// falls in the lookback window. The dedup key is the prompt action's id.
func (c *Collector) derivePromptCycles(ctx context.Context, tx sqlTx, since string, stage func(string, map[string]any) error) error {
	if !contract.TierAllows(c.cfg.Tier, contract.EventPromptCycle) {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, COALESCE(session_id, ''), timestamp FROM actions
		WHERE action_type = 'user_prompt' AND timestamp >= ?
		ORDER BY session_id, id`, since)
	if err != nil {
		return fmt.Errorf("telemetry.derivePromptCycles: prompts: %w", err)
	}
	prompts := map[string][]cyclePrompt{}
	for rows.Next() {
		var (
			id      int64
			sid, ts string
		)
		if err := rows.Scan(&id, &sid, &ts); err != nil {
			_ = rows.Close()
			return fmt.Errorf("telemetry.derivePromptCycles: scan prompt: %w", err)
		}
		t, ok := parseTS(ts)
		if sid == "" || !ok {
			continue
		}
		prompts[sid] = append(prompts[sid], cyclePrompt{id: id, ts: t})
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("telemetry.derivePromptCycles: prompts: %w", err)
	}
	// Order by parsed time, not by the stored string: adapters mix
	// fractional and whole-second timestamps, and "…00.700Z" sorts before
	// "…00Z" as text. Then collapse the same prompt recorded twice.
	for sid, list := range prompts {
		sort.SliceStable(list, func(i, j int) bool { return list[i].ts.Before(list[j].ts) })
		kept := list[:0]
		for _, p := range list {
			if n := len(kept); n > 0 && p.ts.Sub(kept[n-1].ts) < promptDedupGap {
				continue
			}
			kept = append(kept, p)
		}
		prompts[sid] = kept
	}

	sessions := make([]string, 0, len(prompts))
	for sid := range prompts {
		sessions = append(sessions, sid)
	}
	sort.Strings(sessions)
	now := c.now().UTC()
	for _, sid := range sessions {
		if err := c.stageSessionCycles(ctx, tx, sid, prompts[sid], now, stage); err != nil {
			return err
		}
	}
	return nil
}

func (c *Collector) stageSessionCycles(ctx context.Context, tx sqlTx, sid string, ps []cyclePrompt, now time.Time, stage func(string, map[string]any) error) error {
	first := ps[0].ts.Format(time.RFC3339Nano)
	toks, err := sessionTokens(ctx, tx, sid, first)
	if err != nil {
		return err
	}
	lastActivity, err := sessionLastActivity(ctx, tx, sid, first)
	if err != nil {
		return err
	}
	for i, p := range ps {
		var bound time.Time
		if i+1 < len(ps) {
			bound = ps[i+1].ts
		} else {
			// Last cycle: complete only once the session has gone quiet.
			if now.Sub(lastActivity) < cycleQuiet {
				continue
			}
			bound = now
		}
		fields, ok := cycleFields(p.ts, bound, toks)
		if !ok {
			continue // no model activity in the cycle: nothing to report
		}
		fields["k"] = fmt.Sprintf("cycle/%d", p.id)
		if err := stage(contract.EventPromptCycle, fields); err != nil {
			return err
		}
	}
	return nil
}

// cycleFields summarizes the token rows in [start, bound). The model is the
// one with the most output tokens in the cycle; the end is its last token row.
func cycleFields(start, bound time.Time, toks []cycleTokens) (map[string]any, bool) {
	var in, out, cacheRead, create int64
	var end time.Time
	byModel := map[string]int64{}
	for _, t := range toks {
		if t.ts.Before(start) || !t.ts.Before(bound) {
			continue
		}
		in += t.in
		out += t.out
		cacheRead += t.cacheRead
		create += t.create
		byModel[t.model] += t.out + 1 // +1: a model with zero output still counts
		if t.ts.After(end) {
			end = t.ts
		}
	}
	if len(byModel) == 0 {
		return nil, false
	}
	model, best := "", int64(-1)
	for m, n := range byModel {
		if n > best || (n == best && m < model) {
			model, best = m, n
		}
	}
	inputSize := in + cacheRead + create
	cachePct := 0
	if inputSize > 0 {
		cachePct = int(math.Round(float64(cacheRead) * 100 / float64(inputSize)))
	}
	family := modelFamily(model)
	return map[string]any{
		"model":                  family,
		"provider":               family,
		"input_size":             inputSize,
		"output_size":            out,
		"cache_percentage":       cachePct,
		"end_to_end_duration_ms": end.Sub(start).Milliseconds(),
		// The 5.6 classifier refines this (system nudges vs typed prompts);
		// until then every cycle starts from a user_prompt action.
		"segment": "user_prompt",
	}, true
}

func sessionTokens(ctx context.Context, tx sqlTx, sid, since string) ([]cycleTokens, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT timestamp, COALESCE(model, ''), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
		       COALESCE(cache_read_tokens, 0), COALESCE(cache_creation_tokens, 0)
		FROM token_usage WHERE session_id = ? AND timestamp >= ?`, sid, since)
	if err != nil {
		return nil, fmt.Errorf("telemetry.derivePromptCycles: tokens: %w", err)
	}
	defer rows.Close()
	var out []cycleTokens
	for rows.Next() {
		var ts string
		var t cycleTokens
		if err := rows.Scan(&ts, &t.model, &t.in, &t.out, &t.cacheRead, &t.create); err != nil {
			return nil, fmt.Errorf("telemetry.derivePromptCycles: scan tokens: %w", err)
		}
		var ok bool
		if t.ts, ok = parseTS(ts); ok {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

// sessionLastActivity is the newest action or token row of the session.
func sessionLastActivity(ctx context.Context, tx sqlTx, sid, since string) (time.Time, error) {
	var a, b string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT MAX(timestamp) FROM actions WHERE session_id = ? AND timestamp >= ?), ''),
		       COALESCE((SELECT MAX(timestamp) FROM token_usage WHERE session_id = ? AND timestamp >= ?), '')`,
		sid, since, sid, since).Scan(&a, &b); err != nil {
		return time.Time{}, fmt.Errorf("telemetry.derivePromptCycles: last activity: %w", err)
	}
	ta, _ := parseTS(a)
	tb, _ := parseTS(b)
	if tb.After(ta) {
		return tb, nil
	}
	return ta, nil
}

// modelFamily maps a model id onto the contract's closed model/provider
// family vocabulary; anything unrecognized is "other".
func modelFamily(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude"), strings.HasPrefix(m, "anthropic"):
		return "anthropic"
	case strings.Contains(m, "gemini"), strings.HasPrefix(m, "google"):
		return "google"
	case strings.Contains(m, "codex"):
		return "codex"
	case strings.HasPrefix(m, "gpt"), strings.HasPrefix(m, "openai"), strings.HasPrefix(m, "o1"),
		strings.HasPrefix(m, "o3"), strings.HasPrefix(m, "o4"):
		return "openai"
	default:
		return "other"
	}
}

// parseTS reads the RFC 3339 timestamps adapters write (with or without
// fractional seconds).
func parseTS(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
