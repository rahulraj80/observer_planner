//go:build !sbo_omit_terminals

package main

import (
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/termsvc"
)

// terminal_config_live.go is the ONE owner of the [terminal] settings a running
// daemon enforces for launches: the [terminal.launch] policy (fresh agent,
// shell, allowed tools, allowed project roots) and the [terminal.ssh] profile
// list the SSH terminal picker and the instance switcher resolve against
// (T-488 / T-489).
//
// Before this, terminalLaunchPolicy and the instance switcher were fed ONE
// snapshot of config at daemon start, so every dashboard save of the launch
// policy (and every profile change) said "takes effect on the next daemon
// restart", and the New Terminal dialog sent the operator to config.toml and a
// restart. Now termsvc (Options.PolicySource) and the instance switcher
// (sshforward.Options.Source) both read through this holder, so the two can
// never disagree about which machines exist, and a save binds on the next
// launch.
//
// The reload trigger is the same config-file stamp the sandbox holder uses
// (mtime + size, terminal_sandbox.go): the dashboard's save writes the file,
// `observer config reload` and a hand edit change it too, and one os.Stat per
// read catches all three. A config that does not load (a hand edit with a
// typo) keeps the last good settings and logs, rather than tearing down a
// working policy; the dashboard's own writes are validated before they are
// written, so they never land here invalid.
type terminalConfigHolder struct {
	// configPath is the daemon's resolved config.toml. Empty pins the holder
	// to its boot-time settings (nothing to stat).
	configPath string
	logger     *slog.Logger
	// load re-reads the file. A package-level seam only so a test can force
	// the "edited into an invalid config" path without a lying filesystem.
	load func(path string) (config.Config, error)

	cur atomic.Pointer[config.TerminalConfig]

	// mu serializes stat + reload, so two concurrent launches cannot both
	// rebuild, and a reader never sees a stamp that its settings do not match.
	mu       sync.Mutex
	stampMod time.Time
	stampLen int64
	stamped  bool
}

// newTerminalConfigHolder seeds the holder with the boot-time [terminal] block
// (already loaded and validated by the daemon) and stamps the file, so the
// first read is not a redundant reload.
func newTerminalConfigHolder(tc config.TerminalConfig, configPath string, logger *slog.Logger) *terminalConfigHolder {
	h := &terminalConfigHolder{
		configPath: strings.TrimSpace(configPath),
		logger:     logger,
		load: func(path string) (config.Config, error) {
			return config.Load(config.LoadOptions{GlobalPath: path})
		},
	}
	seed := tc
	h.cur.Store(&seed)
	if info, err := os.Stat(h.configPath); h.configPath != "" && err == nil {
		h.stampMod, h.stampLen, h.stamped = info.ModTime(), info.Size(), true
	}
	return h
}

// terminal returns the current [terminal] block, reloading it first when the
// config file changed on disk. Nil-safe: a nil holder returns the zero block
// (every launch shape off), the fail-closed answer.
func (h *terminalConfigHolder) terminal() config.TerminalConfig {
	if h == nil {
		return config.TerminalConfig{}
	}
	h.refresh()
	return *h.cur.Load()
}

// refresh reloads the block when the file's stamp moved. Best-effort: an
// unreadable file or a config that fails to load keeps the current settings.
func (h *terminalConfigHolder) refresh() {
	if h.configPath == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	info, err := os.Stat(h.configPath)
	if err != nil {
		return
	}
	if h.stamped && info.ModTime().Equal(h.stampMod) && info.Size() == h.stampLen {
		return
	}
	cfg, err := h.load(h.configPath)
	if err != nil {
		// Leave the stamp unmoved so the next read retries once the file is
		// fixed; keep enforcing the last good settings meanwhile.
		if h.logger != nil {
			h.logger.Warn("terminal: config changed but could not be reloaded - keeping the previous launch policy and SSH profiles", "err", err)
		}
		return
	}
	tc := cfg.Terminal
	h.cur.Store(&tc)
	h.stampMod, h.stampLen, h.stamped = info.ModTime(), info.Size(), true
}

// Policy is the termsvc.Options.PolicySource: the launch authorization
// resolved from the CURRENT [terminal] block, through the same
// terminalLaunchPolicy mapping the boot path always used.
func (h *terminalConfigHolder) Policy() termsvc.Policy {
	return terminalLaunchPolicy(h.terminal())
}
