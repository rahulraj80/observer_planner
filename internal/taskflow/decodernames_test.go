package taskflow

import (
	"sort"
	"strings"
	"testing"
)

// TestDecoderNamesMirrorsTable pins DecoderNames as an exact, sorted view of
// decoderTable: every (tool, name) pair lookupDecoder resolves is listed,
// and nothing else is.
func TestDecoderNamesMirrorsTable(t *testing.T) {
	got := DecoderNames()
	want := map[string]map[string]bool{}
	for _, e := range decoderTable {
		if want[e.tool] == nil {
			want[e.tool] = map[string]bool{}
		}
		for _, n := range e.names {
			want[e.tool][strings.ToLower(n)] = true
		}
	}
	if len(got) != len(want) {
		t.Fatalf("DecoderNames has %d tools, decoderTable %d", len(got), len(want))
	}
	for tool, names := range got {
		if !sort.StringsAreSorted(names) {
			t.Errorf("%s: names not sorted: %v", tool, names)
		}
		if len(names) != len(want[tool]) {
			t.Errorf("%s: %d names, table has %d", tool, len(names), len(want[tool]))
		}
		for _, n := range names {
			if _, ok := lookupDecoder(tool, n); !ok {
				t.Errorf("%s: %q listed but lookupDecoder misses it", tool, n)
			}
		}
	}
	// The returned map is the caller's: mutating it never reaches the table.
	got["claude-code"] = nil
	if len(DecoderNames()["claude-code"]) == 0 {
		t.Fatal("mutating the returned map changed a later call")
	}
}
