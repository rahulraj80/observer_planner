package cursor

import (
	"context"
	"testing"
)

// TestHooksLogReplayEmitsToolVersions pins T-784 on the hooks-log replay:
// every conversation in the live-captured fixture carries cursor_version
// on its payloads, and each becomes exactly ONE SessionToolVersion (the
// first build seen), keyed to the conversation id the rows use.
func TestHooksLogReplayEmitsToolVersions(t *testing.T) {
	isolateStash(t)
	root, path := placeHooksLog(t, readFixture(t))
	res, err := NewWithOptions(nil, root).ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"8be96a3f-5490-4d9c-bb7c-8459b183a8bf":    "3.20.21",
		"cc8322af-e062-47f1-a4ab-5055b938c963":    "3.17.21",
		"bc-4e387d38-4596-4b70-8ca8-5c1f2db7290a": "3.21.13",
	}
	if len(res.SessionToolVersions) != len(want) {
		t.Fatalf("SessionToolVersions = %+v, want %d entries", res.SessionToolVersions, len(want))
	}
	for _, tv := range res.SessionToolVersions {
		if want[tv.SessionID] != tv.Version {
			t.Errorf("session %q version = %q, want %q", tv.SessionID, tv.Version, want[tv.SessionID])
		}
	}
}

// TestSessionToolVersionFromHook is the decode table: both fields are
// required, whitespace is trimmed, and a non-JSON body is not a stamp.
func TestSessionToolVersionFromHook(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantOK  bool
		wantSID string
		wantVer string
	}{
		{"present", `{"conversation_id":"c1","cursor_version":"3.20.21"}`, true, "c1", "3.20.21"},
		{"trimmed", `{"conversation_id":" c1 ","cursor_version":" 3.20.21 "}`, true, "c1", "3.20.21"},
		{"3.4.x payload without version", `{"conversation_id":"c1"}`, false, "", ""},
		{"no conversation", `{"cursor_version":"3.20.21"}`, false, "", ""},
		{"not json", `INPUT:`, false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tv, ok := SessionToolVersionFromHook([]byte(tc.body))
			if ok != tc.wantOK || tv.SessionID != tc.wantSID || tv.Version != tc.wantVer {
				t.Errorf("got (%+v, %v), want ({%s %s}, %v)", tv, ok, tc.wantSID, tc.wantVer, tc.wantOK)
			}
		})
	}
}
