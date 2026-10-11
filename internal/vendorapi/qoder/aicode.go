package qoder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// SurfaceDailyTrend is the catalog id of the AI code daily trend.
const SurfaceDailyTrend vs.SurfaceID = "qoder.oapi.aicode_daily_trend"

// AI code daily trend vocabulary (lane ENTAPI-L16C, T-710).
const (
	DailyTrendContract = "qoder.oapi.aicode_daily_trend/v1 teams-openapi ai-code-metrics (2026-10-08)"

	// Tool is the registry tool the organization's AI code metrics report on.
	Tool = "qoder"
	// Domain is the coverage domain of the organization-wide report.
	Domain = "qoder.organization"
	// MaxWindow caps one run (the reference allows 90 days).
	MaxWindow = 31 * day
	// MaxLookback is the documented range maximum.
	MaxLookback = 90 * day
	day         = 24 * time.Hour
)

// trendMetric is one row of a chart's metric table: the documented field and
// the fact metric and unit. Every listed field is required on a row; the
// deprecated aliases (aiLinesAdded, equal to aiLinesChanged) and the derived
// rates (aiShareRate, nextAcceptRate) are never landed.
type trendMetric struct {
	field, metric, unit string
}

var itemMetrics = []trendMetric{
	{"totalLinesChanged", "total_lines_changed", "lines"},
	{"aiLinesChanged", "ai_lines_changed", "lines"},
	{"otherLinesChanged", "other_lines_changed", "lines"},
	{"otherLinesAdded", "other_lines_added", "lines"},
	{"commitCount", "commits", "count"},
}

var nextMetrics = []trendMetric{
	{"nextSuggestedCount", "tab_suggestions", "count"},
	{"nextAcceptedCount", "tab_acceptances", "count"},
}

// DailyTrend returns the driver of qoder.oapi.aicode_daily_trend.
//
// Contract (Qoder Teams OpenAPI, "Get AI code daily trend"): GET
// /v1/organizations/{organization_id}/ai-code/stats/daily-trend with
// start_date and end_date (RFC 3339; at most 90 days); response {items:
// [{date, totalLinesChanged, aiLinesChanged, otherLinesChanged, aiLinesAdded
// (deprecated), otherLinesAdded, aiShareRate, commitCount}], extItems: [...
// per fileExtension], nextItems: [{date, nextSuggestedCount,
// nextAcceptedCount, nextAcceptRate}]}. Not paged. Lands per day: committed
// lines changed (total, AI, other), other lines added and commits from items,
// and Tab completion suggestions and acceptances from nextItems. extItems (a
// per-extension split of the same lines) is not landed. That each date is a
// UTC calendar day is Inferred (the example shows midnight Z).
func DailyTrend() vendorapi.Driver {
	return vendorapi.Driver{
		Surface:         SurfaceDailyTrend,
		Build:           buildDailyTrend,
		ParseResponse:   parseDailyTrend,
		Auth:            bearerAuth,
		ContentTypes:    []string{"application/json"},
		Supplies:        []vs.DataClass{vs.ClassLOC, vs.ClassCommits, vs.ClassAdoption},
		ContractVersion: DailyTrendContract,
		Tool:            Tool,
		MaxWindow:       MaxWindow,
		MaxLookback:     MaxLookback,
		KnownPaths: []string{
			"extItems", "extItems[]", "extItems[].aiLinesAdded", "extItems[].aiLinesChanged", "extItems[].date",
			"extItems[].fileExtension", "extItems[].totalLinesAdded", "extItems[].totalLinesChanged", "items",
			"items[]", "items[].aiLinesAdded", "items[].aiLinesChanged", "items[].aiShareRate", "items[].commitCount",
			"items[].date", "items[].otherLinesAdded", "items[].otherLinesChanged", "items[].totalLinesChanged",
			"nextItems", "nextItems[]", "nextItems[].date", "nextItems[].nextAcceptRate",
			"nextItems[].nextAcceptedCount", "nextItems[].nextSuggestedCount",
		},
	}
}

func buildDailyTrend(p vendorapi.Params, w vendorapi.Window, cursor string) (vendorapi.Request, error) {
	s := SurfaceDailyTrend
	if cursor != "" {
		return vendorapi.Request{}, fmt.Errorf("%s: the surface is not paged", s)
	}
	o := p[ParamOrganization]
	if !orgRE.MatchString(o) {
		return vendorapi.Request{}, fmt.Errorf("%s: connection parameter %s is missing or not an organization id", s, ParamOrganization)
	}
	if w.From.IsZero() || !w.To.After(w.From) {
		return vendorapi.Request{}, fmt.Errorf("%s: a non-empty window is required", s)
	}
	if w.To.Sub(w.From) > MaxWindow {
		return vendorapi.Request{}, fmt.Errorf("%s: window longer than %s", s, MaxWindow)
	}
	// Re-read the UTC day before the window so a late day is restated.
	start := w.From.UTC().Truncate(day).Add(-day)
	q := map[string][]string{
		"start_date": {start.Format(time.RFC3339)},
		"end_date":   {w.To.UTC().Format(time.RFC3339)},
	}
	return vendorapi.Request{Method: "GET", Path: "/v1/organizations/" + o + "/ai-code/stats/daily-trend", Query: q}, nil
}

// trendRow is one chart row: its date and the counts the tables name.
type trendRow map[string]json.RawMessage

func parseDailyTrend(r vendorapi.Response) (vendorapi.Page, error) {
	drift := driftOf(SurfaceDailyTrend)
	if r.Status != 200 {
		return vendorapi.Page{}, drift("status %d is not the documented 200", r.Status)
	}
	var doc struct {
		Items     *[]trendRow `json:"items"`
		NextItems *[]trendRow `json:"nextItems"`
	}
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return vendorapi.Page{}, drift("body does not decode as the documented response")
	}
	if doc.Items == nil || doc.NextItems == nil {
		return vendorapi.Page{}, drift("required field items or nextItems is missing")
	}
	// A source record is one UTC day: both charts of a day are one record.
	days := map[int64]bool{}
	page := vendorapi.Page{}
	for _, chart := range []struct {
		name    string
		rows    []trendRow
		metrics []trendMetric
	}{{"items", *doc.Items, itemMetrics}, {"nextItems", *doc.NextItems, nextMetrics}} {
		seen := map[int64]bool{}
		for i, row := range chart.rows {
			var date string
			if err := json.Unmarshal(row["date"], &date); err != nil || date == "" {
				return vendorapi.Page{}, drift("%s[%d].date is missing or not a string", chart.name, i)
			}
			t, err := time.Parse(time.RFC3339, date)
			if err != nil || !t.Equal(t.UTC().Truncate(day)) {
				return vendorapi.Page{}, drift("%s[%d].date is not a UTC midnight", chart.name, i)
			}
			d := t.UTC()
			if seen[d.Unix()] {
				return vendorapi.Page{}, drift("%s[%d] repeats a day", chart.name, i)
			}
			seen[d.Unix()] = true
			days[d.Unix()] = true
			for _, m := range chart.metrics {
				var n count
				raw, ok := row[m.field]
				if !ok || string(raw) == "null" {
					return vendorapi.Page{}, drift("%s[%d] misses %s", chart.name, i, m.field)
				}
				if err := json.Unmarshal(raw, &n); err != nil {
					return vendorapi.Page{}, drift("%s[%d].%s is not a JSON number", chart.name, i, m.field)
				}
				v, err := strconv.ParseInt(string(n), 10, 64)
				if err != nil || v < 0 {
					return vendorapi.Page{}, drift("%s[%d].%s is not a non-negative integer", chart.name, i, m.field)
				}
				micros, ok := vendorapi.CountMicros(v)
				if !ok {
					return vendorapi.Page{}, drift("%s[%d].%s is out of range", chart.name, i, m.field)
				}
				page.Facts = append(page.Facts, vendorapi.FactRow{
					IntervalStart: d, IntervalEnd: d.Add(day), SourceTZ: "UTC", CoverageDomain: Domain,
					Metric: m.metric, Unit: m.unit, ValueMicros: micros,
				})
			}
		}
	}
	page.SourceRecords = len(days)
	return page, nil
}
