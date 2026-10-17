package cline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestLatestClineVersion pins the newest-entry selection for the Cline
// agent version (Issue 2, migration 125), including the empty/absent
// honesty case.
func TestLatestClineVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		meta taskMetadata
		want string
	}{
		{"none", taskMetadata{}, ""},
		{
			name: "single",
			meta: taskMetadata{EnvironmentHistory: []taskEnvironmentRecord{{Ts: 1, ClineVersion: "3.17.4"}}},
			want: "3.17.4",
		},
		{
			name: "newest_wins",
			meta: taskMetadata{EnvironmentHistory: []taskEnvironmentRecord{
				{Ts: 5, ClineVersion: "3.16.0"},
				{Ts: 50, ClineVersion: "3.17.4"},
			}},
			want: "3.17.4",
		},
		{
			name: "empty_versions_skipped",
			meta: taskMetadata{EnvironmentHistory: []taskEnvironmentRecord{
				{Ts: 9, ClineVersion: ""},
				{Ts: 1, ClineVersion: "3.10.0"},
			}},
			want: "3.10.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := latestClineVersion(tc.meta); got != tc.want {
				t.Errorf("latestClineVersion = %q want %q", got, tc.want)
			}
		})
	}
}

// TestEarliestClineVersion pins the oldest-entry selection the adapter
// stamps FIRST, so the store's first-wins tool_version is the build the
// task started on (migration 162, T-784).
func TestEarliestClineVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		meta taskMetadata
		want string
	}{
		{"none", taskMetadata{}, ""},
		{
			name: "oldest_wins",
			meta: taskMetadata{EnvironmentHistory: []taskEnvironmentRecord{
				{Ts: 50, ClineVersion: "3.17.4"},
				{Ts: 5, ClineVersion: "3.16.0"},
			}},
			want: "3.16.0",
		},
		{
			name: "tie_keeps_first_listed",
			meta: taskMetadata{EnvironmentHistory: []taskEnvironmentRecord{
				{Ts: 5, ClineVersion: "3.16.0"},
				{Ts: 5, ClineVersion: "3.16.1"},
			}},
			want: "3.16.0",
		},
		{
			name: "empty_versions_skipped",
			meta: taskMetadata{EnvironmentHistory: []taskEnvironmentRecord{
				{Ts: 1, ClineVersion: ""},
				{Ts: 9, ClineVersion: "3.10.0"},
			}},
			want: "3.10.0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := earliestClineVersion(tc.meta); got != tc.want {
				t.Errorf("earliestClineVersion = %q want %q", got, tc.want)
			}
		})
	}
}

// TestClineToolVersionStamps pins the stamp ORDER the store's first/last
// semantics rely on: earliest first, latest second, one stamp when equal.
func TestClineToolVersionStamps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		meta taskMetadata
		want []string
	}{
		{"none", taskMetadata{}, nil},
		{"one build", taskMetadata{EnvironmentHistory: []taskEnvironmentRecord{{Ts: 1, ClineVersion: "3.17.4"}}}, []string{"3.17.4"}},
		{
			"upgraded: earliest then latest",
			taskMetadata{EnvironmentHistory: []taskEnvironmentRecord{{Ts: 50, ClineVersion: "3.18.0"}, {Ts: 5, ClineVersion: "3.17.4"}}},
			[]string{"3.17.4", "3.18.0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clineToolVersionStamps("s", tc.meta)
			if len(got) != len(tc.want) {
				t.Fatalf("stamps = %+v, want %v", got, tc.want)
			}
			for i, v := range tc.want {
				if got[i].Version != v || got[i].SessionID != "s" {
					t.Errorf("stamp %d = %+v, want %s", i, got[i], v)
				}
			}
		})
	}
}

// TestParseStampsToolVersion is the end-to-end pin: a task dir carrying
// the live task_metadata.json (cline_version 3.88.0) yields a
// SessionToolVersion.
func TestParseStampsToolVersion(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "tasks", "1780707193341")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyTestdata(t, "api_conversation_history_xml.json", filepath.Join(dir, "api_conversation_history.json"))
	copyTestdata(t, "task_metadata.json", filepath.Join(dir, taskMetadataName))

	path := filepath.Join(dir, "api_conversation_history.json")
	res, err := New().ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(res.SessionToolVersions) != 1 {
		t.Fatalf("tool versions = %d want 1", len(res.SessionToolVersions))
	}
	tv := res.SessionToolVersions[0]
	if tv.SessionID != "1780707193341" {
		t.Errorf("tool-version session = %q", tv.SessionID)
	}
	if tv.Version != "3.88.0" {
		t.Errorf("version = %q want 3.88.0", tv.Version)
	}
}

// TestParseWithoutTaskMetadataEmitsNoToolVersion pins the honest-unknown
// path: no sibling metadata ⇒ no tool-version stamp.
func TestParseWithoutTaskMetadataEmitsNoToolVersion(t *testing.T) {
	t.Parallel()
	path := copyFixture(t, "no-meta-task")
	res, err := New().ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(res.SessionToolVersions) != 0 {
		t.Fatalf("tool versions = %+v; want none", res.SessionToolVersions)
	}
}
