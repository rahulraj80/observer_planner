package github

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// Copilot usage metrics report surfaces (lane L8, T-611). Each is a two-step
// report: GET /enterprises/{enterprise}/copilot/metrics/reports/<report>
// answers {"download_links": [...], "report_day"} (or report_start_day and
// report_end_day for a 28-day report); each signed link serves an NDJSON
// file from copilot-reports.github.com (copilot-reports.SUBDOMAIN.ghe.com
// on GHE.com) that the runner downloads with NO credential. Sources:
// rest-api-description ghec.2026-03-10 (the first hop), the Copilot usage
// metrics example-schema page (the file rows) and the 2026-05-20 changelog
// (the download host).
const (
	SurfaceMetricsEnterprise1d  vs.SurfaceID = "github.copilot.metrics.enterprise_1d"
	SurfaceMetricsEnterprise28d vs.SurfaceID = "github.copilot.metrics.enterprise_28d"
	SurfaceMetricsUsers1d       vs.SurfaceID = "github.copilot.metrics.ent_users_1d"
	SurfaceMetricsUsers28d      vs.SurfaceID = "github.copilot.metrics.ent_users_28d"
	SurfaceMetricsRepos1d       vs.SurfaceID = "github.copilot.metrics.ent_repos_1d"

	// Tool is the registry tool the Copilot activity surfaces report on.
	Tool = "copilot"

	// LegacyEngagementKey is the ownership key of the legacy
	// copilotanalytics engagement surface, which reads the same reports.
	LegacyEngagementKey = "copilotanalytics.engagement"

	// ReportLag is how far behind today the newest 1-day report a run asks
	// for is. GitHub restates a day for about two days (the legacy poller's
	// documented assumption); the value is Inferred, not documented.
	ReportLag = 2 * 24 * time.Hour
	// ReportMaxWindow caps one run to fourteen report days (fourteen first
	// hops); a longer backfill runs as consecutive runs.
	ReportMaxWindow = 14 * 24 * time.Hour
	// reportMaxLinks caps the links one first-hop answer may name.
	reportMaxLinks = 64

	// MetricsContract is recorded on every run of a metrics report.
	MetricsContract = "github.copilot.metrics/v1 api=" + APIVersion + " schema=example-schema 2026-10-07"
)

// enterpriseRE is a GitHub enterprise slug: alphanumerics and hyphens.
var enterpriseRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,98}[A-Za-z0-9])?$`)

// lineShape names how one NDJSON line of a report reads.
type lineShape int

const (
	shapeAggregate lineShape = iota // one line per day, enterprise totals
	shapeDayTotals                  // one line holding day_totals[] (28-day enterprise)
	shapeUser                       // one line per user and day
	shapeRepo                       // one line per repository and day
)

// reportSpec is one row of the report table.
type reportSpec struct {
	surface  vs.SurfaceID
	report   string
	latest   bool
	shape    lineShape
	supplies []vs.DataClass
}

// reports is the closed table of built metrics reports.
var reports = []reportSpec{
	{
		SurfaceMetricsEnterprise1d, "enterprise-1-day", false, shapeAggregate,
		[]vs.DataClass{vs.ClassUsage, vs.ClassAdoption, vs.ClassLOC, vs.ClassCommits},
	},
	{
		SurfaceMetricsEnterprise28d, "enterprise-28-day", true, shapeDayTotals,
		[]vs.DataClass{vs.ClassUsage, vs.ClassAdoption},
	},
	{
		SurfaceMetricsUsers1d, "users-1-day", false, shapeUser,
		[]vs.DataClass{vs.ClassUsage, vs.ClassAdoption, vs.ClassLOC},
	},
	{
		SurfaceMetricsUsers28d, "users-28-day", true, shapeUser,
		[]vs.DataClass{vs.ClassUsage, vs.ClassAdoption},
	},
	{
		SurfaceMetricsRepos1d, "repos-1-day", false, shapeRepo,
		[]vs.DataClass{vs.ClassCommits},
	},
}

// MetricsReports returns the driver constructors of the built reports, by
// surface.
func MetricsReports() map[vs.SurfaceID]func() vendorapi.Driver {
	out := map[vs.SurfaceID]func() vendorapi.Driver{}
	for _, r := range reports {
		r := r
		out[r.surface] = func() vendorapi.Driver { return metricsDriver(r) }
	}
	return out
}

func metricsDriver(r reportSpec) vendorapi.Driver {
	d := vendorapi.Driver{
		Surface: r.surface,
		Build: func(p vendorapi.Params, w vendorapi.Window, c string) (vendorapi.Request, error) {
			return buildReport(r, p, w, c)
		},
		ParseResponse: func(resp vendorapi.Response) (vendorapi.Page, error) { return parseReportFile(r, resp) },
		Canary:        canaryReport,
		Auth:          vendorapi.AuthContract{Kind: vs.AuthBearer, Scheme: vendorapi.SchemeBearer, Header: "Authorization"},
		Headers: map[string]string{
			"Accept":               "application/vnd.github+json",
			"X-GitHub-Api-Version": APIVersion,
		},
		ContentTypes:    []string{"application/json"},
		Supplies:        r.supplies,
		LegacyPoller:    LegacyEngagementKey,
		ContractVersion: MetricsContract,
		Tool:            Tool,
		KnownPaths:      knownPaths[r.surface],
		Report: &vendorapi.ReportContract{
			Links:        reportLinks,
			ContentTypes: []string{"application/x-ndjson", "application/json", "application/octet-stream", "text/plain"},
			MaxLinks:     reportMaxLinks,
		},
	}
	if !r.latest {
		d.MaxWindow = ReportMaxWindow
		d.Report.Next = nextReportDay
	}
	return d
}

// reportDays is the day range [first, last] a 1-day report run asks for:
// from the window start less the lag to the window end less the lag
// (exclusive), never empty. Consecutive runs overlap by one day at most, so
// a day the vendor restates is read again and its provisional facts
// replaced, and no day falls between two runs.
func reportDays(w vendorapi.Window) (time.Time, time.Time) {
	day := 24 * time.Hour
	start := w.From.UTC().Truncate(day).Add(-ReportLag)
	end := w.To.UTC().Truncate(day).Add(-ReportLag)
	if !end.After(start) {
		end = start.Add(day)
	}
	return start, end.Add(-day)
}

func nextReportDay(_ vendorapi.Params, w vendorapi.Window, cursor string) string {
	first, last := reportDays(w)
	cur := first
	if cursor != "" {
		t, err := time.Parse(time.DateOnly, cursor)
		if err != nil {
			return ""
		}
		cur = t
	}
	next := cur.Add(24 * time.Hour)
	if next.After(last) {
		return ""
	}
	return next.Format(time.DateOnly)
}

func buildReport(r reportSpec, p vendorapi.Params, w vendorapi.Window, cursor string) (vendorapi.Request, error) {
	ent := p["enterprise"]
	if !enterpriseRE.MatchString(ent) {
		return vendorapi.Request{}, fmt.Errorf("%s: param enterprise must be an enterprise slug", r.surface)
	}
	path := "/enterprises/" + ent + "/copilot/metrics/reports/" + r.report
	if r.latest {
		if cursor != "" {
			return vendorapi.Request{}, fmt.Errorf("%s: the latest report is one request", r.surface)
		}
		return vendorapi.Request{Method: "GET", Path: path + "/latest"}, nil
	}
	if w.From.IsZero() || !w.To.After(w.From) {
		return vendorapi.Request{}, fmt.Errorf("%s: a non-empty window is required", r.surface)
	}
	first, last := reportDays(w)
	day := first
	if cursor != "" {
		t, err := time.Parse(time.DateOnly, cursor)
		if err != nil || t.Format(time.DateOnly) != cursor || t.Before(first) || t.After(last) {
			return vendorapi.Request{}, fmt.Errorf("%s: cursor must be a report day inside the window", r.surface)
		}
		day = t
	}
	return vendorapi.Request{Method: "GET", Path: path, Query: map[string][]string{"day": {day.Format(time.DateOnly)}}}, nil
}

func reportDrift(format string, a ...any) error {
	return fmt.Errorf("%w: github.copilot.metrics: "+format, append([]any{vendorapi.ErrSchemaDrift}, a...)...)
}

// reportLinks reads the first hop: download_links is a required array of
// strings. The links are returned to the runner only.
func reportLinks(r vendorapi.Response) ([]string, error) {
	if r.Status != 200 {
		return nil, reportDrift("status %d is not the documented 200", r.Status)
	}
	var doc struct {
		DownloadLinks *[]json.RawMessage `json:"download_links"`
	}
	if err := json.Unmarshal(r.Body, &doc); err != nil {
		return nil, reportDrift("the report answer does not decode as the documented object")
	}
	if doc.DownloadLinks == nil {
		return nil, reportDrift("required field download_links is missing")
	}
	out := make([]string, 0, len(*doc.DownloadLinks))
	for i, raw := range *doc.DownloadLinks {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || s == "" {
			return nil, reportDrift("download_links[%d] is not a link", i)
		}
		out = append(out, s)
	}
	return out, nil
}

// metricField is one row of a closed metric table: a dotted path inside a
// line object, the fact's metric name and unit.
type metricField struct {
	path, metric, unit string
}

func countFields(unit string, names ...string) []metricField {
	out := make([]metricField, 0, len(names))
	for _, n := range names {
		out = append(out, metricField{n, n, unit})
	}
	return out
}

// activityFields are the per-subject activity counters every usage line
// carries (aggregate and per user).
var activityFields = append(append(
	countFields("count", "user_initiated_interaction_count", "code_generation_activity_count", "code_acceptance_activity_count"),
	countFields("lines", "loc_suggested_to_add_sum", "loc_suggested_to_delete_sum", "loc_added_sum", "loc_deleted_sum")...),
	countFields("count", "distinct_mcp_use_count", "distinct_skill_use_count", "distinct_custom_agent_use_count",
		"distinct_plugin_use_count", "distinct_slash_cmd_use_count")...)

// activeUserFields are the enterprise user gauges (aggregate lines only).
var activeUserFields = countFields("users",
	"daily_active_users", "weekly_active_users", "monthly_active_users", "monthly_active_chat_users",
	"monthly_active_agent_users", "daily_active_cli_users", "daily_active_copilot_app_users",
	"daily_active_copilot_cloud_agent_users", "daily_active_copilot_code_review_users", "daily_active_vscode_agent_users",
	"daily_passive_copilot_code_review_users", "monthly_active_copilot_cloud_agent_users",
	"monthly_active_copilot_code_review_users", "monthly_active_vscode_agent_users",
	"monthly_passive_copilot_code_review_users")

// pullRequestFields are the integer pull request counters.
var pullRequestFields = func() []metricField {
	var out []metricField
	for _, n := range []string{
		"total_created", "total_reviewed", "total_merged", "total_created_by_copilot", "total_reviewed_by_copilot",
		"total_merged_created_by_copilot", "total_merged_reviewed_by_copilot", "total_suggestions",
		"total_applied_suggestions", "total_copilot_suggestions", "total_copilot_applied_suggestions",
	} {
		out = append(out, metricField{"pull_requests." + n, "pull_requests." + n, "count"})
	}
	return out
}()

// segmentFields are the per-client totals (totals_by_cli,
// totals_by_copilot_app), landed with the client as the traffic segment.
var segmentFields = []metricField{
	{"prompt_count", "prompt_count", "count"},
	{"request_count", "request_count", "count"},
	{"session_count", "session_count", "count"},
	{"token_usage.prompt_tokens_sum", "prompt_tokens_sum", "tokens"},
	{"token_usage.output_tokens_sum", "output_tokens_sum", "tokens"},
}

// segments maps a totals object to its traffic segment.
var segments = []struct{ key, segment string }{
	{"totals_by_cli", "cli"}, {"totals_by_copilot_app", "copilot_app"},
}

// featureFields are the per-feature counters of totals_by_feature[].
var featureFields = append(
	countFields("count", "user_initiated_interaction_count", "code_generation_activity_count", "code_acceptance_activity_count"),
	countFields("lines", "loc_suggested_to_add_sum", "loc_suggested_to_delete_sum", "loc_added_sum", "loc_deleted_sum")...)

// featureRE is a feature name the vendor's enum uses (code_completion,
// chat_panel_agent_mode, ...); anything else lands as "other".
var featureRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// maxMicrosInput bounds a value before it is scaled to micro-units.
const maxMicrosInput = 9e12

// toMicros scales a non-negative JSON number to integer micro-units.
func toMicros(n json.Number) (int64, bool) {
	if i, err := n.Int64(); err == nil {
		if i < 0 || float64(i) > maxMicrosInput {
			return 0, false
		}
		return i * 1_000_000, true
	}
	f, err := strconv.ParseFloat(n.String(), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > maxMicrosInput {
		return 0, false
	}
	return int64(math.Round(f * 1_000_000)), true
}

// lookup walks a dotted path inside a decoded object.
func lookup(obj map[string]any, path string) (any, bool) {
	var cur any = obj
	for _, k := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// factSink accumulates the facts of one file and refuses a repeated key.
// The one exception is the "other" feature bucket: every feature name outside
// featureRE folds into it, so two such names on one line (an enum addition
// with a hyphen or an upper-case letter) share a key by construction. Those
// are SUMMED, as billing.go sums its folded rows; refusing them failed the
// whole file as drift on every run, so those days never landed (SEC-SWEEP,
// ledger VCONN-12, the VCONN-5 class).
type factSink struct {
	facts []vendorapi.FactRow
	seen  map[string]int // key -> index into facts
}

func (s *factSink) add(f vendorapi.FactRow) error {
	k := strings.Join([]string{
		f.IntervalStart.Format(time.RFC3339), f.IdentityKind, f.Identity, f.RepoHash,
		f.Feature, f.TrafficSegment, f.Metric, f.Unit,
	}, "\x00")
	if i, dup := s.seen[k]; dup {
		if f.Feature != "other" {
			return reportDrift("a report repeats one subject, day and metric")
		}
		if s.facts[i].ValueMicros > math.MaxInt64-f.ValueMicros {
			return reportDrift("an other-feature total is out of range")
		}
		s.facts[i].ValueMicros += f.ValueMicros
		return nil
	}
	s.seen[k] = len(s.facts)
	s.facts = append(s.facts, f)
	return nil
}

// emit lands every field of a table found in obj. A field that is absent is
// skipped (optional); a field of the wrong JSON type or out of range fails
// the page.
func (s *factSink) emit(obj map[string]any, fields []metricField, base vendorapi.FactRow, where string) error {
	for _, fd := range fields {
		v, ok := lookup(obj, fd.path)
		if !ok || v == nil {
			continue
		}
		n, isNum := v.(json.Number)
		if !isNum {
			return reportDrift("%s%s is not a number", where, fd.path)
		}
		m, ok := toMicros(n)
		if !ok {
			return reportDrift("%s%s is negative or out of range", where, fd.path)
		}
		f := base
		f.Metric, f.Unit, f.ValueMicros = fd.metric, fd.unit, m
		if err := s.add(f); err != nil {
			return err
		}
	}
	return nil
}

func dayOf(obj map[string]any, where string) (time.Time, error) {
	raw, ok := obj["day"].(string)
	if !ok {
		return time.Time{}, reportDrift("%sday is missing or not a string", where)
	}
	t, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return time.Time{}, reportDrift("%sday is not a date", where)
	}
	return t.UTC(), nil
}

// enterpriseBase is the base fact of an enterprise-level line: its day and,
// as the traffic segment, the enterprise the line is for
// ("enterprise:<enterprise_id>", per-client segments append ":cli" and so
// on). A connection names one enterprise, so in practice one id appears; the
// segment keeps two lines for one day apart when a file holds more than one
// (the official example does) instead of folding them.
func enterpriseBase(obj map[string]any, where string, base func(time.Time) vendorapi.FactRow) (vendorapi.FactRow, error) {
	day, err := dayOf(obj, where)
	if err != nil {
		return vendorapi.FactRow{}, err
	}
	id, ok := scalarID(obj["enterprise_id"])
	if !ok || !idRE.MatchString(id) {
		return vendorapi.FactRow{}, reportDrift("%senterprise_id is missing or not an id", where)
	}
	b := base(day)
	b.TrafficSegment = "enterprise:" + id
	return b, nil
}

// idRE is a numeric vendor id rendered as text.
var idRE = regexp.MustCompile(`^[0-9]{1,20}$`)

// repoHash keys a repository fact by a digest of its numeric id: the
// repository name and owner never land.
func repoHash(id json.Number) string {
	sum := sha256.Sum256([]byte("github.repo:" + id.String()))
	return hex.EncodeToString(sum[:16])
}

// scalarID renders a JSON string or number id.
func scalarID(v any) (string, bool) {
	switch x := v.(type) {
	case json.Number:
		if _, err := x.Int64(); err != nil {
			return "", false
		}
		return x.String(), true
	case string:
		return x, x != ""
	}
	return "", false
}

// usageLine lands one usage line (an aggregate day, a day_totals entry, or
// a user's day).
func (s *factSink) usageLine(obj map[string]any, base vendorapi.FactRow, aggregate bool, where string) error {
	if aggregate {
		if err := s.emit(obj, activeUserFields, base, where); err != nil {
			return err
		}
		if err := s.emit(obj, pullRequestFields, base, where); err != nil {
			return err
		}
	} else {
		if err := s.emit(obj, []metricField{{"ai_credits_used", "ai_credits_used", "credits"}}, base, where); err != nil {
			return err
		}
	}
	if err := s.emit(obj, activityFields, base, where); err != nil {
		return err
	}
	for _, sg := range segments {
		raw, ok := obj[sg.key]
		if !ok || raw == nil {
			continue
		}
		seg, ok := raw.(map[string]any)
		if !ok {
			return reportDrift("%s%s is not an object", where, sg.key)
		}
		b := base
		b.TrafficSegment = sg.segment
		if base.TrafficSegment != "" {
			b.TrafficSegment = base.TrafficSegment + ":" + sg.segment
		}
		if err := s.emit(seg, segmentFields, b, where+sg.key+"."); err != nil {
			return err
		}
	}
	raw, ok := obj["totals_by_feature"]
	if !ok || raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return reportDrift("%stotals_by_feature is not an array", where)
	}
	for i, it := range list {
		fo, ok := it.(map[string]any)
		if !ok {
			return reportDrift("%stotals_by_feature[%d] is not an object", where, i)
		}
		name, _ := fo["feature"].(string)
		if !featureRE.MatchString(name) {
			name = "other"
		}
		b := base
		b.Feature = name
		if err := s.emit(fo, featureFields, b, fmt.Sprintf("%stotals_by_feature[%d].", where, i)); err != nil {
			return err
		}
	}
	return nil
}

// parseReportFile parses one downloaded NDJSON file into aggregate facts
// (plan 5.2 G4): one fact per (day, subject, dimension, metric), integer
// micro-units of its unit, provisional (a later restatement of the day
// replaces it). Unknown fields are ignored; a missing day, user id or
// repository id, a wrong JSON type and a repeated subject fail the page.
func parseReportFile(r reportSpec, resp vendorapi.Response) (vendorapi.Page, error) {
	if resp.Status != 200 {
		return vendorapi.Page{}, reportDrift("download status %d is not 200", resp.Status)
	}
	sink := &factSink{seen: map[string]int{}}
	lines := 0
	sc := bufio.NewScanner(bytes.NewReader(resp.Body))
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		lines++
		where := fmt.Sprintf("line %d: ", lines)
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil || obj == nil {
			return vendorapi.Page{}, reportDrift("%snot a JSON object", where)
		}
		if err := sink.line(r, obj, where); err != nil {
			return vendorapi.Page{}, err
		}
	}
	if err := sc.Err(); err != nil {
		return vendorapi.Page{}, reportDrift("the report file does not read as lines")
	}
	return vendorapi.Page{Facts: sink.facts, SourceRecords: lines}, nil
}

func (s *factSink) line(r reportSpec, obj map[string]any, where string) error {
	base := func(day time.Time) vendorapi.FactRow {
		return vendorapi.FactRow{IntervalStart: day, IntervalEnd: day.Add(24 * time.Hour), SourceTZ: "UTC"}
	}
	switch r.shape {
	case shapeDayTotals:
		raw, ok := obj["day_totals"].([]any)
		if !ok {
			return reportDrift("%sday_totals is missing or not an array", where)
		}
		for i, it := range raw {
			entry, ok := it.(map[string]any)
			if !ok {
				return reportDrift("%sday_totals[%d] is not an object", where, i)
			}
			w := fmt.Sprintf("%sday_totals[%d].", where, i)
			b, err := enterpriseBase(entry, w, base)
			if err != nil {
				return err
			}
			if err := s.usageLine(entry, b, true, w); err != nil {
				return err
			}
		}
		return nil
	case shapeAggregate:
		b, err := enterpriseBase(obj, where, base)
		if err != nil {
			return err
		}
		return s.usageLine(obj, b, true, where)
	case shapeUser:
		day, err := dayOf(obj, where)
		if err != nil {
			return err
		}
		id, ok := scalarID(obj["user_id"])
		if !ok || !idRE.MatchString(id) {
			return reportDrift("%suser_id is missing or not an id", where)
		}
		b := base(day)
		b.IdentityKind, b.Identity = "user_id", id
		return s.usageLine(obj, b, false, where)
	case shapeRepo:
		day, err := dayOf(obj, where)
		if err != nil {
			return err
		}
		n, ok := obj["repo_id"].(json.Number)
		if !ok {
			return reportDrift("%srepo_id is missing or not a number", where)
		}
		if _, err := n.Int64(); err != nil {
			return reportDrift("%srepo_id is not an integer", where)
		}
		b := base(day)
		b.RepoHash = repoHash(n)
		return s.emit(obj, pullRequestFields, b, where)
	}
	return reportDrift("unknown report shape")
}

// canaryReport reports drift that does not fail the page: a feature name
// outside the vendor's enum shape (landed as "other").
func canaryReport(p vendorapi.Page) []vendorapi.DriftSignal {
	for _, f := range p.Facts {
		if f.Feature == "other" {
			return []vendorapi.DriftSignal{{Path: "totals_by_feature[].feature", Kind: "unknown_enum"}}
		}
	}
	return nil
}
