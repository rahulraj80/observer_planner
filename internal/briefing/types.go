// Package briefing implements the data-only daily briefing product: a pure,
// deterministic composition layer that turns raw cost/usage facts into a
// human-readable Briefing document with drill-down justifications. It carries
// no network calls, no prompt text, and no file paths; everything it needs is
// injected by the caller (see Facts and PriceFn). See
// docs/design/standup-briefing.md for the binding spec.
package briefing

import (
	"time"

	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
)

// Basis identifies the pricing basis used for cost estimates in a Briefing.
type Basis string

const (
	// BasisList is list pricing computed from the built-in pricing tables.
	BasisList Basis = "list"
	// BasisEffective is effective pricing after per-node config overrides.
	BasisEffective Basis = "effective"
)

// Window is the exact [Since, Until) span (UTC) a Briefing describes.
type Window struct {
	Since time.Time `json:"-"`
	Until time.Time `json:"-"`
}

// Briefing is the composed product (§2.1 of the briefing spec).
type Briefing struct {
	Date          string        `json:"date"`            // YYYY-MM-DD of the most recent completed local day in the window
	GeneratedAt   string        `json:"generated_at"`    // RFC3339 UTC
	Basis         Basis         `json:"basis"`           // list | effective
	Period        string        `json:"period"`          // day | week | month | N-day (multi-day mode, ?window=)
	Window        windowJSON    `json:"window"`          // RFC3339 bounds
	AtGlance      AtGlance      `json:"at_glance"`       // headline KPI chips
	Trend         *Trend        `json:"trend,omitempty"` // multi-day deltas vs the prior window (§ Stage 1)
	Activity      []Activity    `json:"activity"`        // per-tool/harness rows
	TopSessions   []SessionRef  `json:"top_sessions"`    // drill-down targets, cost-desc
	Opportunities []Opportunity `json:"opportunities"`   // efficiency-gain cards
	Narrative     string        `json:"narrative"`       // deterministic data summary
	Questions     []string      `json:"questions"`       // open questions for the human
}

// windowJSON is the serializable projection of Window.
type windowJSON struct {
	Since string `json:"since"`
	Until string `json:"until"`
}

// AtGlance is the headline KPI chip set for the day.
type AtGlance struct {
	Sessions            int     `json:"sessions"`
	Turns               int     `json:"turns"`
	SpendUSD            float64 `json:"spend_usd"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheHitRate        float64 `json:"cache_hit_rate"`
	ErrorTurns          int     `json:"error_turns"`
	ActiveHours         float64 `json:"active_hours"`
	FastShare           float64 `json:"fast_share"`
	TTFTP50MS           int64   `json:"ttft_p50_ms"`  // nearest-rank percentile over real proxy TTFT captures
	TTFTP95MS           int64   `json:"ttft_p95_ms"`  // (api_turns.time_to_first_token_ms > 0 only)
	TotalP50MS          int64   `json:"total_p50_ms"` // total-response-time percentile (api_turns.total_response_ms)
	TotalP95MS          int64   `json:"total_p95_ms"`
}

// Trend is the multi-day comparison block: the current window's KPIs next to
// the prior window of the same length, with the deltas (§2.3 "comparison vs
// the prior window for context"). Absent (nil) for the single-day briefing.
type Trend struct {
	WindowDays        int     `json:"window_days"`
	SpendUSD          float64 `json:"spend_usd"`
	SpendPriorUSD     float64 `json:"spend_prior_usd,omitempty"`
	SpendDeltaUSD     float64 `json:"spend_delta_usd,omitempty"`
	SpendDeltaPct     float64 `json:"spend_delta_pct,omitempty"`
	Turns             int     `json:"turns"`
	TurnsPrior        int     `json:"turns_prior,omitempty"`
	TurnsDeltaPct     float64 `json:"turns_delta_pct,omitempty"`
	Sessions          int     `json:"sessions"`
	SessionsPrior     int     `json:"sessions_prior,omitempty"`
	SessionsDeltaPct  float64 `json:"sessions_delta_pct,omitempty"`
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	CacheReadTokens   int64   `json:"cache_read_tokens"`
	CacheHitRate      float64 `json:"cache_hit_rate"`
	CacheHitRateDelta float64 `json:"cache_hit_rate_delta,omitempty"`
	ErrorRate         float64 `json:"error_rate"`
	ErrorRatePrior    float64 `json:"error_rate_prior,omitempty"`
	ErrorRateDelta    float64 `json:"error_rate_delta,omitempty"`
	TTFTP50MS         int64   `json:"ttft_p50_ms"`
	TTFTP95MS         int64   `json:"ttft_p95_ms"`
	TTFTP50DeltaMS    int64   `json:"ttft_p50_delta_ms,omitempty"`
	TTFTP95DeltaMS    int64   `json:"ttft_p95_delta_ms,omitempty"`
}

// Activity is one tool/harness row in the per-activity breakdown.
type Activity struct {
	Tool     string  `json:"tool"`
	Sessions int     `json:"sessions"`
	Turns    int     `json:"turns"`
	SpendUSD float64 `json:"spend_usd"`
}

// SessionRef is a drill-down target for a top session.
type SessionRef struct {
	ID      string            `json:"id"`
	Tool    string            `json:"tool,omitempty"`
	Turns   int               `json:"turns"`
	CostUSD float64           `json:"cost_usd"`
	Errors  int               `json:"errors,omitempty"` // error turns in the session
	Models  []SessionModelRef `json:"models,omitempty"` // model mix, name-sorted, content-free
}

// SessionModelRef is one model's share of a top session (content-free: model
// name + counts/cost only — never prompts or turn bodies).
type SessionModelRef struct {
	Name    string  `json:"name"`
	Turns   int     `json:"turns"`
	CostUSD float64 `json:"cost_usd"`
}

// Opportunity is one efficiency-gain claim with its full drill-down
// justification (§2.3 of the briefing spec).
type Opportunity struct {
	DedupKey      string        `json:"dedup_key"`
	Detector      string        `json:"detector"`
	Category      string        `json:"category"`
	Scope         string        `json:"scope"`
	Severity      string        `json:"severity"`
	Title         string        `json:"title"`
	Nudge         string        `json:"nudge"`
	SavingsUSD    float64       `json:"savings_usd,omitempty"`
	SavingsMin    float64       `json:"savings_min,omitempty"`
	Confidence    float64       `json:"confidence"`
	Recurrence    bool          `json:"recurrence,omitempty"` // multi-day pattern card (never suppressed by the dollar filter)
	Report        bool          `json:"report,omitempty"`     // status/report card (e.g. realized-vs-estimated); never suppressed by the dollar filter
	Justification Justification `json:"justification"`
}

// Justification is the drill-down evidence spine for an Opportunity (§2.4).
// It mirrors the advisor.Suggestion evidence spine so the same card can render
// identically in the dashboard, the digest email, and MCP tools.
type Justification struct {
	Claim           string        `json:"claim"`              // what the data says, in one line
	Source          string        `json:"source"`             // api_turns | token_usage | router_decisions | model_calibration | license_usage
	GroupBy         string        `json:"group_by,omitempty"` // session | model | project | tool | none
	Since           string        `json:"since"`              // RFC3339 UTC
	Until           string        `json:"until"`              // RFC3339 UTC
	Tool            string        `json:"tool,omitempty"`
	Project         string        `json:"project,omitempty"`
	Days            []string      `json:"days,omitempty"` // affected local days (multi-day recurrence cards)
	PriceBasis      Basis         `json:"price_basis"`
	EstimateVersion string        `json:"estimate_version,omitempty"`
	Evidence        Evidence      `json:"evidence"`
	Confidence      float64       `json:"confidence"`
	FlipConditions  string        `json:"flip_conditions,omitempty"`
	Reprice         []RepriceTurn `json:"reprice,omitempty"`
}

// Evidence is the raw numbers + step-by-step math behind a Justification.
type Evidence struct {
	Numbers map[string]float64 `json:"numbers,omitempty"`
	Items   []EvidenceItem     `json:"items,omitempty"`
	Math    string             `json:"math,omitempty"`
}

// EvidenceItem is one labeled number on the evidence spine.
type EvidenceItem struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}

// RepriceTurn is one counterfactual replay step: the recorded model/cost vs
// what a candidate model/policy would have cost on the same token bundle.
type RepriceTurn struct {
	OriginalModel string  `json:"original_model"`
	SelectedModel string  `json:"selected_model"`
	EstimatedCost float64 `json:"estimated_cost_usd"`
	Reason        string  `json:"reason,omitempty"`
}

// PriceFn prices a token bundle for a model, returning whether the model had a
// price at all. Injected by the loader so this package stays deterministic.
type PriceFn func(model string, b cost.TokenBundle) (float64, bool)

// Turn is one recorded LLM turn within the briefing window. The loader is
// responsible for pricing it (CostUSD) and for computing the per-turn
// long-context surcharge and cache-savings figures; the composer only sums and
// reasons over them.
type Turn struct {
	SessionID       string
	Tool            string
	Model           string
	Timestamp       time.Time
	Tokens          cost.TokenBundle
	CostUSD         float64 // recorded (proxy) or engine-priced (JSONL) cost for this turn
	LCSurchargeUSD  float64 // what this turn would have cost without its long-context premium
	CacheSavingsUSD float64 // what this turn's cache reads would have cost if re-read as input
	Error           bool
	ErrorMessage    string // scrubbed error message from api_turns.error_message (proxy turns only)
	Fast            bool
	TTFTMS          int64 // proxy-captured time-to-first-token in ms (0 when not a streamed proxy turn)
	TotalMS         int64 // proxy-captured total response time in ms
}

// SessionFacts is the per-session aggregate the composer reasons over.
type SessionFacts struct {
	ID         string
	Tool       string
	Turns      int
	CostUSD    float64
	Errors     int                         // error turns in the session
	Models     map[string]cost.TokenBundle // model -> aggregated token bundle for that session
	ModelTurns map[string]int              // model -> turn count for that session
}

// ShadowFacts is a routing advise-shadow report (yesterday's would-have-done
// under the candidate policy).
type ShadowFacts struct {
	PolicyName               string
	PolicyVersion            string
	Rerouted                 int // would-have-rerouted turns
	WouldSaveUSD             float64
	ExtraCostUSD             float64 // turns that would have been rerouted up (usually 0)
	NetSaveUSD               float64
	QualityFlags             int
	Coverage                 float64 // fraction of turns the candidate policy could decide
	StickinessHolds          int
	StickinessHoldSavingsUSD float64
}

// Thresholds carries the tunables the composer uses for opportunity detection.
type Thresholds struct {
	HealthyCacheHitRate float64 // below this, a cache-hint opportunity fires (default 0.6)
	TrivialPromptTokens int64   // session with fewer prompt tokens than this is "trivial" (default 50_000)
	TrivialOutputTokens int64   // same for output tokens (default 50_000)
	RoutingTargetModel  string  // the policy's candidate model for trivial sessions (default "gpt-5-mini")
	LCFloorUSD          float64 // min absolute surcharge to report the long-context card (default 0.20)
	MinSavingsUSD       float64 // cards below this threshold are suppressed (default 0.05)
}

// Facts is the full deterministic input bundle for the composer. Every field
// is either raw data or an injected pure function; Compose never touches the
// database or the network.
type Facts struct {
	Window              Window
	Basis               Basis
	GeneratedAt         time.Time
	Date                string
	WindowDays          int       // 1 = daily; 7/28 = weekly/monthly; N = ?window=Nd
	Prior               *AtGlance // the same-length window immediately before Window (nil for daily mode)
	Turns               []Turn
	Sessions            []SessionFacts
	Shadow              *ShadowFacts
	RealizedVSEstimated []RealizedVSEstimated // CTA applies with projected vs realized (in-window)
	VirtuousCycle       *VirtuousCycleFacts   // cumulative history across ALL applies
	Price               PriceFn
	Thresholds          Thresholds
}

// EstimateVersion is the self-describing version string stamped onto every
// justification so the math can be reproduced exactly.
const EstimateVersion = "briefing/v1"

// RealizedVSEstimated is one CTA apply with its projected vs realized
// outcome. Surfaces in the briefing so the user sees "applied X on DATE →
// saved $Y of $Z estimated" (Stage 3 virtuous-cycle reporting).
type RealizedVSEstimated struct {
	ApplyStamp          time.Time `json:"apply_stamp"`
	Detector            string    `json:"detector"`
	ChangeID            string    `json:"change_id"`
	ConfigSurface       string    `json:"config_surface"`
	From                string    `json:"from"`
	To                  string    `json:"to"`
	ProjectedMonthlyUSD float64   `json:"projected_monthly_usd"`
	RealizedUSD         float64   `json:"realized_usd"`
	RealizedTurns       int       `json:"realized_turns"`
	Confidence          float64   `json:"confidence"`
}

// VirtuousCycleFacts is the cumulative adoption/benefit history across all
// recorded CTA applies. It powers the "virtuous-cycle trend" card showing
// how many changes were applied and how much savings they actually realized
// (Stage 3 item 2).
type VirtuousCycleFacts struct {
	TotalApplies       int                 `json:"total_applies"`
	TotalProjectedUSD  float64             `json:"total_projected_usd"`
	TotalRealizedUSD   float64             `json:"total_realized_usd"`
	TotalRealizedTurns int                 `json:"total_realized_turns"`
	ByWeek             []VirtuousCycleWeek `json:"by_week,omitempty"`
}

// VirtuousCycleWeek is one weekly bucket in the cumulative trend.
type VirtuousCycleWeek struct {
	WeekStart     string  `json:"week_start"` // YYYY-MM-DD (Monday)
	Applies       int     `json:"applies"`
	ProjectedUSD  float64 `json:"projected_usd"`
	RealizedUSD   float64 `json:"realized_usd"`
	RealizedTurns int     `json:"realized_turns"`
}
