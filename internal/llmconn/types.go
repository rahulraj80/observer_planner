// Package llmconn provides optional, opt-in LLM analysis connections
// (docs/design/standup-briefing.md §3): one endpoint you can point analysis
// at, a shared client for the anthropic / openai_compat / local_openai_compat
// dialects, and pure task-based selection. It is never load-bearing — zero
// configured connections reproduces exactly the data-only behavior. The
// package is import-light: callers inject scrubbing and resolve
// credentials, mirroring the judge client's posture. The vocabulary lives in
// llmtypes (no network imports) and is aliased here.
package llmconn

import "github.com/marmutapp/superbased-observer/internal/llmconn/llmtypes"

type (
	// Kind identifies the API dialect a connection speaks.
	Kind = llmtypes.Kind
	// Task is a role an analysis call may fill.
	Task = llmtypes.Task
	// Health is the last connectivity check for a connection.
	Health = llmtypes.Health
	// Connection is one LLM endpoint analysis can be pointed at.
	Connection = llmtypes.Connection
)

// The dialects and task roles (see llmtypes).
const (
	KindAnthropic    = llmtypes.KindAnthropic
	KindOpenAICompat = llmtypes.KindOpenAICompat
	KindLocalOpenAI  = llmtypes.KindLocalOpenAI

	TaskNarrative = llmtypes.TaskNarrative
	TaskAsk       = llmtypes.TaskAsk
	TaskSummarize = llmtypes.TaskSummarize
	TaskAskPast   = llmtypes.TaskAskPast
	TaskCoach     = llmtypes.TaskCoach
	TaskTriage    = llmtypes.TaskTriage
	TaskDiagnose  = llmtypes.TaskDiagnose
	TaskQuestions = llmtypes.TaskQuestions
)

// usable reports whether the connection may be selected for a task: it must
// be enabled, serve the task, and — for a non-loopback host — explicitly opt
// into remote use (allow_remote=true).
func usable(c Connection, task Task) bool {
	if !c.Enabled {
		return false
	}
	if !c.Allows(task) {
		return false
	}
	if c.Remote() && !c.AllowRemote {
		return false
	}
	return true
}
