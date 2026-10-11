//go:build sbo_omit_codeintel && sbo_omit_capture_browser

package diag

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestFeatureStubsSayNotInThisBuild: with codeintel and browser capture
// compiled out, their doctor rows say "not in this build" and never warn.
func TestFeatureStubsSayNotInThisBuild(t *testing.T) {
	if c := checkCodeIntel(config.Default()); c.Status != StatusOK || c.Message != "not in this build (code intelligence is compiled out)" {
		t.Errorf("checkCodeIntel = %+v", c)
	}
	var got []string
	browserExtensionHealthNote("chatgpt-web", config.Default(), func(s Status, m string) {
		if s != StatusOK {
			t.Errorf("status = %v, want OK", s)
		}
		got = append(got, m)
	})
	if len(got) != 1 || got[0] != "browser capture is not in this build (compiled out)" {
		t.Errorf("notes = %v", got)
	}
}
