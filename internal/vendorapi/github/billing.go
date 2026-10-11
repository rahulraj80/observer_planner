package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// AI-credit usage surfaces (lane L8, T-611): the enhanced-billing usage
// report, GET /organizations/{org}/settings/billing/ai_credit/usage and
// GET /enterprises/{enterprise}/settings/billing/ai_credit/usage
// (rest-api-description ghec.2026-03-10, operations
// billing/get-github-billing-ai-credit-usage-report-org and -ghe). Each
// usage item lands as aggregate facts over the interval the response's
// timePeriod names (a day, a month or a year: never booked to a day the
// vendor did not echo).
const (
	SurfaceAICreditOrg vs.SurfaceID = "github.billing.ai_credit_usage_org" //nolint:gosec // G101: a catalog surface id, not a credential
	SurfaceAICreditEnt vs.SurfaceID = "github.billing.ai_credit_usage_ent" //nolint:gosec // G101: a catalog surface id, not a credential

	// LegacyBillingKey is the ownership key of the legacy copilotanalytics
	// billing surface, which reads the same report.
	LegacyBillingKey = "copilotanalytics.billing"

	// BillingLag is how far behind the window start the requested day is:
	// a run asks for the day before its window starts, so the latest day a
	// run reads has closed. Inferred (the docs state no availability lag).
	BillingLag = 24 * time.Hour

	// AICreditContract is recorded on every run of the AI-credit report.
	AICreditContract = "github.billing.ai_credit_usage/v1 api=" + APIVersion
)

// creditSpec is one row of the AI-credit surface table.
type creditSpec struct {
	surface  vs.SurfaceID
	param    string
	prefix   string
	re       *regexp.Regexp
	supplies []vs.DataClass
}

var creditSurfaces = []creditSpec{
	{SurfaceAICreditOrg, "org", "/organizations/", orgLoginRE, []vs.DataClass{vs.ClassCost}},
	{SurfaceAICreditEnt, "enterprise", "/enterprises/", enterpriseRE, []vs.DataClass{vs.ClassCost, vs.ClassUsage}},
}

// AICreditReports returns the driver constructors of the AI-credit usage
// surfaces, by surface.
func AICreditReports() map[vs.SurfaceID]func() vendorapi.Driver {
	out := map[vs.SurfaceID]func() vendorapi.Driver{}
	for _, c := range creditSurfaces {
		c := c
		out[c.surface] = func() vendorapi.Driver {
			return vendorapi.Driver{
				Surface: c.surface,
				Build: func(p vendorapi.Params, w vendorapi.Window, cur string) (vendorapi.Request, error) {
					return buildCredit(c, p, w, cur)
				},
				ParseResponse: parseCredit,
				Auth:          vendorapi.AuthContract{Kind: vs.AuthBearer, Scheme: vendorapi.SchemeBearer, Header: "Authorization"},
				Headers: map[string]string{
					"Accept":               "application/vnd.github+json",
					"X-GitHub-Api-Version": APIVersion,
				},
				ContentTypes:    []string{"application/json"},
				Supplies:        c.supplies,
				LegacyPoller:    LegacyBillingKey,
				ContractVersion: AICreditContract,
				Tool:            Tool,
				MaxWindow:       24 * time.Hour,
				KnownPaths:      knownPaths[c.surface],
			}
		}
	}
	return out
}

// buildCredit asks for ONE day: the day before the window starts. With the
// driver's one-day MaxWindow a backfill runs one day per run, and once
// caught up each hourly run re-reads the closed day before (a restatement
// replaces the provisional facts).
func buildCredit(c creditSpec, p vendorapi.Params, w vendorapi.Window, cursor string) (vendorapi.Request, error) {
	owner := p[c.param]
	if !c.re.MatchString(owner) {
		return vendorapi.Request{}, fmt.Errorf("%s: param %s must be a GitHub %s name", c.surface, c.param, c.param)
	}
	if cursor != "" {
		return vendorapi.Request{}, fmt.Errorf("%s: the report is one request per day", c.surface)
	}
	if w.From.IsZero() || !w.To.After(w.From) {
		return vendorapi.Request{}, fmt.Errorf("%s: a non-empty window is required", c.surface)
	}
	d := w.From.UTC().Truncate(24 * time.Hour).Add(-BillingLag)
	return vendorapi.Request{Method: "GET", Path: c.prefix + owner + "/settings/billing/ai_credit/usage", Query: map[string][]string{
		"year": {strconv.Itoa(d.Year())}, "month": {strconv.Itoa(int(d.Month()))}, "day": {strconv.Itoa(d.Day())},
	}}, nil
}

type creditDoc struct {
	TimePeriod *struct {
		Year  *int `json:"year"`
		Month *int `json:"month"`
		Day   *int `json:"day"`
	} `json:"timePeriod"`
	UsageItems *[]creditItem `json:"usageItems"`
}

type creditItem struct {
	Product          *string       `json:"product"`
	SKU              *string       `json:"sku"`
	Model            *string       `json:"model"`
	UnitType         *string       `json:"unitType"`
	GrossQuantity    *strictNumber `json:"grossQuantity"`
	GrossAmount      *strictNumber `json:"grossAmount"`
	DiscountQuantity *strictNumber `json:"discountQuantity"`
	DiscountAmount   *strictNumber `json:"discountAmount"`
	NetQuantity      *strictNumber `json:"netQuantity"`
	NetAmount        *strictNumber `json:"netAmount"`
}

// strictNumber is a JSON number that refuses a quoted number (json.Number
// alone accepts "1"): a string where the contract says number is drift.
type strictNumber json.Number

// UnmarshalJSON accepts a bare JSON number only.
func (n *strictNumber) UnmarshalJSON(b []byte) error {
	var v any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return err
	}
	num, ok := v.(json.Number)
	if !ok {
		return fmt.Errorf("not a number")
	}
	*n = strictNumber(num)
	return nil
}

func creditDrift(format string, a ...any) error {
	return fmt.Errorf("%w: github.billing.ai_credit_usage: "+format, append([]any{vendorapi.ErrSchemaDrift}, a...)...)
}

// creditInterval is the UTC interval the echoed timePeriod names: a day, a
// month or a year, whichever the vendor answered.
func creditInterval(year, month, day *int) (time.Time, time.Time, error) {
	if year == nil || *year < 2000 || *year > 9999 {
		return time.Time{}, time.Time{}, creditDrift("timePeriod.year is missing or out of range")
	}
	switch {
	case month == nil && day == nil:
		s := time.Date(*year, 1, 1, 0, 0, 0, 0, time.UTC)
		return s, s.AddDate(1, 0, 0), nil
	case month != nil && *month >= 1 && *month <= 12 && day == nil:
		s := time.Date(*year, time.Month(*month), 1, 0, 0, 0, 0, time.UTC)
		return s, s.AddDate(0, 1, 0), nil
	case month != nil && *month >= 1 && *month <= 12 && day != nil && *day >= 1 && *day <= 31:
		s := time.Date(*year, time.Month(*month), *day, 0, 0, 0, 0, time.UTC)
		if s.Day() != *day {
			return time.Time{}, time.Time{}, creditDrift("timePeriod is not a calendar day")
		}
		return s, s.AddDate(0, 0, 1), nil
	}
	return time.Time{}, time.Time{}, creditDrift("timePeriod is not a year, a month or a day")
}

var unitNameRE = regexp.MustCompile(`^[a-z][a-z_]{0,31}$`)

// unitName normalizes the vendor's unitType into the unit vocabulary
// ("ai-credits" -> "ai_credits"); a value that cannot be a unit lands as
// "other" (never guessed into another unit).
func unitName(s string) string {
	u := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", "_"))
	if unitNameRE.MatchString(u) {
		return u
	}
	return "other"
}

// parseCredit lands each usage item as facts: quantities in the item's own
// unit and amounts in usd, summed per (product, sku, model, unit) so one
// response never repeats a key. Money is integer micro-units; nothing here
// reaches a spend, budget or alert path (plan invariant I4).
func parseCredit(r vendorapi.Response) (vendorapi.Page, error) {
	if r.Status != 200 {
		return vendorapi.Page{}, creditDrift("status %d is not the documented 200", r.Status)
	}
	var doc creditDoc
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return vendorapi.Page{}, creditDrift("body does not decode as the documented object")
	}
	if doc.TimePeriod == nil || doc.UsageItems == nil {
		return vendorapi.Page{}, creditDrift("required field timePeriod or usageItems is missing")
	}
	start, end, err := creditInterval(doc.TimePeriod.Year, doc.TimePeriod.Month, doc.TimePeriod.Day)
	if err != nil {
		return vendorapi.Page{}, err
	}
	type key struct{ product, sku, model, unit, metric string }
	sums := map[key]int64{}
	for i, it := range *doc.UsageItems {
		if it.Product == nil || it.SKU == nil || it.UnitType == nil || it.NetQuantity == nil || it.NetAmount == nil {
			return vendorapi.Page{}, creditDrift("usageItems[%d] misses product, sku, unitType, netQuantity or netAmount", i)
		}
		model := ""
		if it.Model != nil {
			model = *it.Model
		}
		unit := unitName(*it.UnitType)
		for _, m := range []struct {
			v            *strictNumber
			metric, unit string
		}{
			{it.NetQuantity, "net_quantity", unit},
			{it.GrossQuantity, "gross_quantity", unit},
			{it.DiscountQuantity, "discount_quantity", unit},
			{it.NetAmount, "net_amount", "usd"},
			{it.GrossAmount, "gross_amount", "usd"},
			{it.DiscountAmount, "discount_amount", "usd"},
		} {
			if m.v == nil {
				continue
			}
			micros, ok := toMicros(json.Number(*m.v))
			if !ok {
				return vendorapi.Page{}, creditDrift("usageItems[%d] has a negative or out-of-range %s", i, m.metric)
			}
			sums[key{*it.Product, *it.SKU, model, m.unit, m.metric}] += micros
		}
	}
	keys := make([]key, 0, len(sums))
	for k := range sums {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		return a.product+a.sku+a.model+a.unit+a.metric < b.product+b.sku+b.model+b.unit+b.metric
	})
	page := vendorapi.Page{SourceRecords: len(*doc.UsageItems)}
	for _, k := range keys {
		page.Facts = append(page.Facts, vendorapi.FactRow{
			IntervalStart: start, IntervalEnd: end, SourceTZ: "UTC", CoverageDomain: k.product,
			ModelRaw: k.model, Feature: k.sku, Metric: k.metric, Unit: k.unit, ValueMicros: sums[k],
		})
	}
	return page, nil
}
