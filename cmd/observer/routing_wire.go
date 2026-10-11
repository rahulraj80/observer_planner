//go:build !sbo_omit_proxy_routing && !sbo_omit_capture_proxy

package main

import (
	"context"
	"log/slog"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/proxy"
	"github.com/marmutapp/superbased-observer/internal/routing"
	"github.com/marmutapp/superbased-observer/internal/routingconfig"
	"github.com/marmutapp/superbased-observer/internal/selfobs/emit"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// routing_wire.go holds wireRouting, moved out of proxy.go so that a
// `-tags sbo_omit_proxy_routing` build (component proxy.routing compiled
// out, lane MP-L1I, T-715) links no router: routing_omit.go wires nothing.

// wireRouting assembles the Channel B routing seam (model-routing spec
// §R5/§R11) onto opts. Gated on the [routing] master switch + a non-off
// mode: when off, no router is wired and the request path is
// byte-identical to a pre-routing build (pinned by the proxy seam
// tests). The policy compiles from config (template expansion + appended
// custom rules, §R6.3); lint errors degrade affected rules to fail-open
// and are logged loudly, never fatal - a typo'd policy must not take
// the proxy down.
//
// Returns the live router's §R18.3 demotion accessor (R2.4) so the
// same-process dashboard can surface calibration demotions - they are
// in-memory state only a live router can answer for. Nil when routing
// is not wired.
func wireRouting(ctx context.Context, cfg config.Config, s *store.Store, opts *proxy.Options, logger *slog.Logger, selfObsSink emit.Sink) (func() map[string]string, *RoutingStateHandle) {
	if !cfg.Routing.Enabled || cfg.Routing.Mode == "off" {
		return nil, nil
	}
	// localSpec is the IMMUTABLE node-local spec (BEFORE org composition),
	// retained on the liveRouter so P0-7 ReloadOrgPolicy can reproduce this
	// construction exactly (plan §4 B5). spec is the working copy composition
	// mutates below.
	localSpec := routingconfig.Spec(cfg.Routing)
	spec := localSpec
	// composedOrgVersion is the org routing-policy version actually COMPOSED
	// into the live policy (0 when none / a compose failure fell back to
	// local). It is the RoutingStateHandle's runningVersion - the org-layer
	// running identity for the P0-6 reporter (R5-B2).
	var composedOrgVersion int64
	// orgBodyMode is the composed org body's [routing].mode, captured for the
	// Arc 4 P3b managed-enforce lift (honored only for a managed node holding
	// enforce.routing; ignored by ComposeOrgPolicy itself under §R23).
	var orgBodyMode string
	// §R19.1 org policy composition: the cached (signature-verified)
	// org document's hard constraints intersect in; its soft rules
	// rank under local. The cache never carries an enforce switch -
	// the [routing] gate above is the only one (§R23).
	if orgPol, ok, perr := s.GetOrgRoutingPolicy(ctx); perr != nil {
		logger.Warn("routing: org policy cache read failed", "err", perr)
	} else if ok {
		composed, cerr := routingconfig.ComposeOrgPolicy(spec, orgPol.Body)
		if cerr != nil {
			logger.Warn("routing: org policy body unusable; continuing with local policy", "err", cerr)
		} else {
			spec = composed
			composedOrgVersion = orgPol.Version
			orgBodyMode = routingconfig.OrgBodyMode(orgPol.Body)
			logger.Info("routing: org policy composed", "version", orgPol.Version)
		}
	}
	policy, issues := routing.Compile(spec)
	for _, issue := range issues {
		logger.Warn("routing: policy lint finding",
			"check", issue.Check, "rule", issue.RuleName,
			"severity", string(issue.Severity), "msg", issue.Message)
	}
	resolver := routing.NewTierResolver()
	overrides, provenance, benchErrs := routingconfig.ResolvedTierOverrides(cfg.Routing)
	for _, berr := range benchErrs {
		logger.Warn("routing: benchmark import skipped", "err", berr)
	}
	for _, prov := range provenance {
		logger.Info("routing: benchmark tiers imported", "provenance", prov)
	}
	// §R11.7 local upstreams: declared models place in TierLocal,
	// the OpenAI-shape local representative installs, and the
	// proxy learns the per-model endpoint routes.
	luTiers, luReps, luRoutes := routingconfig.LocalUpstreamRouting(cfg.Routing)
	if luTiers != nil {
		overrides = routing.MergeTierOverrides(luTiers, overrides)
	}
	if luReps != nil {
		resolver.ReloadWithLocalUpstreams(overrides, luReps)
	} else if overrides != nil {
		resolver.Reload(overrides)
	}
	if len(luRoutes) > 0 {
		opts.LocalUpstreams = luRoutes
		logger.Info("routing: local upstreams wired", "models", len(luRoutes))
	}
	// §R12.4 key pools - local-only config; never log key material.
	if len(cfg.Routing.KeyPool) > 0 {
		opts.KeyPools = cfg.Routing.KeyPool
		logger.Info("routing: key pools wired", "providers", len(cfg.Routing.KeyPool))
	}
	refresher := store.NewRoutingRefresher(s, policy, resolver, routingPriceFn(acquireProcessCostEngine(ctx, cfg, nil, logger)))
	go refresher.Run(ctx)
	lr := newLiveRouter(policy, cfg.Routing.Mode, refresher, s, logger)
	// Seed the composed org body mode so the managed-enforce lift (once
	// start.go injects the predicate via SetManagedEnforce) can honor a
	// managed node's cached enforce policy from the first request, before the
	// first hot-reload cycle. No-op on a local-only node (orgBodyMode "").
	lr.orgMode = orgBodyMode
	opts.ModelRouter = lr
	// §R7.2/§R18.3 calibration job: refreshes model_calibration
	// through the existing one-owner seam and (auto_demote) grades
	// the policy's downshift rules - pure SQL, off the hot path,
	// same daemon as the watcher in `observer start`.
	if cfg.Routing.Calibration.Enabled {
		go routingCalibrationLoop(ctx, s, cfg, lr, logger)
	}
	// §R12.2 same-target retries follow the routing master gate
	// (config lives under [routing.reliability]).
	opts.Reliability = proxy.ReliabilityOptions{
		MaxRetries:           cfg.Routing.Reliability.MaxRetries,
		RetryOverloaded:      cfg.Routing.Reliability.RetryOverloaded,
		RetryUnavailable:     cfg.Routing.Reliability.RetryUnavailable,
		RetryConnectionReset: cfg.Routing.Reliability.RetryConnectionReset,
	}
	logger.Info("routing: Channel B wired",
		"mode", cfg.Routing.Mode, "policy", policy.Name, "hash", policy.Hash())
	// P0-6 router read handle (§4.2): capture the org-layer running identity.
	// effectiveHash is the live policy hash ONLY when an org policy was
	// composed (composedOrgVersion > 0); local-only routing reports version 0
	// + empty hash so the reader resolves none/no_policy (R5-B2).
	handle := &RoutingStateHandle{}
	init := routingState{mode: cfg.Routing.Mode}
	if composedOrgVersion > 0 {
		init.version = composedOrgVersion
		init.hash = policy.Hash()
	}
	handle.store(init)
	// P0-7 hot-reload wiring (plan §4.4 decision A): retain the immutable
	// local spec + the state handle on the router, and hang the reload
	// closure off the handle so start.go can register the org reload sink
	// without a new buildProxy return value (arity frozen at 8).
	lr.localSpec = localSpec
	lr.handle = handle
	handle.reload = lr.ReloadOrgPolicy
	handle.setME = lr.SetManagedEnforce
	if cfg.SelfObs.Enabled {
		lr.SetSelfObs(selfObsSink, cfg.SelfObs.RoutingSampleN)
	}
	return lr.DemotedRules, handle
}
