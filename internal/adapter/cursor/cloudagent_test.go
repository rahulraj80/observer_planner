package cursor

import (
	"context"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/scrub"
)

// Anonymized ids and text only: no real payload text appears below.
const (
	testCloudID = "bc-0a0b0c0d-1111-5222-8333-444455556666"
	testDraftID = "draft-0a0b0c0d-1111-4222-8333-444455556666"
	testLocalID = "0a0b0c0d-1111-4222-8333-444455556666"
)

// cloudPayload builds a Cursor 3.23.12-shaped hook payload: root-less,
// generation_id empty, is_background_agent false (as observed live).
func cloudPayload(event, conversationID, extra string) []byte {
	return []byte(`{"conversation_id":"` + conversationID + `","generation_id":"",` + extra +
		`"model":"cursor-grok-4.6-high","is_background_agent":false,"composer_mode":"chat",` +
		`"session_id":"` + conversationID + `","hook_event_name":"` + event + `",` +
		`"cursor_version":"3.23.12","workspace_roots":[],"user_email":"dev@example.com","transcript_path":null}`)
}

func TestClassifyConversationTable(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		bg     bool
		remote bool
		boot   bool
	}{
		{"cloud agent id", testCloudID, false, true, true},
		{"cloud agent id with flag", testCloudID, true, true, true},
		{"draft never bootstraps", testDraftID, false, false, false},
		{"draft wins over the flag", testDraftID, true, false, false},
		{"local chat", testLocalID, false, false, false},
		{"flag alone marks a cloud agent", testLocalID, true, true, true},
		{"empty id", "", false, false, false},
	}
	for _, tc := range cases {
		got := classifyConversation(tc.id, tc.bg)
		if got.RemoteTurns != tc.remote || got.BootstrapsRootless != tc.boot {
			t.Errorf("%s: classifyConversation(%q, %v) = %+v, want remote=%v boot=%v", tc.name, tc.id, tc.bg, got, tc.remote, tc.boot)
		}
	}
}

// TestBuildEventCloudAgentLifecycleBootstraps is the red/green pin for the
// 3.23.12 gap: a Cloud Agent's root-less sessionStart / sessionEnd used to
// return an empty root (the hook then dropped it, "missing project_root"),
// so the conversation never got a session row.
func TestBuildEventCloudAgentLifecycleBootstraps(t *testing.T) {
	isolateStash(t)
	cases := []struct {
		event, id, extra string
		wantRoot         string
		wantAction       string
	}{
		{EventSessionStart, testCloudID, ``, SyntheticProjectRoot, models.ActionSessionStart},
		{EventSessionEnd, testCloudID, `"reason":"completed",`, SyntheticProjectRoot, models.ActionSessionEnd},
		// Today's exclusion stays for drafts and folder-less local chats.
		{EventSessionStart, testDraftID, ``, "", models.ActionSessionStart},
		{EventSessionEnd, testDraftID, ``, "", models.ActionSessionEnd},
		{EventSessionStart, testLocalID, ``, "", models.ActionSessionStart},
	}
	for _, tc := range cases {
		ev, ok, err := BuildEvent(tc.event, cloudPayload(tc.event, tc.id, tc.extra), scrub.New())
		if err != nil || !ok {
			t.Fatalf("%s %s: ok=%v err=%v", tc.event, tc.id, ok, err)
		}
		if ev.ProjectRoot != tc.wantRoot || ev.ActionType != tc.wantAction || ev.SessionID != tc.id {
			t.Errorf("%s %s: root=%q action=%q session=%q, want root=%q action=%q",
				tc.event, tc.id, ev.ProjectRoot, ev.ActionType, ev.SessionID, tc.wantRoot, tc.wantAction)
		}
	}
	// The payload flag alone (a build that sets it) bootstraps too.
	ev, ok, err := BuildEvent(EventSessionStart, []byte(`{"conversation_id":"`+testLocalID+`","is_background_agent":true,"workspace_roots":[]}`), nil)
	if err != nil || !ok || ev.ProjectRoot != SyntheticProjectRoot {
		t.Fatalf("flagged sessionStart: root=%q ok=%v err=%v", ev.ProjectRoot, ok, err)
	}
	// A real root always wins over the placeholder.
	ev, _, _ = BuildEvent(EventSessionStart, []byte(`{"conversation_id":"`+testCloudID+`","workspace_roots":["/repo"]}`), nil)
	if ev.ProjectRoot != "/repo" {
		t.Fatalf("rooted cloud sessionStart root = %q, want /repo", ev.ProjectRoot)
	}
}

// TestBuildEventCloudAgentThoughtLandsRow: a root-less Cloud Agent thought
// lands a thin step row (reasoning in PrecedingReasoning) instead of a
// stash no successor ever claims; a local thought keeps the B3 stash.
func TestBuildEventCloudAgentThoughtLandsRow(t *testing.T) {
	isolateStash(t)
	thought := cloudPayload(EventAfterAgentThought, testCloudID, `"text":"planning the next step","duration_ms":759,`)
	ev, ok, err := BuildEvent(EventAfterAgentThought, thought, scrub.New())
	if err != nil || !ok {
		t.Fatalf("cloud thought: ok=%v err=%v (want a row)", ok, err)
	}
	if ev.ProjectRoot != SyntheticProjectRoot || ev.SessionID != testCloudID ||
		ev.ActionType != models.ActionHarnessCall || ev.RawToolName != cloudAgentThoughtRawName ||
		ev.PrecedingReasoning != "planning the next step" || ev.DurationMs != 759 ||
		ev.SourceFile != "cursor:hook" || !strings.HasPrefix(ev.SourceEventID, testCloudID+":"+EventAfterAgentThought+":") {
		t.Fatalf("cloud thought row = %+v", ev)
	}
	if got := defaultStash.take(testCloudID, "next"); got != "" {
		t.Fatalf("cloud thought was also stashed: %q", got)
	}
	// Identity is content-derived: same payload, same id; next step, new id.
	again, _, _ := BuildEvent(EventAfterAgentThought, thought, scrub.New())
	next, _, _ := BuildEvent(EventAfterAgentThought, cloudPayload(EventAfterAgentThought, testCloudID, `"text":"reading the result","duration_ms":120,`), scrub.New())
	if again.SourceEventID != ev.SourceEventID || next.SourceEventID == ev.SourceEventID {
		t.Fatalf("ids: first=%s again=%s next=%s", ev.SourceEventID, again.SourceEventID, next.SourceEventID)
	}
	// Empty text carries nothing: no row, no stash.
	if _, ok, _ := BuildEvent(EventAfterAgentThought, cloudPayload(EventAfterAgentThought, testCloudID, `"text":"  ",`), nil); ok {
		t.Fatal("empty cloud thought minted a row")
	}
	// A local (non-cloud) root-less thought still mints no row and is
	// stashed for its successor.
	if _, ok, _ := BuildEvent(EventAfterAgentThought, cloudPayload(EventAfterAgentThought, testLocalID, `"text":"local thought",`), nil); ok {
		t.Fatal("local thought minted a row")
	}
	if got := defaultStash.take(testLocalID, "succ"); got != "local thought" {
		t.Fatalf("local thought stash = %q", got)
	}
}

func TestThoughtMintsRowAndRemotePayload(t *testing.T) {
	cases := []struct {
		name   string
		body   []byte
		mints  bool
		remote bool
	}{
		{"cloud thought", cloudPayload(EventAfterAgentThought, testCloudID, `"text":"t",`), true, true},
		{"cloud thought, empty text", cloudPayload(EventAfterAgentThought, testCloudID, `"text":"",`), false, true},
		{"cloud thought with a folder", []byte(`{"conversation_id":"` + testCloudID + `","text":"t","workspace_roots":["/repo"]}`), false, true},
		{"local thought", cloudPayload(EventAfterAgentThought, testLocalID, `"text":"t",`), false, false},
		{"draft thought", cloudPayload(EventAfterAgentThought, testDraftID, `"text":"t",`), false, false},
		{"garbage", []byte(`not json`), false, false},
	}
	for _, tc := range cases {
		if got := ThoughtMintsRow(tc.body); got != tc.mints {
			t.Errorf("%s: ThoughtMintsRow = %v, want %v", tc.name, got, tc.mints)
		}
		if got := IsRemoteAgentPayload(tc.body); got != tc.remote {
			t.Errorf("%s: IsRemoteAgentPayload = %v, want %v", tc.name, got, tc.remote)
		}
	}
}

// cloudHooksLog renders an anonymized Cursor hooks output log holding the
// Cloud Agent's sessionStart and one thought, plus a draft's root-less
// sessionStart, in the exact block shape Cursor writes.
func cloudHooksLog() string {
	block := func(ts, event string, body []byte) string {
		return "[" + ts + "] Running hook 1 with timeout 60000ms\n" +
			"═══\n" + event + "\n═══\n" +
			"Command: observer hook cursor " + event + " (5ms) exit code: 0\n\nINPUT:\n" +
			string(body) + "\n\nOUTPUT:\n{}\n═══\n"
	}
	return block("2026-10-05T09:16:26.356Z", EventSessionStart, cloudPayload(EventSessionStart, testCloudID, ``)) +
		block("2026-10-05T09:17:06.638Z", EventAfterAgentThought, cloudPayload(EventAfterAgentThought, testCloudID, `"text":"planning the next step","duration_ms":759,`)) +
		block("2026-10-05T09:18:00.000Z", EventSessionStart, cloudPayload(EventSessionStart, testDraftID, ``))
}

// TestHooksLogReplayCloudAgentPair: the replay goes through the same
// buildEvent, so it lands the Cloud Agent's session and thought rows under
// the placeholder with the LIVE hook's identity (dedup by UNIQUE key),
// and still drops the draft's root-less sessionStart.
func TestHooksLogReplayCloudAgentPair(t *testing.T) {
	isolateStash(t)
	root, path := placeHooksLog(t, []byte(cloudHooksLog()))
	res, err := NewWithOptions(scrub.New(), root).ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	liveStart, _, _ := BuildEvent(EventSessionStart, cloudPayload(EventSessionStart, testCloudID, ``), scrub.New())
	liveThought, _, _ := BuildEvent(EventAfterAgentThought, cloudPayload(EventAfterAgentThought, testCloudID, `"text":"planning the next step","duration_ms":759,`), scrub.New())
	byID := map[string]models.ToolEvent{}
	for _, ev := range res.ToolEvents {
		if ev.SessionID == testDraftID {
			t.Fatalf("draft sessionStart replayed into a row: %+v", ev)
		}
		byID[ev.SourceEventID] = ev
	}
	if len(res.ToolEvents) != 2 {
		t.Fatalf("replayed rows = %d, want 2 (cloud sessionStart + thought): %+v", len(res.ToolEvents), res.ToolEvents)
	}
	for _, live := range []models.ToolEvent{liveStart, liveThought} {
		got, ok := byID[live.SourceEventID]
		if !ok || got.SourceFile != live.SourceFile || got.ProjectRoot != SyntheticProjectRoot || got.ActionType != live.ActionType {
			t.Fatalf("replay of %s = %+v (ok=%v), want the live identity %s/%s under %s", live.RawToolName, got, ok, live.SourceFile, live.SourceEventID, SyntheticProjectRoot)
		}
	}
	if got := byID[liveThought.SourceEventID]; got.PrecedingReasoning != "planning the next step" || got.Timestamp.Format("15:04:05") != "09:17:06" {
		t.Fatalf("replayed thought = %+v", got)
	}
}
