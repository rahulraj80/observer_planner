//go:build sbo_omit_notify_email

package main

import (
	"context"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// notify_email_omit.go stands in for digest.go and obs_alerts_email.go in a
// build without the notify.email component (-tags sbo_omit_notify_email,
// lane MP-L1E, T-715): internal/notify/email and internal/notify/digest are
// not linked. [email] and [digest] still load (their shape is core,
// internal/notifycfg) but nothing sends: the digest scheduler does not run,
// `observer digest` answers "not in this build" (exit 3), and node alerts
// keep their webhook channel without the email one. The share card keeps
// its reporting window (internal/notifycfg.DuePeriod).
func init() { components.MarkOmitted("notify.email") }

// digestLoop runs no scheduler. A configured digest is reported once.
func digestLoop(_ context.Context, configPath string) {
	cfg, err := config.Load(config.LoadOptions{GlobalPath: configPath})
	if err != nil || !cfg.Digest.Enabled {
		return
	}
	slog.Default().Warn("digest disabled: email and digest (notify.email) is not in this build; [digest] is ignored")
}

// buildAlertEmailNotifier returns no sender: alerts keep the webhook only.
func buildAlertEmailNotifier(cfg config.Config, logger *slog.Logger) alertEmailSender {
	if cfg.Email.Enabled && cfg.Observability.Alerts.Email && logger != nil {
		logger.Warn("obs-alert email disabled: email and digest (notify.email) is not in this build")
	}
	return nil
}

// newDigestCmd registers `observer digest` as a compiled-out verb.
func newDigestCmd() *cobra.Command {
	return compiledOutVerb("digest", "Personal cost digest by email (not in this build)", "notify.email")
}
