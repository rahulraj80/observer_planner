package mistralcode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/models"
)

// This file implements the THIRD on-disk layout this package captures: the
// vibe "unified harness" session store that vibe 2.25 writes INSTEAD of the
// legacy session_<ts>_<8hex>/ directory (grounded read-only on demo node 1,
// vibe 2.25.8, 2026-10-03, against both a live session and vibe's own
// mistralai_vibe_local_harness/vibe/_storage.py + _projection.py):
//
//	<save_dir>/unified/<session-uuid>/
//	  CURRENT                       pointer {generation, snapshot_sequence,
//	                                manifest_sha256, store_format}; replaced
//	                                atomically on every publication
//	  meta.json                     start/end time, working_directory, branch
//	  generations/<16-digit>/       the newest TWO publications are kept
//	    manifest.json               names the documents below + the journal
//	                                segment that continues this snapshot
//	    projection-state.json       the public session state: session envelope
//	                                (cumulative tokenUsage, latestTurn) and the
//	                                history entries — DETACHED into chunks/
//	    runtime-state.json          session_metadata {active_model, cwd},
//	                                import_provenance
//	    checkpoint.json             the model-facing context (never read)
//	  chunks/<sha256>.json          content-addressed runs of history entries,
//	                                concatenated in manifest order
//	  journal/<16-digit>.jsonl      append-only recovery journal; the
//	                                `projection_delta` records carry the
//	                                history ops published since the snapshot.
//	                                Superseded segments are DELETED (only the
//	                                newest two are kept), so the journal is
//	                                never a durable source on its own.
//
// The adapter tracks CURRENT. A parse resolves exactly what vibe's own loader
// resolves (CURRENT -> manifest -> projection-state + chunks, then replays the
// manifest's journal segment), so it sees everything published so far,
// including deltas journaled mid-turn. Every history entry carries a stable
// id (`user` messages the client message id, `assistant-<action>`,
// `effect-<action>`, ...), so re-parsing the whole store each pass collapses on
// the store's (source_file, source_event_id) uniqueness exactly like the IDE
// layout's whole-document reparse.
//
// Tokens stay SESSION-LEVEL: the envelope's tokenUsage is the cumulative
// {inputTokens (GROSS, prompt_tokens), outputTokens, cachedInputTokens}; per
// call usage only ever lives in the transient journal, so one MAX-upgraded
// session row is emitted, the legacy layout's tier.
//
// Off-limits, as for the legacy layout: nothing outside the session directory
// is read (no ~/.vibe/.env, config.toml, vibehistory, caches), and inside it
// checkpoint.json and the checkpoint chunks are never decoded.

const (
	unifiedDirName     = "unified"
	unifiedPointerName = "CURRENT"
	// unifiedMaxDocBytes mirrors vibe's own document cap (_MAX_DOCUMENT_BYTES).
	unifiedMaxDocBytes = 64 << 20
	// unifiedOutputCap bounds a tool's rendered output carried as an excerpt.
	unifiedOutputCap = 4000
)

// matchesUnifiedShape reports whether path is a unified-store pointer file:
// <save_dir>/unified/<session-id>/CURRENT, where save_dir ends in
// logs/session (so a $VIBE_HOME store matches as well as ~/.vibe).
func matchesUnifiedShape(path string) bool {
	norm := strings.ReplaceAll(path, `\`, "/")
	if filepath.Base(norm) != unifiedPointerName {
		return false
	}
	sessDir := filepath.Dir(filepath.FromSlash(norm))
	if strings.TrimSpace(filepath.Base(sessDir)) == "" {
		return false
	}
	if !strings.EqualFold(filepath.Base(filepath.Dir(sessDir)), unifiedDirName) {
		return false
	}
	return strings.Contains(strings.ToLower(norm), "/logs/session/"+unifiedDirName+"/")
}

// unifiedPointer is CURRENT.
type unifiedPointer struct {
	Generation       string `json:"generation"`
	SessionID        string `json:"session_id"`
	SnapshotSequence int64  `json:"snapshot_sequence"`
}

// unifiedDocRef names one generation document (+ its detached chunks).
type unifiedDocRef struct {
	Path   string   `json:"path"`
	Chunks []string `json:"chunks"`
}

// unifiedManifest is generations/<gen>/manifest.json.
type unifiedManifest struct {
	SessionID              string        `json:"session_id"`
	Generation             string        `json:"generation"`
	ProjectionState        unifiedDocRef `json:"projection_state"`
	RuntimeState           unifiedDocRef `json:"runtime_state"`
	RecoveryJournalSegment struct {
		Path          string `json:"path"`
		FirstSequence int64  `json:"first_sequence"`
	} `json:"recovery_journal_segment"`
}

// unifiedEnvelope is the public session state minus its history entries.
type unifiedEnvelope struct {
	LatestTurn *unifiedTurn `json:"latestTurn"`
	Session    struct {
		ID         string             `json:"id"`
		CreatedAt  int64              `json:"createdAt"`
		UpdatedAt  int64              `json:"updatedAt"`
		TokenUsage *unifiedTokenUsage `json:"tokenUsage"`
	} `json:"session"`
}

// unifiedSnapshot is the envelope plus its history entries.
type unifiedSnapshot struct {
	unifiedEnvelope
	History struct {
		Entries []json.RawMessage `json:"entries"`
	} `json:"history"`
}

// unifiedTurn is the envelope's latestTurn.
type unifiedTurn struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	StartedAt   int64  `json:"startedAt"`
	CompletedAt int64  `json:"completedAt"`
	Error       *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details struct {
			Model string `json:"model"`
		} `json:"details"`
	} `json:"error"`
}

// unifiedTokenUsage is the cumulative session usage (vibe's TokenUsage).
// inputTokens is the provider's prompt_tokens: GROSS, cached included.
type unifiedTokenUsage struct {
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	TotalTokens       int64 `json:"totalTokens"`
	CachedInputTokens int64 `json:"cachedInputTokens"`
}

// unifiedRuntime is the subset of runtime-state.json this adapter reads.
type unifiedRuntime struct {
	SessionMetadata struct {
		ActiveModel *string `json:"active_model"`
		CWD         string  `json:"cwd"`
	} `json:"session_metadata"`
	ImportProvenance *struct {
		Source struct {
			Backend string `json:"backend"`
		} `json:"source"`
		ImportedAt string `json:"imported_at"`
	} `json:"import_provenance"`
}

// unifiedEntry is one public history entry (the fields this adapter reads).
type unifiedEntry struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
	TurnID           string          `json:"turnId"`
	CreatedAt        int64           `json:"createdAt"`
	GenerationStatus string          `json:"generationStatus"`
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	Text             string          `json:"text"`
	Kind             string          `json:"kind"`
	Outcome          *struct {
		Type string `json:"type"`
	} `json:"outcome"`
	Details struct {
		Model string `json:"model"`
	} `json:"details"`
	Detail *struct {
		Kind     string          `json:"kind"`
		ToolName string          `json:"toolName"`
		Input    json.RawMessage `json:"input"`
	} `json:"detail"`
	State *struct {
		Status     string `json:"status"`
		OutputText string `json:"outputText"`
		Reason     string `json:"reason"`
		Error      *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"state"`
}

// unifiedOp is one projection delta op (vibe's ProjectionOp union).
type unifiedOp struct {
	Op      string            `json:"op"`
	ID      string            `json:"id"`
	Entry   json.RawMessage   `json:"entry"`
	Entries []json.RawMessage `json:"entries"`
	State   json.RawMessage   `json:"state"`
}

// unifiedJournalRecord is one recovery-journal line.
type unifiedJournalRecord struct {
	Type     string `json:"type"`
	Sequence int64  `json:"sequence"`
	Payload  struct {
		Delta []unifiedOp `json:"delta"`
	} `json:"payload"`
}

// unifiedStore is a fully resolved unified session: the replayed history and
// the envelope, plus the runtime/meta facts the events need.
type unifiedStore struct {
	sessionUUID string
	entries     []json.RawMessage
	envelope    unifiedEnvelope
	runtime     unifiedRuntime
	meta        vibeMeta
}

// errUnifiedRetry marks a store caught between publications (a generation
// collected under the read): the caller retries on a later pass.
var errUnifiedRetry = errors.New("unified store changed during read")

// parseUnifiedSession parses one unified store, addressed by its CURRENT.
// fromOffset is ignored: the store is resolved whole every pass and NewOffset
// is the pointer's size (see CursorSemanticsFor).
func (a *Adapter) parseUnifiedSession(ctx context.Context, pointerPath string) (adapter.ParseResult, error) {
	var res adapter.ParseResult
	st, size, err := loadUnifiedStore(pointerPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errUnifiedRetry) {
			res.RetrySuggested = true
			return res, nil
		}
		res.Warnings = append(res.Warnings, "unified store: "+err.Error())
		return res, nil
	}
	if err := ctx.Err(); err != nil {
		return adapter.ParseResult{}, err
	}
	res.NewOffset = size
	a.emitUnified(pointerPath, st, &res)
	return res, nil
}

// loadUnifiedStore resolves CURRENT -> manifest -> projection (+ chunks) ->
// journal replay, the order vibe's own _load uses. size is CURRENT's length.
func loadUnifiedStore(pointerPath string) (unifiedStore, int64, error) {
	sessDir := filepath.Dir(pointerPath)
	var st unifiedStore
	st.sessionUUID = filepath.Base(sessDir)

	pointerBytes, err := readCapped(pointerPath)
	if err != nil {
		return st, 0, err
	}
	var ptr unifiedPointer
	if err := json.Unmarshal(pointerBytes, &ptr); err != nil {
		return st, 0, fmt.Errorf("CURRENT: %w", err)
	}
	if !isGenerationName(ptr.Generation) {
		return st, 0, fmt.Errorf("CURRENT names an invalid generation %q", ptr.Generation)
	}
	if ptr.SessionID != "" {
		st.sessionUUID = ptr.SessionID
	}
	genDir := filepath.Join(sessDir, "generations", ptr.Generation)
	var man unifiedManifest
	if err := readJSONDoc(filepath.Join(genDir, "manifest.json"), &man); err != nil {
		// The generation CURRENT names was collected after a newer publication
		// replaced CURRENT under this read: retry rather than warn.
		if errors.Is(err, os.ErrNotExist) {
			return st, 0, errUnifiedRetry
		}
		return st, 0, fmt.Errorf("manifest: %w", err)
	}

	projPath, err := withinDir(genDir, man.ProjectionState.Path)
	if err != nil {
		return st, 0, err
	}
	var snap unifiedSnapshot
	if err := readJSONDoc(projPath, &struct {
		Snapshot *unifiedSnapshot `json:"snapshot"`
	}{&snap}); err != nil {
		return st, 0, fmt.Errorf("projection-state: %w", err)
	}
	entries := snap.History.Entries
	if man.ProjectionState.Chunks != nil {
		entries = entries[:0:0]
		for _, digest := range man.ProjectionState.Chunks {
			chunk, err := readChunk(sessDir, digest)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return st, 0, errUnifiedRetry
				}
				return st, 0, err
			}
			entries = append(entries, chunk...)
		}
	}
	st.entries = entries
	st.envelope = snap.unifiedEnvelope

	if man.RuntimeState.Path != "" {
		if rtPath, err := withinDir(genDir, man.RuntimeState.Path); err == nil {
			_ = readJSONDoc(rtPath, &st.runtime) // optional: model + cwd fallbacks only
		}
	}
	if seg := man.RecoveryJournalSegment.Path; seg != "" {
		journalPath, err := withinDir(sessDir, seg)
		if err != nil {
			return st, 0, err
		}
		if err := st.replayJournal(journalPath, man.RecoveryJournalSegment.FirstSequence); err != nil {
			return st, 0, err
		}
	}
	if data, err := readCapped(filepath.Join(sessDir, metaName)); err == nil {
		_ = json.Unmarshal(data, &st.meta)
	}
	return st, int64(len(pointerBytes)), nil
}

// replayJournal folds the segment's projection_delta records over the
// snapshot, exactly as vibe's apply_projection_delta does. Only COMPLETE lines
// are applied (a half-written tail lands on a later pass), records below the
// segment's first sequence are ignored, and an op that names an absent entry
// is skipped rather than failing the session (vibe would refuse the store; a
// reader must not lose the rest of the history over it).
func (st *unifiedStore) replayJournal(path string, firstSeq int64) error {
	f, err := os.Open(path) //nolint:gosec // inside the watched session dir
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // a freshly published generation's segment may not exist yet
		}
		return fmt.Errorf("journal: %w", err)
	}
	defer f.Close()
	r := bufio.NewReaderSize(io.LimitReader(f, unifiedMaxDocBytes), 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && bytes.HasSuffix(line, []byte("\n")) {
			st.applyJournalLine(bytes.TrimSpace(line), firstSeq)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("journal: %w", err)
		}
	}
}

func (st *unifiedStore) applyJournalLine(line []byte, firstSeq int64) {
	if len(line) == 0 {
		return
	}
	var rec unifiedJournalRecord
	if json.Unmarshal(line, &rec) != nil || rec.Type != "projection_delta" || rec.Sequence < firstSeq {
		return
	}
	for _, op := range rec.Payload.Delta {
		st.applyOp(op)
	}
}

func (st *unifiedStore) applyOp(op unifiedOp) {
	switch op.Op {
	case "append_entry":
		if len(op.Entry) > 0 {
			st.entries = append(st.entries, op.Entry)
		}
	case "replace_entry":
		for i, raw := range st.entries {
			if entryID(raw) == op.ID {
				st.entries[i] = op.Entry
				return
			}
		}
	case "remove_entry":
		kept := st.entries[:0:0]
		for _, raw := range st.entries {
			if entryID(raw) != op.ID {
				kept = append(kept, raw)
			}
		}
		st.entries = kept
	case "set_history_entries":
		st.entries = append([]json.RawMessage(nil), op.Entries...)
	case "set_envelope":
		var env unifiedEnvelope
		if json.Unmarshal(op.State, &env) == nil {
			st.envelope = env
		}
	}
}

func entryID(raw json.RawMessage) string {
	var e struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &e)
	return e.ID
}

// emitUnified turns the resolved store into events. The history is walked in
// order; reasoning is carried onto the next assistant message or tool call of
// the same turn, the way the legacy layout attaches reasoning_content.
func (a *Adapter) emitUnified(sourceFile string, st unifiedStore, res *adapter.ParseResult) {
	sessID := unifiedSessionID(st.sessionUUID)
	cwd := firstNonEmpty(st.meta.Environment.WorkingDirectory, st.runtime.SessionMetadata.CWD)
	root, branch, projectIdentity := resolveProjectRoot(cwd)
	if branch == "" {
		branch = st.meta.GitBranch
	}
	importedBefore := importCutoff(st.runtime)
	base := unifiedBase{source: sourceFile, sessID: sessID, root: root, branch: branch}

	reasoning := map[string]string{} // turn id -> pending reasoning
	var lastModelChange string
	for _, raw := range st.entries {
		var e unifiedEntry
		if json.Unmarshal(raw, &e) != nil || e.ID == "" {
			continue
		}
		if e.Type == "checkpoint" && e.Kind == "model_change" && e.Details.Model != "" {
			lastModelChange = e.Details.Model
		}
		if !importedBefore.IsZero() && !time.UnixMilli(e.CreatedAt).After(importedBefore) {
			continue // carried over from the imported legacy session, captured there
		}
		if ev, ok := a.unifiedEvent(base, e, reasoning); ok {
			res.ToolEvents = append(res.ToolEvents, ev)
		}
	}
	if ev, ok := a.unifiedTurnError(base, st.envelope.LatestTurn); ok {
		res.ToolEvents = append(res.ToolEvents, ev)
	}
	model := unifiedModel(st, lastModelChange)
	if tk, ok := unifiedTokenEvent(base, st.envelope, model); ok {
		res.TokenEvents = append(res.TokenEvents, tk)
	}
	adapter.ApplyProjectIdentity(res, projectIdentity)
	if sessID != "" {
		// Same producer family as the legacy layout (the `vibe` binary and
		// its bundled vibe-acp write this store); stamped like it.
		res.SessionSurfaces = append(res.SessionSurfaces, models.SessionSurface{
			SessionID:   sessID,
			Surface:     models.SurfaceCLI,
			SurfaceHost: surfaceHostCLI,
		})
	}
}

// unifiedBase is the per-session context every unified event shares.
type unifiedBase struct {
	source, sessID, root, branch string
}

func (b unifiedBase) event(id string, at int64, action string) models.ToolEvent {
	return models.ToolEvent{
		SourceFile: b.source, SourceEventID: id, SessionID: b.sessID,
		ProjectRoot: b.root, GitBranch: b.branch, Timestamp: time.UnixMilli(at).UTC(),
		Tool: models.ToolMistralCode, ActionType: action, Success: true,
	}
}

// unifiedEvent maps one history entry to at most one event. Entries that are
// still in progress are deferred (their committed form replaces them under the
// same id), and assistant content the harness discarded is never recorded as
// an answer.
func (a *Adapter) unifiedEvent(b unifiedBase, e unifiedEntry, reasoning map[string]string) (models.ToolEvent, bool) {
	if e.GenerationStatus != "" && e.GenerationStatus != "completed" {
		return models.ToolEvent{}, false
	}
	discarded := e.Outcome != nil && e.Outcome.Type == "discarded"
	switch {
	case e.Type == "reasoning":
		if !discarded && e.Text != "" {
			reasoning[e.TurnID] = truncate(a.scrubber.String(e.Text), maxReasoningLen)
		}
	case e.Type == "message" && e.Role == "user":
		text := a.scrubber.String(unifiedContentText(e.Content))
		if text == "" {
			return models.ToolEvent{}, false
		}
		ev := b.event("user:"+b.sessID+":"+e.ID, e.CreatedAt, models.ActionUserPrompt)
		ev.Target = truncate(text, maxTargetLen)
		return ev, true
	case e.Type == "message" && e.Role == "assistant" && !discarded:
		text := a.scrubber.String(unifiedContentText(e.Content))
		if text == "" {
			return models.ToolEvent{}, false
		}
		ev := b.event("asst:"+b.sessID+":"+e.ID, e.CreatedAt, models.ActionAssistantMessage)
		ev.Target = truncate(text, maxTargetLen)
		ev.PrecedingReasoning = reasoning[e.TurnID]
		delete(reasoning, e.TurnID)
		return ev, true
	case e.Type == "effect" && e.Detail != nil && e.Detail.ToolName != "":
		args := string(e.Detail.Input)
		action, target := mapUnifiedTool(e.Detail.ToolName, args)
		ev := b.event("tool:"+b.sessID+":"+e.ID, e.CreatedAt, action)
		ev.RawToolName = e.Detail.ToolName
		if len(e.Detail.Input) > 0 && string(e.Detail.Input) != "null" {
			ev.RawToolInput = a.scrubber.RawJSON(e.Detail.Input)
		}
		ev.Target = truncate(a.scrubber.String(target), maxTargetLen)
		ev.PrecedingReasoning = reasoning[e.TurnID]
		delete(reasoning, e.TurnID)
		if s := e.State; s != nil {
			ev.Success = s.Status == "completed"
			out := s.OutputText
			if !ev.Success {
				out = firstNonEmpty(errMessage(s.Error), s.Reason, out)
			}
			ev.ToolOutput = truncate(a.scrubber.String(out), unifiedOutputCap)
		}
		return ev, true
	}
	return models.ToolEvent{}, false
}

func errMessage(e *struct {
	Message string `json:"message"`
},
) string {
	if e == nil {
		return ""
	}
	return e.Message
}

// unifiedTurnError records the envelope's latest turn when it FAILED (a
// provider error such as a 429): vibe keeps no history entry for it, and the
// envelope only ever holds the latest turn, so this is the one durable trace.
func (a *Adapter) unifiedTurnError(b unifiedBase, t *unifiedTurn) (models.ToolEvent, bool) {
	if t == nil || t.Status != "failed" || t.ID == "" || t.Error == nil {
		return models.ToolEvent{}, false
	}
	ev := b.event("turnerr:"+b.sessID+":"+t.ID, firstNonZero(t.CompletedAt, t.StartedAt), models.ActionAPIError)
	ev.Success = false
	ev.Target = truncate(firstNonEmpty(t.Error.Code, "turn_failed"), maxTargetLen)
	ev.ErrorMessage = truncate(a.scrubber.String(t.Error.Message), unifiedOutputCap)
	return ev, true
}

// unifiedTokenEvent builds the single session-level token row from the
// envelope's cumulative tokenUsage (GROSS input netted against cached).
func unifiedTokenEvent(b unifiedBase, env unifiedEnvelope, model string) (models.TokenEvent, bool) {
	u := env.Session.TokenUsage
	if u == nil || (u.InputTokens == 0 && u.OutputTokens == 0 && u.CachedInputTokens == 0) {
		return models.TokenEvent{}, false
	}
	net := u.InputTokens - u.CachedInputTokens
	if net < 0 {
		net = 0
	}
	return models.TokenEvent{
		SourceFile: b.source, SourceEventID: "tokens:session:" + b.sessID, SessionID: b.sessID,
		ProjectRoot: b.root, Timestamp: time.UnixMilli(firstNonZero(env.Session.UpdatedAt, env.Session.CreatedAt)).UTC(),
		Tool: models.ToolMistralCode, Model: model,
		InputTokens: net, OutputTokens: u.OutputTokens, CacheReadTokens: u.CachedInputTokens,
		Source: models.TokenSourceJSONL, Reliability: models.ReliabilityApproximate,
	}, true
}

// unifiedModelRungs is the ordered model-resolution ladder (first non-empty
// wins). Nothing is guessed: with no rung answering the model stays empty.
var unifiedModelRungs = []func(unifiedStore, string) string{
	// The session's explicit model (set by /model or a resume); null means
	// "the configured default", which this adapter cannot read.
	func(st unifiedStore, _ string) string {
		if m := st.runtime.SessionMetadata.ActiveModel; m != nil {
			return strings.TrimSpace(*m)
		}
		return ""
	},
	// The last in-session model switch recorded in the history.
	func(_ unifiedStore, lastChange string) string { return lastChange },
	// The model a failed latest turn actually called (the provider error
	// names it).
	func(st unifiedStore, _ string) string {
		if t := st.envelope.LatestTurn; t != nil && t.Error != nil {
			return strings.TrimSpace(t.Error.Details.Model)
		}
		return ""
	},
}

func unifiedModel(st unifiedStore, lastChange string) string {
	for _, rung := range unifiedModelRungs {
		if m := rung(st, lastChange); m != "" {
			return m
		}
	}
	return ""
}

// unifiedSessionID is the 8-character handle `vibe --resume` accepts for a
// unified session (vibe's _SHORT_SESSION_ID_LENGTH prefix match), the same
// shape the legacy layout keys on.
func unifiedSessionID(uuid string) string {
	uuid = strings.TrimSpace(uuid)
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}

// importCutoff returns the import time of a session vibe imported from the
// LEGACY store; entries at or before it are the legacy session's history,
// which the legacy layout already captured.
func importCutoff(rt unifiedRuntime) time.Time {
	p := rt.ImportProvenance
	if p == nil || p.Source.Backend != "legacy" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, p.ImportedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// unifiedToolActions maps vibe's unified tool names (grounded off vibe
// 2.25.8's _unified_tool_projection.py call/label tables) to action types.
// Bare names fall back to the legacy table (mapVibeTool).
var unifiedToolActions = map[string]string{
	"file_system.read_file":           models.ActionReadFile,
	"file_system.write_file":          models.ActionWriteFile,
	"file_system.search_replace":      models.ActionEditFile,
	"search_replace":                  models.ActionEditFile,
	"file_system.bash":                models.ActionRunCommand,
	"process.start":                   models.ActionRunCommand,
	"process.output":                  models.ActionRunCommand,
	"process.list":                    models.ActionRunCommand,
	"process.stop":                    models.ActionRunCommand,
	"process.write":                   models.ActionStdinWrite,
	"skill.read":                      models.ActionSkillInvoke,
	"ui.ask_user_question":            models.ActionAskUser,
	"vibe.todo":                       models.ActionTodoUpdate,
	"subagent.spawn":                  models.ActionSpawnSubagent,
	"subagent.wait":                   models.ActionSubagentWait,
	"subagent.send_message":           models.ActionAgentMessage,
	"subagent.interrupt":              models.ActionAgentControl,
	"subagent.stop":                   models.ActionAgentControl,
	"search_tool_functions":           models.ActionToolSearch,
	"vibe.unified_harness_scratchpad": models.ActionUnknown,
}

func mapUnifiedTool(name, args string) (string, string) {
	if action, ok := unifiedToolActions[name]; ok {
		return action, targetFromArgs(args)
	}
	return mapVibeTool(name, args)
}

// unifiedContentText joins a message's text blocks.
func unifiedContentText(raw json.RawMessage) string {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return jsonToText(nil, raw)
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func isGenerationName(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isChunkDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// withinDir joins a manifest-relative path and refuses one that escapes dir
// (vibe's store rejects the same; a tampered manifest must not steer a read).
func withinDir(dir, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid store path %q", rel)
	}
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if r, err := filepath.Rel(dir, p); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("store path %q escapes the session directory", rel)
	}
	return p, nil
}

func readChunk(sessDir, digest string) ([]json.RawMessage, error) {
	if !isChunkDigest(digest) {
		return nil, fmt.Errorf("invalid chunk digest %q", digest)
	}
	var items []json.RawMessage
	if err := readJSONDoc(filepath.Join(sessDir, "chunks", digest+".json"), &items); err != nil {
		return nil, fmt.Errorf("chunk %s: %w", digest, err)
	}
	return items, nil
}

func readJSONDoc(path string, v any) error {
	data, err := readCapped(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// readCapped reads a store document, refusing one past vibe's own cap and
// any symlink (vibe's loader rejects both).
func readCapped(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s: refusing a symbolic link", filepath.Base(path))
	}
	if fi.Size() > unifiedMaxDocBytes {
		return nil, fmt.Errorf("%s: %d bytes exceeds the %d-byte document cap", filepath.Base(path), fi.Size(), unifiedMaxDocBytes)
	}
	return os.ReadFile(path) //nolint:gosec // inside the watched session dir, size-capped above
}

func firstNonZero(vals ...int64) int64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

// CursorSemanticsFor implements adapter.CursorSemantics. Only the unified
// pointer differs from the byte-offset default: its persisted cursor is the
// size of CURRENT, a fingerprint of the publication last read, not a byte
// position in the data parsed (every pass re-resolves the whole store).
func (a *Adapter) CursorSemanticsFor(path string) adapter.FileCursorSemantics {
	if classifyLayout(path) == layoutUnified {
		return adapter.FileCursorSemantics{
			Kind: adapter.CursorWatermark,
			Detail: "vibe unified-store pointer (CURRENT): every pass re-reads the generation it names " +
				"(projection snapshot, chunks, journal); the cursor is the pointer size, not a byte lag",
		}
	}
	return adapter.FileCursorSemantics{}
}
