// Package digest is the org-server SCHEDULER + data-assembly boundary for
// scheduled report digests (gap-register G13). It mirrors the budget / obsalert
// evaluator pattern: a goroutine that ticks on an interval, composes a
// content-free org spend rollup for the most-recently-completed period, and
// delivers it through the shared fail-soft email notifier.
//
// The PURE composition lives in internal/notify/digest; this package owns the
// SQL (rollup reuse + the send-once de-dup marker in digest_state) and the
// clock. Fail-soft + P1 like every evaluator: a digest failure logs a warning
// and never affects the server.
package digest

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	notifydigest "github.com/marmutapp/superbased-observer/internal/notify/digest"
	"github.com/marmutapp/superbased-observer/internal/notify/email"
	orgdb "github.com/marmutapp/superbased-observer/internal/orgserver/db"
	"github.com/marmutapp/superbased-observer/internal/orgserver/rollup"
)

// stateKind is the digest_state row key for the org digest; briefingKind is
// the key for the daily manager briefing digest (Phase C of
// docs/design/standup-briefing.md §4). Each digest variant gets its own row so
// their send-once markers are independent.
const (
	stateKind    = "org"
	briefingKind = "briefing"
)

// tickInterval is how often the scheduler re-evaluates whether a digest is due.
// Hourly gives the send_hour gate its granularity.
const tickInterval = time.Hour

// topMoversN caps the movers list and each spend breakdown in the email.
const topMoversN = 8

// Scheduler drives a report digest. Non-nil only when [digest].enabled AND the
// shared [email] notifier is present; the server launches Run alongside the
// budget evaluator. A briefing scheduler (NewBriefingScheduler) composes the
// daily manager briefing instead of the weekly/monthly spend digest; the two
// share the fail-soft tick/marker machinery and only the assemble step + the
// digest_state kind differ.
type Scheduler struct {
	db        *sql.DB
	org       orgdb.Org
	cfg       notifydigest.Config
	notifier  *email.Notifier
	version   string
	defaultTo []string
	logger    *slog.Logger
	now       func() time.Time
	// kind is the digest_state row key this scheduler reads/writes.
	kind string
	// briefing selects the assemble path: the daily manager briefing (true)
	// vs the org spend digest (false).
	briefing bool
	// interval is the tick cadence (hourly in production; overridable in tests).
	interval time.Duration
	// send delivers one composed message. Defaults to notifier.Send; tests
	// substitute a recorder (mirrors budget.Evaluator.Deliver).
	send func(context.Context, email.Message)
}

// NewScheduler builds the org spend-digest scheduler. notifier must be non-nil
// (the caller only constructs a scheduler when [email] is enabled). defaultTo is
// the [email].to fallback used when [digest].to is empty.
func NewScheduler(db *sql.DB, org orgdb.Org, cfg notifydigest.Config, notifier *email.Notifier, version string, defaultTo []string, logger *slog.Logger) *Scheduler {
	return newScheduler(db, org, cfg, notifier, version, defaultTo, logger, stateKind, false)
}

// NewBriefingScheduler builds the daily manager-briefing scheduler (Phase C of
// docs/design/standup-briefing.md §4). Same contract as NewScheduler, but the
// assemble step composes rollup.Briefing for the most-recently-completed day
// (admin scope) and the send-once marker lives under kind='briefing', so the
// daily email and the weekly/monthly spend digest are independent.
func NewBriefingScheduler(db *sql.DB, org orgdb.Org, cfg notifydigest.Config, notifier *email.Notifier, version string, defaultTo []string, logger *slog.Logger) *Scheduler {
	return newScheduler(db, org, cfg, notifier, version, defaultTo, logger, briefingKind, true)
}

func newScheduler(db *sql.DB, org orgdb.Org, cfg notifydigest.Config, notifier *email.Notifier, version string, defaultTo []string, logger *slog.Logger, kind string, briefing bool) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Scheduler{
		db: db, org: org, cfg: cfg, notifier: notifier, version: version,
		defaultTo: defaultTo, logger: logger,
		kind:     kind,
		briefing: briefing,
		now:      func() time.Time { return time.Now().UTC() },
		interval: tickInterval,
	}
	s.send = func(ctx context.Context, m email.Message) { s.notifier.Send(ctx, m) }
	return s
}

// Run evaluates immediately, then on every interval tick until ctx is done.
func (s *Scheduler) Run(ctx context.Context) {
	s.logger.Info("digest scheduler started",
		"frequency", s.cfg.FrequencyOrDefault(), "send_hour", s.cfg.SendHourOrDefault())
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		s.tickOnce(ctx)
		select {
		case <-ctx.Done():
			s.logger.Info("digest scheduler stopping")
			return
		case <-t.C:
		}
	}
}

// tickOnce dispatches one evaluation: the daily briefing path when this
// scheduler is a briefing scheduler, else the org spend digest path.
func (s *Scheduler) tickOnce(ctx context.Context) {
	if s.briefing {
		s.tickBriefing(ctx)
		return
	}
	s.tick(ctx)
}

// tick sends the digest for the most-recently-completed period exactly once,
// gated by send_hour and the persisted marker. Fail-soft throughout.
func (s *Scheduler) tick(ctx context.Context) {
	period, ready := notifydigest.DuePeriod(s.cfg.FrequencyOrDefault(), s.cfg.SendHourOrDefault(), s.now())
	if !ready {
		return
	}
	last, err := s.lastSentPeriod(ctx)
	if err != nil {
		s.logger.Warn("digest: read marker failed", "err", err)
		return
	}
	if last == period.Key {
		return // already sent this period
	}
	data, err := s.assemble(ctx, period)
	if err != nil {
		s.logger.Warn("digest: assemble failed", "period", period.Key, "err", err)
		return
	}
	s.send(ctx, notifydigest.Compose(data))
	if err := s.recordSent(ctx, period.Key); err != nil {
		s.logger.Warn("digest: persist marker failed", "period", period.Key, "err", err)
		return
	}
	s.logger.Info("digest sent", "period", period.Key, "total_usd", data.TotalUSD)
}

// tickBriefing sends the daily manager briefing for the most-recently-completed
// day exactly once, gated by send_hour and the persisted marker. Fail-soft
// throughout (same posture as tick).
func (s *Scheduler) tickBriefing(ctx context.Context) {
	period, ready := notifydigest.DuePeriod(notifydigest.Daily, s.cfg.SendHourOrDefault(), s.now())
	if !ready {
		return
	}
	last, err := s.lastSentPeriod(ctx)
	if err != nil {
		s.logger.Warn("briefing digest: read marker failed", "err", err)
		return
	}
	if last == period.Key {
		return // already sent this day
	}
	data, err := s.assembleBriefing(ctx, period)
	if err != nil {
		s.logger.Warn("briefing digest: assemble failed", "period", period.Key, "err", err)
		return
	}
	s.send(ctx, notifydigest.ComposeBriefing(data))
	if err := s.recordSent(ctx, period.Key); err != nil {
		s.logger.Warn("briefing digest: persist marker failed", "period", period.Key, "err", err)
		return
	}
	s.logger.Info("briefing digest sent", "period", period.Key, "total_usd", data.TotalUSD)
}

// Preview assembles the digest Message for the most-recently-completed period,
// ignoring the send_hour gate and the marker. It NEVER sends and NEVER touches
// the marker — the `observer-org digest send --dry-run` path. An empty period
// still composes a valid (zero-total) message.
func (s *Scheduler) Preview(ctx context.Context) (email.Message, error) {
	period, _ := notifydigest.DuePeriod(s.frequency(), s.cfg.SendHourOrDefault(), s.now())
	if s.briefing {
		data, err := s.assembleBriefing(ctx, period)
		if err != nil {
			return email.Message{}, err
		}
		return notifydigest.ComposeBriefing(data), nil
	}
	data, err := s.assemble(ctx, period)
	if err != nil {
		return email.Message{}, err
	}
	return notifydigest.Compose(data), nil
}

// SendNow composes and delivers the digest for the most-recently-completed
// period immediately, bypassing the send_hour gate and the marker (an operator-
// triggered `observer-org digest send`). It DOES update the marker so a manual
// send suppresses the same period's scheduled send. Requires a notifier.
func (s *Scheduler) SendNow(ctx context.Context) error {
	period, _ := notifydigest.DuePeriod(s.frequency(), s.cfg.SendHourOrDefault(), s.now())
	if s.briefing {
		data, err := s.assembleBriefing(ctx, period)
		if err != nil {
			return err
		}
		s.send(ctx, notifydigest.ComposeBriefing(data))
		if err := s.recordSent(ctx, period.Key); err != nil {
			s.logger.Warn("briefing digest: persist marker failed", "period", period.Key, "err", err)
		}
		return nil
	}
	data, err := s.assemble(ctx, period)
	if err != nil {
		return err
	}
	s.send(ctx, notifydigest.Compose(data))
	if err := s.recordSent(ctx, period.Key); err != nil {
		s.logger.Warn("digest: persist marker failed", "period", period.Key, "err", err)
	}
	return nil
}

// frequency is the digest frequency the scheduler evaluates on: daily for the
// briefing path (regardless of the weekly/monthly [digest].frequency, which is
// a separate product), the configured one otherwise.
func (s *Scheduler) frequency() notifydigest.Frequency {
	if s.briefing {
		return notifydigest.Daily
	}
	return s.cfg.FrequencyOrDefault()
}

// assemble builds the content-free digest Data for a period: current + prior
// spend rollups (org-wide admin scope), ranked movers, and the alert count.
func (s *Scheduler) assemble(ctx context.Context, period notifydigest.Period) (notifydigest.Data, error) {
	scope := rollup.Scope{Admin: true}
	now := s.now()
	cur, err := rollup.WindowReport(ctx, s.db, scope, period.Start, period.End, now)
	if err != nil {
		return notifydigest.Data{}, err
	}
	priorStart := period.Start.Add(-period.End.Sub(period.Start))
	prior, err := rollup.WindowReport(ctx, s.db, scope, priorStart, period.Start, now)
	if err != nil {
		return notifydigest.Data{}, err
	}

	title := "Weekly org spend digest"
	if s.cfg.FrequencyOrDefault() == notifydigest.Monthly {
		title = "Monthly org spend digest"
	}
	data := notifydigest.Data{
		Title:         title,
		OrgName:       s.org.OrgName,
		PeriodLabel:   period.Label,
		TotalUSD:      cur.TotalUSD,
		PriorTotalUSD: prior.TotalUSD,
		HasPrior:      true,
		Breakdowns: []notifydigest.Breakdown{
			{Title: "Spend by developer", Items: toItems(cur.ByDeveloper, topMoversN)},
			{Title: "Spend by model", Items: toItems(cur.ByModel, topMoversN)},
			{Title: "Spend by project", Items: toItems(cur.ByProject, topMoversN)},
		},
		Movers:  notifydigest.RankMovers(toItems(cur.ByModel, 0), toItems(prior.ByModel, 0), topMoversN),
		Version: s.version,
		To:      s.cfg.To,
	}
	if len(data.To) == 0 {
		data.To = s.defaultTo
	}

	if n, err := s.alertCount(ctx, period.Start, period.End); err != nil {
		s.logger.Warn("digest: alert count failed", "err", err)
	} else {
		data.AlertCount = n
		data.HasAlertCount = true
	}
	return data, nil
}

// assembleBriefing builds the content-free daily manager briefing DigestData
// for a period (one completed UTC day, admin scope) by composing the SAME
// org rollup.Briefing the dashboard serves and projecting its at_glance +
// activity onto the email shape — mirroring the solo briefingDigestRunner.
func (s *Scheduler) assembleBriefing(ctx context.Context, period notifydigest.Period) (notifydigest.BriefingData, error) {
	b, err := rollup.Briefing(ctx, s.db, period.Start, rollup.Scope{Admin: true}, s.now())
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

	// Fetch virtuous-cycle trend (Stage 3) for the exec memo.
	vcRows, err := rollup.SelectVirtuousCycle(ctx, s.db, 90, time.Now())
	if err != nil {
		return notifydigest.BriefingData{}, fmt.Errorf("assembleBriefing: virtuous cycle: %w", err)
	}
	var vcApplies int64
	var vcProjected, vcRealized float64
	var vcTurns int64
	for _, r := range vcRows {
		vcApplies += r.Applies
		vcProjected += r.ProjectedUSD
		vcRealized += r.RealizedUSD
		vcTurns += r.RealizedTurns
	}

	to := s.cfg.To
	if len(to) == 0 {
		to = s.defaultTo
	}
	return notifydigest.BriefingData{
		Title:                           "Daily briefing",
		PeriodLabel:                     period.Label,
		TotalUSD:                        g.SpendUSD,
		Sessions:                        g.Sessions,
		Turns:                           g.Turns,
		InputTokens:                     g.InputTokens,
		OutputTokens:                    g.OutputTokens,
		CacheReadTokens:                 g.CacheReadTokens,
		CacheCreationTokens:             g.CacheCreationTokens,
		CacheHitRate:                    g.CacheHitRate,
		ErrorTurns:                      g.ErrorTurns,
		ActiveHours:                     g.ActiveHours,
		FastShare:                       g.FastShare,
		Tools:                           tools,
		OpportunityCount:                len(b.Opportunities),
		OpportunitySavingsUSD:           savings,
		VirtuousCycleTotalApplies:       vcApplies,
		VirtuousCycleTotalProjectedUSD:  vcProjected,
		VirtuousCycleTotalRealizedUSD:   vcRealized,
		VirtuousCycleTotalRealizedTurns: vcTurns,
		Version:                         s.version,
		To:                              to,
	}, nil
}

// toItems converts rollup KeyCost rows to digest LineItems, optionally capping
// to the first limit rows (they arrive already sorted by cost desc). limit ≤ 0
// keeps all rows.
func toItems(rows []rollup.KeyCost, limit int) []notifydigest.LineItem {
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]notifydigest.LineItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, notifydigest.LineItem{Label: r.Key, CostUSD: r.CostUSD, Tokens: r.Tokens})
	}
	return out
}

// alertCount returns the number of obs_alert_events fired in [start, end) for
// this org.
func (s *Scheduler) alertCount(ctx context.Context, start, end time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM obs_alert_events WHERE org_id = ? AND fired_at >= ? AND fired_at < ?`,
		s.org.OrgID, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("digest.alertCount: %w", err)
	}
	return n, nil
}

// lastSentPeriod returns the Period.Key of the last-sent digest of this
// scheduler's kind, or "" when none has been sent.
func (s *Scheduler) lastSentPeriod(ctx context.Context) (string, error) {
	var key string
	err := s.db.QueryRowContext(ctx, `SELECT last_period FROM digest_state WHERE kind = ?`, s.kind).Scan(&key)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("digest.lastSentPeriod: %w", err)
	}
	return key, nil
}

// recordSent upserts the send-once marker for period under this scheduler's
// kind.
func (s *Scheduler) recordSent(ctx context.Context, periodKey string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO digest_state (kind, last_period, sent_at) VALUES (?, ?, ?)
ON CONFLICT(kind) DO UPDATE SET last_period = excluded.last_period, sent_at = excluded.sent_at`,
		s.kind, periodKey, s.now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("digest.recordSent: %w", err)
	}
	return nil
}
