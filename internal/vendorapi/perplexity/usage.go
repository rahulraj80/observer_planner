package perplexity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// Catalog ids of the per-member analytics surfaces: one row, one driver, one
// dataset each (lane ENTAPI-L16D split query_volume off the credit row).
const (
	SurfaceMemberUsage       vs.SurfaceID = "perplexity.ent.analytics_v2_member"
	SurfaceMemberQueryVolume vs.SurfaceID = "perplexity.ent.analytics_v2_member_query_volume"
)

// Per-member usage vocabulary.
const (
	MemberUsageContract       = "perplexity.ent.analytics_v2_member/v1 credit_usage openapi (2026-10-08)"
	MemberQueryVolumeContract = "perplexity.ent.analytics_v2_member_query_volume/v1 query_volume openapi (2026-10-08)"

	// Tool is the registry tool the organization analytics report on.
	Tool = "perplexity-web"
	// PageSize is the documented maximum of limit.
	PageSize = 100
	// MaxWindow caps one run (the reference allows 90 days; a month keeps one
	// run small).
	MaxWindow = 31 * day
	// MaxLookback is the documented window maximum.
	MaxLookback = 90 * day
	day         = 24 * time.Hour

	// MetricCredits is the member's credits for the day; per model the same
	// metric carries ModelRaw. The reference says the total may EXCEED the
	// sum of a breakdown (usage without a category attribution), so the two
	// are never added.
	MetricCredits = "credits"
	// UnitCredits is Perplexity's credit, never money.
	UnitCredits = "credits"
	// MetricQueries is the member's query count for the day; per feature the
	// same metric carries Feature. Like the credits, a breakdown never adds
	// to its total.
	MetricQueries = "queries"
	// UnitCount is a plain count.
	UnitCount = "count"
	// Domain is the coverage domain of the organization report.
	Domain = "perplexity.organization"
)

var (
	bearerAuth = vendorapi.AuthContract{Kind: vs.AuthBearer, Scheme: vendorapi.SchemeBearer, Header: "Authorization"}
	pageRE     = regexp.MustCompile(`^[A-Za-z0-9_=+/.:-]{1,1000}$`)
)

// dataset is one row of the per-member dataset table: the dataset query
// value, the landed metric and unit, and the ONE documented breakdown label
// that lands (as ModelRaw or as Feature). Every other breakdown label is read
// past, never landed.
type dataset struct {
	surface   vs.SurfaceID
	name      string
	contract  string
	metric    string
	unit      string
	breakdown string
	asModel   bool
	supplies  []vs.DataClass
	known     []string
}

var (
	// creditUsage: the Model breakdown lands per model; Credit Source
	// (paid, promo) is not landed.
	creditUsage = dataset{
		surface: SurfaceMemberUsage, name: "credit_usage", contract: MemberUsageContract,
		metric: MetricCredits, unit: UnitCredits, breakdown: "Model", asModel: true,
		supplies: []vs.DataClass{vs.ClassUsage, vs.ClassCost},
		known: []string{
			"categories", "categories[]", "data", "data[]", "data[].end_time", "data[].results", "data[].results[]",
			"data[].results[].by_categories", "data[].results[].by_categories.Credit Source",
			"data[].results[].by_categories.Credit Source[]", "data[].results[].by_categories.Credit Source[].category",
			"data[].results[].by_categories.Credit Source[].count", "data[].results[].by_categories.Model",
			"data[].results[].by_categories.Model[]", "data[].results[].by_categories.Model[].category",
			"data[].results[].by_categories.Model[].count", "data[].results[].count", "data[].results[].user_email",
			"data[].start_time", "has_more", "next_page",
		},
	}
	// queryVolume (lane ENTAPI-L16D): the Feature breakdown lands per
	// feature. Project (admin-named Spaces, free text) and Comet are not
	// landed, and the dataset carries no Model breakdown at this grain.
	queryVolume = dataset{
		surface: SurfaceMemberQueryVolume, name: "query_volume", contract: MemberQueryVolumeContract,
		metric: MetricQueries, unit: UnitCount, breakdown: "Feature",
		supplies: []vs.DataClass{vs.ClassUsage, vs.ClassAdoption},
		known: []string{
			"categories", "categories[]", "data", "data[]", "data[].end_time", "data[].results", "data[].results[]",
			"data[].results[].by_categories", "data[].results[].by_categories.Comet",
			"data[].results[].by_categories.Comet[]", "data[].results[].by_categories.Comet[].category",
			"data[].results[].by_categories.Comet[].count", "data[].results[].by_categories.Feature",
			"data[].results[].by_categories.Feature[]", "data[].results[].by_categories.Feature[].category",
			"data[].results[].by_categories.Feature[].count", "data[].results[].by_categories.Project",
			"data[].results[].by_categories.Project[]", "data[].results[].by_categories.Project[].category",
			"data[].results[].by_categories.Project[].count", "data[].results[].count", "data[].results[].user_email",
			"data[].start_time", "has_more", "next_page",
		},
	}
)

func (d dataset) driver() vendorapi.Driver {
	return vendorapi.Driver{
		Surface:         d.surface,
		Build:           d.build,
		ParseResponse:   d.parse,
		Auth:            bearerAuth,
		ContentTypes:    []string{"application/json"},
		Supplies:        d.supplies,
		ContractVersion: d.contract,
		Tool:            Tool,
		MaxWindow:       MaxWindow,
		MaxLookback:     MaxLookback,
		KnownPaths:      d.known,
	}
}

// MemberUsage returns the driver of perplexity.ent.analytics_v2_member for
// the credit_usage dataset.
//
// Contract (OpenAPI getComputerUsageAnalyticsV2): GET
// /v2/analytics/computer/usage with dataset=credit_usage, group_by=user_email,
// start_time and end_time (Unix seconds, UTC; start snapped to the day grid,
// at most 90 days), limit (max 100) and page (the previous next_page);
// response {categories, data: [{start_time, end_time, results: [{user_email,
// count, by_categories: {Model: [{category, count}], "Credit Source": [...]}}]}],
// has_more, next_page}. Only complete UTC days are returned. Each member-day
// lands its credits total and one fact per model; the Credit Source breakdown
// (paid, promo) is not landed.
func MemberUsage() vendorapi.Driver { return creditUsage.driver() }

// MemberQueryVolume returns the driver of
// perplexity.ent.analytics_v2_member_query_volume (lane ENTAPI-L16D, T-713):
// the same endpoint with dataset=query_volume, whose rows carry the Feature,
// Project and Comet breakdowns. Each member-day lands its query count and one
// fact per feature.
func MemberQueryVolume() vendorapi.Driver { return queryVolume.driver() }

func buildMemberUsage(p vendorapi.Params, w vendorapi.Window, cursor string) (vendorapi.Request, error) {
	return creditUsage.build(p, w, cursor)
}

func parseMemberUsage(r vendorapi.Response) (vendorapi.Page, error) { return creditUsage.parse(r) }

func (d dataset) build(_ vendorapi.Params, w vendorapi.Window, cursor string) (vendorapi.Request, error) {
	s := d.surface
	if w.From.IsZero() || !w.To.After(w.From) {
		return vendorapi.Request{}, fmt.Errorf("%s: a non-empty window is required", s)
	}
	if w.To.Sub(w.From) > MaxWindow {
		return vendorapi.Request{}, fmt.Errorf("%s: window longer than %s", s, MaxWindow)
	}
	// Re-read the UTC day before the window: only complete days are served,
	// so a day absent while the store synced is picked up on the next run.
	start := w.From.UTC().Truncate(day).Add(-day)
	q := map[string][]string{
		"dataset":    {d.name},
		"group_by":   {"user_email"},
		"start_time": {strconv.FormatInt(start.Unix(), 10)},
		"end_time":   {strconv.FormatInt(w.To.UTC().Unix(), 10)},
		"limit":      {strconv.Itoa(PageSize)},
	}
	if cursor != "" {
		if !pageRE.MatchString(cursor) {
			return vendorapi.Request{}, fmt.Errorf("%s: cursor is not an opaque page token", s)
		}
		q["page"] = []string{cursor}
	}
	return vendorapi.Request{Method: "GET", Path: "/v2/analytics/computer/usage", Query: q}, nil
}

// number is a JSON number kept as its literal text; a quoted value is a type
// change and fails the page (plan 12A.5).
type number string

// UnmarshalJSON accepts a bare JSON number only.
func (n *number) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || (b[0] != '-' && (b[0] < '0' || b[0] > '9')) {
		return fmt.Errorf("perplexity: %.16q is not a JSON number", b)
	}
	*n = number(b)
	return nil
}

type category struct {
	Category *string `json:"category"`
	Count    *number `json:"count"`
}

type usageDoc struct {
	Categories *[]string `json:"categories"`
	Data       *[]struct {
		Start   *number `json:"start_time"`
		End     *number `json:"end_time"`
		Results *[]struct {
			Email        *string               `json:"user_email"`
			Count        *number               `json:"count"`
			ByCategories map[string][]category `json:"by_categories"`
		} `json:"results"`
	} `json:"data"`
	HasMore  *bool   `json:"has_more"`
	NextPage *string `json:"next_page"`
}

func (d dataset) drift(format string, a ...any) error {
	return fmt.Errorf("%w: %s: "+format, append([]any{vendorapi.ErrSchemaDrift, d.surface}, a...)...)
}

func unixDay(n *number) (time.Time, bool) {
	if n == nil {
		return time.Time{}, false
	}
	v, err := strconv.ParseInt(string(*n), 10, 64)
	if err != nil || v < 0 || v > 253402300799 {
		return time.Time{}, false
	}
	return time.Unix(v, 0).UTC(), true
}

func amount(n *number) (int64, bool) {
	if n == nil {
		return 0, false
	}
	v, ok := vendorapi.DecimalMicros(string(*n), 0)
	return v, ok && v >= 0
}

func (d dataset) parse(r vendorapi.Response) (vendorapi.Page, error) {
	if r.Status != 200 {
		return vendorapi.Page{}, d.drift("status %d is not the documented 200", r.Status)
	}
	var doc usageDoc
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return vendorapi.Page{}, d.drift("body does not decode as the documented response")
	}
	if doc.Categories == nil || doc.Data == nil || doc.HasMore == nil {
		return vendorapi.Page{}, d.drift("required field categories, data or has_more is missing")
	}
	page := vendorapi.Page{}
	if *doc.HasMore {
		np := ""
		if doc.NextPage != nil {
			np = *doc.NextPage
		}
		if !pageRE.MatchString(np) {
			return vendorapi.Page{}, d.drift("has_more is true without a usable next_page")
		}
		if r.Cursor != "" && np == r.Cursor {
			return vendorapi.Page{}, d.drift("next_page repeats the cursor the page was asked with")
		}
		page.NextCursor = np
	}
	seen := map[string]bool{}
	for i, b := range *doc.Data {
		start, ok1 := unixDay(b.Start)
		end, ok2 := unixDay(b.End)
		if !ok1 || !ok2 || !end.After(start) || end.Sub(start) > day {
			return vendorapi.Page{}, d.drift("data[%d] is not a period of at most one day in Unix seconds", i)
		}
		if b.Results == nil {
			return vendorapi.Page{}, d.drift("data[%d].results is missing", i)
		}
		for j, m := range *b.Results {
			email := ""
			if m.Email != nil {
				email = strings.TrimSpace(*m.Email)
			}
			if email == "" {
				return vendorapi.Page{}, d.drift("data[%d].results[%d].user_email is missing", i, j)
			}
			key := strconv.FormatInt(start.Unix(), 10) + "|" + email
			if seen[key] {
				return vendorapi.Page{}, d.drift("data[%d].results[%d] repeats a member and day", i, j)
			}
			seen[key] = true
			total, ok := amount(m.Count)
			if !ok {
				return vendorapi.Page{}, d.drift("data[%d].results[%d].count is not a non-negative number", i, j)
			}
			fact := func(dim string, v int64) vendorapi.FactRow {
				f := vendorapi.FactRow{
					IntervalStart: start, IntervalEnd: end, SourceTZ: "UTC", CoverageDomain: Domain,
					IdentityKind: "email", Identity: email, Metric: d.metric, Unit: d.unit, ValueMicros: v,
				}
				if d.asModel {
					f.ModelRaw = dim
				} else {
					f.Feature = dim
				}
				return f
			}
			page.Facts = append(page.Facts, fact("", total))
			names := map[string]bool{}
			for k, c := range m.ByCategories[d.breakdown] {
				name := ""
				if c.Category != nil {
					name = strings.TrimSpace(*c.Category)
				}
				v, ok := amount(c.Count)
				if name == "" || !ok {
					return vendorapi.Page{}, d.drift("data[%d].results[%d].by_categories.%s[%d] is not a category and a count",
						i, j, d.breakdown, k)
				}
				if names[name] {
					return vendorapi.Page{}, d.drift("data[%d].results[%d] repeats %s %d", i, j, d.breakdown, k)
				}
				names[name] = true
				page.Facts = append(page.Facts, fact(name, v))
			}
		}
	}
	// The vendor's record container is the daily bucket (data); a bucket
	// becomes several member facts.
	page.SourceRecords = len(*doc.Data)
	return page, nil
}
