//go:build !sbo_omit_ui_dashboard && !sbo_omit_notify_email

package main

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/intelligence/dashboard"
	notifydigest "github.com/marmutapp/superbased-observer/internal/notify/digest"
)

// digest_briefing.go holds the dashboard-backed half of the observer_planner
// daily briefing digest; digest_briefing_omit.go is its twin in a
// -tags sbo_omit_ui_dashboard build.

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
