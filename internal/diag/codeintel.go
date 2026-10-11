//go:build !sbo_omit_codeintel

package diag

import (
	"fmt"

	"github.com/marmutapp/superbased-observer/internal/codeintel"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// checkCodeIntel reports the code-intelligence module's posture. Code
// intelligence is OPT-IN (operator ruling D-307, 2026-10-05: indexing is CPU
// and disk heavy), so OFF is the expected state of most installs and is
// reported as StatusOK with the shared "how to turn it on" sentence, never as
// a warning or failure. ON reports how indexing is scheduled.
func checkCodeIntel(cfg config.Config) Check {
	const name = "codeintel"
	if !cfg.CodeIntel.Enabled {
		return Check{Name: name, Status: StatusOK, Message: "off by choice (not a failure): " + codeintel.DisabledNote}
	}
	mode := cfg.CodeIntel.Index.Mode
	if mode == "" {
		mode = "auto"
	}
	return Check{
		Name:    name,
		Status:  StatusOK,
		Message: fmt.Sprintf("on (index on start: %v, mode: %s, retention: %d days)", cfg.CodeIntel.Index.OnStart, mode, cfg.CodeIntel.RetentionDays),
	}
}
