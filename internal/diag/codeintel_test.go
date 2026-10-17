//go:build !sbo_omit_codeintel

package diag

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestCheckCodeIntel pins the doctor wording for D-307: off is the default
// and is reported as a choice (StatusOK, names the Settings control), never
// as a warning or failure; on reports its schedule.
func TestCheckCodeIntel(t *testing.T) {
	t.Parallel()
	on := config.Default()
	on.CodeIntel.Enabled = true
	tests := []struct {
		name         string
		cfg          config.Config
		wantContains []string
	}{
		{name: "default is off by choice", cfg: config.Default(), wantContains: []string{"off by choice", "not a failure", "Settings > Intelligence > Code intelligence"}},
		{name: "explicitly on", cfg: on, wantContains: []string{"on (index on start: true", "mode: auto"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := checkCodeIntel(tc.cfg)
			if c.Name != "codeintel" {
				t.Errorf("Name = %q, want codeintel", c.Name)
			}
			if c.Status != StatusOK {
				t.Errorf("Status = %v, want ok (off is a choice, not a failure)", c.Status)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(c.Message, want) {
					t.Errorf("Message %q missing %q", c.Message, want)
				}
			}
			if strings.ContainsAny(c.Message, "—–") {
				t.Errorf("Message %q carries an em or en dash", c.Message)
			}
		})
	}
}
