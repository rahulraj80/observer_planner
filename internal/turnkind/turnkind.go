// Package turnkind provides a pure, deterministic requirement-kind classifier
// for API turns. It reuses the routing taxonomy (TurnKind constants) but
// exposes a simpler input interface suitable for offline/backfill use where
// the full routingcore.DecisionInput (session window, client phase) is not available.
//
// Classification sources (precedence):
//  1. Explicit: turn_kind already set on the turn (from live routing).
//  2. Structural: prompt_tokens >= long_context threshold, stream flag, etc.
//  3. Heuristic: prompt/response token ratio, tool_use presence, message count.
//  4. Unknown fallback.
//
// The classifier is pure (no I/O) and deterministic — identical inputs always
// produce identical outputs, enabling idempotent backfill.
package turnkind

import "github.com/marmutapp/superbased-observer/internal/routingcore"

// Kind is an alias for routingcore.TurnKind so this package doesn't import routing
// in its public API (internal/evidence etc. can depend on turnkind without a
// cycle). The values are the routing taxonomy v1.
type Kind = routingcore.TurnKind

const (
	KindPlan         = routingcore.TurnPlan
	KindReadOnly     = routingcore.TurnReadOnly
	KindEdit         = routingcore.TurnEdit
	KindTestRun      = routingcore.TurnTestRun
	KindHousekeeping = routingcore.TurnHousekeeping
	KindSubagent     = routingcore.TurnSubagent
	KindLongContext  = routingcore.TurnLongContext
	KindUnknown      = routingcore.TurnUnknown
)

// AllKinds enumerates the taxonomy for lint/tests.
func AllKinds() []Kind {
	return routingcore.AllTurnKinds()
}

// ClassifierInput is the minimal data needed to classify a turn offline.
// When the full routingcore.DecisionInput is available (live), callers should
// prefer routingcore.ClassifyTurnKind.
type ClassifierInput struct {
	// PromptTokens is the input prompt token count for this turn.
	PromptTokens int64
	// ResponseTokens is the output token count (may be 0 for errors).
	ResponseTokens int64
	// HasToolUse indicates the request contained tool calls.
	HasToolUse bool
	// IsStream indicates the request was streamed.
	IsStream bool
	// SystemPromptHash is a hash of the system prompt (empty if none).
	SystemPromptHash string
	// MessageCount is the number of messages in the request (incl. system).
	MessageCount int
	// SessionTurnIndex is the 0-based index of this turn in its session.
	SessionTurnIndex int
	// WasRoutedKind is the kind from router_decisions if available ("" = not routed).
	WasRoutedKind string
}

// Classify classifies a single turn using the offline heuristic.
// Deterministic; no I/O. Returns the kind and the rule name that matched.
func Classify(in ClassifierInput) (Kind, string) {
	// 1. Explicit: if the turn was already classified by the live router, trust it.
	if k := in.WasRoutedKind; k != "" {
		return Kind(k), "routed_kind"
	}

	// 2. Structural: long_context band (matches routing default threshold)
	const longContextThreshold = 200_000
	if in.PromptTokens >= longContextThreshold {
		return KindLongContext, "long_context_band"
	}

	// 3. Heuristic: tool-use presence + prompt/response patterns.
	//    - HasToolUse + high output = likely edit/test_run
	//    - No tools + low output + early turn = likely plan/read_only
	//    - Mid-session + tools = likely edit
	if in.HasToolUse {
		if in.ResponseTokens > in.PromptTokens/2 && in.ResponseTokens > 500 {
			return KindEdit, "tool_use_heavy_output"
		}
		if in.SessionTurnIndex <= 2 {
			return KindPlan, "tool_use_early_turn"
		}
		return KindEdit, "tool_use_general"
	}

	// 4. No tools: look at response size and position.
	if in.ResponseTokens == 0 {
		return KindUnknown, "no_response"
	}
	if in.SessionTurnIndex == 0 {
		// First turn: plan or read_only depending on output size
		if in.ResponseTokens < in.PromptTokens/4 {
			return KindPlan, "first_turn_small_response"
		}
		return KindReadOnly, "first_turn_exploration"
	}
	if in.ResponseTokens < in.PromptTokens/8 {
		return KindReadOnly, "small_response_mid_session"
	}
	if in.ResponseTokens > in.PromptTokens*2 {
		return KindTestRun, "large_response_likely_test"
	}

	// 5. Default: unknown (conservative — routing never routes unknown)
	return KindUnknown, "heuristic_unknown"
}

// ClassifyBatch classifies multiple turns (useful for backfill CLI).
func ClassifyBatch(inputs []ClassifierInput) []Classification {
	out := make([]Classification, len(inputs))
	for i, in := range inputs {
		k, r := Classify(in)
		out[i] = Classification{Kind: k, Rule: r}
	}
	return out
}

// Classification holds the result of a single classification.
type Classification struct {
	Kind Kind
	Rule string
}
