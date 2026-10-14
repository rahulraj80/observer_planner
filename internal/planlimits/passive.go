package planlimits

import (
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// passive.go maps the passive signals Observer already captures. They are
// read, never written, by this module: the proxy's limit_snapshots rows, the
// Codex log rate_limits envelope (ActionRateLimit rows), and the Claude Code
// status line rate_limits block, which the `observer statusline` command
// forwards numbers-only into this module's own table.

// weeklyThresholdMinutes classifies a window by its DECLARED length, never by
// primary/secondary position (Codex plans can report only the weekly window
// as primary). Same threshold as internal/store/ratelimit.go.
const weeklyThresholdMinutes = 1440

// windowKeyForMinutes classifies a window by its declared length.
func windowKeyForMinutes(m int64) string {
	if m > weeklyThresholdMinutes {
		return "weekly"
	}
	return "5h"
}

// LimitRow is the plain projection of one limit_snapshots row (agent
// migration 049) the store seam hands over.
type LimitRow struct {
	ScopeHash     string
	Provider      string // anthropic | openai
	SessionID     string
	Tool          string // sessions.tool of the observing session, "" when unknown
	ObservedAt    time.Time
	Window5hUtil  *float64
	Window5hReset *int64
	Window7dUtil  *float64
	Window7dReset *int64
	Status        string
}

// limitRowProvider maps the proxy provider column to a plan provider. The
// OpenAI per-minute counters carry no plan window, so only Anthropic maps.
var limitRowProvider = map[string]string{"anthropic": ProviderClaude}

// FromLimitRow maps a proxy snapshot. Only the subscription windows matter
// here; the per-minute request/token counters stay predictor-only.
func FromLimitRow(r LimitRow) (Snapshot, bool) {
	prov := limitRowProvider[r.Provider]
	if prov == "" {
		return Snapshot{}, false
	}
	s := newSnap(prov, SourceClaudeProxy, KindPassiveProxy, ConfPercentOnly, r.ObservedAt)
	add := func(key string, util *float64, reset *int64, length time.Duration) {
		if util == nil {
			return
		}
		w := Window{Key: key, Unit: UnitPercent, UsedFrac: F(*util), Length: dur(length), Rolling: true}
		if reset != nil {
			w.ResetsAt = unixPtr(*reset)
		}
		s.Windows = append(s.Windows, w)
	}
	add("5h", r.Window5hUtil, r.Window5hReset, 5*time.Hour)
	add("weekly", r.Window7dUtil, r.Window7dReset, 7*24*time.Hour)
	if r.Status != "" && r.Status != "allowed" && r.Status != "allowed_warning" {
		s.Status = StatusThrottled
	}
	return s, len(s.Windows) > 0
}

// PassiveLogTools maps a tool whose own logs carry the Codex rate_limits
// envelope to the plan it draws from.
var PassiveLogTools = map[string]string{"codex": ProviderChatGPT}

// LogRow is one ActionRateLimit row the store seam hands over.
type LogRow struct {
	SessionID  string
	Tool       string
	ObservedAt time.Time
	Raw        []byte // actions.raw_tool_input, the codex rate_limits envelope
}

type codexEnvelope struct {
	Primary   *codexWindow `json:"primary"`
	Secondary *codexWindow `json:"secondary"`
	PlanType  string       `json:"plan_type"`
	Reached   *string      `json:"rate_limit_reached_type"`
	Credits   *struct {
		HasCredits bool    `json:"has_credits"`
		Unlimited  bool    `json:"unlimited"`
		Balance    *string `json:"balance"`
	} `json:"credits"`
}

type codexWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int64   `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

// FromLogRow maps one Codex rate_limits envelope. A same-class collision
// keeps the more urgent reading.
func FromLogRow(r LogRow) (Snapshot, bool) {
	prov := PassiveLogTools[r.Tool]
	if prov == "" {
		return Snapshot{}, false
	}
	var e codexEnvelope
	if json.Unmarshal(r.Raw, &e) != nil {
		return Snapshot{}, false
	}
	s := newSnap(prov, SourceCodexLogs, KindPassiveLog, ConfPercentOnly, r.ObservedAt)
	s.PlanCode = SanitizeLabel(e.PlanType)
	for _, w := range []*codexWindow{e.Primary, e.Secondary} {
		if w == nil {
			continue
		}
		key := windowKeyForMinutes(w.WindowMinutes)
		if prev, ok := FindWindow(s, key); ok && *prev.UsedFrac >= w.UsedPercent/100 {
			continue
		}
		s.Windows = removeWindow(s.Windows, key)
		s.Windows = append(s.Windows, Window{
			Key: key, Unit: UnitPercent, UsedFrac: fracFromPercent(w.UsedPercent),
			ResetsAt: unixPtr(w.ResetsAt), Length: dur(time.Duration(w.WindowMinutes) * time.Minute), Rolling: true,
		})
	}
	if c := e.Credits; c != nil && c.HasCredits && !c.Unlimited && c.Balance != nil {
		if m, err := MicrosFromDecimal(*c.Balance, 0); err == nil {
			s.Balances = append(s.Balances, Balance{Kind: "credits", Unit: UnitCredits, Amount: m / 1_000_000})
		}
	}
	if e.Reached != nil && *e.Reached != "" {
		s.Status = StatusThrottled
	}
	sort.SliceStable(s.Windows, func(i, j int) bool { return s.Windows[i].Key < s.Windows[j].Key })
	return s, len(s.Windows)+len(s.Balances) > 0
}

func removeWindow(ws []Window, key string) []Window {
	out := ws[:0]
	for _, w := range ws {
		if w.Key != key {
			out = append(out, w)
		}
	}
	return out
}

// --- Claude Code status line ----------------------------------------------------

// StatusLineWindow is the documented status line window: used_percentage 0..100,
// resets_at unix epoch seconds.
type StatusLineWindow struct {
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       *int64   `json:"resets_at"`
}

// StatusLineSpend is the status line spend-limit block (extra usage), when
// the plan has one.
type StatusLineSpend struct {
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       *int64   `json:"resets_at"`
	UsedUSD        *float64 `json:"used_usd"`
	LimitUSD       *float64 `json:"limit_usd"`
	Period         string   `json:"period"`
}

// StatusLineLimits is the numbers-only rate_limits block of the Claude Code
// status line payload (https://code.claude.com/docs/en/statusline).
type StatusLineLimits struct {
	FiveHour   *StatusLineWindow `json:"five_hour,omitempty"`
	SevenDay   *StatusLineWindow `json:"seven_day,omitempty"`
	SpendLimit *StatusLineSpend  `json:"spend_limit,omitempty"`
}

// StatusLineForward is the record `observer statusline` forwards to the
// daemon. It deliberately carries no path, model, cost or prompt text: only
// the session id (for the account binding), the Claude Code version and the
// numbers.
type StatusLineForward struct {
	SessionID  string            `json:"session_id"`
	CCVersion  string            `json:"cc_version,omitempty"`
	ObservedAt int64             `json:"observed_at"`
	RateLimits *StatusLineLimits `json:"rate_limits"`
}

// ErrNoRateLimits means the status line payload has no usable rate_limits
// block (an API-key user, no response yet, or every window expired).
var ErrNoRateLimits = errors.New("planlimits: no rate_limits in the status line payload")

// maxForwardSessionID caps the forwarded session id.
const maxForwardSessionID = 128

// ExtractStatusLineForward reduces a full status line payload to the forward
// record.
func ExtractStatusLineForward(payload []byte, now time.Time) (StatusLineForward, error) {
	var in struct {
		SessionID  string            `json:"session_id"`
		Version    string            `json:"version"`
		RateLimits *StatusLineLimits `json:"rate_limits"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return StatusLineForward{}, err
	}
	if in.RateLimits == nil || (in.RateLimits.FiveHour == nil && in.RateLimits.SevenDay == nil && in.RateLimits.SpendLimit == nil) {
		return StatusLineForward{}, ErrNoRateLimits
	}
	sid := in.SessionID
	if len(sid) > maxForwardSessionID {
		sid = sid[:maxForwardSessionID]
	}
	return StatusLineForward{SessionID: sid, CCVersion: SanitizeLabel(in.Version), ObservedAt: now.Unix(), RateLimits: in.RateLimits}, nil
}

// StatusLineChanged reports whether a forward differs from the last one sent,
// so the status line (which re-runs every few hundred ms) forwards on change
// only.
func StatusLineChanged(prev, next StatusLineForward) bool {
	a, _ := json.Marshal(prev.RateLimits)
	b, _ := json.Marshal(next.RateLimits)
	return prev.SessionID != next.SessionID || string(a) != string(b)
}

var spendPeriodLength = map[string]time.Duration{"daily": 24 * time.Hour, "weekly": 7 * 24 * time.Hour, "monthly": 30 * 24 * time.Hour}

// FromStatusLine maps a forward record onto the model.
func FromStatusLine(f StatusLineForward) (Snapshot, error) {
	if f.RateLimits == nil {
		return Snapshot{}, ErrNoRateLimits
	}
	s := newSnap(ProviderClaude, SourceClaudeStatusLine, KindStatusLine, ConfPercentOnly, time.Unix(f.ObservedAt, 0))
	add := func(key string, w *StatusLineWindow, length time.Duration) {
		if w == nil || w.UsedPercentage == nil || *w.UsedPercentage < 0 {
			return
		}
		nw := Window{Key: key, Unit: UnitPercent, UsedFrac: F(min(*w.UsedPercentage/100, 2)), Rolling: true, Length: dur(length)}
		if w.ResetsAt != nil {
			nw.ResetsAt = unixPtr(*w.ResetsAt)
		}
		s.Windows = append(s.Windows, nw)
	}
	add("5h", f.RateLimits.FiveHour, 5*time.Hour)
	add("weekly", f.RateLimits.SevenDay, 7*24*time.Hour)
	if sl := f.RateLimits.SpendLimit; sl != nil && sl.UsedPercentage != nil && *sl.UsedPercentage >= 0 {
		w := Window{Key: "spend_limit", Unit: UnitUSDMicros, UsedFrac: F(min(*sl.UsedPercentage/100, 2))}
		if sl.ResetsAt != nil {
			w.ResetsAt = unixPtr(*sl.ResetsAt)
		}
		if sl.UsedUSD != nil {
			w.Used = F(float64(microsFromFloat(*sl.UsedUSD)))
		}
		if sl.LimitUSD != nil {
			w.Limit = F(float64(microsFromFloat(*sl.LimitUSD)))
		}
		if d, ok := spendPeriodLength[sl.Period]; ok {
			w.Length = dur(d)
		}
		s.Windows = append(s.Windows, w)
	}
	if len(s.Windows) == 0 {
		return s, ErrNoRateLimits
	}
	return s, nil
}

// --- Read-time account-scope binding ----------------------------------------------

// AccountObs is the projection of one tool_account_observations row (agent
// migration 111) the binding needs. AccountKey is the node-local toolaccount
// hash; it never leaves the node.
type AccountObs struct {
	SessionID  string
	Tool       string
	AccountKey string
	Label      string // the observed login (email), shown only on this machine
}

// DefaultScope is the value the proxy writes to limit_snapshots.scope_hash
// today.
const DefaultScope = "default"

// Binding reasons.
const (
	ScopeBound         = "bound"
	ScopeNoSession     = "no_session"
	ScopeNoObservation = "no_observation"
	ScopeConflict      = "conflict"
	ScopeExplicit      = "explicit" // a writer already stored a non-default scope
)

// ScopeResult is the outcome of binding one passive reading to an account.
type ScopeResult struct {
	Scope  string // "acct:<account_key>" | "default" | an explicit stored scope
	Reason string
}

// scopeRule is one row of the binding rule table, walked top-down.
type scopeRule struct {
	reason string
	match  func(sessionID string, keys map[string]bool) bool
	scope  func(keys map[string]bool) string
}

func defaultScope(map[string]bool) string { return DefaultScope }

// scopeRules is the read-time binding (Phase 0 design option A): a reading is
// bound to the ONE account its session was observed using, for the tool that
// owns the session. Disagreement is preserved, never guessed (the
// toolaccount rule), and another tool's login is never borrowed.
var scopeRules = []scopeRule{
	{ScopeNoSession, func(sid string, _ map[string]bool) bool { return sid == "" }, defaultScope},
	{ScopeNoObservation, func(_ string, k map[string]bool) bool { return len(k) == 0 }, defaultScope},
	{ScopeConflict, func(_ string, k map[string]bool) bool { return len(k) > 1 }, defaultScope},
	{ScopeBound, func(string, map[string]bool) bool { return true }, func(k map[string]bool) string {
		for key := range k {
			return "acct:" + key
		}
		return DefaultScope
	}},
}

// ResolveScope binds one reading to an account. Observations are considered
// regardless of time order: hooks can land before or after the proxy
// response, and a session draws from one login unless observations disagree.
func ResolveScope(sessionID, tool string, obs []AccountObs) ScopeResult {
	keys := map[string]bool{}
	for _, o := range obs {
		if sessionID != "" && o.SessionID == sessionID && o.Tool == tool && o.AccountKey != "" {
			keys[o.AccountKey] = true
		}
	}
	for _, r := range scopeRules {
		if r.match(sessionID, keys) {
			return ScopeResult{Scope: r.scope(keys), Reason: r.reason}
		}
	}
	return ScopeResult{Scope: DefaultScope, Reason: ScopeNoObservation}
}

// ScopeFor resolves a stored scope: a non-default stored value is honoured
// as-is; "" and "default" are resolved through ResolveScope.
func ScopeFor(stored, sessionID, tool string, obs []AccountObs) ScopeResult {
	if stored != "" && stored != DefaultScope {
		return ScopeResult{Scope: stored, Reason: ScopeExplicit}
	}
	return ResolveScope(sessionID, tool, obs)
}

// StatusLineTool is the tool whose status line feeds SourceClaudeStatusLine;
// its session ids bind through that tool's login observations.
const StatusLineTool = "claude-code"

// SessionSnapshot is a stored passive snapshot with its session id.
type SessionSnapshot struct {
	SessionID string
	Snapshot
}

// BindPassive maps every passive reading onto the model and binds it to an
// account scope with the read-time rule table.
func BindPassive(limits []LimitRow, logs []LogRow, statusLine []SessionSnapshot, obs []AccountObs) []ScopedSnapshot {
	var out []ScopedSnapshot
	bind := func(s Snapshot, stored, sid, tool string) {
		r := ScopeFor(stored, sid, tool, obs)
		out = append(out, ScopedSnapshot{Snapshot: s, Scope: r.Scope, Reason: r.Reason, ScopeLabel: ScopeLabel(r.Scope, obs)})
	}
	for _, l := range limits {
		if s, ok := FromLimitRow(l); ok {
			bind(s, l.ScopeHash, l.SessionID, l.Tool)
		}
	}
	for _, l := range logs {
		if s, ok := FromLogRow(l); ok {
			bind(s, "", l.SessionID, l.Tool)
		}
	}
	for _, s := range statusLine {
		bind(s.Snapshot, "", s.SessionID, StatusLineTool)
	}
	return out
}

// ScopeLabel returns the observed login for a bound scope, "" otherwise.
func ScopeLabel(scope string, obs []AccountObs) string {
	for _, o := range obs {
		if "acct:"+o.AccountKey == scope && o.Label != "" {
			return o.Label
		}
	}
	return ""
}
