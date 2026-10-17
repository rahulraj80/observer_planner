package statusline

import (
	"strings"
	"testing"
)

// TestBudgetSegment pins the budget segment: omitted with no tile value (so a
// node with no org budget sees the pre-change line), rendered last by
// default, control characters stripped.
func TestBudgetSegment(t *testing.T) {
	today := 1.26
	base := Render(Input{}, DaemonTile{TodayUSD: &today}, RenderOptions{})
	if strings.Contains(base, "budget") {
		t.Fatalf("no budget value rendered a segment: %q", base)
	}
	label := "budget 168% of $0.75/day"
	got := Render(Input{}, DaemonTile{TodayUSD: &today, Budget: &label}, RenderOptions{})
	if !strings.HasSuffix(got, label) {
		t.Fatalf("line = %q, want it to end with %q", got, label)
	}
	evil := "budget\n9%"
	if got := Render(Input{}, DaemonTile{Budget: &evil}, RenderOptions{Segments: []string{SegmentBudget}}); strings.Contains(got, "\n") {
		t.Fatalf("control rune leaked: %q", got)
	}
	empty := ""
	if got := Render(Input{}, DaemonTile{Budget: &empty}, RenderOptions{Segments: []string{SegmentBudget}}); strings.Contains(got, "budget") {
		t.Fatalf("empty budget rendered: %q", got)
	}
}
