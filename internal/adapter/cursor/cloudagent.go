package cursor

import (
	"encoding/json"
	"strings"
)

// # Cursor Cloud Agents (`bc-<uuid>` conversations)
//
// A Cloud Agent (Cursor's renamed "Background Agent") runs its turns in a
// Cursor-hosted VM. Grounded live on Cursor 3.23.12 (2026-10-05): the IDE
// fires only sessionStart, afterAgentThought and (on 3.21.13, 2026-09-22)
// subagentStart for such a conversation, every one of them with
// `workspace_roots: []`; preToolUse / postToolUse / stop /
// afterAgentResponse / beforeSubmitPrompt never fire locally because the
// tools run remotely. `is_background_agent` is FALSE on the 3.23.12
// sessionStart (and absent from the thoughts), so the id prefix is the signal that actually
// carries the fact; the flag is honoured as a second signal for builds that
// set it. state.vscdb keeps the conversation under
// `bcCachedDetails:<bc-id>` (a protobuf blob this adapter never decodes).
//
// The class is resolved HERE, at the adapter boundary, through one ordered
// rule table, so the hook path, the hooks-log replay and the state.vscdb
// reader all ask the same question the same way (CLAUDE.md rules 3 and 5).

// conversationClass is what a Cursor conversation resolves to before any
// row is built.
type conversationClass struct {
	// RemoteTurns: the agent's turns run in Cursor's remote VM, so no
	// local tool or turn hook will ever follow a thought, and state.vscdb
	// has no bubble rows for it.
	RemoteTurns bool
	// BootstrapsRootless: a root-less lifecycle event (sessionStart /
	// sessionEnd) may file the conversation under SyntheticProjectRoot.
	// A local chat must not (drafts and folder sessions fire root-less
	// lifecycle markers too, see rootlessNoBootstrap); a Cloud Agent has
	// no local folder at all, and its sessionStart is often the only
	// lifecycle evidence it leaves.
	BootstrapsRootless bool
}

// conversationRule is one row of conversationRules. A row matches when the
// conversation id carries prefix (prefix non-empty) or, for a flag row,
// when the payload says is_background_agent.
type conversationRule struct {
	prefix         string
	backgroundFlag bool
	class          conversationClass
}

// conversationRules is walked top-down; the first matching row wins and an
// unmatched conversation is a plain local chat (the zero class).
var conversationRules = []conversationRule{
	// A draft never ran anywhere, whatever else the payload claims.
	// Cursor fires root-less sessionStart / sessionEnd pairs for it and
	// writes a composerData row; neither may conjure a session.
	{prefix: "draft-", class: conversationClass{}},
	// The Cloud Agent id family (3.21.13 through 3.23.12).
	{prefix: "bc-", class: conversationClass{RemoteTurns: true, BootstrapsRootless: true}},
	// A build that marks the payload itself.
	{backgroundFlag: true, class: conversationClass{RemoteTurns: true, BootstrapsRootless: true}},
}

// classifyConversation resolves a conversation id (plus the payload's
// is_background_agent flag, false when absent) to its class.
func classifyConversation(conversationID string, isBackgroundAgent bool) conversationClass {
	for _, r := range conversationRules {
		if r.prefix != "" && strings.HasPrefix(conversationID, r.prefix) {
			return r.class
		}
		if r.backgroundFlag && isBackgroundAgent {
			return r.class
		}
	}
	return conversationClass{}
}

// classOf is classifyConversation over a decoded hook payload.
func classOf(raw rawHookPayload) conversationClass {
	return classifyConversation(raw.ConversationID, raw.IsBackgroundAgent)
}

// cloudAgentThoughtRawName is the RawToolName of the row a Cloud Agent's
// afterAgentThought mints (see thoughtMintsRow). It is a step marker, not
// a reasoning action: the thought itself rides the row's
// PrecedingReasoning column, exactly where a local conversation's thought
// lands on its successor row.
const cloudAgentThoughtRawName = "cursor.cloud_agent_thought"

// thoughtMintsRow reports whether an afterAgentThought lands its own row
// instead of being stashed for a successor: true only for a remote-turn
// conversation filed under the placeholder (no local folder), because no
// local successor event will ever arrive to claim the stash.
func thoughtMintsRow(class conversationClass, projectRoot string) bool {
	return class.RemoteTurns && projectRoot == SyntheticProjectRoot
}

// IsRemoteAgentPayload reports whether a Cursor hook payload belongs to a
// conversation whose turns run in Cursor's remote VM (a Cloud Agent). An
// undecodable body is not one.
func IsRemoteAgentPayload(body []byte) bool {
	var raw rawHookPayload
	if json.Unmarshal(body, &raw) != nil {
		return false
	}
	return classOf(raw).RemoteTurns
}

// ThoughtMintsRow reports whether an afterAgentThought payload lands a row
// of its own (a root-less Cloud Agent thought with text). The live hook
// uses it to decide whether the event needs the database at all: every
// other thought only stashes a preview and must stay database-free.
func ThoughtMintsRow(body []byte) bool {
	var raw rawHookPayload
	if json.Unmarshal(body, &raw) != nil || raw.ConversationID == "" || strings.TrimSpace(raw.Text) == "" {
		return false
	}
	class := classOf(raw)
	return thoughtMintsRow(class, hookProjectRoot(EventAfterAgentThought, raw.WorkspaceRoots, class))
}
