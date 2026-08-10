// Package licenses provides the license/plan inventory, marginal cost model,
// and usage telemetry for the API Intelligence backbone (docs/design/
// api-intelligence.md §5.2).
package licenses

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
)

// EffectiveRates is the per-token marginal cost for a given license basis.
type EffectiveRates struct {
	Input           float64 // per 1M tokens
	Output          float64 // per 1M tokens
	CacheRead       float64 // per 1M tokens (read from cache)
	CacheWrite      float64 // per 1M tokens (write to cache)
	Basis           string  // "list" | "effective" | "blend"
	LicenseID       string
	EntitlementKind string
}

// PricingEngine is the interface for list-price lookups (implemented by cost.Engine).
type PricingEngine interface {
	Lookup(model string) (cost.Pricing, bool)
}

// CostModel computes effective/marginal rates from a license inventory.
// It is pure (no I/O) and deterministic — identical inputs always produce
// identical outputs. This enables reproducible routing decisions and
// dashboard break-even analysis.
type CostModel struct {
	Licenses []config.LicenseEntry
	ListRate PricingEngine
}

// NewCostModel creates a cost model from the license inventory and a list-price
// pricing engine for fallback.
func NewCostModel(licenses []config.LicenseEntry, listRate PricingEngine) *CostModel {
	return &CostModel{
		Licenses: licenses,
		ListRate: listRate,
	}
}

// RatesFor returns the effective rates for a (model, provider, entitlement) tuple.
// If no license matches, falls back to list rates with Basis="list".
func (c *CostModel) RatesFor(model, provider, entitlementKind string, withinQuota bool) EffectiveRates {
	// Find a matching license
	var match *config.LicenseEntry
	for i := range c.Licenses {
		le := &c.Licenses[i]
		if !le.Enabled {
			continue
		}
		if strings.EqualFold(le.Provider, provider) && le.Kind.String() == entitlementKind {
			if le.Entitles(model) {
				match = le
				break
			}
		}
	}

	if match == nil {
		// No license → list price
		r, ok := c.ListRate.Lookup(model)
		if !ok {
			return EffectiveRates{
				Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0,
				Basis:           "list",
				EntitlementKind: entitlementKind,
			}
		}
		return EffectiveRates{
			Input:           r.Input,
			Output:          r.Output,
			CacheRead:       r.CacheRead,
			CacheWrite:      r.CacheCreation, // CacheCreation is the write rate
			Basis:           "list",
			EntitlementKind: entitlementKind,
		}
	}

	// Has license — compute effective rates
	switch match.Kind {
	case config.LicenseKindSubscription:
		if withinQuota {
			// Inside subscription quota → ~0 marginal (only fast-tier marginal)
			return EffectiveRates{
				Input:           0,
				Output:          0,
				CacheRead:       0,
				CacheWrite:      0,
				Basis:           "effective",
				LicenseID:       match.ID,
				EntitlementKind: "subscription",
			}
		}
		// Over quota → list price
		fallthrough
	case config.LicenseKindAPI:
		// Pay-per-token → list price
		r, ok := c.ListRate.Lookup(model)
		if !ok {
			return EffectiveRates{
				Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0,
				Basis:           "list",
				LicenseID:       match.ID,
				EntitlementKind: match.Kind.String(),
			}
		}
		return EffectiveRates{
			Input:           r.Input,
			Output:          r.Output,
			CacheRead:       r.CacheRead,
			CacheWrite:      r.CacheCreation,
			Basis:           "list",
			LicenseID:       match.ID,
			EntitlementKind: match.Kind.String(),
		}
	case config.LicenseKindPlan:
		// Blended/enterprise → blend of list and effective (placeholder: list for now)
		r, ok := c.ListRate.Lookup(model)
		if !ok {
			return EffectiveRates{
				Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0,
				Basis:           "blend",
				LicenseID:       match.ID,
				EntitlementKind: "plan",
			}
		}
		return EffectiveRates{
			Input:           r.Input,
			Output:          r.Output,
			CacheRead:       r.CacheRead,
			CacheWrite:      r.CacheCreation,
			Basis:           "blend",
			LicenseID:       match.ID,
			EntitlementKind: "plan",
		}
	}

	// Fallback (should not reach)
	r, _ := c.ListRate.Lookup(model)
	return EffectiveRates{
		Input:           r.Input,
		Output:          r.Output,
		CacheRead:       r.CacheRead,
		CacheWrite:      r.CacheCreation,
		Basis:           "list",
		EntitlementKind: entitlementKind,
	}
}

// BreakEvenTokens returns the token volume where a subscription seat breaks
// even vs. pay-per-token list price. Formula: seat_cost_month / blended_price_per_token.
// Returns 0 if seat cost unknown or no list price.
func (c *CostModel) BreakEvenTokens(licenseID, model string) int64 {
	var match *config.LicenseEntry
	for i := range c.Licenses {
		if c.Licenses[i].ID == licenseID {
			match = &c.Licenses[i]
			break
		}
	}
	if match == nil || match.SeatCostMonth <= 0 {
		return 0
	}

	// Get list rates for the model
	r, ok := c.ListRate.Lookup(model)
	if !ok {
		return 0
	}
	blended := (r.Input + r.Output) / 2_000_000 // per token
	if blended <= 0 {
		return 0
	}
	return int64(match.SeatCostMonth / blended)
}

// LicenseUsageRow is one row of license usage telemetry (for migration 075).
type LicenseUsageRow struct {
	LicenseID           string
	ObservedAt          time.Time
	EntitlementKind     string
	Provider            string
	ModelsUsed          []string
	Turns               int64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	CostUSDList         float64
	QuotaMonthlyTokens  *int64
}

// UpsertLicenseUsage writes a license usage row. Caller should batch per hour
// or day; this function is the store seam.
func UpsertLicenseUsage(ctx context.Context, db *sql.DB, row LicenseUsageRow) error {
	modelsJSON, _ := json.Marshal(row.ModelsUsed)
	var quotaPtr *int64 = row.QuotaMonthlyTokens

	_, err := db.ExecContext(ctx, `
		INSERT INTO license_usage (
			license_id, observed_at, entitlement_kind, provider,
			models_used_json, turns, input_tokens, output_tokens,
			cache_read_tokens, cache_creation_tokens, cost_usd_list, quota_monthly_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, row.LicenseID, row.ObservedAt.UTC().Format(time.RFC3339), row.EntitlementKind, row.Provider,
		string(modelsJSON), row.Turns, row.InputTokens, row.OutputTokens,
		row.CacheReadTokens, row.CacheCreationTokens, row.CostUSDList, quotaPtr)
	return err
}

// SelectLicenseUsage loads license usage for a window (for dashboard rollups).
func SelectLicenseUsage(ctx context.Context, db *sql.DB, since, until time.Time) ([]LicenseUsageRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT license_id, observed_at, entitlement_kind, provider,
		       models_used_json, turns, input_tokens, output_tokens,
		       cache_read_tokens, cache_creation_tokens, cost_usd_list, quota_monthly_tokens
		FROM license_usage
		WHERE observed_at >= ? AND observed_at < ?
		ORDER BY observed_at
	`, since.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LicenseUsageRow
	for rows.Next() {
		var r LicenseUsageRow
		var modelsJSON string
		var obsStr string
		var quota *int64
		if err := rows.Scan(&r.LicenseID, &obsStr, &r.EntitlementKind, &r.Provider,
			&modelsJSON, &r.Turns, &r.InputTokens, &r.OutputTokens,
			&r.CacheReadTokens, &r.CacheCreationTokens, &r.CostUSDList, &quota); err != nil {
			return nil, err
		}
		r.QuotaMonthlyTokens = quota
		_ = json.Unmarshal([]byte(modelsJSON), &r.ModelsUsed)
		r.ObservedAt, _ = time.Parse(time.RFC3339, obsStr)
		out = append(out, r)
	}
	return out, rows.Err()
}
