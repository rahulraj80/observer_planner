package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// itemUserMessageRollout is a synthesized Codex Desktop 0.160 rollout in the
// item_completed-only shape (T-559): user prompts arrive ONLY as a role=user
// response_item followed by event_msg/item_completed{item.type=UserMessage};
// event_msg/user_message never appears. Three turns; the second carries an
// image attachment (input_image on the response_item, local_image on the
// item). All text is synthetic.
var itemUserMessageRollout = []string{
	`{"timestamp":"2026-10-06T08:03:08.966Z","type":"session_meta","payload":{"id":"thread-item-user","cwd":"/tmp/proj","originator":"Codex Desktop","cli_version":"0.160.1","source":"vscode","model":"gpt-6.1-sol"}}`,
	`{"timestamp":"2026-10-06T08:03:08.969Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1","started_at":1791273788}}`,
	`{"timestamp":"2026-10-06T08:03:09.604Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>synthetic</environment_context>"}]}}`,
	`{"timestamp":"2026-10-06T08:03:09.783Z","type":"turn_context","payload":{"turn_id":"turn-1","cwd":"/tmp/proj","model":"gpt-6.1-sol"}}`,
	`{"timestamp":"2026-10-06T08:03:12.748Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"first synthetic prompt"}]}}`,
	`{"timestamp":"2026-10-06T08:03:12.764Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"thread-item-user","turn_id":"turn-1","item":{"type":"UserMessage","id":"um-1","client_id":"c-1","content":[{"type":"text","text":"first synthetic prompt"}]}}}`,
	`{"timestamp":"2026-10-06T08:03:16.097Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"thread-item-user","turn_id":"turn-1","item":{"type":"AgentMessage","id":"am-1","content":[{"type":"Text","text":"first synthetic reply"}]}}}`,
	`{"timestamp":"2026-10-06T08:05:37.264Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1"}}`,
	`{"timestamp":"2026-10-06T08:09:36.632Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-2","started_at":1791274176}}`,
	`{"timestamp":"2026-10-06T08:09:36.692Z","type":"turn_context","payload":{"turn_id":"turn-2","cwd":"/tmp/proj","model":"gpt-6.1-sol"}}`,
	`{"timestamp":"2026-10-06T08:09:41.066Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"second synthetic prompt"},{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="}]}}`,
	`{"timestamp":"2026-10-06T08:09:41.090Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"thread-item-user","turn_id":"turn-2","item":{"type":"UserMessage","id":"um-2","client_id":"c-2","content":[{"type":"text","text":"second synthetic prompt"},{"type":"local_image","path":"/synthetic/shot.png"}]}}}`,
	`{"timestamp":"2026-10-06T08:09:46.471Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"thread-item-user","turn_id":"turn-2","item":{"type":"AgentMessage","id":"am-2","content":[{"type":"Text","text":"second synthetic reply"}]}}}`,
	`{"timestamp":"2026-10-06T08:12:57.226Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-3","started_at":1791274377}}`,
	`{"timestamp":"2026-10-06T08:12:57.284Z","type":"turn_context","payload":{"turn_id":"turn-3","cwd":"/tmp/proj","model":"gpt-6.1-sol"}}`,
	`{"timestamp":"2026-10-06T08:13:00.518Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"third synthetic prompt"}]}}`,
	`{"timestamp":"2026-10-06T08:13:00.543Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"thread-item-user","turn_id":"turn-3","item":{"type":"UserMessage","id":"um-3","client_id":"c-3","content":[{"type":"text","text":"third synthetic prompt"}]}}}`,
	`{"timestamp":"2026-10-06T08:13:06.834Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"thread-item-user","turn_id":"turn-3","item":{"type":"AgentMessage","id":"am-3","content":[{"type":"Text","text":"third synthetic reply"}]}}}`,
}

func writeItemUserRollout(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-2026-10-06T13-32-28-thread-item-user.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(itemUserMessageRollout, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func userPrompts(evs []models.ToolEvent) []models.ToolEvent {
	var out []models.ToolEvent
	for _, e := range evs {
		if e.ActionType == models.ActionUserPrompt {
			out = append(out, e)
		}
	}
	return out
}

// TestParseSessionFile_ItemCompletedUserMessage_T559 pins that every user
// prompt of an item_completed-only rollout is captured, keyed to its own
// turn, with the attachment on the turn that sent it.
func TestParseSessionFile_ItemCompletedUserMessage_T559(t *testing.T) {
	t.Parallel()
	dir, path := writeItemUserRollout(t)
	a := NewWithOptions(nil, dir)
	res, err := a.ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	prompts := userPrompts(res.ToolEvents)
	if len(prompts) != 3 {
		t.Fatalf("user prompts: %d want 3 (one per UserMessage item)", len(prompts))
	}
	wantText := []string{"first synthetic prompt", "second synthetic prompt", "third synthetic prompt"}
	for i, p := range prompts {
		if p.Target != wantText[i] {
			t.Errorf("prompt %d target = %q want %q", i, p.Target, wantText[i])
		}
		if want := "user:turn-" + string(rune('1'+i)); p.MessageID != want {
			t.Errorf("prompt %d MessageID = %q want %q", i, p.MessageID, want)
		}
		if p.SessionID != "thread-item-user" {
			t.Errorf("prompt %d SessionID = %q", i, p.SessionID)
		}
	}
	if len(prompts[0].UserAttachments) != 0 {
		t.Errorf("turn 1 must not carry turn 2's attachment: %+v", prompts[0].UserAttachments)
	}
	if len(prompts[1].UserAttachments) != 1 || prompts[1].UserAttachments[0].Kind != "image" ||
		prompts[1].UserAttachments[0].MediaType != "image/png" {
		t.Errorf("turn 2 attachments = %+v want one image/png", prompts[1].UserAttachments)
	}
	if len(prompts[2].UserAttachments) != 0 {
		t.Errorf("turn 3 attachments = %+v want none", prompts[2].UserAttachments)
	}
}

// TestParseSessionFile_ItemCompletedUserMessageResume_T559 pins that an
// incremental resume (the watcher tailing a live rollout) captures the later
// prompts with the SAME SourceEventIDs a whole-file parse produces, so a
// rescan cannot duplicate them.
func TestParseSessionFile_ItemCompletedUserMessageResume_T559(t *testing.T) {
	t.Parallel()
	dir, path := writeItemUserRollout(t)
	a := NewWithOptions(nil, dir)
	full, err := a.ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("full parse: %v", err)
	}
	// Cut after turn 1 (through its task_complete line).
	cut := int64(len(strings.Join(itemUserMessageRollout[:8], "\n")) + 1)
	tail, err := a.ParseSessionFile(context.Background(), path, cut)
	if err != nil {
		t.Fatalf("resume parse: %v", err)
	}
	fullPrompts := userPrompts(full.ToolEvents)
	tailPrompts := userPrompts(tail.ToolEvents)
	if len(tailPrompts) != 2 {
		t.Fatalf("resumed prompts: %d want 2", len(tailPrompts))
	}
	for i, p := range tailPrompts {
		want := fullPrompts[i+1]
		if p.SourceEventID != want.SourceEventID || p.MessageID != want.MessageID {
			t.Errorf("resumed prompt %d ids = (%q,%q) want (%q,%q)", i,
				p.SourceEventID, p.MessageID, want.SourceEventID, want.MessageID)
		}
		if p.SessionID != "thread-item-user" {
			t.Errorf("resumed prompt %d SessionID = %q", i, p.SessionID)
		}
	}
}
