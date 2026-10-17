package diag

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/hookdisable"
)

// checkHooksDisabled reports a vendor-side switch that silences every hook
// the vendor runs, Observer's included (hook-parity T-046): Claude Code's
// disableAllHooks (which also silences the hosts that replay Claude Code's
// hook settings), the managed allowManagedHooksOnly, Gemini CLI's
// hooksConfig.enabled=false, Factory's hooksDisabled, and the rest of the
// internal/hookdisable table. Observer's own files are untouched by such a
// switch, so neither the hook checksum check nor the registrar notices.
//
// StatusWarn when any switch is set (the developer may have meant it; the
// finding names what Observer stops seeing), StatusOK otherwise. Pure file
// inspection. A sandboxed run (homeOverride non-empty) skips the
// system-wide managed-settings file, as checkCursorHooks skips the
// cross-mounted home.
//
// Named "hooks.disabled" so `observer doctor hooks` scopes to it.
func checkHooksDisabled(homeDir, homeOverride string) Check {
	const name = "hooks.disabled"
	goos := runtime.GOOS
	if homeOverride != "" {
		goos = ""
	}
	fs := hookdisable.Detect(hookdisable.Options{Home: homeDir, GOOS: goos, ReadFile: os.ReadFile})
	if len(fs) == 0 {
		return Check{Name: name, Status: StatusOK, Message: "no vendor switch turns hooks off"}
	}
	details := make([]string, 0, len(fs))
	for _, f := range fs {
		details = append(details, fmt.Sprintf("%s: %s (%s scope): %s; Observer is silent on %s",
			f.Path, f.Key, f.Scope, f.Effect, strings.Join(f.Tools, ", ")))
	}
	return Check{
		Name:   name,
		Status: StatusWarn,
		Message: fmt.Sprintf("%d vendor switch(es) turn hooks off; Observer's hooks do not run on %s",
			len(fs), strings.Join(hookdisable.AffectedTools(fs), ", ")),
		Details: details,
	}
}
