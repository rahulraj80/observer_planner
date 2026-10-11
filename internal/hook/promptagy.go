package hook

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Antigravity CLI (`agy`) PreInvocation: the near-pre-send WARN seam
// (audit proposal P1, operator ruling 2026-10-02).
//
// Grounding (antigravity.google/docs/hooks, fetched 2026-10-02): agy's
// hooks are PreToolUse / PostToolUse / PreInvocation / PostInvocation /
// Stop. None sees or can block the user's prompt. PreInvocation runs
// before every model invocation; its stdin carries conversationId and
// transcriptPath (<app_data_dir>/brain/<conversationId>/.system_generated/
// logs/transcript.jsonl), and its only output is `injectSteps`, whose
// `ephemeralMessage` is "a transient system message" to the MODEL; the
// docs say hook output is not shown to the user. So the strongest action
// this seam can deliver is a warning: the model is told, and the
// developer gets the desktop toast (guard.PromptSeamWarnOnly is
// WarnIndirect). It cannot stop the first model call.
//
// Transcript shape (live-grounded on demo node 1, agy session
// b9cba337, 2026-10-02): one JSON object per line; the user's prompt is
// {"type":"USER_INPUT","source":"USER_EXPLICIT","content":
// "<USER_REQUEST>\n...\n</USER_REQUEST>", ...}.

// agyTranscriptTailBytes bounds how much of a long transcript the hook
// reads: only the tail, where the latest user input is.
const agyTranscriptTailBytes = 4 << 20

// agyTranscriptNames are the transcript file names agy has pointed
// PreInvocation's transcriptPath at: transcript.jsonl (agy 1.2.16, demo node
// 1, 2026-10-03) and transcript_full.jsonl (agy 1.3.0, demo node 1,
// 2026-10-06, T-476: the USER_INPUT step is already in it at invocationNum
// 0, before the first model call). Any other file name is not read.
var agyTranscriptNames = map[string]bool{
	"transcript.jsonl":      true,
	"transcript_full.jsonl": true,
}

type agyPreInvocationPayload struct {
	ConversationID string `json:"conversationId"`
	TranscriptPath string `json:"transcriptPath"`
}

type agyTranscriptStep struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// extractAntigravityPreInvocationPrompt reads the latest user input from
// the transcript the payload points at. A payload without a readable
// transcript (none yet, or not a known transcript file) returns ok=false: the
// caller replies without scanning, and the post-hoc capture lane still
// covers the prompt. Firing the FIX-2 "schema drift" degrade here would
// warn on every model invocation of every turn.
func extractAntigravityPreInvocationPrompt(body []byte) (prompt, sessionID string, ok, fieldMissing bool) {
	var p agyPreInvocationPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return "", "", false, false
	}
	text, found := latestAgyUserInput(p.TranscriptPath)
	if !found {
		return "", p.ConversationID, false, false
	}
	return text, p.ConversationID, true, false
}

// latestAgyUserInput returns the content of the last USER_INPUT step in
// an agy transcript, without its <USER_REQUEST> wrapper. Only a file named
// in agyTranscriptNames is read, and only its tail.
func latestAgyUserInput(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		path = filepath.Join(home, path[2:])
	}
	if !agyTranscriptNames[filepath.Base(path)] {
		return "", false
	}
	f, err := os.Open(path) //nolint:gosec // agy-supplied transcript path, basename-checked above
	if err != nil {
		return "", false
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > agyTranscriptTailBytes {
		if _, err := f.Seek(st.Size()-agyTranscriptTailBytes, io.SeekStart); err != nil {
			return "", false
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), agyTranscriptTailBytes)
	latest, found := "", false
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"USER_INPUT"`) {
			continue
		}
		var step agyTranscriptStep
		if err := json.Unmarshal(line, &step); err != nil || step.Type != "USER_INPUT" {
			continue
		}
		latest, found = unwrapAgyUserRequest(step.Content), true
	}
	return latest, found
}

// unwrapAgyUserRequest strips agy's <USER_REQUEST> wrapper.
func unwrapAgyUserRequest(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<USER_REQUEST>")
	s = strings.TrimSuffix(s, "</USER_REQUEST>")
	return strings.TrimSpace(s)
}

// agyInjectStep / agyPreInvocationReply are PreInvocation's documented
// output: {"injectSteps":[{"ephemeralMessage": "..."}]}.
type agyInjectStep struct {
	EphemeralMessage string `json:"ephemeralMessage"`
}

type agyPreInvocationReply struct {
	InjectSteps []agyInjectStep `json:"injectSteps,omitempty"`
}

// buildAntigravityPreInvocationReply tells the model (the only audience
// the seam reaches) about the finding and asks it to relay the notice to
// the developer. The text names detector types only, never the value.
func buildAntigravityPreInvocationReply(out promptReplyOutcome) any {
	if !out.Warn && !out.Blocking {
		return agyPreInvocationReply{}
	}
	msg := "SuperBased Observer guard notice for the user: " + out.Reason +
		" Before continuing, tell the user about this notice in one sentence. Do not repeat, store, or use the flagged value."
	return agyPreInvocationReply{InjectSteps: []agyInjectStep{{EphemeralMessage: msg}}}
}
