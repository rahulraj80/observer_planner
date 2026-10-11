package sessionmsg

import (
	"strings"
	"testing"
)

// TestDerive_PromptHookShadowFold pins T-563: with the tool's
// Hook.PromptShadowsTranscript capability, a hook-captured user_prompt folds
// onto the transcript prompt of the same turn (the transcript row is
// canonical); without it, or without a twin, nothing changes. One row per
// rule of promptShadowRules plus the guards around them.
func TestDerive_PromptHookShadowFold(t *testing.T) {
	t.Parallel()
	hook := func(turn, ts string) ActionRow {
		return ActionRow{
			ActionID: "h-" + turn, SourceEventID: turn + ":UserPromptSubmit", MessageID: "user:" + turn,
			ActionType: "user_prompt", Timestamp: ts, HookCaptured: true,
		}
	}
	transcript := func(msgID, seid, ts string) ActionRow {
		return ActionRow{
			ActionID: "t-" + seid, SourceEventID: seid, MessageID: msgID,
			ActionType: "user_prompt", Timestamp: ts, Tool: "user_message",
		}
	}
	type want struct {
		key     string
		actions []string // ActionIDs on the row
		aliases []string // keys that must resolve to the row
	}
	tests := []struct {
		name    string
		shadow  bool
		actions []ActionRow
		want    []want
	}{
		{
			name:   "rule 1: same turn key, one row with the transcript prompt only",
			shadow: true,
			actions: []ActionRow{
				hook("T1", "2026-10-06T10:00:00.100Z"),
				transcript("user:T1", "user:r.jsonl:L4:aa", "2026-10-06T10:00:00.900Z"),
			},
			want: []want{{key: "user:T1", actions: []string{"t-user:r.jsonl:L4:aa"}, aliases: []string{"T1:UserPromptSubmit"}}},
		},
		{
			name:   "rule 2: transcript keyed without a turn (legacy / first turn), same moment",
			shadow: true,
			actions: []ActionRow{
				hook("T1", "2026-10-06T10:00:00Z"),
				transcript("user:r.jsonl:L3:bb", "user:r.jsonl:L3:bb", "2026-10-06T10:00:02Z"),
			},
			want: []want{{key: "user:r.jsonl:L3:bb", actions: []string{"t-user:r.jsonl:L3:bb"}, aliases: []string{"user:T1", "T1:UserPromptSubmit"}}},
		},
		{
			name:   "capability off: two prompts, exactly as before",
			shadow: false,
			actions: []ActionRow{
				hook("T1", "2026-10-06T10:00:00Z"),
				transcript("user:r.jsonl:L3:bb", "user:r.jsonl:L3:bb", "2026-10-06T10:00:02Z"),
			},
			want: []want{
				{key: "user:T1", actions: []string{"h-T1"}},
				{key: "user:r.jsonl:L3:bb", actions: []string{"t-user:r.jsonl:L3:bb"}},
			},
		},
		{
			name:    "hook prompt with no transcript twin stays the user message",
			shadow:  true,
			actions: []ActionRow{hook("T1", "2026-10-06T10:00:00Z")},
			want:    []want{{key: "user:T1", actions: []string{"h-T1"}}},
		},
		{
			name:   "a transcript prompt keyed to another hook's turn is never taken by time",
			shadow: true,
			actions: []ActionRow{
				hook("T1", "2026-10-06T10:00:00Z"),
				hook("T2", "2026-10-06T10:00:05Z"),
				transcript("user:T2", "user:r.jsonl:L9:cc", "2026-10-06T10:00:05.500Z"),
			},
			want: []want{
				{key: "user:T1", actions: []string{"h-T1"}},
				{key: "user:T2", actions: []string{"t-user:r.jsonl:L9:cc"}},
			},
		},
		{
			name:   "outside the window: two prompts",
			shadow: true,
			actions: []ActionRow{
				hook("T1", "2026-10-06T10:00:00Z"),
				transcript("user:r.jsonl:L3:bb", "user:r.jsonl:L3:bb", "2026-10-06T10:02:00Z"),
			},
			want: []want{
				{key: "user:T1", actions: []string{"h-T1"}},
				{key: "user:r.jsonl:L3:bb", actions: []string{"t-user:r.jsonl:L3:bb"}},
			},
		},
		{
			name:   "one-to-one, closest first",
			shadow: true,
			actions: []ActionRow{
				hook("T1", "2026-10-06T10:00:00Z"),
				transcript("user:r.jsonl:L3:aa", "user:r.jsonl:L3:aa", "2026-10-06T10:00:01Z"),
				hook("T2", "2026-10-06T10:00:10Z"),
				transcript("user:r.jsonl:L8:bb", "user:r.jsonl:L8:bb", "2026-10-06T10:00:11Z"),
			},
			want: []want{
				{key: "user:r.jsonl:L3:aa", actions: []string{"t-user:r.jsonl:L3:aa"}, aliases: []string{"user:T1"}},
				{key: "user:r.jsonl:L8:bb", actions: []string{"t-user:r.jsonl:L8:bb"}, aliases: []string{"user:T2"}},
			},
		},
		{
			name:   "unparseable timestamps never pair on time",
			shadow: true,
			actions: []ActionRow{
				hook("T1", "garbage"),
				transcript("user:r.jsonl:L3:bb", "user:r.jsonl:L3:bb", "garbage"),
			},
			want: []want{
				{key: "user:T1", actions: []string{"h-T1"}},
				{key: "user:r.jsonl:L3:bb", actions: []string{"t-user:r.jsonl:L3:bb"}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := Derive(DeriveInput{ActionRows: tc.actions, PromptHookShadow: tc.shadow})
			if len(rows) != len(tc.want) {
				t.Fatalf("rows = %d, want %d: %s", len(rows), len(tc.want), describeRows(rows))
			}
			idx := AliasIndex(rows)
			for i, w := range tc.want {
				r := rows[i]
				if r.Key != w.key || r.Role != "user" {
					t.Fatalf("row %d = %s/%s, want user/%s: %s", i, r.Role, r.Key, w.key, describeRows(rows))
				}
				var got []string
				for _, a := range r.Actions {
					got = append(got, a.ActionID)
				}
				if strings.Join(got, ",") != strings.Join(w.actions, ",") {
					t.Fatalf("row %d actions = %v, want %v", i, got, w.actions)
				}
				for _, k := range w.aliases {
					if j, ok := idx[k]; !ok || j != i {
						t.Fatalf("alias %q resolves to row %d (found %v), want %d", k, j, ok, i)
					}
				}
			}
		})
	}
}

func describeRows(rows []*Row) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.Role + ":" + r.Key + "[")
		for _, a := range r.Actions {
			b.WriteString(a.ActionID + " ")
		}
		b.WriteString("] ")
	}
	return b.String()
}
