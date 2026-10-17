package orgcontract

import "testing"

// TestIntelTombstoneCursorOlder pins the node's replay rule (review C156-29):
// only an earlier PURGE TIME is a replay; a set at the floor's own second is
// accepted whatever its session-id tie-break, because that order is the
// server database's collation, not byte order.
func TestIntelTombstoneCursorOlder(t *testing.T) {
	floor := IntelTombstoneCursor("2026-10-05T10:00:00Z", "s-b")
	cases := []struct {
		name string
		c    string
		want bool
	}{
		{"earlier second", IntelTombstoneCursor("2026-10-05T09:59:59Z", "s-z"), true},
		{"same second, lower session id", IntelTombstoneCursor("2026-10-05T10:00:00Z", "s-a"), false},
		{"same cursor", floor, false},
		{"later second", IntelTombstoneCursor("2026-10-05T10:00:01Z", "s-a"), false},
		{"empty set cursor below a floor", "", true},
	}
	for _, tc := range cases {
		if got := IntelTombstoneCursorOlder(tc.c, floor); got != tc.want {
			t.Errorf("%s: IntelTombstoneCursorOlder(%q) = %v, want %v", tc.name, tc.c, got, tc.want)
		}
	}
	if IntelTombstoneCursorOlder("", "") || IntelTombstoneCursorOlder(floor, "") {
		t.Error("no floor yet: nothing is older")
	}
}
