package hook

import (
	"encoding/json"
	"strings"
)

// Another host can run the hook commands a user registered for Claude
// Code. cursor-agent does (grounded against the 2026.08.25 bundle, and the
// 2026.09.18 debug log's `cli.hook.executed` records with
// hookSource "claude-user"): it loads ~/.claude/settings.json (plus the
// project's .claude/settings*.json), maps each Claude event onto its own
// step (UserPromptSubmit -> beforeSubmitPrompt, PreToolUse -> preToolUse,
// PostToolUse -> postToolUse, Stop -> stop, SubagentStop -> subagentStop,
// SessionStart -> sessionStart, SessionEnd -> sessionEnd, PreCompact ->
// preCompact) and runs the command with the SAME stdin payload it gives its
// own hooks: Cursor's field set, `hook_event_name` set to the Cursor step
// name, and `cursor_version` always present. The hookSource label lives
// only in Cursor's own analytics log, never in the payload.
//
// Such a payload reaching the claude-code receiver is not a Claude Code
// event. Observer's cursor receiver already sees the same call through
// Cursor's own hooks.json, so handling it again would evaluate the guard
// twice, write pidbridge / compaction / account rows under the wrong tool,
// and count nothing Observer does not already have. So the receiver
// records nothing and evaluates nothing; it only answers.
//
// Hook-parity Lane C3 / T-034h (2026-10-03): the answer used to be an EMPTY
// stdout, which cursor-agent logs as a FAILED hook (errorClass empty_stdout,
// node 1 debug log 2026-10-03T09:00, every claude-user step). The reply is
// now the host's own neutral answer for the step (ForeignVerdict.Reply), so
// the replayed hook runs cleanly and still decides nothing.
//
// The ruling for every other host (audit section 5.3, Lane C3): a payload
// whose host is POSITIVELY identified by a signature row is answered as
// that row says (Action); a payload that is recognisably NOT Claude Code's
// but matches no row is a no-op (ForeignNoop: empty stdout, exit 0, nothing
// recorded, nothing evaluated).

// ForeignAction is what the claude-code receiver does with a payload another
// host sent.
type ForeignAction string

const (
	// ForeignNeutral: the host's own Observer hook already observes the
	// call; answer with the host's neutral reply, record nothing.
	ForeignNeutral ForeignAction = "neutral"
	// ForeignNoop: recognisably foreign, host not identified: empty
	// stdout, exit 0, nothing recorded, nothing evaluated.
	ForeignNoop ForeignAction = "noop"
	// ForeignDispatch: the host has no Observer hook of its own; the
	// payload is dispatched to the HOST's registry binding for its event
	// (ForeignVerdict.Tool + Step), and answered with the host's neutral
	// reply when that binding has no handler.
	ForeignDispatch ForeignAction = "dispatch"
)

// ForeignVerdict is the classification of one claude-code hook payload.
type ForeignVerdict struct {
	// Foreign is false for a genuine Claude Code payload (or one that does
	// not decode): the receiver proceeds exactly as before.
	Foreign bool
	// Host is the identified host ("" when unidentified).
	Host string
	// Tool is the registry tool id a ForeignDispatch verdict dispatches
	// to.
	Tool string
	// Action says how to answer.
	Action ForeignAction
	// Step is the host's own event name from the payload.
	Step string
	// Reply is the exact stdout bytes to write (nil: write nothing).
	Reply []byte
}

// hostProbe is the part of a hook payload the signature table reads.
type hostProbe struct {
	CursorVersion *string `json:"cursor_version"`
	HookEventName string  `json:"hook_event_name"`
	// HookEventCamel is Grok Build's own spelling of the event
	// (snake_case value, camelCase key), sent beside hook_event_name.
	HookEventCamel string `json:"hookEventName"`
	SessionID      string `json:"session_id"`
	SessionIDCamel string `json:"sessionId"`
	ConversationID string `json:"conversation_id"`
}

// cursorSteps are the Cursor step names cursor-agent puts in
// hook_event_name when it runs a Claude Code hook (the targets of its
// Claude-event mapping). Claude Code itself spells every event in
// PascalCase, so none of these can come from Claude Code.
//
// They are spelled out here, not taken from internal/adapter/cursor: this
// classifier belongs to the claude-code receiver, which a build without
// adapter.cursor still has (lane MP-L1D, T-598).
// TestForeignCursorStepsMatchTheAdapter pins them to the adapter's own
// constants in every build that has the adapter.
var cursorSteps = map[string]bool{
	"beforeSubmitPrompt": true,
	"preToolUse":         true,
	"postToolUse":        true,
	"stop":               true,
	"subagentStop":       true,
	"sessionStart":       true,
	"sessionEnd":         true,
	"preCompact":         true,
}

// cursorNeutralReplies is cursor-agent's neutral answer per step: the bytes
// Observer's OWN cursor receiver writes when it has no opinion on that step
// (internal/hook/cursor.go: {"continue":true} on beforeSubmitPrompt, a reply
// without a permission field elsewhere). A step not listed answers {}.
var cursorNeutralReplies = map[string][]byte{
	"beforeSubmitPrompt": []byte(`{"continue":true}` + "\n"),
}

// neutralObject is the default neutral reply: an empty JSON object.
var neutralObject = []byte("{}\n")

func cursorNeutral(step string) []byte {
	if r, ok := cursorNeutralReplies[step]; ok {
		return r
	}
	return neutralObject
}

// foreignHostSignatures is the ordered signature table: the first row
// whose match holds names the host. Adding a host that runs Claude Code
// hooks is adding a row; a row may only DISPATCH or answer for a host once a
// live capture grounds its signature (audit section 5.3 ruling).
//
// Live grounding (node 1, 2026-10-03, hook-parity Lane C):
//   - cursor-agent 2026.10.01-14929f9: cursor_version on every replayed
//     step; the neutral replies below log status "success" for every
//     claude-user step (sessionStart, preToolUse, postToolUse,
//     sessionEnd), and the shell tool still ran with and without --force.
//   - Grok Build 1.0.46 (`grok -p`): runs ~/.claude/settings.json hooks
//     with BOTH spellings: session_id + sessionId, hook_event_name
//     (PascalCase, e.g. "SessionEnd") + hookEventName (snake_case value,
//     "session_end"), plus workspaceRoot. Claude Code never sends
//     hookEventName. Its sessionId is the ~/.grok/sessions/<cwd>/<sid>/
//     bundle directory the grok adapter keys sessions on, so its lifecycle
//     events are DISPATCHED to the grok row's lifecycle bindings (grok has
//     no Observer hook of its own).
//   - Devin CLI 3000.11.3 (`devin -p`): ran NO Claude Code hook, so no row.
var foreignHostSignatures = []struct {
	host   string
	tool   string
	match  func(hostProbe) bool
	action ForeignAction
	reply  func(step string) []byte
}{
	{host: "cursor", match: func(p hostProbe) bool { return p.CursorVersion != nil }, action: ForeignNeutral, reply: cursorNeutral},
	{host: "cursor", match: func(p hostProbe) bool { return cursorSteps[p.HookEventName] }, action: ForeignNeutral, reply: cursorNeutral},
	{host: "grok", tool: "grok", match: func(p hostProbe) bool { return p.HookEventCamel != "" }, action: ForeignDispatch, reply: func(string) []byte { return neutralObject }},
}

// recognisablyForeign reports a payload Claude Code cannot have sent:
// Claude Code spells every hook_event_name in PascalCase and always sends
// session_id, so an event name that starts lower-case, or a session key
// spelled the way other hosts spell it (sessionId, conversation_id) with no
// session_id at all, came from another host. Deliberately narrow: a
// payload that merely LOOKS like Claude Code's is treated as Claude Code's
// (today's behaviour) until a live-grounded signature row says otherwise.
func recognisablyForeign(p hostProbe) bool {
	if n := p.HookEventName; n != "" && n[0] >= 'a' && n[0] <= 'z' {
		return true
	}
	if strings.TrimSpace(p.SessionID) == "" && (p.SessionIDCamel != "" || p.ConversationID != "") {
		return true
	}
	return false
}

// ClassifyClaudeHookPayload classifies a payload that reached a Claude Code
// hook command: genuine (Foreign false), from an identified host, or
// recognisably foreign but unidentified (ForeignNoop).
func ClassifyClaudeHookPayload(body []byte) ForeignVerdict {
	var p hostProbe
	if err := json.Unmarshal(body, &p); err != nil {
		return ForeignVerdict{}
	}
	for _, row := range foreignHostSignatures {
		if row.match(p) {
			v := ForeignVerdict{Foreign: true, Host: row.host, Tool: row.tool, Action: row.action, Step: p.HookEventName}
			if row.reply != nil {
				v.Reply = row.reply(p.HookEventName)
			}
			return v
		}
	}
	if recognisablyForeign(p) {
		return ForeignVerdict{Foreign: true, Action: ForeignNoop, Step: p.HookEventName}
	}
	return ForeignVerdict{}
}

// ForeignClaudeHookHost reports the host that sent a payload to a Claude
// Code hook command when that host is NOT Claude Code ("" and false for a
// genuine Claude Code payload, or one that does not decode - the caller
// then proceeds exactly as before). An unidentified foreign payload reports
// host "unidentified".
func ForeignClaudeHookHost(body []byte) (string, bool) {
	v := ClassifyClaudeHookPayload(body)
	if !v.Foreign {
		return "", false
	}
	if v.Host == "" {
		return "unidentified", true
	}
	return v.Host, true
}

// RecordForeignReplay appends the forensic hook-events.jsonl row for a
// foreign payload the claude-code receiver answered without recording.
func RecordForeignReplay(label string, v ForeignVerdict, bytes int) {
	host := v.Host
	if host == "" {
		host = "unidentified"
	}
	appendHookEventLog(hookEvent{Event: label, Bytes: bytes, Action: "foreign:" + host + ":" + string(v.Action)})
}
