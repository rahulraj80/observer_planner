//go:build sbo_omit_codeintel

package diag

import "github.com/marmutapp/superbased-observer/internal/config"

// checkCodeIntel is the off side of codeintel.go: in a `-tags
// sbo_omit_codeintel` build the doctor reports code intelligence as not in
// this build instead of reading its config, and diag does not import
// internal/codeintel (modular packaging plan 5.1, lane MP-L1B).
func checkCodeIntel(_ config.Config) Check {
	return Check{Name: "codeintel", Status: StatusOK, Message: "not in this build (code intelligence is compiled out)"}
}
