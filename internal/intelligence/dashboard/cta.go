package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/briefing"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/configapply"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// cta.go is the Stage 2 "Apply CTA" surface (ADDING_BRIEFING_PLANNER.md):
// the briefing opportunity cards become actionable through a
// simulate-first, one-click land with undo. Every card maps to one or
// more config levers (internal/configapply.LeversForDetector). For
// routing_shadow the levers are: mode (advise→enforce), policy (swap
// template), and stickiness (lower coherence floor). The plan endpoint
// is the pure preview (no writes); apply + revert land / unwind the
// selected change under configWriteMu with a stamped snapshot + JSONL
// undo ledger; ledger is the read-only history. API + tests only — no
// web/CLI (the card's action slot is a later pass).
//
// Privacy posture matches every routing surface: the plan and ledger
// carry only the config lever and aggregate dollar figures — never
// transcripts, prompts, or machine paths.

// ctaRequest is the body shared by plan + apply: the card's dedup key
// plus the briefing window it was composed under. For apply, an
// optional change_id selects which lever to actuate.
type ctaRequest struct {
	DedupKey string `json:"dedup_key"`
	Window   string `json:"window"`
	ChangeID string `json:"change_id,omitempty"`
}

// ctaPlan is the re-derived, simulate-first answer for one opportunity
// card: the config levers that would realize its savings, the exact
// writes, the projected monthly impact, and whether each write is a
// no-op (already active).
type ctaPlan struct {
	DedupKey            string                `json:"dedup_key"`
	Detector            string                `json:"detector"`
	Title               string                `json:"title,omitempty"`
	WindowDays          int                   `json:"window_days"`
	Appliable           bool                  `json:"appliable"`
	ConfigPathSet       bool                  `json:"config_path_set"`
	Changes             []*configapply.Change `json:"changes,omitempty"`
	SavingsUSD          float64               `json:"savings_usd,omitempty"`
	ProjectedMonthlyUSD float64               `json:"projected_monthly_usd,omitempty"`
	Evidence            map[string]float64    `json:"evidence,omitempty"`
	Note                string                `json:"note,omitempty"`
	// Applied/Suspended reflect the ledger-derived CTA state of the card
	// right now: whether any lever is currently applied (AppliedLevers
	// are the change_ids) and whether the card is snoozed or paused.
	Applied       bool                     `json:"applied,omitempty"`
	AppliedLevers []string                 `json:"applied_levers,omitempty"`
	Suspended     bool                     `json:"suspended,omitempty"`
	SuspendedKind string                   `json:"suspended_kind,omitempty"` // "snooze" | "pause"
	Suspensions   []configapply.Suspension `json:"suspensions,omitempty"`
}

var (
	// errCTANotFound means no opportunity card matches the dedup key in
	// the window (the card is not active right now).
	errCTANotFound = errors.New("no active briefing opportunity matches the dedup key in this window")
	// errCTAChangeNotFound means the requested change_id was not in the plan.
	errCTAChangeNotFound = errors.New("change_id not found in plan — re-run the plan")
	// errCTAAlreadyActive means the lever's target value already matches
	// the live config — there is nothing to write.
	errCTAAlreadyActive = errors.New("already active")
)

// decodeCTARequest reads the shared {dedup_key, window, change_id?} body.
func decodeCTARequest(w http.ResponseWriter, r *http.Request) (ctaRequest, error) {
	var req ctaRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		return ctaRequest{}, err
	}
	if req.DedupKey == "" {
		return ctaRequest{}, errors.New("dedup_key is required")
	}
	return req, nil
}

// deriveCTAPlan re-derives the briefing for the window, finds the card
// by dedup key, maps its detector to config levers, and re-reads the
// lever evidence so the plan is backed by the data RIGHT NOW — never
// a stale or hand-crafted body.
func (s *Server) deriveCTAPlan(ctx context.Context, opts briefingQueryOptions, dedupKey string) (ctaPlan, error) {
	b, err := s.composeBriefing(ctx, opts)
	if err != nil {
		return ctaPlan{}, err
	}
	for i := range b.Opportunities {
		if b.Opportunities[i].DedupKey == dedupKey {
			return s.ctaPlanForCard(ctx, opts, &b.Opportunities[i])
		}
	}
	return ctaPlan{}, errCTANotFound
}

// ctaPlanForCard builds the simulate-first plan for one already-derived
// opportunity card, without re-composing the briefing. Shared by the
// per-card CTA plan endpoint and the "Actions recommended" rail (which
// iterates every active card).
func (s *Server) ctaPlanForCard(ctx context.Context, opts briefingQueryOptions, card *briefing.Opportunity) (ctaPlan, error) {
	days, err := parseBriefingWindowDays(opts.window)
	if err != nil {
		return ctaPlan{}, err
	}
	plan := ctaPlan{
		DedupKey: card.DedupKey, Detector: card.Detector, Title: card.Title,
		WindowDays: days, SavingsUSD: card.SavingsUSD, ConfigPathSet: s.opts.ConfigPath != "",
	}
	levers := configapply.LeversForDetector(card.Detector)
	if len(levers) == 0 {
		plan.Note = fmt.Sprintf("detector %q has no config lever — its savings are behavioral (guidance only)", card.Detector)
		return plan, nil
	}

	switch card.Detector {
	case "routing_shadow":
		plan.Changes, plan.Appliable, plan.ProjectedMonthlyUSD, plan.Evidence, plan.Note, err = s.ctaRoutingShadowPlan(ctx)
		if err != nil {
			return ctaPlan{}, err
		}
	default:
		plan.Note = fmt.Sprintf("no plan builder for detector %q", card.Detector)
	}

	// Attach the ledger-derived CTA state so the UI can badge the card
	// as applied (with which levers) or suspended (snooze/pause).
	if st, serr := s.ctaLedger().CardState(card.DedupKey, s.now()); serr == nil {
		plan.Applied = st.Applied
		for _, lv := range st.AppliedLevers {
			plan.AppliedLevers = append(plan.AppliedLevers, lv.ChangeID)
		}
		plan.Suspended = st.Suspended
		plan.Suspensions = st.Suspensions
		if len(st.Suspensions) > 0 {
			plan.SuspendedKind = st.Suspensions[0].Kind
		}
	}
	return plan, nil
}

// ctaRoutingShadowPlan computes all three levers for the routing_shadow
// card: mode (advise→enforce), policy (swap template), and stickiness
// (lower min_turns_between_switches when holds indicate held savings).
func (s *Server) ctaRoutingShadowPlan(ctx context.Context) ([]*configapply.Change, bool, float64, map[string]float64, string, error) {
	engine := s.opts.CostEngine
	if engine == nil {
		engine = cost.NewEngine(config.IntelligenceConfig{})
	}
	st := store.New(s.db())
	sh, err := st.AdviseShadowSignal(ctx, 1, enginePriceFn(engine))
	if err != nil {
		return nil, false, 0, nil, "", err
	}
	if sh.AdviseDecisions == 0 || sh.WouldSaveUSD <= 0 || sh.PolicyName == "" {
		return nil, false, 0, nil, "shadow inactive — no advise decisions with savings in the last 24h", nil
	}

	cfg, cerr := loadConfigForDashboard(s.opts.ConfigPath)
	if cerr != nil {
		return nil, false, 0, nil, cerr.Error(), nil
	}

	var changes []*configapply.Change
	appliable := false

	// Lever 1: mode — flip advise → enforce. This is the primary lever
	// that realizes the shadow's reroutes (the shadow runs in advise mode).
	// Emitted always: AlreadyActive marks the no-op when already enforced.
	changes = append(changes, &configapply.Change{
		Section:         "routing",
		Key:             "mode",
		From:            cfg.Routing.Mode,
		To:              "enforce",
		RestartRequired: true,
		ChangeID:        "mode",
		AlreadyActive:   cfg.Routing.Mode == "enforce",
	})

	// Lever 2: policy — swap to the shadow's decide-under template.
	// Skipped when the shadow policy isn't a known template (a custom
	// inline policy can't be expressed via the template map); marked
	// already-active when the current policy already matches.
	if routingTemplateKnown(sh.PolicyName) {
		changes = append(changes, &configapply.Change{
			Section:         "routing",
			Key:             "policy",
			From:            cfg.Routing.Policy,
			To:              sh.PolicyName,
			RestartRequired: true,
			ChangeID:        "policy",
			AlreadyActive:   cfg.Routing.Policy == sh.PolicyName,
		})
	}

	// Lever 3: stickiness — lower min_turns_between_switches when
	// stickiness holds exist with estimated savings. Target is current
	// minus one (floor at 1); already-active when the floor is already 1.
	if sh.StickinessHolds > 0 {
		target := cfg.Routing.Stickiness.MinTurnsBetweenSwitches - 1
		if target < 1 {
			target = 1
		}
		changes = append(changes, &configapply.Change{
			Section:         "routing",
			Key:             "stickiness.min_turns_between_switches",
			From:            fmt.Sprintf("%d", cfg.Routing.Stickiness.MinTurnsBetweenSwitches),
			To:              fmt.Sprintf("%d", target),
			RestartRequired: true,
			ChangeID:        "stickiness",
			AlreadyActive:   cfg.Routing.Stickiness.MinTurnsBetweenSwitches <= 1,
		})
	}

	for _, ch := range changes {
		if !ch.AlreadyActive {
			appliable = true
			break
		}
	}

	// Monthly projection: mode lever projects the full shadow savings;
	// policy lever projects 0 (the shadow already runs under that policy);
	// stickiness lever projects the estimated held savings × 30.
	// For the plan summary we show the MAX projected monthly across levers.
	projectedMonthly := round2f(sh.WouldSaveUSD * 30) // mode lever

	evidence := map[string]float64{
		"would_reroute":           float64(sh.WouldReroute),
		"would_save_usd":          round2f(sh.WouldSaveUSD),
		"net_save_usd":            round2f(sh.WouldSaveUSD),
		"quality_flags":           float64(sh.QualityFlags),
		"stickiness_holds":        float64(sh.StickinessHolds),
		"stickiness_hold_savings": round2f(sh.StickinessHoldSavingsUSD),
	}

	note := "shadow savings are a rolling 24h sample; projected monthly = 24h savings × 30. " +
		"Routing binds at daemon start — applies need a restart to take effect. " +
		"Mode lever flips advise→enforce to realize the shadow reroutes. " +
		"Policy lever swaps to the shadow's decide-under template. " +
		"Stickiness lever lowers the coherence floor when holds indicate held savings."

	return changes, appliable, projectedMonthly, evidence, note, nil
}

// handleBriefingCTAPlan serves POST /api/briefing/cta/plan: the
// simulate-first preview. Pure read/compute — nothing is written. 404
// when the card isn't active in the window; otherwise 200 with the
// levers (or an honest "no lever" answer) + the projected monthly impact.
func (s *Server) handleBriefingCTAPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeCTARequest(w, r)
	if err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}
	plan, err := s.deriveCTAPlan(r.Context(), briefingQueryOptions{window: req.Window}, req.DedupKey)
	if errors.Is(err, errCTANotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, plan)
}

// routingValue reads one live routing lever's value as a string.
func routingValue(cfg config.Config, key string) (string, error) {
	switch key {
	case "mode":
		return cfg.Routing.Mode, nil
	case "policy":
		return cfg.Routing.Policy, nil
	case "stickiness.min_turns_between_switches":
		return fmt.Sprintf("%d", cfg.Routing.Stickiness.MinTurnsBetweenSwitches), nil
	default:
		return "", fmt.Errorf("unknown routing key %q", key)
	}
}

// setRoutingValue writes one routing lever from a string, validating
// numeric bounds. Callers must still run config.Validate before persisting.
func setRoutingValue(cfg *config.Config, key, value string) error {
	switch key {
	case "mode":
		cfg.Routing.Mode = value
	case "policy":
		cfg.Routing.Policy = value
	case "stickiness.min_turns_between_switches":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("stickiness.min_turns_between_switches: %w", err)
		}
		if n < 1 {
			return errors.New("stickiness.min_turns_between_switches must be >= 1")
		}
		cfg.Routing.Stickiness.MinTurnsBetweenSwitches = n
	default:
		return fmt.Errorf("unknown routing key %q", key)
	}
	return nil
}

// persistConfigSnapshot validates cfg, takes a stamped pre-write snapshot,
// writes the file, and notifies listeners. Callers must hold
// configWriteMu (or be otherwise serialized against config writers).
func (s *Server) persistConfigSnapshot(cfg config.Config) (backup, stamp string, err error) {
	if err := config.Validate(cfg); err != nil {
		return "", "", fmt.Errorf("refusing to write invalid config: %w", err)
	}
	stamp = configapply.Stamp(s.now())
	backup, err = configapply.BackupFile(s.opts.ConfigPath, stamp)
	if err != nil {
		return "", "", err
	}
	if err := writeConfigToml(s.opts.ConfigPath, cfg); err != nil {
		return "", "", err
	}
	s.notifyConfigSaved()
	return backup, stamp, nil
}

// applyChangeLocked lands one lever (change.To) against the live config
// under configWriteMu: reads the current value, refuses when the target
// is already live, writes the value, persists with a stamped snapshot,
// and mutates change.From to the live current value. Callers must hold
// configWriteMu and verify ConfigPath is set. Returns errCTAAlreadyActive
// when there is nothing to write.
func (s *Server) applyChangeLocked(change *configapply.Change) (backup, stamp string, err error) {
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		return "", "", err
	}
	cur, err := routingValue(cfg, change.Key)
	if err != nil {
		return "", "", err
	}
	if cur == change.To {
		return "", "", fmt.Errorf("%w: [%s].%s already %q", errCTAAlreadyActive, change.Section, change.Key, change.To)
	}
	change.From = cur
	if err := setRoutingValue(&cfg, change.Key, change.To); err != nil {
		return "", "", err
	}
	return s.persistConfigSnapshot(cfg)
}

// setValueLocked writes one routing lever to an explicit value under
// configWriteMu and persists with a stamped snapshot. Returns the
// previous value and backup; changed=false when the value was already
// live (no write, no backup). Used by the pause arm of suspend (restore
// the pre-apply value) and resume (re-apply the applied value).
func (s *Server) setValueLocked(key, value string) (from, backup, stamp string, changed bool, err error) {
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		return "", "", "", false, err
	}
	from, err = routingValue(cfg, key)
	if err != nil {
		return "", "", "", false, err
	}
	if from == value {
		return from, "", "", false, nil
	}
	if err := setRoutingValue(&cfg, key, value); err != nil {
		return "", "", "", false, err
	}
	b, st, err := s.persistConfigSnapshot(cfg)
	if err != nil {
		return "", "", "", false, err
	}
	return from, b, st, true, nil
}

// handleBriefingCTAApply serves POST /api/briefing/cta/apply: one-click
// land of the selected lever. The plan is re-derived BEFORE the write
// and re-checked UNDER configWriteMu, so the endpoint only ever writes
// evidence-backed, still-current changes (a stale or already-applied
// plan refuses 409/422, never writes). The write takes a stamped
// pre-write snapshot and records into the undo ledger.
func (s *Server) handleBriefingCTAApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeCTARequest(w, r)
	if err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}
	plan, err := s.deriveCTAPlan(r.Context(), briefingQueryOptions{window: req.Window}, req.DedupKey)
	if errors.Is(err, errCTANotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	if !plan.Appliable || len(plan.Changes) == 0 {
		http.Error(w, plan.Note, http.StatusUnprocessableEntity)
		return
	}

	// Select the change by change_id (required for multi-lever plans)
	var change *configapply.Change
	if req.ChangeID == "" {
		http.Error(w, "change_id required for multi-lever plan", http.StatusBadRequest)
		return
	}
	for _, ch := range plan.Changes {
		if ch.ChangeID == req.ChangeID {
			change = ch
			break
		}
	}
	if change == nil {
		http.Error(w, errCTAChangeNotFound.Error(), http.StatusNotFound)
		return
	}
	if change.AlreadyActive {
		http.Error(w, fmt.Sprintf("already active: [%s].%s is already %q — nothing to apply", change.Section, change.Key, change.To), http.StatusConflict)
		return
	}

	// The write path: serialize against every other config writer (the
	// same configWriteMu the section-save handlers hold), and re-check
	// currency under the lock.
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	if s.opts.ConfigPath == "" {
		http.Error(w, "config path not configured — server has no file to save to", http.StatusConflict)
		return
	}
	backup, stamp, err := s.applyChangeLocked(change)
	if err != nil {
		if errors.Is(err, errCTAAlreadyActive) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeErr(w, err)
		return
	}

	out := map[string]any{
		"applied":               true,
		"change":                change,
		"backup":                backup,
		"dedup_key":             plan.DedupKey,
		"projected_monthly_usd": plan.ProjectedMonthlyUSD,
		"restart_required":      change.RestartRequired,
		"note":                  "routing binds at daemon start — this takes effect on the next `observer` restart. Revert: POST /api/briefing/cta/revert. Suspend: POST /api/briefing/cta/suspend.",
	}
	// Audit trail: best-effort — the write already landed with its
	// snapshot; a ledger failure is surfaced, never a write failure.
	if lerr := s.ctaLedger().RecordApply(*change, plan.DedupKey, plan.Detector, plan.ProjectedMonthlyUSD, stamp, backup, "briefing-cta"); lerr != nil {
		out["ledger_error"] = lerr.Error()
	}
	writeJSON(w, out)
}

// handleBriefingCTARevert serves POST /api/briefing/cta/revert: restore
// a CTA-applied lever from its stamped pre-write snapshot.
//
//   - {section, key} — restore the NEWEST apply recorded for that lever
//     (the per-change undo mirror of the apply path).
//   - {all: true} — restore every recorded CTA apply (the full unwind).
//
// Both arms restore from the LEDGER-RECORDED snapshot path (not a
// recomputed one), so the rollback is exact even after later writes.
func (s *Server) handleBriefingCTARevert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Section string `json:"section"`
		Key     string `json:"key"`
		All     bool   `json:"all"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if s.opts.ConfigPath == "" {
		http.Error(w, "config path not configured — server has no file to restore", http.StatusConflict)
		return
	}
	ledger := s.ctaLedger()
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()

	if req.All {
		events, err := ledger.AppliedBackups()
		if err != nil {
			writeErr(w, err)
			return
		}
		if len(events) == 0 {
			http.Error(w, "no briefing-CTA applies recorded", http.StatusNotFound)
			return
		}
		var ledgerErr error
		restored := make([]map[string]any, 0, len(events))
		for _, ev := range events {
			if err := configapply.RestoreFile(s.opts.ConfigPath, ev.Backup); err != nil {
				writeErr(w, err)
				return
			}
			c := configapply.Change{Section: ev.Section, Key: ev.Key, From: ev.From, To: ev.To}
			if ledgerErr == nil {
				ledgerErr = ledger.RecordRevert(c, ev.DedupKey, ev.ChangeID, ev.Backup, "briefing-cta")
			}
			restored = append(restored, map[string]any{
				"section": ev.Section, "key": ev.Key,
				"from": ev.To, "to": ev.From, "backup": ev.Backup,
			})
		}
		out := map[string]any{"restored": len(restored), "changes": restored}
		if ledgerErr != nil {
			out["ledger_error"] = ledgerErr.Error()
		}
		writeJSON(w, out)
		return
	}

	if req.Section == "" || req.Key == "" {
		http.Error(w, "section and key are required (or pass all=true)", http.StatusBadRequest)
		return
	}
	ev, ok, err := ledger.LatestApply(req.Section, req.Key)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		http.Error(w, fmt.Sprintf("no briefing-CTA apply recorded for [%s].%s", req.Section, req.Key), http.StatusNotFound)
		return
	}
	if err := configapply.RestoreFile(s.opts.ConfigPath, ev.Backup); err != nil {
		writeErr(w, err)
		return
	}
	out := map[string]any{
		"restored": true, "section": ev.Section, "key": ev.Key,
		"from": ev.To, "to": ev.From, "backup": ev.Backup,
	}
	if lerr := ledger.RecordRevert(configapply.Change{Section: ev.Section, Key: ev.Key, From: ev.From, To: ev.To}, ev.DedupKey, ev.ChangeID, ev.Backup, "briefing-cta"); lerr != nil {
		out["ledger_error"] = lerr.Error()
	}
	writeJSON(w, out)
}

// handleBriefingCTALedger serves GET /api/briefing/cta/ledger?limit= —
// the "what did the briefing change and how do I undo it?" view: every
// CTA apply + revert, newest first.
func (s *Server) handleBriefingCTALedger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	limit := intArg(r, "limit", 50, 1, 1000)
	ledger := s.ctaLedger()
	events, skipped, err := ledger.Read()
	if err != nil {
		writeErr(w, err)
		return
	}
	slices.Reverse(events)
	if len(events) > limit {
		events = events[:limit]
	}
	if events == nil {
		events = []configapply.LedgerEvent{}
	}
	writeJSON(w, map[string]any{
		"events":  events,
		"skipped": skipped,
		"path":    ledger.Path(),
	})
}

// ctaLedger resolves the briefing-CTA undo ledger — the same node-local
// file the CLI would share, placed next to the observer DB.
func (s *Server) ctaLedger() *configapply.Ledger {
	return configapply.NewLedger(configapply.DefaultLedgerPath(s.opts.DBPath))
}

// parseSuspendUntil parses the suspend snooze "until" value: empty →
// now+7d; a bare integer → days; "Nw" / "Nd" → weeks / days; a Go
// duration ("24h"); or an RFC3339 timestamp. Bounded to 1..365 days.
func parseSuspendUntil(now time.Time, raw string) (time.Time, error) {
	oneDay := 24 * time.Hour
	if raw == "" {
		return now.Add(7 * oneDay), nil
	}
	switch {
	case strings.HasSuffix(raw, "w"):
		n, err := strconv.Atoi(strings.TrimSuffix(raw, "w"))
		if err != nil || n <= 0 || n > 52 {
			return time.Time{}, fmt.Errorf("invalid suspend weeks %q (1..52)", raw)
		}
		return now.Add(time.Duration(n) * 7 * oneDay), nil
	case strings.HasSuffix(raw, "d"):
		n, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if err != nil || n <= 0 || n > 365 {
			return time.Time{}, fmt.Errorf("invalid suspend days %q (1..365)", raw)
		}
		return now.Add(time.Duration(n) * oneDay), nil
	}
	if n, err := strconv.Atoi(raw); err == nil {
		if n <= 0 || n > 365 {
			return time.Time{}, fmt.Errorf("invalid suspend days %q (1..365)", raw)
		}
		return now.Add(time.Duration(n) * oneDay), nil
	}
	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return time.Time{}, fmt.Errorf("invalid suspend duration %q", raw)
		}
		return now.Add(d), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("cannot parse suspend until %q (use days, 1w, a duration like 24h, or an RFC3339 timestamp)", raw)
}

// suspendRequest is the body for POST /api/briefing/cta/suspend: the
// card to suspend, an optional change_id to pause just one applied
// lever, and an optional until for the snooze arm.
type suspendRequest struct {
	DedupKey string `json:"dedup_key"`
	ChangeID string `json:"change_id,omitempty"`
	Until    string `json:"until,omitempty"`
	Detector string `json:"detector,omitempty"`
	Title    string `json:"title,omitempty"`
}

// handleBriefingCTASuspend serves POST /api/briefing/cta/suspend — the
// two-shape suspension from the stage plan:
//
//   - pause (card has an applied lever): restore the applied lever(s)
//     to their pre-apply values under configWriteMu (a temporary revert
//     that KEEPS the apply + backup so resume re-applies exactly), and
//     record a pause per lever. change_id narrows to one lever; empty
//     pauses the whole card.
//   - snooze (card not applied): mute the recommendation until `until`
//     (default 7 days) with NO config write.
//
// Both shapes are ledger events — the ledger stays the single audit
// source of truth for the CTA lifecycle.
func (s *Server) handleBriefingCTASuspend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req suspendRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.DedupKey == "" {
		http.Error(w, "dedup_key is required", http.StatusBadRequest)
		return
	}
	ledger := s.ctaLedger()
	now := s.now()
	st, err := ledger.CardState(req.DedupKey, now)
	if err != nil {
		writeErr(w, err)
		return
	}

	// Pause arm: restore every applied lever (or the selected one).
	if st.Applied {
		var levers []configapply.AppliedLever
		if req.ChangeID == "" {
			levers = st.AppliedLevers
		} else {
			for _, lv := range st.AppliedLevers {
				if lv.ChangeID == req.ChangeID {
					levers = append(levers, lv)
				}
			}
		}
		if len(levers) == 0 {
			http.Error(w, fmt.Sprintf("no applied lever %q for card %s — nothing to suspend", req.ChangeID, req.DedupKey), http.StatusNotFound)
			return
		}
		if s.opts.ConfigPath == "" {
			http.Error(w, "config path not configured — server has no file to restore", http.StatusConflict)
			return
		}
		s.configWriteMu.Lock()
		defer s.configWriteMu.Unlock()
		restored := make([]map[string]any, 0, len(levers))
		var ledgerErr error
		for _, lv := range levers {
			from, _, _, _, err := s.setValueLocked(lv.Key, lv.From)
			if err != nil {
				writeErr(w, err)
				return
			}
			restored = append(restored, map[string]any{
				"change_id": lv.ChangeID, "section": lv.Section, "key": lv.Key,
				"from": from, "to": lv.To,
			})
			if ledgerErr == nil {
				ledgerErr = ledger.RecordSuspend(req.DedupKey, lv.Detector, req.Title, "briefing-cta",
					lv.ChangeID, lv.Section, lv.Key, lv.From, lv.To, lv.Backup, time.Time{})
			}
		}
		out := map[string]any{
			"suspended": true, "kind": "pause", "dedup_key": req.DedupKey,
			"restored": restored,
			"note":     "paused — the applied lever is restored to its pre-apply value until resumed. Resume: POST /api/briefing/cta/resume.",
		}
		if ledgerErr != nil {
			out["ledger_error"] = ledgerErr.Error()
		}
		writeJSON(w, out)
		return
	}

	// Snooze arm: card not applied — mute the recommendation until
	// `until` (default 7 days). No config write.
	until, err := parseSuspendUntil(now, req.Until)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := ledger.RecordSuspend(req.DedupKey, req.Detector, req.Title, "briefing-cta", "", "", "", "", "", "", until); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"suspended": true, "kind": "snooze", "dedup_key": req.DedupKey,
		"until": until.UTC().Format(time.RFC3339),
		"note":  "recommendation muted until " + until.UTC().Format(time.RFC3339) + ". Resume: POST /api/briefing/cta/resume.",
	})
}

// resumeRequest is the body for POST /api/briefing/cta/resume: the card
// to un-suspend, optionally narrowed to one change_id.
type resumeRequest struct {
	DedupKey string `json:"dedup_key"`
	ChangeID string `json:"change_id,omitempty"`
}

// handleBriefingCTAResume serves POST /api/briefing/cta/resume: clears a
// card's suspension. A snooze is just cleared (ledger resume event). A
// pause RE-APPLIES the paused lever with a fresh stamped snapshot (new
// "apply" event) and then records the resume — so the card's applied
// state and undo chain are exact again.
func (s *Server) handleBriefingCTAResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req resumeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.DedupKey == "" {
		http.Error(w, "dedup_key is required", http.StatusBadRequest)
		return
	}
	ledger := s.ctaLedger()
	st, err := ledger.CardState(req.DedupKey, s.now())
	if err != nil {
		writeErr(w, err)
		return
	}
	var susp []configapply.Suspension
	for _, su := range st.Suspensions {
		if req.ChangeID == "" || su.ChangeID == req.ChangeID {
			susp = append(susp, su)
		}
	}
	if len(susp) == 0 {
		http.Error(w, fmt.Sprintf("no active suspension for card %s", req.DedupKey), http.StatusNotFound)
		return
	}
	needsWrite := false
	for _, su := range susp {
		if su.Kind == "pause" {
			needsWrite = true
			break
		}
	}
	if needsWrite {
		if s.opts.ConfigPath == "" {
			http.Error(w, "config path not configured — server has no file to write", http.StatusConflict)
			return
		}
		s.configWriteMu.Lock()
		defer s.configWriteMu.Unlock()
	}

	resumed := make([]map[string]any, 0, len(susp))
	var ledgerErr error
	for _, su := range susp {
		if su.Kind == "snooze" {
			if ledgerErr == nil {
				ledgerErr = ledger.RecordResume(req.DedupKey, "briefing-cta", "", "", "")
			}
			resumed = append(resumed, map[string]any{"change_id": "", "kind": "snooze", "re_applied": false})
			continue
		}
		// Pause → re-apply the lever with a fresh snapshot + audit.
		from, backup, stamp, changed, err := s.setValueLocked(su.Key, su.To)
		if err != nil {
			writeErr(w, err)
			return
		}
		c := configapply.Change{Section: su.Section, Key: su.Key, From: from, To: su.To, ChangeID: su.ChangeID, RestartRequired: true}
		if ledgerErr == nil {
			ledgerErr = ledger.RecordApply(c, req.DedupKey, su.Detector, 0, stamp, backup, "briefing-cta")
		}
		if ledgerErr == nil {
			ledgerErr = ledger.RecordResume(req.DedupKey, "briefing-cta", su.ChangeID, su.Section, su.Key)
		}
		resumed = append(resumed, map[string]any{
			"change_id": su.ChangeID, "kind": "pause", "section": su.Section,
			"key": su.Key, "re_applied": changed, "backup": backup,
		})
	}
	out := map[string]any{"resumed": len(resumed), "dedup_key": req.DedupKey, "details": resumed}
	if ledgerErr != nil {
		out["ledger_error"] = ledgerErr.Error()
	}
	writeJSON(w, out)
}

// handleBriefingSuspensions serves GET /api/briefing/suspensions: every
// currently-active card suspension (snooze + pause), for the
// "Suspended recommendations" strip. Pure read.
func (s *Server) handleBriefingSuspensions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	ledger := s.ctaLedger()
	susp, err := ledger.ActiveSuspensions(s.now())
	if err != nil {
		writeErr(w, err)
		return
	}
	if susp == nil {
		susp = []configapply.Suspension{}
	}
	writeJSON(w, map[string]any{
		"suspensions": susp,
		"path":        ledger.Path(),
	})
}

// briefingAction is one row on the "Actions recommended" rail
// (ADDING_BRIEFING_PLANNER.md Stage 2 item 3, Pull rail): an active
// opportunity card turned into a concrete "do this next" item. Levered
// cards (those with a config lever) carry the exact config write to land
// (surface, change_id for the CTA apply endpoint, from→to, restart flag);
// guidance-only cards (no honest config lever) render an actionable=false
// row so the rail reads as "here's what to do next" for the WHOLE card set.
type briefingAction struct {
	DedupKey            string  `json:"dedup_key"`
	Detector            string  `json:"detector"`
	Category            string  `json:"category"`
	Severity            string  `json:"severity"`
	Title               string  `json:"title"`
	Nudge               string  `json:"nudge"`
	Actionable          bool    `json:"actionable"`
	ConfigSurface       string  `json:"config_surface,omitempty"` // e.g. "routing.mode"
	ChangeID            string  `json:"change_id,omitempty"`      // select with the CTA apply
	From                string  `json:"from,omitempty"`           // current value
	To                  string  `json:"to,omitempty"`             // target value
	RestartRequired     bool    `json:"restart_required"`
	SavingsUSD          float64 `json:"savings_usd,omitempty"`
	ProjectedMonthlyUSD float64 `json:"projected_monthly_usd,omitempty"`
	// Applied/Suspended reflect the ledger-derived CTA state of the CARD
	// (same for every row of a card): whether any lever is currently
	// applied, which levers (change_id + section/key), and whether the
	// card is snoozed or paused. The UI badges cards and offers
	// Pause/Resume/Revert from these without a separate status call.
	Applied       bool                       `json:"applied,omitempty"`
	AppliedLevers []configapply.AppliedLever `json:"applied_levers,omitempty"`
	Suspended     bool                       `json:"suspended,omitempty"`
	SuspendedKind string                     `json:"suspended_kind,omitempty"`
	Suspensions   []configapply.Suspension   `json:"suspensions,omitempty"`
}

// actionsRailResponse is the /api/briefing/actions reply.
type actionsRailResponse struct {
	Date    string           `json:"date"`
	Basis   briefing.Basis   `json:"basis"`
	Window  string           `json:"window"`
	Actions []briefingAction `json:"actions"`
}

// buildBriefingActions composes the briefing once, then maps every active
// opportunity card to a recommended action. Levered cards reuse the exact
// CTA plan builder (ctaPlanForCard) so the rail's config surface, change_id,
// projected impact, and already-active status can never drift from what the
// CTA apply endpoint will actually do. Cards whose detector has no config
// lever (cache/long-context/trivial/recurrence/failure) surface as
// guidance-only rows. Sorted: actionable first by projected monthly USD
// desc, then guidance by savings desc.
func (s *Server) buildBriefingActions(ctx context.Context, opts briefingQueryOptions) ([]briefingAction, error) {
	b, err := s.composeBriefing(ctx, opts)
	if err != nil {
		return nil, err
	}

	actions := []briefingAction{}
	for i := range b.Opportunities {
		card := &b.Opportunities[i]
		base := briefingAction{
			DedupKey: card.DedupKey, Detector: card.Detector, Category: card.Category,
			Severity: card.Severity, Title: card.Title, Nudge: card.Nudge,
			SavingsUSD: card.SavingsUSD,
		}
		// Attach the ledger-derived CTA state so every row of the card
		// badges it the same way and the UI can Pause/Resume/Revert
		// without extra round-trips. A broken ledger degrades to the
		// plain (unbadged) shape rather than failing the rail.
		if st, serr := s.ctaLedger().CardState(card.DedupKey, s.now()); serr == nil {
			base.Applied = st.Applied
			base.AppliedLevers = st.AppliedLevers
			base.Suspended = st.Suspended
			base.Suspensions = st.Suspensions
			if len(st.Suspensions) > 0 {
				base.SuspendedKind = st.Suspensions[0].Kind
			}
		}
		if configapply.LeversForDetector(card.Detector) == nil {
			// Guidance-only: no honest config lever realizes its savings.
			actions = append(actions, base)
			continue
		}
		plan, perr := s.ctaPlanForCard(ctx, opts, card)
		if perr != nil {
			// A card that can't build a plan is not actionable now; fall
			// back to the guidance-only shape rather than failing the rail.
			actions = append(actions, base)
			continue
		}
		emitted := false
		for _, ch := range plan.Changes {
			if ch.AlreadyActive {
				continue
			}
			a := base
			a.Actionable = true
			a.ConfigSurface = ch.Section + "." + ch.Key
			a.ChangeID = ch.ChangeID
			a.From = ch.From
			a.To = ch.To
			a.RestartRequired = ch.RestartRequired
			a.ProjectedMonthlyUSD = plan.ProjectedMonthlyUSD
			actions = append(actions, a)
			emitted = true
		}
		if !emitted {
			// Levered detector but every lever already active → not
			// actionable; keep the guidance-only shape.
			actions = append(actions, base)
		}
	}

	sort.SliceStable(actions, func(i, j int) bool {
		if actions[i].Actionable != actions[j].Actionable {
			return actions[i].Actionable
		}
		if actions[i].ProjectedMonthlyUSD != actions[j].ProjectedMonthlyUSD {
			return actions[i].ProjectedMonthlyUSD > actions[j].ProjectedMonthlyUSD
		}
		return actions[i].SavingsUSD > actions[j].SavingsUSD
	})
	if actions == nil {
		actions = []briefingAction{}
	}
	return actions, nil
}

// handleBriefingActions — GET /api/briefing/actions?date=&window=&...
// the "Actions recommended" rail: every active opportunity card as a
// "do this next" row, levered cards carrying the config surface + change_id
// to land it via /api/briefing/cta/apply. Pure read (View-class); nothing
// is written. 405 on any non-GET.
func (s *Server) handleBriefingActions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	opts := briefingQueryOptions{
		date:    r.URL.Query().Get("date"),
		tool:    r.URL.Query().Get("tool"),
		project: r.URL.Query().Get("project"),
		basis:   r.URL.Query().Get("basis"),
		window:  r.URL.Query().Get("window"),
	}
	actions, err := s.buildBriefingActions(r.Context(), opts)
	if err != nil {
		writeErr(w, err)
		return
	}
	days, err := parseBriefingWindowDays(opts.window)
	if err != nil {
		writeErr(w, err)
		return
	}
	date, _, _, err := briefingWindow(s.now(), opts.date, days)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, actionsRailResponse{
		Date:    date,
		Basis:   basisOf(opts.basis),
		Window:  opts.window,
		Actions: actions,
	})
}
