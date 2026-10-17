package hooklifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/scrub"
)

// Payload is a lifecycle hook payload normalized across hosts.
type Payload struct {
	SessionID            string
	Cwd                  string
	TranscriptPath       string
	HookEventName        string
	Source               string
	Reason               string
	AgentID              string
	AgentType            string
	LastAssistantMessage string
	PermissionMode       string
	EffortLevel          string
}

// rawPayload is the union of the spellings hosts use. The FIRST non-empty
// field of each group wins (sessionIDFields order).
type rawPayload struct {
	SessionID      string   `json:"session_id"`
	SessionIDCamel string   `json:"sessionId"`
	ConversationID string   `json:"conversation_id"`
	Cwd            string   `json:"cwd"`
	WorkspaceRoots []string `json:"workspace_roots"`
	TranscriptPath string   `json:"transcript_path"`
	HookEventName  string   `json:"hook_event_name"`
	Source         string   `json:"source"`
	Reason         string   `json:"reason"`
	AgentID        string   `json:"agent_id"`
	SubagentID     string   `json:"subagent_id"`
	AgentType      string   `json:"agent_type"`
	LastAssistant  string   `json:"last_assistant_message"`
	// Grok Build's camelCase spellings (it sends both shapes).
	LastAssistantCamel  string `json:"lastAssistantMessage"`
	TranscriptPathCamel string `json:"transcriptPath"`
	WorkspaceRoot       string `json:"workspaceRoot"`
	PermissionMode      string `json:"permission_mode"`
	Effort              struct {
		Level string `json:"level"`
	} `json:"effort"`
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Normalize decodes a lifecycle payload. ok=false when the body is not a
// JSON object.
func Normalize(body []byte) (Payload, bool) {
	var r rawPayload
	if err := json.Unmarshal(body, &r); err != nil {
		return Payload{}, false
	}
	cwd := r.Cwd
	if cwd == "" && len(r.WorkspaceRoots) > 0 {
		cwd = r.WorkspaceRoots[0]
	}
	if cwd == "" {
		cwd = r.WorkspaceRoot
	}
	return Payload{
		SessionID:            firstNonEmpty(r.SessionID, r.SessionIDCamel, r.ConversationID),
		Cwd:                  cwd,
		TranscriptPath:       firstNonEmpty(r.TranscriptPath, r.TranscriptPathCamel),
		HookEventName:        r.HookEventName,
		Source:               r.Source,
		Reason:               r.Reason,
		AgentID:              firstNonEmpty(r.AgentID, r.SubagentID),
		AgentType:            r.AgentType,
		LastAssistantMessage: firstNonEmpty(r.LastAssistant, r.LastAssistantCamel),
		PermissionMode:       r.PermissionMode,
		EffortLevel:          r.Effort.Level,
	}, true
}

// Plan is what the lifecycle handler does for one invocation.
type Plan struct {
	// Row is the action row to record (spooled, DB-free), or nil.
	Row *models.ToolEvent
	// SeedPidbridge asks the caller to register the agent's process
	// ancestry for this session (proxy attribution). Only for a binding
	// whose process relation makes the ancestor walk meaningful.
	SeedPidbridge bool
	// SessionID is the resolved session id ("" when the payload had none:
	// the caller records nothing and seeds nothing).
	SessionID string
	// Cwd is the resolved working directory.
	Cwd string
}

// Handled reports whether the capability is one the lifecycle handler
// serves.
func Handled(c integration.HookCap) bool {
	for _, r := range rules {
		if r.cap == c {
			return true
		}
	}
	return false
}

// rule is one row of the lifecycle table: the first row whose capability
// and phase match decides the plan.
type rule struct {
	cap   integration.HookCap
	phase string
	// seed reports whether a session start seeds pidbridge.
	seed bool
	// build returns the row for this event, or ok=false for none.
	build func(tool string, p Payload, body []byte, now time.Time) (models.ToolEvent, bool)
}

// rules is the lifecycle table (audit section 2.2). A session START records
// no row: every transcript adapter this handler serves already owns the
// session_start row, and a second one would double-count; the start's
// value here is the pidbridge seed. A session END records session_end,
// the close internal/sessionend reads (the transcript carries no end
// marker). A turn END records task_complete when the payload carries the
// final assistant text (the turn terminus Claude Code's Stop hook also
// records). Sub-agent start / stop record observe-only fan-out rows.
var rules = []rule{
	{cap: integration.HookCapSessionLifecycle, phase: integration.HookPhaseStart, seed: true},
	{cap: integration.HookCapSessionLifecycle, phase: integration.HookPhaseEnd, build: buildSessionEnd},
	{cap: integration.HookCapTurnLifecycle, phase: integration.HookPhaseEnd, build: buildTurnEnd},
	{cap: integration.HookCapSubagentLifecycle, phase: integration.HookPhaseStart, build: buildSubagent(models.ActionSubagentStart, "subagent_start")},
	{cap: integration.HookCapSubagentLifecycle, phase: integration.HookPhaseEnd, build: buildSubagent(models.ActionSubagentStop, "subagent_stop")},
}

// Decide returns the plan for one lifecycle invocation of tool on binding b.
func Decide(tool string, b integration.HookBinding, body []byte, now time.Time) Plan {
	p, ok := Normalize(body)
	if !ok || p.SessionID == "" {
		return Plan{}
	}
	plan := Plan{SessionID: p.SessionID, Cwd: p.Cwd}
	for _, r := range rules {
		if r.cap != b.Cap || r.phase != b.Phase {
			continue
		}
		plan.SeedPidbridge = r.seed && b.ProcRel == integration.ProcDescendant
		if r.build != nil {
			if ev, ok := r.build(tool, p, body, now); ok {
				plan.Row = &ev
			}
		}
		return plan
	}
	return plan
}

// SourceFile is the source_file every lifecycle hook row carries: the
// "<tool>:hook" convention the Claude Code and Codex hook rows already use.
func SourceFile(tool string) string { return tool + ":hook" }

// IsHookSource reports whether an action row was written by tool's hook:
// its source_file is SourceFile(tool) or, on a metadata-only org row whose
// raw source_file was stripped, its source_file_hash is that value's
// sha256 hex (the hash every node and the org ingest stamp). Empty tool
// never matches.
func IsHookSource(tool, sourceFile, sourceFileHash string) bool {
	if tool == "" {
		return false
	}
	want := SourceFile(tool)
	if sourceFile != "" {
		return sourceFile == want
	}
	if sourceFileHash == "" {
		return false
	}
	sum := sha256.Sum256([]byte(want))
	return sourceFileHash == hex.EncodeToString(sum[:])
}

func base(tool string, p Payload, action, eventName string, now time.Time) models.ToolEvent {
	ev := models.ToolEvent{
		SourceFile:    SourceFile(tool),
		SourceEventID: p.SessionID + ":" + eventName,
		SessionID:     p.SessionID,
		ProjectRoot:   p.Cwd,
		Timestamp:     now.UTC(),
		Tool:          tool,
		ActionType:    action,
		Success:       true,
	}
	m := models.ActionMetadata{PermissionMode: p.PermissionMode, EffortLevel: p.EffortLevel}
	if !m.IsZero() {
		ev.Metadata = &m
	}
	return ev
}

// buildSessionEnd is byte-for-byte the row Claude Code's own SessionEnd
// builder writes (same source event id, so a re-fire upserts).
func buildSessionEnd(tool string, p Payload, _ []byte, now time.Time) (models.ToolEvent, bool) {
	ev := base(tool, p, models.ActionSessionEnd, "session_end", now)
	ev.Target = "session_ended"
	return ev, true
}

// previewMax bounds the turn-end preview stored in target / reasoning.
const previewMax = 200

func preview(s string) string {
	if len(s) <= previewMax {
		return s
	}
	return s[:previewMax]
}

// ContentHash is the per-occurrence discriminator for rows a session emits
// many times (Claude Code's claudeContentHash): deterministic, so a
// double-wired hook still collapses to one row.
func ContentHash(body []byte) string {
	h := fnv.New32a()
	_, _ = h.Write(body)
	return strconv.FormatUint(uint64(h.Sum32()), 16)
}

// maxAssistantBytes caps the stored final assistant text.
const maxAssistantBytes = 64 << 10

func buildTurnEnd(tool string, p Payload, body []byte, now time.Time) (models.ToolEvent, bool) {
	if strings.TrimSpace(p.LastAssistantMessage) == "" {
		return models.ToolEvent{}, false
	}
	ev := base(tool, p, models.ActionTaskComplete, "stop", now)
	ev.SourceEventID = p.SessionID + ":stop:" + ContentHash(body)
	scrubbed := scrub.New().String(p.LastAssistantMessage)
	pv := preview(scrubbed)
	ev.Target = pv
	ev.PrecedingReasoning = pv
	ev.RawToolName = tool + ".assistant_text"
	if len(scrubbed) > maxAssistantBytes {
		scrubbed = scrubbed[:maxAssistantBytes]
	}
	ev.ToolOutput = scrubbed
	return ev, true
}

func buildSubagent(action, eventName string) func(string, Payload, []byte, time.Time) (models.ToolEvent, bool) {
	return func(tool string, p Payload, body []byte, now time.Time) (models.ToolEvent, bool) {
		ev := base(tool, p, action, eventName, now)
		if p.AgentID != "" {
			ev.SourceEventID = p.AgentID + ":" + eventName
			ev.RawToolName = p.AgentID
			m := models.ActionMetadata{}
			if ev.Metadata != nil {
				m = *ev.Metadata
			}
			m.AgentID = p.AgentID
			ev.Metadata = &m
		} else {
			ev.SourceEventID = p.SessionID + ":" + eventName + ":" + ContentHash(body)
		}
		ev.Target = p.AgentType
		ev.IsSidechain = true
		return ev, true
	}
}
