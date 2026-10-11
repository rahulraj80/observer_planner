package statusline

import (
	"strings"
	"testing"
)

// TestGrantsSegment: the grants segment renders only while an org exception
// is in force, so every other node's line is unchanged (request-grants G6).
func TestGrantsSegment(t *testing.T) {
	base := Render(Input{}, DaemonTile{}, RenderOptions{})
	text := "1 exception, 3h left"
	with := Render(Input{}, DaemonTile{Grants: &text}, RenderOptions{})
	if strings.Contains(base, "exception") {
		t.Fatalf("no grants but the line mentions one: %q", base)
	}
	if !strings.Contains(with, text) {
		t.Fatalf("grants segment missing: %q", with)
	}
}
