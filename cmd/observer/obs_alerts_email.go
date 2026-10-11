//go:build !sbo_omit_notify_email

package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/notify/email"
)

// obs_alerts_email.go is the email channel of the node alert evaluator
// (obs_alerts.go), split out so a build without notify.email (-tags
// sbo_omit_notify_email, lane MP-L1E) leaves internal/notify/email out;
// obs_alerts_email_omit.go is its pair.

// buildAlertEmailNotifier returns a fail-soft email notifier when [email] is
// enabled AND [observability.alerts].email is set, else nil (email off). A
// structurally-invalid [email] block logs a warning and yields nil so alerting
// is never blocked by a mail misconfiguration. EmailTo overrides [email].to.
func buildAlertEmailNotifier(cfg config.Config, logger *slog.Logger) alertEmailSender {
	if !cfg.Email.Enabled || !cfg.Observability.Alerts.Email {
		return nil
	}
	ec := cfg.Email
	if to := cfg.Observability.Alerts.EmailTo; len(to) > 0 {
		ec.To = to
	}
	n, err := email.NewNotifier(ec, logger)
	if err != nil {
		logger.Warn("obs-alert email disabled: invalid [email] config", "err", err)
		return nil
	}
	return func(ctx context.Context, f obsFiredAlert) { n.Send(ctx, composeAlertEmail(f, version)) }
}

// composeAlertEmail renders a fired node alert as an email Message from the
// SAME content-bounded fields the webhook payload carries. version stamps the
// footer. Pure - no I/O; the Notifier does the delivery.
func composeAlertEmail(f obsFiredAlert, version string) email.Message {
	subject := fmt.Sprintf("[Observer] Alert: %s - %s", f.RuleName, f.Metric)
	heading := fmt.Sprintf("Rule %q fired: %s is %g (%s %g) over the last %d minute(s).",
		f.RuleName, f.Metric, f.Value, comparatorSymbol(f.Comparator), f.Threshold, f.WindowMinutes)
	return email.Compose(email.ComposeParams{
		Subject: subject,
		Heading: heading,
		Fields: []email.Field{
			{Label: "Rule", Value: f.RuleName},
			{Label: "Metric", Value: f.Metric},
			{Label: "Observed value", Value: fmt.Sprintf("%g", f.Value)},
			{Label: "Condition", Value: fmt.Sprintf("%s %g", comparatorSymbol(f.Comparator), f.Threshold)},
			{Label: "Window", Value: fmt.Sprintf("%d minute(s)", f.WindowMinutes)},
			{Label: "Fired at", Value: f.FiredAt.Format(time.RFC3339)},
		},
		Version: version,
	})
}
