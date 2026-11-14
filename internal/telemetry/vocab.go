package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// contractPairs mirrors the server contract's event → (classification,
// purpose) matrix locally: the mirrored contract.go keeps its copy
// unexported (byte-identical mirror discipline — no additions to the
// mirrored file), so the batcher maps event names here. TestContractPairsMatchGolden
// pins this map against golden contract.json, so it cannot fork from the
// contract.
var contractPairs = map[string][2]string{
	contract.EventAppStart:     {contract.ClassSystemMeta, contract.PurposePerfHealth},
	contract.EventAppStop:      {contract.ClassSystemMeta, contract.PurposePerfHealth},
	contract.EventSessionStart: {contract.ClassSystemMeta, contract.PurposeFeature},
	contract.EventSessionEnd:   {contract.ClassSystemMeta, contract.PurposeFeature},
	contract.EventToolUse:      {contract.ClassSystemMeta, contract.PurposeFeature},
	contract.EventError:        {contract.ClassCallstack, contract.PurposePerfHealth},
	contract.EventTokenUse:     {contract.ClassSystemMeta, contract.PurposeBusiness},
	contract.EventDayActive:    {contract.ClassSystemMeta, contract.PurposeFeature},
	contract.EventBatchMeta:    {contract.ClassSystemMeta, contract.PurposePerfHealth},
	contract.EventDiagCommand:  {contract.ClassPseudonymized, contract.PurposeFeature},
	contract.EventPromptCycle:  {contract.ClassSystemMeta, contract.PurposeBusiness},
}

// SchemaVersion is the v1 contract schema timestamp stamped on every event
// (pinned against golden contract.json by TestSchemaVersionMatchesGolden).
const SchemaVersion = "2026-09-14T00:00:00Z"

// runtimeOS / runtimeArch feed the envelope's os/arch fields.
func runtimeOS() string { return runtime.GOOS }

func runtimeArch() string { return runtime.GOARCH }

// sqlTx is the transaction interface the derive helpers take — *sql.Tx in
// production, a stub in tests.
type sqlTx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// adapterName maps the open local sessions.tool vocabulary onto the
// contract's closed adapter vocabulary; unknown names bucket to other
// (plan §D5 — the honest other-bucket, never a name leak).
func adapterName(tool string) string {
	t := strings.ToLower(tool)
	if a, ok := adapterAlias[t]; ok {
		t = a
	}
	if contract.Adapter[t] {
		return t
	}
	return "other"
}

// adapterAlias maps the store's tool ids (internal/models Tool*) onto the
// contract's closed adapter spelling where the two differ.
var adapterAlias = map[string]string{
	"claude-code": "claudecode",
	"gemini-cli":  "gemini",
}

// actionToolName maps the store's action types (internal/models Action*)
// onto the contract's closed tool_name vocabulary. Only these action types
// are tool calls; prompts, assistant text and bookkeeping rows are not
// counted at all. Tool calls with no closer contract name count as other.
var actionToolName = map[string]string{
	"read_file":      "read",
	"write_file":     "write",
	"edit_file":      "edit",
	"run_command":    "shell",
	"search_text":    "grep",
	"search_files":   "glob",
	"web_search":     "web_search",
	"web_fetch":      "read_url",
	"browser_action": "browser_navigate",
	"todo_update":    "todo_write",
	"ask_user":       "ask_user",
	"mcp_call":       "other",
	"spawn_subagent": "other",
}

// modelTier maps a local model id onto the closed model-tier vocabulary.
// Deliberately coarse: model IDS never leave the node, only the tier
// bucket (plan §D5).
func modelTier(model string) string {
	m := strings.ToLower(model)
	switch {
	case m == "":
		return "standard"
	case strings.HasPrefix(m, "gpt-5"), strings.HasPrefix(m, "claude-opus"), strings.HasPrefix(m, "o1"),
		strings.HasPrefix(m, "opus"), strings.HasPrefix(m, "frontier"):
		return "frontier"
	case strings.Contains(m, "mini"), strings.Contains(m, "haiku"), strings.Contains(m, "flash"),
		strings.Contains(m, "nano"), strings.Contains(m, "fast"):
		return "fast"
	case strings.Contains(m, "local"), strings.Contains(m, "ollama"), strings.Contains(m, "llama.cpp"):
		return "local"
	default:
		return "standard"
	}
}

// errorClass maps a local failure_context.error_category onto the closed
// (error_class, surface) pair. Unknown categories bucket to (other, tool) —
// the vocabulary never grows silently.
func errorClass(category string) (string, string) {
	switch strings.ToLower(category) {
	case "timeout":
		return "tool_timeout", "tool"
	case "exit", "command_failed", "tool_failed":
		return "tool_failed", "tool"
	case "hook":
		return "hook_failed", "hook"
	case "proxy_5xx":
		return "proxy_5xx", "proxy"
	case "proxy_4xx":
		return "proxy_4xx", "proxy"
	case "adapter", "parse":
		return "adapter_parse", "adapter"
	case "db", "locked", "db_locked":
		return "db_locked", "db"
	case "scrub":
		return "scrub_reject", "tool"
	case "consent":
		return "consent_missing", "batcher"
	case "network":
		return "network_unreachable", "batcher"
	default:
		return "other", "tool"
	}
}

// durationRFC parses two RFC-3339 timestamps and returns the duration in
// whole milliseconds; negative or unparseable pairs are an error the
// caller skips honestly.
func durationRFC(started, ended string) (int64, error) {
	s, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return 0, err
	}
	e, err := time.Parse(time.RFC3339, ended)
	if err != nil {
		return 0, err
	}
	d := e.Sub(s).Milliseconds()
	if d < 0 {
		return 0, fmt.Errorf("telemetry.durationRFC: negative duration for %s..%s", started, ended)
	}
	return d, nil
}
