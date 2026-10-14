package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/planlimits"
)

// Plans & limits status line feed (docs/plans-and-limits.md). Claude Code
// hands `observer statusline` a payload that carries the plan's rate_limits
// (5-hour, weekly, spend limit). This file forwards ONLY those numbers, plus
// the session id and the Claude Code version, to the daemon's
// POST /api/plans/statusline, which stores them in the module's own table.
// No path, model, cost or prompt text is forwarded. It runs only behind the
// tile fetch's own gate (a live daemon on a loopback address), only when
// the numbers changed since the last forward, with a short timeout, and
// every failure is silent: the status line itself never waits on it.

// planForwardStateFile remembers the last forwarded reading (numbers and
// session id only), so an unchanged status line sends nothing.
const planForwardStateFile = "plans-statusline-last.json"

// planForwardTimeout bounds the one POST.
const planForwardTimeout = 400 * time.Millisecond

// planForwardClient is swapped by tests.
var planForwardClient = &http.Client{Timeout: planForwardTimeout}

func forwardPlanStatusLine(ctx context.Context, base, dbDir string, raw []byte, now time.Time) {
	if len(raw) == 0 || base == "" || dbDir == "" {
		return
	}
	next, err := planlimits.ExtractStatusLineForward(raw, now)
	if err != nil {
		return // an API-key user, no response yet, or not JSON
	}
	statePath := filepath.Join(dbDir, planForwardStateFile)
	var prev planlimits.StatusLineForward
	if b, err := os.ReadFile(statePath); err == nil && len(b) < 16<<10 { // #nosec G304 -- our own state file beside the db
		_ = json.Unmarshal(b, &prev)
	}
	if !planlimits.StatusLineChanged(prev, next) {
		return
	}
	body, err := json.Marshal(next)
	if err != nil {
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, planForwardTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, strings.TrimRight(base, "/")+"/api/plans/statusline", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := planForwardClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return
	}
	tmp := statePath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, statePath); err != nil {
		_ = os.Remove(tmp)
	}
}
