package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// LicenseConfig is the [licenses] surface — the set of API licenses/plans the
// user/team actually pays for, with the models each entitles, quota, and
// marginal-cost basis. This is the input to Ask-2 recommendations and to
// effective-cost-aware dispatch (docs/design/api-intelligence.md §5.2).
//
// LOCAL-ONLY, never distributed via [org_client.share]. OPT-IN: default empty
// → current behavior unchanged (list-price everywhere). Secrets are env-ref or
// OS-keychain refs only — never written to config.
type LicenseConfig struct {
	// Licenses maps a license id (slug, e.g. "anthropic-team") to its config.
	// Empty = no licenses configured.
	Licenses map[string]LicenseEntryConfig `toml:"licenses"`
}

// LicenseEntryConfig is one license/plan entry. Keys follow the license model
// in api-intelligence.md §5.2.
type LicenseEntryConfig struct {
	// Name is the human label shown in the UI/CLI. Empty falls back to the
	// license id.
	Name string `toml:"name"`
	// Provider is the provider this license covers: anthropic | openai | google | etc.
	Provider string `toml:"provider"`
	// Kind is the entitlement kind: subscription | api | plan.
	//   - subscription = per-seat subscription with included quota (e.g. Anthropic Team, GitHub Copilot)
	//   - api = pay-per-token API key (e.g. Anthropic API key, OpenAI API key)
	//   - plan = blended/enterprise plan with custom terms
	Kind string `toml:"kind"`
	// Models is the list of model IDs this license entitles access to.
	// Empty = all models for the provider (broad entitlement).
	Models []string `toml:"models"`
	// SeatCostMonth is the monthly cost per seat in USD (for subscription/plans).
	// Used for break-even analysis. 0 = unknown/not applicable.
	SeatCostMonth float64 `toml:"seat_cost_month"`
	// Quota is the monthly included quota for subscription/plans.
	Quota LicenseQuotaConfig `toml:"quota"`
	// Enabled gates the license. false = never used for effective cost.
	Enabled bool `toml:"enabled"`
	// Notes is free-form operator notes.
	Notes string `toml:"notes"`
}

// LicenseQuotaConfig is the monthly quota for a subscription/plan.
type LicenseQuotaConfig struct {
	// MonthlyTokens is the included token quota per month. 0 = unmetered/unknown.
	MonthlyTokens int64 `toml:"monthly_tokens"`
	// FastRequests is the included fast-tier request quota per month (for providers with fast/slow tiers).
	// 0 = unmetered/unknown.
	FastRequests int64 `toml:"fast_requests"`
}

// validLicenseKinds is the closed kind vocabulary.
var validLicenseKinds = map[string]bool{
	"subscription": true,
	"api":          true,
	"plan":         true,
}

// validLicenseProviders is the closed provider vocabulary (extensible).
var validLicenseProviders = map[string]bool{
	"anthropic": true,
	"openai":    true,
	"google":    true,
	"azure":     true,
	"vertex":    true,
	"aws":       true,
	"cohere":    true,
	"mistral":   true,
	"groq":      true,
	"ollama":    true,
}

// ToLicenseEntries converts the static [licenses.licenses] map into a sorted
// slice of LicenseEntry for runtime use.
func (c LicenseConfig) ToLicenseEntries() []LicenseEntry {
	ids := make([]string, 0, len(c.Licenses))
	for id := range c.Licenses {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]LicenseEntry, 0, len(ids))
	for _, id := range ids {
		le := c.Licenses[id]
		out = append(out, LicenseEntry{
			ID:            id,
			Name:          le.Name,
			Provider:      le.Provider,
			Kind:          LicenseKind(le.Kind),
			Models:        le.Models,
			SeatCostMonth: le.SeatCostMonth,
			Quota:         le.Quota,
			Enabled:       le.Enabled,
			Notes:         le.Notes,
		})
	}
	return out
}

// LicenseEntry is the runtime representation of a license entry.
type LicenseEntry struct {
	ID            string
	Name          string
	Provider      string
	Kind          LicenseKind
	Models        []string
	SeatCostMonth float64
	Quota         LicenseQuotaConfig
	Enabled       bool
	Notes         string
}

// Entitles reports whether the license covers a given model.
// Empty Models = all models for the provider.
func (le *LicenseEntry) Entitles(model string) bool {
	if len(le.Models) == 0 {
		return true
	}
	for _, m := range le.Models {
		if m == model {
			return true
		}
	}
	return false
}

// LicenseKind is the entitlement kind.
type LicenseKind string

const (
	LicenseKindSubscription = LicenseKind("subscription")
	LicenseKindAPI          = LicenseKind("api")
	LicenseKindPlan         = LicenseKind("plan")
)

// String returns the string representation of the LicenseKind.
func (k LicenseKind) String() string {
	return string(k)
}

// LicenseQuota is the runtime quota.
type LicenseQuota struct {
	MonthlyTokens int64
	FastRequests  int64
}

// validateLicense checks the [licenses] block. The zero value is always valid.
func validateLicense(c LicenseConfig) error {
	for id, le := range c.Licenses {
		if strings.TrimSpace(id) == "" {
			return errors.New("config: licenses.licenses: license id must not be empty")
		}
		if !validLicenseKinds[le.Kind] {
			return fmt.Errorf("config: licenses.licenses.%s.kind %q not in {subscription, api, plan}", id, le.Kind)
		}
		if !validLicenseProviders[le.Provider] {
			return fmt.Errorf("config: licenses.licenses.%s.provider %q not recognized (known: anthropic, openai, google, azure, vertex, aws, cohere, mistral, groq, ollama)", id, le.Provider)
		}
		if le.SeatCostMonth < 0 {
			return fmt.Errorf("config: licenses.licenses.%s.seat_cost_month must be >= 0", id)
		}
		if le.Quota.MonthlyTokens < 0 || le.Quota.FastRequests < 0 {
			return fmt.Errorf("config: licenses.licenses.%s.quota monthly_tokens/fast_requests must be >= 0", id)
		}
	}
	return nil
}
