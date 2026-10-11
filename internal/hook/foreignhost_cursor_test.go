//go:build !sbo_omit_adapter_cursor

package hook

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/adapter/cursor"
)

// TestForeignCursorStepsMatchTheAdapter pins foreignhost.go's spelled-out
// Cursor step names to internal/adapter/cursor's constants, so the
// classifier cannot drift from the adapter in a build that has both.
func TestForeignCursorStepsMatchTheAdapter(t *testing.T) {
	want := []string{
		cursor.EventBeforeSubmitPrompt, cursor.EventPreToolUse, cursor.EventPostToolUse,
		cursor.EventStop, cursor.EventSubagentStop, cursor.EventSessionStart,
		cursor.EventSessionEnd, cursor.EventPreCompact,
	}
	if len(cursorSteps) != len(want) {
		t.Fatalf("cursorSteps has %d steps, the adapter names %d", len(cursorSteps), len(want))
	}
	for _, w := range want {
		if !cursorSteps[w] {
			t.Errorf("cursorSteps lacks the adapter's step %q", w)
		}
	}
	if _, ok := cursorNeutralReplies[cursor.EventBeforeSubmitPrompt]; !ok {
		t.Errorf("cursorNeutralReplies lacks %q", cursor.EventBeforeSubmitPrompt)
	}
}
