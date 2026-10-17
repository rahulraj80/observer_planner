package fixturecorpus

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

// SourceKind is the derivation source of a fixture, most authoritative first
// (plan 12A.1 derivation order).
type SourceKind string

// Source kinds.
const (
	SourceOpenAPISpec          SourceKind = "openapi_spec"
	SourceSDKType              SourceKind = "sdk_type"
	SourceAPIReferenceExample  SourceKind = "api_reference_example"
	SourceChangelog            SourceKind = "changelog"
	SourceThirdPartySource     SourceKind = "third_party_source"
	SourceSynthesizedFromSchma SourceKind = "synthesized_from_schema"
)

var sourceKinds = map[SourceKind]bool{
	SourceOpenAPISpec: true, SourceSDKType: true, SourceAPIReferenceExample: true,
	SourceChangelog: true, SourceThirdPartySource: true, SourceSynthesizedFromSchma: true,
}

// Scenario names what a fixture exercises (plan 12A.1 fixture set).
type Scenario string

// Scenarios.
const (
	ScenarioNormalPage  Scenario = "normal_page"
	ScenarioLastPage    Scenario = "last_page"
	ScenarioEmptyPage   Scenario = "empty_page"
	ScenarioEnumSweep   Scenario = "enum_sweep"
	ScenarioNullsAbsent Scenario = "nulls_and_absent"
	ScenarioMaxValues   Scenario = "max_values"
	ScenarioTimestamps  Scenario = "timestamp_edges"
	ScenarioReportLink  Scenario = "report_link"
	ScenarioReportFile  Scenario = "report_file"
	ScenarioError400    Scenario = "error_400"
	ScenarioError401    Scenario = "error_401"
	ScenarioError403    Scenario = "error_403"
	ScenarioError404    Scenario = "error_404"
	ScenarioError422    Scenario = "error_422"
	ScenarioError429    Scenario = "error_429"
)

var scenarios = map[Scenario]bool{
	ScenarioNormalPage: true, ScenarioLastPage: true, ScenarioEmptyPage: true,
	ScenarioEnumSweep: true, ScenarioNullsAbsent: true, ScenarioMaxValues: true, ScenarioTimestamps: true,
	ScenarioReportLink: true, ScenarioReportFile: true, ScenarioError400: true, ScenarioError401: true,
	ScenarioError403: true, ScenarioError404: true, ScenarioError422: true, ScenarioError429: true,
}

// IsError reports whether a scenario is an error body.
func (s Scenario) IsError() bool { return strings.HasPrefix(string(s), "error_") }

// HTTPExchange describes the request the fixture answers.
type HTTPExchange struct {
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Query  map[string]string `json:"query,omitempty"`
	Status int               `json:"status"`
	// Headers lists response header NAMES with example values from the
	// source (for example Retry-After, Link); never a credential.
	Headers map[string]string `json:"headers,omitempty"`
}

// Sidecar is <name>.source.json.
type Sidecar struct {
	SurfaceID               vendorsurface.SurfaceID                `json:"surface_id"`
	Scenario                Scenario                               `json:"scenario"`
	SourceURL               string                                 `json:"source_url"`
	SourceKind              SourceKind                             `json:"source_kind"`
	CheckedOn               string                                 `json:"checked_on"`
	SpecVersion             string                                 `json:"sdk_or_spec_version_or_commit"`
	SourceSHA256            string                                 `json:"source_sha256"`
	FixtureSHA256           string                                 `json:"fixture_sha256"`
	HTTP                    HTTPExchange                           `json:"http"`
	FieldLedger             map[string]vendorsurface.EvidenceGrade `json:"field_ledger"`
	CapturedFromRealAccount bool                                   `json:"captured_from_real_account"`
	Status                  string                                 `json:"status"`
	Notes                   string                                 `json:"notes,omitempty"`
}

var hexRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// FixtureSHA256 returns the hex SHA-256 of fixture bytes.
func FixtureSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// DecodeSidecar decodes a sidecar strictly (unknown fields rejected).
func DecodeSidecar(b []byte) (Sidecar, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var s Sidecar
	if err := dec.Decode(&s); err != nil {
		return Sidecar{}, fmt.Errorf("fixturecorpus.DecodeSidecar: %w", err)
	}
	return s, nil
}

// SurfaceLookup answers whether a surface id is a catalog row and its build
// state; injected so this package stays free of the catalog's embedded data.
type SurfaceLookup func(vendorsurface.SurfaceID) (vendorsurface.BuildState, bool)

// Validate checks one sidecar against its fixture bytes. ndjson selects
// newline-delimited JSON (each line one record; ledger paths are resolved in
// every record).
func Validate(s Sidecar, fixture []byte, ndjson bool, lookup SurfaceLookup) []error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	build, ok := lookup(s.SurfaceID)
	switch {
	case !ok:
		bad("surface %q is not a catalog row", s.SurfaceID)
	case !vendorsurface.Driverable(build):
		bad("surface %q is %s: fixtures are owed only for driverable surfaces (AwaitingReference and CatalogOnly stay catalog rows)", s.SurfaceID, build)
	}
	if !scenarios[s.Scenario] {
		bad("unknown scenario %q", s.Scenario)
	}
	if u, err := url.Parse(s.SourceURL); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		bad("source_url %q is not a public https URL", s.SourceURL)
	}
	if !sourceKinds[s.SourceKind] {
		bad("unknown source_kind %q", s.SourceKind)
	}
	if !vendorsurface.ValidDate(s.CheckedOn) {
		bad("checked_on %q is not YYYY-MM-DD", s.CheckedOn)
	}
	if strings.TrimSpace(s.SpecVersion) == "" {
		bad("sdk_or_spec_version_or_commit is empty")
	}
	if !hexRE.MatchString(s.SourceSHA256) {
		bad("source_sha256 is not a hex SHA-256")
	}
	if got := FixtureSHA256(fixture); s.FixtureSHA256 != got {
		bad("fixture_sha256 %s does not match the fixture bytes (%s)", s.FixtureSHA256, got)
	}
	if s.CapturedFromRealAccount {
		bad("captured_from_real_account must be false: the corpus is built from public sources only")
	}
	if s.Status != vendorsurface.StatusLabel {
		bad("status must be %q", vendorsurface.StatusLabel)
	}
	if s.HTTP.Method == "" || !strings.HasPrefix(s.HTTP.Path, "/") || s.HTTP.Status < 100 || s.HTTP.Status > 599 {
		bad("http exchange needs a method, an absolute path and a status")
	}
	if s.Scenario.IsError() != (s.HTTP.Status >= 400) {
		bad("scenario %s does not agree with status %d", s.Scenario, s.HTTP.Status)
	}
	if len(s.FieldLedger) == 0 {
		bad("field_ledger is empty")
	}
	for path, g := range s.FieldLedger {
		if !vendorsurface.ValidEvidenceGrade(g) {
			bad("ledger %s: unknown grade %q", path, g)
		}
	}
	docs, err := decodeDocs(fixture, ndjson)
	if err != nil {
		bad("fixture is not valid JSON: %v", err)
		return errs
	}
	for _, path := range sortedKeys(s.FieldLedger) {
		if !pathPresent(docs, path) {
			bad("ledger path %q is not present in the fixture", path)
		}
	}
	return errs
}

func decodeDocs(b []byte, ndjson bool) ([]any, error) {
	if !ndjson {
		var v any
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		return []any{v}, nil
	}
	var out []any
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var v any
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty ndjson")
	}
	return out, nil
}

// pathPresent reports whether a ledger path ("data[].results[].input_tokens",
// "$" for the document root) names a member present in at least one record.
// "[]" steps into every array element; a key may be present with a null value.
func pathPresent(docs []any, path string) bool {
	for _, d := range docs {
		if resolve(d, path) {
			return true
		}
	}
	return false
}

// cutPath splits off the first segment at the first unescaped dot; a key that
// itself contains a dot (OpenAI audit events such as "api_key.created") is
// written with the dot escaped as "\.".
func cutPath(path string) (head, rest string) {
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case '\\':
			i++
		case '.':
			return strings.ReplaceAll(path[:i], `\.`, "."), path[i+1:]
		}
	}
	return strings.ReplaceAll(path, `\.`, "."), ""
}

func resolve(v any, path string) bool {
	if path == "$" || path == "" {
		return true
	}
	head, rest := cutPath(path)
	arr := strings.HasSuffix(head, "[]")
	key := strings.TrimSuffix(head, "[]")
	if key != "" {
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		v, ok = m[key]
		if !ok {
			return false
		}
	}
	if arr {
		items, ok := v.([]any)
		if !ok {
			return false
		}
		if rest == "" {
			return true
		}
		for _, it := range items {
			if resolve(it, rest) {
				return true
			}
		}
		return false
	}
	if rest == "" {
		return true
	}
	return resolve(v, rest)
}

// RequiredGrades returns the grades of a ledger, for SyntheticCeiling.
func RequiredGrades(ledger map[string]vendorsurface.EvidenceGrade) []vendorsurface.EvidenceGrade {
	out := make([]vendorsurface.EvidenceGrade, 0, len(ledger))
	for _, k := range sortedKeys(ledger) {
		out = append(out, ledger[k])
	}
	return out
}

func sortedKeys(m map[string]vendorsurface.EvidenceGrade) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
