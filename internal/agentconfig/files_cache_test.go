package agentconfig

import (
	"sort"
	"testing"
)

// TestFilesIsCompiledOnce pins T-399: the Files table is compiled once per
// process, so the per-setting lookups the doc generator and the org lint run
// for every catalogue row no longer rebuild and re-sort it. Measured as
// allocations: before the change UserFileFor allocated the whole table (well
// over a hundred rows) and the sort on every call.
func TestFilesIsCompiledOnce(t *testing.T) {
	var id string
	for _, r := range Catalog() {
		if UserFileFor(r.ID) != "" {
			id = r.ID
			break
		}
	}
	if id == "" {
		t.Fatal("no catalogue row has a user file")
	}
	_ = UserFileFor(id) // warm the compiled table
	cases := []struct {
		name string
		fn   func()
	}{
		{"UserFileFor", func() { _ = UserFileFor(id) }},
		{"ProjectFileFor", func() { _ = ProjectFileFor(id, false) }},
		{"TrackedShadow", func() { _ = TrackedShadow(id) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Setting() clones its row (a few small allocations); the table
			// itself must not be rebuilt.
			if got := testing.AllocsPerRun(20, c.fn); got > 16 {
				t.Fatalf("%s allocates %.0f objects per call: the Files table is being rebuilt", c.name, got)
			}
		})
	}
}

// TestFilesReturnsACopy: a caller that modifies what Files returns cannot
// change the compiled table every other caller reads.
func TestFilesReturnsACopy(t *testing.T) {
	a := Files()
	if len(a) == 0 {
		t.Fatal("empty Files table")
	}
	want := a[0]
	a[0].ID = "mutated/by-a-caller"
	a[0].Note = "mutated"
	b := Files()
	if b[0] != want {
		t.Fatalf("Files()[0] = %+v after a caller mutated its copy, want %+v", b[0], want)
	}
	if !sort.SliceIsSorted(b, func(i, j int) bool { return b[i].ID < b[j].ID }) {
		t.Fatal("Files() is not sorted by id")
	}
	if len(b) != len(a) {
		t.Fatalf("Files() has %d rows, then %d", len(a), len(b))
	}
}
