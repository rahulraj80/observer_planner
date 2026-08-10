package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
	"github.com/marmutapp/superbased-observer/internal/intelligence/dashboard"
	notifydigest "github.com/marmutapp/superbased-observer/internal/notify/digest"
	"github.com/marmutapp/superbased-observer/internal/notify/email"
)

// digest.go is the NODE-SIDE scheduled digest surface (gap-register G13):
// a weekly/monthly per-tool/per-model spend rollup plus the daily briefing,
// both emailed through the shared [email] channel. It mirrors obsAlertLoop's
// daemon-resident, fail-soft, P1 posture — a digest failure logs a warning and
// never cancels proxy / watcher / dashboard. The PURE composition lives in
// internal/notify/digest; this file owns the data assembly (cost.Engine,
// dashboard.Server.ComposeBriefing) + the send-once markers (digest_state).

const (
	nodeDigestKind      = "node"
	nodeDigestTopN      = 8
	nodeDigestTickEvery = time.Hour
	briefingDigestKind  = "briefing"
)

// nodeDigestRunner assembles + delivers the node digest. It is built from a
// loaded config + DB handle and reused by both the daemon loop and the CLI.
type nodeDigestRunner struct {
	cfg      config.Config
	db       *sql.DB
	engine   *cost.Engine
	notifier *email.Notifier // may be nil for dry-run
	logger   *slog.Logger
	now      func() time.Time
	// send delivers one composed message. Defaults to notifier.Send; tests
	// substitute a recorder.
	send func(context.Context, email.Message)
}

// digestClock is the wall-clock source for a CLI-built digest runner. It is a
// package var (not an inline closure) so the CLI-path test can pin it to a
// fixed instant — the direct-runner tests already inject r.now, but the cobra
// `digest send` command builds its own runner with no injection point.
var digestClock = func() time.Time { return time.Now().UTC() }

func newNodeDigestRunner(cfg config.Config, db *sql.DB, notifier *email.Notifier, logger *slog.Logger) *nodeDigestRunner {
	r := &nodeDigestRunner{
		cfg:      cfg,
		db:       db,
		engine:   cost.NewEngine(cfg.Intelligence),
		notifier: notifier,
		logger:   logger,
		now:      digestClock,
	}
	r.send = func(ctx context.Context, m email.Message) { r.notifier.Send(ctx, m) }
	return r
}

// digestLoop is the daemon-resident scheduler: it ticks hourly and, once the
// send_hour gate has passed and the current period hasn't been sent yet,
// composes + emails the digest exactly once. Returns immediately when the
// digest is off (or [email] is off, since a digest is delivered by email).
func digestLoop(ctx context.Context, configPath string) {
	cfg, db, cleanup, err := loadConfigAndDB(ctx, configPath)
	if err != nil {
		return
	}
	defer cleanup()
	if !cfg.Digest.Enabled {
		return
	}
	logger := newLogger(cfg.Observer.LogLevel)
	if !cfg.Email.Enabled {
		logger.Warn("digest disabled: [digest].enabled set but [email] is not enabled")
		return
	}
	notifier, nerr := email.NewNotifier(cfg.Email, logger)
	if nerr != nil {
		logger.Warn("digest disabled: invalid [email] config", "err", nerr)
		return
	}
	r := newNodeDigestRunner(cfg, db, notifier, logger)
	br := newBriefingDigestRunner(cfg, db, notifier, logger)
	logger.Info("digest scheduler started",
		"frequency", cfg.Digest.FrequencyOrDefault(), "send_hour", cfg.Digest.SendHourOrDefault())

	r.tick(ctx)
	br.tick(ctx)
	ticker := time.NewTicker(nodeDigestTickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
			br.tick(ctx)
		}
	}
}

// tick sends the digest for the most-recently-completed period exactly once,
// gated by send_hour and the persisted marker. Fail-soft throughout.
func (r *nodeDigestRunner) tick(ctx context.Context) {
	period, ready := notifydigest.DuePeriod(r.cfg.Digest.FrequencyOrDefault(), r.cfg.Digest.SendHourOrDefault(), r.now())
	if !ready {
		return
	}
	last, err := r.lastSentPeriod(ctx)
	if err != nil {
		r.logger.Warn("digest: read marker failed", "err", err)
		return
	}
	if last == period.Key {
		return
	}
	data, err := r.assemble(ctx, period)
	if err != nil {
		r.logger.Warn("digest: assemble failed", "period", period.Key, "err", err)
		return
	}
	r.send(ctx, notifydigest.Compose(data))
	if err := r.recordSent(ctx, period.Key); err != nil {
		r.logger.Warn("digest: persist marker failed", "period", period.Key, "err", err)
		return
	}
	r.logger.Info("digest sent", "period", period.Key, "total_usd", data.TotalUSD)
}

// assemble builds the content-free node digest Data for a period: current +
// prior per-model/per-tool spend, ranked movers, and the local alert count.
func (r *nodeDigestRunner) assemble(ctx context.Context, period notifydigest.Period) (notifydigest.Data, error) {
	curModels, curTotal, err := r.spendItems(ctx, cost.GroupByModel, period.Start, period.End)
	if err != nil {
		return notifydigest.Data{}, err
	}
	curTools, _, err := r.spendItems(ctx, cost.GroupByTool, period.Start, period.End)
	if err != nil {
		return notifydigest.Data{}, err
	}
	priorStart := period.Start.Add(-period.End.Sub(period.Start))
	priorModels, priorTotal, err := r.spendItems(ctx, cost.GroupByModel, priorStart, period.Start)
	if err != nil {
		return notifydigest.Data{}, err
	}

	title := "Weekly cost digest"
	if r.cfg.Digest.FrequencyOrDefault() == notifydigest.Monthly {
		title = "Monthly cost digest"
	}
	data := notifydigest.Data{
		Title:         title,
		PeriodLabel:   period.Label,
		TotalUSD:      curTotal,
		PriorTotalUSD: priorTotal,
		HasPrior:      true,
		Breakdowns: []notifydigest.Breakdown{
			{Title: "Spend by tool", Items: capItems(curTools, nodeDigestTopN)},
			{Title: "Spend by model", Items: capItems(curModels, nodeDigestTopN)},
		},
		Movers:  notifydigest.RankMovers(curModels, priorModels, nodeDigestTopN),
		Version: version,
		To:      r.cfg.Digest.To,
	}
	if len(data.To) == 0 {
		data.To = r.cfg.Email.To
	}
	if n, ok := r.alertCount(ctx, period.Start, period.End); ok {
		data.AlertCount = n
		data.HasAlertCount = true
	}
	return data, nil
}

// spendItems runs one cost summary over [start, end) and returns its rows as
// digest line items plus the window total.
func (r *nodeDigestRunner) spendItems(ctx context.Context, groupBy cost.GroupBy, start, end time.Time) ([]notifydigest.LineItem, float64, error) {
	sum, err := r.engine.Summary(ctx, r.db, cost.Options{
		Since:   start,
		Until:   end,
		GroupBy: groupBy,
		Source:  cost.SourceAuto,
		Limit:   200,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("digest.spendItems: %w", err)
	}
	items := make([]notifydigest.LineItem, 0, len(sum.Rows))
	for _, row := range sum.Rows {
		items = append(items, notifydigest.LineItem{Label: row.Key, CostUSD: row.CostUSD})
	}
	return items, sum.TotalCost, nil
}

// capItems truncates items to the first n (already cost-sorted by the engine).
func capItems(items []notifydigest.LineItem, n int) []notifydigest.LineItem {
	if n > 0 && len(items) > n {
		return items[:n]
	}
	return items
}

// alertCount returns the number of node obs_alert_events fired in [start, end).
// It is tolerant: when the table is absent (obs compiled out / never created)
// it returns ok=false so the digest simply omits the alert line.
func (r *nodeDigestRunner) alertCount(ctx context.Context, start, end time.Time) (int, bool) {
	var exists int
	if err := r.db.QueryRowContext(ctx,
		`SELECT 1 FROM sqlite_master WHERE type='table' AND name='obs_alert_events'`).Scan(&exists); err != nil {
		return 0, false
	}
	var n int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM obs_alert_events WHERE fired_at >= ? AND fired_at < ?`,
		start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339)).Scan(&n); err != nil {
		return 0, false
	}
	return n, true
}

// lastSentPeriod returns the Period.Key of the last-sent node digest, or "".
func (r *nodeDigestRunner) lastSentPeriod(ctx context.Context) (string, error) {
	return digestLastSent(ctx, r.db, nodeDigestKind)
}

// recordSent upserts the node digest's send-once marker.
func (r *nodeDigestRunner) recordSent(ctx context.Context, periodKey string) error {
	return digestRecordSent(ctx, r.db, nodeDigestKind, periodKey, r.now())
}

// digestLastSent returns the last-sent Period.Key for a digest kind, or "".
func digestLastSent(ctx context.Context, db *sql.DB, kind string) (string, error) {
	var key string
	err := db.QueryRowContext(ctx, `SELECT last_period FROM digest_state WHERE kind = ?`, kind).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("digest.lastSent: %w", err)
	}
	return key, nil
}

// digestRecordSent upserts the send-once marker for a digest kind. The
// digest_state table is keyed by kind, so each digest variant (node, briefing)
// de-dups independently.
func digestRecordSent(ctx context.Context, db *sql.DB, kind, periodKey string, now time.Time) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO digest_state (kind, last_period, sent_at) VALUES (?, ?, ?)
ON CONFLICT(kind) DO UPDATE SET last_period = excluded.last_period, sent_at = excluded.sent_at`,
		kind, periodKey, now.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("digest.recordSent: %w", err)
	}
	return nil
}

// briefingDigestRunner assembles + delivers the daily briefing digest: one
// completed local-machine day rendered through notifydigest.ComposeBriefing.
// It reuses the EXACT facts scan + pricing the dashboard serves (/api/briefing)
// via a minimal dashboard.Server, and keeps its own send-once marker
// (digest_state kind='briefing') so a day is never emailed twice. Same
// fail-soft P1 posture as nodeDigestRunner.
type briefingDigestRunner struct {
	cfg      config.Config
	db       *sql.DB
	engine   *cost.Engine
	notifier *email.Notifier
	logger   *slog.Logger
	now      func() time.Time
	send     func(context.Context, email.Message)
}

func newBriefingDigestRunner(cfg config.Config, db *sql.DB, notifier *email.Notifier, logger *slog.Logger) *briefingDigestRunner {
	r := &briefingDigestRunner{
		cfg:      cfg,
		db:       db,
		engine:   cost.NewEngine(cfg.Intelligence),
		notifier: notifier,
		logger:   logger,
		now:      digestClock,
	}
	r.send = func(ctx context.Context, m email.Message) { r.notifier.Send(ctx, m) }
	return r
}

// tick sends the daily briefing digest for the most-recently-completed local
// day exactly once, gated by send_hour and the persisted marker.
func (r *briefingDigestRunner) tick(ctx context.Context) {
	period, ready := notifydigest.DuePeriod(notifydigest.Daily, r.cfg.Digest.SendHourOrDefault(), r.now())
	if !ready {
		return
	}
	last, err := digestLastSent(ctx, r.db, briefingDigestKind)
	if err != nil {
		r.logger.Warn("briefing digest: read marker failed", "err", err)
		return
	}
	if last == period.Key {
		return
	}
	data, err := r.assemble(ctx, period)
	if err != nil {
		r.logger.Warn("briefing digest: assemble failed", "period", period.Key, "err", err)
		return
	}
	r.send(ctx, notifydigest.ComposeBriefing(data))
	if err := digestRecordSent(ctx, r.db, briefingDigestKind, period.Key, r.now()); err != nil {
		r.logger.Warn("briefing digest: persist marker failed", "err", err)
		return
	}
	r.logger.Info("briefing digest sent", "period", period.Key)
}

// assemble builds the content-free daily briefing DigestData for a period by
// composing the real briefing through a minimal dashboard server and projecting
// its at_glance + activity onto the email shape.
func (r *briefingDigestRunner) assemble(ctx context.Context, period notifydigest.Period) (notifydigest.BriefingData, error) {
	srv, err := dashboard.New(dashboard.Options{DB: r.db, CostEngine: r.engine, Logger: r.logger})
	if err != nil {
		return notifydigest.BriefingData{}, err
	}
	b, err := srv.ComposeBriefing(ctx, period.Start.Format("2006-01-02"))
	if err != nil {
		return notifydigest.BriefingData{}, err
	}
	g := b.AtGlance
	tools := make([]notifydigest.LineItem, 0, len(b.Activity))
	for _, a := range b.Activity {
		tools = append(tools, notifydigest.LineItem{Label: a.Tool, CostUSD: a.SpendUSD})
	}
	savings := 0.0
	for _, o := range b.Opportunities {
		savings += o.SavingsUSD
	}
	to := r.cfg.Digest.To
	if len(to) == 0 {
		to = r.cfg.Email.To
	}
	return notifydigest.BriefingData{
		Title:                 "Daily briefing",
		PeriodLabel:           period.Label,
		TotalUSD:              g.SpendUSD,
		Sessions:              g.Sessions,
		Turns:                 g.Turns,
		InputTokens:           g.InputTokens,
		OutputTokens:          g.OutputTokens,
		CacheReadTokens:       g.CacheReadTokens,
		CacheCreationTokens:   g.CacheCreationTokens,
		CacheHitRate:          g.CacheHitRate,
		ErrorTurns:            g.ErrorTurns,
		ActiveHours:           g.ActiveHours,
		FastShare:             g.FastShare,
		Tools:                 tools,
		OpportunityCount:      len(b.Opportunities),
		OpportunitySavingsUSD: savings,
		Version:               version,
		To:                    to,
	}, nil
}

// newDigestCmd is `observer digest send [--dry-run]`: compose the current
// period's personal cost digest and email it (or print it, with --dry-run).
func newDigestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "digest",
		Short: "Scheduled personal cost digests (compose/send on demand)",
	}
	cmd.AddCommand(newDigestSendCmd())
	return cmd
}

func newDigestSendCmd() *cobra.Command {
	var configPath string
	var dryRun bool
	var kind string
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Compose the scheduled digest for the most-recent completed period and email it",
		Long: "Compose the scheduled digest for the most-recently-COMPLETED period and\n" +
			"email it, then record the send-once marker. With --kind node (default):\n" +
			"the personal cost digest ([digest].frequency, default weekly) — per-tool\n" +
			"+ per-model observed spend with the prior-period delta. With --kind\n" +
			"briefing: the daily briefing for the most-recently-completed local day\n" +
			"(sessions, turns, spend, tokens, cache hit rate, opportunities).\n" +
			"Recipients come from [digest].to (or [email].to). With --dry-run the\n" +
			"composed email is printed to stdout and nothing is sent; a real send\n" +
			"requires [email].enabled.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, db, cleanup, err := loadConfigAndDB(cmd.Context(), configPath)
			if err != nil {
				return err
			}
			defer cleanup()
			logger := newLogger(cfg.Observer.LogLevel)

			var notifier *email.Notifier
			if !dryRun {
				if !cfg.Email.Enabled {
					return errors.New("digest send: [email].enabled is false — enable [email] or use --dry-run")
				}
				n, nerr := email.NewNotifier(cfg.Email, logger)
				if nerr != nil {
					return fmt.Errorf("digest send: %w", nerr)
				}
				notifier = n
			}

			switch kind {
			case "briefing":
				br := newBriefingDigestRunner(cfg, db, notifier, logger)
				period, _ := notifydigest.DuePeriod(notifydigest.Daily, cfg.Digest.SendHourOrDefault(), br.now())
				data, err := br.assemble(cmd.Context(), period)
				if err != nil {
					return err
				}
				msg := notifydigest.ComposeBriefing(data)
				if dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "To: %v\nSubject: %s\n\n%s\n", data.To, msg.Subject, msg.Text)
					return nil
				}
				notifier.Send(cmd.Context(), msg)
				if err := digestRecordSent(cmd.Context(), db, briefingDigestKind, period.Key, br.now()); err != nil {
					logger.Warn("briefing digest: persist marker failed", "err", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "briefing digest sent")
				return nil
			default:
				r := newNodeDigestRunner(cfg, db, notifier, logger)
				period, _ := notifydigest.DuePeriod(cfg.Digest.FrequencyOrDefault(), cfg.Digest.SendHourOrDefault(), r.now())
				data, err := r.assemble(cmd.Context(), period)
				if err != nil {
					return err
				}
				msg := notifydigest.Compose(data)
				if dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "To: %v\nSubject: %s\n\n%s\n", data.To, msg.Subject, msg.Text)
					return nil
				}
				notifier.Send(cmd.Context(), msg)
				if err := r.recordSent(cmd.Context(), period.Key); err != nil {
					logger.Warn("digest: persist marker failed", "err", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "digest sent")
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to config.toml")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the composed email to stdout instead of sending")
	cmd.Flags().StringVar(&kind, "kind", "node", "digest kind: node (weekly/monthly cost) or briefing (daily)")
	return cmd
}
