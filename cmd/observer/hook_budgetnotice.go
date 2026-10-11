package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/budgetwarnfile"
	"github.com/marmutapp/superbased-observer/internal/hook"
)

// The next-prompt budget notice (2026-10-02 budget-latency arc, orchestrator
// Q4): a hook-capable tool's prompt-submit reply carries the org budget
// warning the daemon's budget-warning service recorded, in the field that
// dialect documents as USER-VISIBLE. Each notice is shown in-band ONCE per
// budget period, node-wide (the hook-owned shown marker,
// budgetwarnfile.MarkShown), and only when the reply actually went out as an
// allow - a blocked prompt (no stdout) leaves it pending for the next one.
//
// The hook does no I/O beyond two small files beside the DB: no database, no
// network, well inside the hook's time budget.

// budgetNoticeFields maps a prompt dialect onto its user-visible reply field.
// Only dialects whose vendor docs name such a field get a row (Claude Code's
// top-level systemMessage, Gemini CLI's systemMessage); every other dialect
// has no documented channel for a non-blocking notice, so it shows nothing
// in-band and the dashboard banner / statusline / desktop channels carry the
// warning instead (honest, not guessed).
var budgetNoticeFields = map[string]string{
	hook.PromptDialectClaudeCode: "systemMessage",
	hook.PromptDialectGemini:     "systemMessage",
}

// budgetNoticeMaxAge bounds how old a crossing may be and still be news for
// the next prompt.
const budgetNoticeMaxAge = 12 * time.Hour

// wrapBudgetNotice returns w unchanged when there is nothing to show (or the
// dialect has no user-visible field), else a writer that adds the notice to
// the first JSON object the reply writes.
func wrapBudgetNotice(w io.Writer, dialect, dbPath string, now time.Time) io.Writer {
	field := budgetNoticeFields[dialect]
	if field == "" || dbPath == "" {
		return w
	}
	file, err := budgetwarnfile.Read(budgetwarnfile.StatusPath(dbPath))
	if err != nil || file == nil {
		return w
	}
	shownPath := budgetwarnfile.ShownPath(dbPath)
	pending := budgetwarnfile.PendingInBand(file, budgetwarnfile.ReadShown(shownPath), now, budgetNoticeMaxAge)
	if len(pending) == 0 {
		return w
	}
	msgs := make([]string, 0, len(pending))
	ids := make([]string, 0, len(pending))
	for _, n := range pending {
		msgs = append(msgs, n.Message)
		ids = append(ids, n.ID)
	}
	return &budgetNoticeWriter{w: w, field: field, notice: "Observer: " + strings.Join(msgs, " "), ids: ids, shownPath: shownPath}
}

// budgetNoticeWriter injects the notice into the first JSON object written.
type budgetNoticeWriter struct {
	w         io.Writer
	field     string
	notice    string
	ids       []string
	shownPath string
	done      bool
}

func (b *budgetNoticeWriter) Write(p []byte) (int, error) {
	if b.done {
		return b.w.Write(p)
	}
	b.done = true
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(p), &m); err != nil || m == nil {
		return b.w.Write(p)
	}
	if _, has := m[b.field]; has {
		// The reply already speaks to the user (a guard warning); never
		// overwrite it. The budget notice stays pending for the next prompt.
		return b.w.Write(p)
	}
	m[b.field] = b.notice
	out, err := json.Marshal(m)
	if err != nil {
		return b.w.Write(p)
	}
	out = append(out, '\n')
	if _, err := b.w.Write(out); err != nil {
		return 0, err
	}
	_ = budgetwarnfile.MarkShown(b.shownPath, b.ids...)
	return len(p), nil
}
