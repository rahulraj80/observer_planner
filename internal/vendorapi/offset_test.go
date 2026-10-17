package vendorapi

import "testing"

// TestOffsetCursorRules (lane ENTAPI-L16C, T-710): the offset cursor carries
// the next offset and a digest of the page's last record; a page that repeats
// that record is a loop; a total that does not cover the page is drift.
func TestOffsetCursorRules(t *testing.T) {
	next, ok := NextOffsetCursor(0, 3, []string{"a", "b"})
	if !ok || next == "" {
		t.Fatalf("first page of three: %q %v", next, ok)
	}
	if off, ok := OffsetOf(next); !ok || off != 2 {
		t.Fatalf("offset of %q = %d %v", next, off, ok)
	}
	if OffsetLoop(next, []string{"c"}) {
		t.Error("a fresh page was called a loop")
	}
	if !OffsetLoop(next, []string{"a", "b"}) {
		t.Error("page one served again was not called a loop")
	}
	if last, ok := NextOffsetCursor(2, 3, []string{"c"}); !ok || last != "" {
		t.Errorf("terminal page: %q %v", last, ok)
	}
	for name, c := range map[string]struct {
		off, total int
		ids        []string
	}{
		"page beyond total":    {2, 3, []string{"c", "d"}},
		"empty page with more": {2, 5, nil},
		"negative total":       {0, -1, nil},
	} {
		if _, ok := NextOffsetCursor(c.off, c.total, c.ids); ok {
			t.Errorf("%s accepted", name)
		}
	}
	for _, bad := range []string{"2", "x:0123456789abcdef", "0:0123456789abcdef", "2:0123456789ABCDEF", "-1:0123456789abcdef"} {
		if _, ok := OffsetOf(bad); ok {
			t.Errorf("cursor %q accepted", bad)
		}
	}
	if off, ok := OffsetOf(""); !ok || off != 0 {
		t.Error("empty cursor is not offset 0")
	}
}
