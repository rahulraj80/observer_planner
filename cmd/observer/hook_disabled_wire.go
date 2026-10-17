package main

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/hookdisable"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

// hooksDisabledVerdict is the guard event for a vendor switch that turned
// every hook off (hook-parity T-046): a guard_error flag (nothing was
// blocked; Observer simply stops seeing those tools), degraded_from
// "hooks_disabled", so it reaches the org on the normal guard_events push
// exactly as a posture tamper does (postureTamperVerdict).
func hooksDisabledVerdict(f hookdisable.Finding, at time.Time) guard.ActionVerdict {
	return guard.ActionVerdict{
		Input:    guard.ActionInput{Tool: "observer", Target: f.Path, Timestamp: at.UTC()},
		Kind:     policy.KindConfigChange,
		Category: "guard",
		Verdict: policy.Verdict{
			Decision: policy.DecisionFlag,
			RuleID:   guard.GuardErrorRuleID,
			Severity: policy.SeverityHigh,
			Reason:   "hooks switched off: " + f.Key + " in " + f.Path + " (" + string(f.Scope) + " scope); " + f.Effect,
			Advice:   "Observer's hooks no longer run on " + strings.Join(f.Tools, ", ") + ". Remove the setting, or run `observer doctor hooks` for detail.",
			Source:   policy.SourceBuiltin,
		},
		GuardError:   true,
		DegradedFrom: "hooks_disabled",
	}
}

// hookDisableWatch is the daemon's ONE watcher of vendor hook-disable
// switches. A switch is reported when it turns on (and once at daemon start
// when it is already on), never on every tick.
type hookDisableWatch struct {
	home    string
	goos    string
	read    func(string) ([]byte, error)
	tracker hookdisable.Tracker
}

func newHookDisableWatch() *hookDisableWatch {
	home, _ := os.UserHomeDir()
	return &hookDisableWatch{home: home, goos: runtime.GOOS, read: os.ReadFile}
}

// verdictPersister is the store seam the watch writes through.
type verdictPersister interface {
	PersistGuardVerdicts(ctx context.Context, vs []guard.ActionVerdict) (int, error)
}

// check detects, and persists a guard event per switch that turned on.
// Returns the number of events persisted.
func (w *hookDisableWatch) check(ctx context.Context, st verdictPersister, now time.Time, logger *slog.Logger) int {
	turned := w.tracker.Turned(hookdisable.Detect(hookdisable.Options{Home: w.home, GOOS: w.goos, ReadFile: w.read}))
	if len(turned) == 0 || st == nil {
		return 0
	}
	vs := make([]guard.ActionVerdict, 0, len(turned))
	for _, f := range turned {
		logger.Warn("vendor switch turned hooks off", "file", f.Path, "key", f.Key, "tools", f.Tools)
		vs = append(vs, hooksDisabledVerdict(f, now))
	}
	if _, err := st.PersistGuardVerdicts(ctx, vs); err != nil {
		logger.Warn("hooks disabled: persist guard events", "err", err)
		return 0
	}
	return len(vs)
}
