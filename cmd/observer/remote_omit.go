//go:build sbo_omit_remote

package main

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/termcore"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// remote_omit.go stands in for remote.go, remote_enable.go and
// remotenotify.go in a build without the remote component (-tags
// sbo_omit_remote, lane MP-L1I, T-715): internal/remoteauth, remotecfg,
// remotenotify and tailnet are not linked. `observer remote` answers "not in
// this build" (exit 3), no RemoteController is built (the dashboard stays
// loopback-only, the same as remote disabled), no standing credential is
// ever recognised, and no session-finished push is sent.
//
// Operator rule D-514: a [remote] or [remote.notify] block this build cannot
// honour never stops `observer start`; the feature stays off and one log
// line names the build and the fix.
func init() { components.MarkOmitted("remote") }

// newRemoteCmd registers `observer remote` as a compiled-out verb.
func newRemoteCmd() *cobra.Command {
	return compiledOutVerb("remote", "Remote dashboard access (not in this build)", "remote")
}

// buildRemoteController builds nothing: remote access is not in this build.
func buildRemoteController(cfg config.Config, _ *sql.DB) dashapi.RemoteController {
	if line, off := settingOffLine("remote.enabled", cfg.Remote.Enabled, nil); off {
		slog.Default().Warn("remote: " + line + " The dashboard stays loopback-only.")
	}
	return nil
}

// isStandingCredential is always false: no standing terminal-control
// secret exists without the remote component.
func isStandingCredential(string) bool { return false }

// newSessionFinishedNotifier sends nothing: the remote notify rail is not in
// this build.
func newSessionFinishedNotifier(cfg config.Config, logger *slog.Logger) func(context.Context, termcore.SessionExit) error {
	if line, off := settingOffLine("remote.notify.enabled", cfg.Remote.Notify.Enabled, nil); off && logger != nil {
		logger.Warn("remote notify: " + line)
	}
	return nil
}
