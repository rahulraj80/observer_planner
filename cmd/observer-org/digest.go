// digest.go — `observer-org digest send` on-demand / dry-run report digest.

package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	notifyemail "github.com/marmutapp/superbased-observer/internal/notify/email"
	orgdb "github.com/marmutapp/superbased-observer/internal/orgserver/db"
	orgdigest "github.com/marmutapp/superbased-observer/internal/orgserver/digest"
)

// newDigestCmd groups the scheduled-digest on-demand triggers (gap-register
// G13). The scheduler runs inside `serve`; this command lets an operator
// compose/send the current period's digest out of band for testing.
func newDigestCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "digest",
		Short: "Scheduled report digests (compose/send on demand)",
	}
	cmd.AddCommand(newDigestSendCmd(configPath))
	return cmd
}

func newDigestSendCmd(configPath *string) *cobra.Command {
	var dryRun bool
	var kind string
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Compose the scheduled digest for the most-recent completed period and email it",
		Long: "Compose the scheduled digest for the most-recently-COMPLETED period and\n" +
			"email it to [digest].to (or [email].to). With --kind org (default):\n" +
			"the org spend digest ([digest].frequency, default weekly) — spend by\n" +
			"developer/model/project with the prior-period delta. With --kind\n" +
			"briefing: the daily manager briefing for the most-recently-completed\n" +
			"day (sessions, turns, spend, tokens, cache hit rate, opportunities;\n" +
			"docs/design/standup-briefing.md §4 Phase C). With --dry-run the\n" +
			"composed email is printed to stdout and nothing is sent. A real send\n" +
			"requires [email].enabled; --dry-run does not. On-demand sends bypass\n" +
			"the send_hour gate; a real send updates the send-once marker so the\n" +
			"same period's scheduled send is suppressed.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			db, err := orgdb.Open(ctx, orgdb.Options{Path: cfg.Server.DBPath})
			if err != nil {
				return err
			}
			defer db.Close()
			org, err := orgdb.EnsureOrg(ctx, db, cfg.Server.ExternalURL)
			if err != nil {
				return err
			}
			logger := newLogger(cfg.Server.LogLevel, "")

			// Real send needs the shared email channel; dry-run does not.
			var notifier *notifyemail.Notifier
			if !dryRun {
				if !cfg.Email.Enabled {
					return errors.New("digest send: [email].enabled is false — enable [email] or use --dry-run")
				}
				n, nerr := notifyemail.NewNotifier(cfg.Email, logger)
				if nerr != nil {
					return fmt.Errorf("digest send: %w", nerr)
				}
				notifier = n
			}

			var sched *orgdigest.Scheduler
			switch kind {
			case "briefing":
				sched = orgdigest.NewBriefingScheduler(db, org, cfg.Digest, notifier, version, cfg.Email.To, logger)
			case "", "org":
				sched = orgdigest.NewScheduler(db, org, cfg.Digest, notifier, version, cfg.Email.To, logger)
			default:
				return fmt.Errorf("digest send: unknown kind %q (want org or briefing)", kind)
			}
			if dryRun {
				msg, perr := sched.Preview(ctx)
				if perr != nil {
					return perr
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "To: %v\nSubject: %s\n\n%s\n", digestRecipients(cfg.Digest.To, cfg.Email.To), msg.Subject, msg.Text)
				return nil
			}
			if err := sched.SendNow(ctx); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "digest sent")
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the composed email to stdout instead of sending")
	cmd.Flags().StringVar(&kind, "kind", "org", "digest variant: org (spend digest) or briefing (daily manager briefing)")
	return cmd
}

// digestRecipients resolves the effective recipient list for the dry-run
// preview: [digest].to when set, else [email].to.
func digestRecipients(digestTo, emailTo []string) []string {
	if len(digestTo) > 0 {
		return digestTo
	}
	return emailTo
}
