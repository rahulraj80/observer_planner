//go:build !sbo_omit_processobs

package dashboard

import (
	"log/slog"
	"sync"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/processbridge/setup"
)

// processObsState is the process-observation part of Server (component
// processobs), embedded so its fields are promoted. It lives here, beside
// the process pages, so that a `-tags sbo_omit_processobs` build (which
// links process_feature_omit.go instead) keeps internal/processobs/*,
// internal/processbridge/setup and their modules out of the binary
// (modular packaging plan 5.1, lane MP-L1B).
type processObsState struct {
	// processCapabilityFn probes what OS process-capture backends can ACTUALLY
	// capture on this host (poll native enumerator by GOOS, eBPF privilege
	// probe, WSL bridge resolution): the platform truth the enable-capture verb
	// grounds its decision in, distinct from processBackendRunnable's
	// selector-vocabulary check. Injected as a field so the enable-capture
	// handler tests exercise every GOOS-shaped capability set without needing
	// the real platform (repo injected-IO discipline). Defaults to
	// realProcessCapability in New (initProcessObs).
	processCapabilityFn func(config.ProcessConfig) processCapability

	// etwSetupEnvFn supplies the I/O seam GET /api/process/etw/status and
	// POST /api/process/etw/register both plan through (PATH lookup for
	// schtasks.exe, the read-only `schtasks /Query` probe, the Windows
	// binary and username resolution). Injected as a field so handler tests
	// exercise every planner outcome WITHOUT touching the real Task
	// Scheduler, the same injected-IO discipline processCapabilityFn follows
	// and the same reason browserhost's tests inject fake registry hives. nil
	// means the production probes.
	etwSetupEnvFn func() setup.Env

	// etwProbes rate-bounds the STATUS endpoint's probes (see etwProbeTTL).
	// GET /api/process/etw/status is capability View, so a paired remote device
	// can poll it, and each applicable poll execs schtasks.exe (plus cmd.exe
	// for the Windows user name). The elevation broker does not share it.
	etwProbes etwProbeCache

	// etwRegisterOps remembers what the last elevated-ETW registration PTY was
	// actually started with, keyed by its handle. The launch manager's labelled
	// setup ops are idempotent (a second POST while one is live gets the SAME
	// handle back), so without this the response would describe the plan THIS
	// request computed rather than the command the reused PTY is running.
	etwRegisterMu  sync.Mutex
	etwRegisterOps etwRegisterRecord
}

// initProcessObs binds the production capability probe; New calls it once.
func (s *Server) initProcessObs(logger *slog.Logger) {
	s.processCapabilityFn = realProcessCapability(logger)
}
