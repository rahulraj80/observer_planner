//go:build !sbo_omit_terminals

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/store"
	"github.com/marmutapp/superbased-observer/internal/toolinstall"
	"github.com/marmutapp/superbased-observer/internal/toolresolve"
)

// toolInstallLedger is the cmd-side dashapi.ToolInstallLedger: the node's
// record of guided installs the dashboard ran (tool_installs, agent migration
// 148, written ONLY through internal/store/toolinstalls.go) plus the exit
// tracking that settles each record, and the uninstall built from it.
//
// Decisions are delegated, never re-implemented here: the lifecycle
// (installing -> installed ...) is internal/toolinstall's transition table,
// and the uninstall argv is internal/toolinstall.PlanUninstall over the argv
// the install ACTUALLY ran (recorded verbatim). This file only does the I/O:
// the store calls, the PTY exit wiring and the post-run binary re-check.
//
// Exit tracking: the terminal stack's ONE termsession OnExit closure calls
// observeExit for every session; a handle this ledger registered settles its
// record. The pre-registration window (a setup PTY that exits before track
// runs) is closed the way termsvc closes it — track reconciles against the
// manager's authoritative ExitStatus right after registering — and the
// pending map makes settlement exactly-once whichever path gets there first.
//
// A record left in an in-flight status by a PREVIOUS daemon (no live process
// here tracks it) is settled as interrupted lazily, on the next read.
type toolInstallLedger struct {
	st     *store.Store
	logger *slog.Logger
	now    func() time.Time
	// planFor re-derives the install plan for a tool — the same plan the
	// install route ran — so the record can store its channel.
	planFor func(tool string) (dashboardInstallPlan, bool)
	// resolveBin re-checks where the tool's binary resolves after a run
	// settles ("" = not found).
	resolveBin func(tool string) string
	// lookArgv0 re-resolves a recorded absolute program that no longer
	// exists (a node version switch) by its base name.
	lookArgv0 installArgv0Lookup
	// onInstalled, when set, runs once an install settles as installed with
	// its binary resolving (attach_standalone.go wires the post-install hook
	// registration here).
	onInstalled func(tool string)
	// bootAt is when this ledger (this daemon process) started. Only an
	// in-flight phase that began BEFORE it can be an orphan of a previous
	// daemon; a phase this process began is always settled by this process
	// (track handles even an already-reaped handle), so the lazy sweep never
	// races a just-inserted row that track has not registered yet.
	bootAt time.Time

	mu sync.Mutex
	// exitStatus is the PTY manager's authoritative exit status. Set once the
	// manager exists (the manager's OnExit closure is built before it).
	exitStatus func(handle string) (exited bool, code int, ok bool)
	// pending: setup handle -> the record it settles.
	pending map[string]pendingToolInstallOp
	// tracked: record ids a live PTY in THIS process will settle.
	tracked map[int64]bool
}

type pendingToolInstallOp struct {
	id   int64
	tool string
	from toolinstall.Status
}

// newToolInstallLedger builds the production ledger over st.
func newToolInstallLedger(st *store.Store, logger *slog.Logger) *toolInstallLedger {
	if logger == nil {
		logger = slog.Default()
	}
	return &toolInstallLedger{
		st:         st,
		logger:     logger,
		now:        time.Now,
		planFor:    dashboardInstallPlanForTool,
		resolveBin: installedBinFor,
		lookArgv0:  dashInstallArgv0,
		bootAt:     time.Now(),
		pending:    map[string]pendingToolInstallOp{},
		tracked:    map[int64]bool{},
	}
}

// newToolInstallLedgerFor returns the ledger over database, or nil when no
// database is wired (every method below is nil-receiver safe where the
// terminal stack calls it unconditionally).
func newToolInstallLedgerFor(database *sql.DB, logger *slog.Logger) *toolInstallLedger {
	if database == nil {
		return nil
	}
	return newToolInstallLedger(store.New(database), logger)
}

// setExitStatus wires the manager's exit-status reader. Nil-safe.
func (l *toolInstallLedger) setExitStatus(fn func(handle string) (bool, int, bool)) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.exitStatus = fn
	l.mu.Unlock()
}

// setOnInstalled wires the post-install callback. Nil-safe.
func (l *toolInstallLedger) setOnInstalled(fn func(tool string)) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.onInstalled = fn
	l.mu.Unlock()
}

// dashboardInstallPlanForTool is the SAME plan toolInstallHintSeam hands the
// install route (one lookup ladder, one selection rule).
func dashboardInstallPlanForTool(tool string) (dashboardInstallPlan, bool) {
	installs, ok := installHintsFor(tool)
	if !ok {
		return dashboardInstallPlan{}, false
	}
	return dashboardInstallPlanFor(installs, runtime.GOOS, dashboardInstallHome(), dashInstallArgv0)
}

// installedBinFor re-runs the registry resolution ladder for a tool (terminal
// row first, then GUI row) and returns the resolved binary, or "".
func installedBinFor(tool string) string {
	env := dashResolveEnv()
	if ic, ok := integration.For(tool); ok && ic.Binary != nil {
		if r := toolresolve.Resolve(*ic.Binary, env); guiPreflightLaunchable[r.Verdict] {
			return r.Bin
		}
		return ""
	}
	if g, ok := integration.GUILaunchFor(tool); ok {
		if r := toolresolve.ResolveGUI(g.Spec, env); guiPreflightLaunchable[r.Verdict] {
			return r.Bin
		}
	}
	return ""
}

// RecordInstall implements dashapi.ToolInstallLedger.
func (l *toolInstallLedger) RecordInstall(ctx context.Context, tool, handle string, argv []string, display string) error {
	plan, ok := l.planFor(tool)
	if !ok || plan.Channel == "" {
		return fmt.Errorf("toolInstallLedger.RecordInstall: no install plan for %q", tool)
	}
	id, err := l.st.InsertToolInstall(ctx, store.ToolInstallRow{
		Tool:      tool,
		Channel:   plan.Channel,
		Package:   toolinstall.PackageOf(plan.Channel, argv),
		Argv:      argv,
		Display:   display,
		Status:    string(toolinstall.StatusInstalling),
		StartedAt: l.now(),
	})
	if err != nil {
		return fmt.Errorf("toolInstallLedger.RecordInstall: %w", err)
	}
	l.track(handle, pendingToolInstallOp{id: id, tool: tool, from: toolinstall.StatusInstalling})
	return nil
}

// track registers handle as settling op, then reconciles against the
// manager's exit status so an exit that fired before registration is not
// lost.
func (l *toolInstallLedger) track(handle string, op pendingToolInstallOp) {
	l.mu.Lock()
	l.pending[handle] = op
	l.tracked[op.id] = true
	status := l.exitStatus
	l.mu.Unlock()
	if status == nil {
		return
	}
	exited, code, known := status(handle)
	switch {
	case !known:
		// The handle is already gone: its exit was never observed here.
		l.settle(handle, toolinstall.EventOrphaned, 0)
	case exited:
		l.settle(handle, toolinstall.ExitEvent(code), code)
	}
}

// observeExit is called from the terminal stack's OnExit closure for EVERY
// PTY session; only handles this ledger registered settle anything.
func (l *toolInstallLedger) observeExit(handle string, code int) {
	if l == nil {
		return
	}
	l.settle(handle, toolinstall.ExitEvent(code), code)
}

// liveHandle returns the PTY handle a live install / uninstall of record id
// runs in ("" when no PTY in this process tracks it).
func (l *toolInstallLedger) liveHandle(id int64) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	for h, op := range l.pending {
		if op.id == id {
			return h
		}
	}
	return ""
}

// liveRecord renders row and adds what only this process knows: the live
// terminal handle and whether the run may be waiting for input (T-034j).
func (l *toolInstallLedger) liveRecord(row store.ToolInstallRow) dashapi.ToolInstallRecord {
	rec := toToolInstallRecord(row)
	st := toolinstall.Status(row.Status)
	if !toolinstall.InFlight(st) {
		return rec
	}
	rec.TerminalHandle = l.liveHandle(row.ID)
	phaseStart := row.StartedAt
	if st == toolinstall.StatusUninstalling && row.UninstallStartedAt != nil {
		phaseStart = *row.UninstallStartedAt
	}
	rec.MaybeWaitingForInput = toolinstall.MaybeWaitingForInput(st, phaseStart, l.now())
	return rec
}

// settle applies ev to the record handle tracks — exactly once.
func (l *toolInstallLedger) settle(handle string, ev toolinstall.Event, code int) {
	l.mu.Lock()
	op, ok := l.pending[handle]
	if ok {
		delete(l.pending, handle)
		delete(l.tracked, op.id)
	}
	l.mu.Unlock()
	if !ok {
		return
	}
	to, allowed := toolinstall.Next(op.from, ev)
	if !allowed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	finish := store.ToolInstallFinish{Status: string(to), ExitCode: code, At: l.now()}
	if ev != toolinstall.EventOrphaned && l.resolveBin != nil {
		finish.ResolvedBin = l.resolveBin(op.tool)
	}
	// An install that exited 0 is "installed" only when its binary resolves
	// afterwards; otherwise it is its own outcome (T-034j).
	if op.from == toolinstall.StatusInstalling && ev == toolinstall.EventExitOK {
		ev = toolinstall.InstallExitEvent(code, finish.ResolvedBin != "")
		if to, allowed = toolinstall.Next(op.from, ev); !allowed {
			return
		}
		finish.Status = string(to)
	}
	var err error
	if op.from == toolinstall.StatusInstalling {
		_, err = l.st.FinishToolInstall(ctx, op.id, string(op.from), finish)
	} else {
		_, err = l.st.FinishToolUninstall(ctx, op.id, string(op.from), finish)
	}
	if err != nil {
		l.logger.Warn("tool install ledger: settling record failed", "tool", op.tool, "id", op.id, "err", err)
		return
	}
	l.logger.Info("tool install ledger: record settled", "tool", op.tool, "id", op.id, "status", to, "exit_code", code, "resolved_bin", finish.ResolvedBin)
	if to == toolinstall.StatusInstalled && finish.ResolvedBin != "" {
		l.mu.Lock()
		fn := l.onInstalled
		l.mu.Unlock()
		if fn != nil {
			fn(op.tool)
		}
	}
}

// reconcileOrphan settles an in-flight row no live process here tracks (the
// daemon that spawned it is gone) and returns the row as it now stands.
func (l *toolInstallLedger) reconcileOrphan(ctx context.Context, row store.ToolInstallRow) store.ToolInstallRow {
	st := toolinstall.Status(row.Status)
	if !toolinstall.InFlight(st) {
		return row
	}
	phaseStart := row.StartedAt
	if st == toolinstall.StatusUninstalling {
		phaseStart = time.Time{}
		if row.UninstallStartedAt != nil {
			phaseStart = *row.UninstallStartedAt
		}
	}
	if !phaseStart.Before(l.bootAt) {
		return row // begun by THIS process: it settles it
	}
	l.mu.Lock()
	live := l.tracked[row.ID]
	l.mu.Unlock()
	if live {
		return row
	}
	to, ok := toolinstall.Next(st, toolinstall.EventOrphaned)
	if !ok {
		return row
	}
	if done, err := l.st.SetToolInstallStatus(ctx, row.ID, row.Status, string(to)); err == nil && done {
		row.Status = string(to)
	} else if err == nil {
		// Lost the CAS to a concurrent settle: re-read.
		if fresh, found, err := l.st.GetToolInstall(ctx, row.ID); err == nil && found {
			return fresh
		}
	}
	return row
}

// toRecord renders a row for the wire, resolving every capability through
// internal/toolinstall (never a channel or tool name branch here).
func toToolInstallRecord(row store.ToolInstallRow) dashapi.ToolInstallRecord {
	st := toolinstall.Status(row.Status)
	rec := dashapi.ToolInstallRecord{
		ID:                 row.ID,
		Tool:               row.Tool,
		Channel:            row.Channel,
		Package:            row.Package,
		Command:            row.Display,
		Status:             row.Status,
		ExitCode:           row.ExitCode,
		InstalledAt:        row.StartedAt,
		FinishedAt:         row.FinishedAt,
		ResolvedBin:        row.ResolvedBin,
		CanForget:          toolinstall.Forgettable(st),
		UninstallStartedAt: row.UninstallStartedAt,
		UninstalledAt:      row.UninstallFinishedAt,
		UninstallExitCode:  row.UninstallExitCode,
	}
	if toolinstall.HasDefinedUninstall(row.Channel) {
		if plan, err := toolinstall.PlanUninstall(row.Channel, row.Argv); err == nil {
			rec.UninstallCommand = plan.Display
			rec.CanUninstall = toolinstall.Uninstallable(st)
		} else {
			rec.UninstallGuidance = toolinstall.NoDefinedUninstallGuidance
		}
	} else {
		rec.UninstallGuidance = toolinstall.GuidanceFor(row.Channel)
	}
	if row.UninstallDisplay != "" {
		rec.UninstallCommand = row.UninstallDisplay
	}
	return rec
}

// Lookup implements dashapi.ToolInstallLedger.
func (l *toolInstallLedger) Lookup(ctx context.Context, tool string) (dashapi.ToolInstallRecord, bool, error) {
	row, ok, err := l.st.LatestToolInstall(ctx, tool)
	if err != nil || !ok {
		return dashapi.ToolInstallRecord{}, ok, err
	}
	return l.liveRecord(l.reconcileOrphan(ctx, row)), true, nil
}

// List implements dashapi.ToolInstallLedger.
func (l *toolInstallLedger) List(ctx context.Context) ([]dashapi.ToolInstallRecord, error) {
	rows, err := l.st.ListLatestToolInstalls(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dashapi.ToolInstallRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, l.liveRecord(l.reconcileOrphan(ctx, row)))
	}
	return out, nil
}

// latestFor loads the record the operator confirmed: it must be tool's latest
// not-forgotten record.
func (l *toolInstallLedger) latestFor(ctx context.Context, tool string, id int64) (store.ToolInstallRow, error) {
	row, ok, err := l.st.LatestToolInstall(ctx, tool)
	if err != nil {
		return store.ToolInstallRow{}, err
	}
	if !ok {
		return store.ToolInstallRow{}, dashapi.ErrToolInstallNotFound
	}
	if row.ID != id {
		return store.ToolInstallRow{}, dashapi.ErrToolInstallStale
	}
	return l.reconcileOrphan(ctx, row), nil
}

// Uninstall implements dashapi.ToolInstallLedger.
func (l *toolInstallLedger) Uninstall(ctx context.Context, tool string, id int64, spawn func(argv []string) (string, error)) (dashapi.ToolUninstallStarted, error) {
	row, err := l.latestFor(ctx, tool, id)
	if err != nil {
		return dashapi.ToolUninstallStarted{}, err
	}
	plan, err := toolinstall.PlanUninstall(row.Channel, row.Argv)
	if err != nil {
		guidance := toolinstall.GuidanceFor(row.Channel)
		if guidance == "" {
			guidance = toolinstall.NoDefinedUninstallGuidance
		}
		return dashapi.ToolUninstallStarted{}, fmt.Errorf("%w: %s", dashapi.ErrToolUninstallUnsupported, guidance)
	}
	if !toolinstall.Uninstallable(toolinstall.Status(row.Status)) {
		return dashapi.ToolUninstallStarted{}, dashapi.ErrToolInstallStale
	}
	argv := append([]string(nil), plan.Argv...)
	argv[0] = l.liveProgram(argv[0])
	from := toolinstall.UninstallableFrom()
	fromStr := make([]string, len(from))
	for i, s := range from {
		fromStr[i] = string(s)
	}
	prev, claimed, err := l.st.ClaimToolUninstall(ctx, id, fromStr, string(toolinstall.StatusUninstalling), argv, plan.Display, l.now())
	if err != nil {
		return dashapi.ToolUninstallStarted{}, err
	}
	if !claimed {
		return dashapi.ToolUninstallStarted{}, dashapi.ErrToolInstallStale
	}
	handle, err := spawn(argv)
	if err != nil {
		rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, rerr := l.st.RevertToolUninstall(rctx, id, string(toolinstall.StatusUninstalling), prev); rerr != nil {
			l.logger.Warn("tool install ledger: reverting a failed uninstall claim failed", "tool", tool, "id", id, "err", rerr)
		}
		return dashapi.ToolUninstallStarted{}, err
	}
	l.track(handle, pendingToolInstallOp{id: id, tool: tool, from: toolinstall.StatusUninstalling})
	return dashapi.ToolUninstallStarted{RecordID: id, Handle: handle, Tool: tool, Command: plan.Display, Package: plan.Package}, nil
}

// liveProgram keeps the recorded program when it still exists, else
// re-resolves it by base name on the merged PATH (an nvm switch can move
// npm), else falls back to the bare name for the spawner's own PATH lookup.
func (l *toolInstallLedger) liveProgram(prog string) string {
	if !filepath.IsAbs(prog) {
		return prog
	}
	if fi, err := os.Stat(prog); err == nil && fi.Mode().IsRegular() {
		return prog
	}
	base := filepath.Base(prog)
	if l.lookArgv0 != nil {
		if abs, ok := l.lookArgv0(base); ok {
			return abs
		}
	}
	return base
}

// Forget implements dashapi.ToolInstallLedger.
func (l *toolInstallLedger) Forget(ctx context.Context, tool string, id int64) error {
	row, err := l.latestFor(ctx, tool, id)
	if err != nil {
		return err
	}
	if !toolinstall.Forgettable(toolinstall.Status(row.Status)) {
		return dashapi.ErrToolInstallStale
	}
	from := toolinstall.ForgettableFrom()
	fromStr := make([]string, len(from))
	for i, s := range from {
		fromStr[i] = string(s)
	}
	ok, err := l.st.ForgetToolInstall(ctx, id, fromStr, l.now())
	if err != nil {
		return err
	}
	if !ok {
		return dashapi.ErrToolInstallStale
	}
	return nil
}

// Compile-time check.
var _ dashapi.ToolInstallLedger = (*toolInstallLedger)(nil)
