package vendormock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"reflect"
	"sort"
	"strings"
)

// MaxSafeInteger is the largest integer the generator emits: 2^53-1, the
// largest integer every JSON consumer (including a float64 field) represents
// exactly. Counts and token totals in the vendor schemas are far below it.
const MaxSafeInteger = 1<<53 - 1

// GenOptions tunes the generator. Zero values are sensible defaults.
type GenOptions struct {
	// MaxArray bounds generated array length (default 3).
	MaxArray int
	// MaxDepth bounds nesting (default 12).
	MaxDepth int
}

// Generate returns a random instance of a dereferenced OpenAPI 3.0 or 3.1
// schema: required properties always present, optional ones sometimes
// absent, nullable values sometimes null, arrays sometimes empty, numbers
// sometimes at their documented bounds or MaxSafeInteger, timestamps on UTC
// midnight and DST-transition instants, and objects sometimes carrying an
// unknown extra member (plan 12A.1 schema-derived property tests).
func Generate(schema map[string]any, rng *rand.Rand, o GenOptions) any {
	if o.MaxArray <= 0 {
		o.MaxArray = 3
	}
	if o.MaxDepth <= 0 {
		o.MaxDepth = 12
	}
	g := &gen{rng: rng, o: o}
	return g.value(schema, 0)
}

type gen struct {
	rng *rand.Rand
	o   GenOptions
}

func (g *gen) chance(p float64) bool { return g.rng.Float64() < p }

func types(s map[string]any) []string {
	switch t := s["type"].(type) {
	case string:
		return []string{t}
	case []any:
		out := []string{}
		for _, x := range t {
			if str, ok := x.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

func nullable(s map[string]any) bool {
	if b, _ := s["nullable"].(bool); b {
		return true
	}
	for _, t := range types(s) {
		if t == "null" {
			return true
		}
	}
	return false
}

func mergeAllOf(s map[string]any) map[string]any {
	parts, ok := s["allOf"].([]any)
	if !ok {
		return s
	}
	out := map[string]any{}
	for k, v := range s {
		if k != "allOf" {
			out[k] = v
		}
	}
	props := map[string]any{}
	if p, ok := out["properties"].(map[string]any); ok {
		for k, v := range p {
			props[k] = v
		}
	}
	var req []any
	if r, ok := out["required"].([]any); ok {
		req = append(req, r...)
	}
	for _, p := range parts {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		pm = mergeAllOf(pm)
		if pp, ok := pm["properties"].(map[string]any); ok {
			for k, v := range pp {
				props[k] = v
			}
		}
		if r, ok := pm["required"].([]any); ok {
			req = append(req, r...)
		}
		for k, v := range pm {
			if _, has := out[k]; !has && k != "properties" && k != "required" {
				out[k] = v
			}
		}
	}
	if len(props) > 0 {
		out["properties"] = props
	}
	if len(req) > 0 {
		out["required"] = req
	}
	return out
}

func (g *gen) value(s map[string]any, depth int) any {
	s = mergeAllOf(s)
	if nullable(s) && g.chance(0.15) {
		return nil
	}
	if depth > g.o.MaxDepth {
		return nil
	}
	if v, ok := g.alternative(s, depth); ok {
		return v
	}
	if v, ok := g.literal(s); ok {
		return v
	}
	switch schemaType(s) {
	case "object":
		return g.object(s, depth)
	case "array":
		return g.array(s, depth)
	case "integer":
		return g.integer(s)
	case "number":
		return g.number(s)
	case "boolean":
		return g.chance(0.5)
	case "string":
		return g.str(s)
	}
	return nil
}

// alternative picks one branch of oneOf or anyOf (a null branch sometimes
// yields null).
func (g *gen) alternative(s map[string]any, depth int) (any, bool) {
	for _, key := range []string{"oneOf", "anyOf"} {
		alts, ok := s[key].([]any)
		if !ok || len(alts) == 0 {
			continue
		}
		var cands []map[string]any
		for _, a := range alts {
			am, ok := a.(map[string]any)
			if !ok {
				continue
			}
			if ts := types(am); len(ts) == 1 && ts[0] == "null" {
				if g.chance(0.15) {
					return nil, true
				}
				continue
			}
			cands = append(cands, am)
		}
		if len(cands) == 0 {
			return nil, true
		}
		return g.value(cands[g.rng.Intn(len(cands))], depth+1), true
	}
	return nil, false
}

// literal answers const and enum schemas.
func (g *gen) literal(s map[string]any) (any, bool) {
	if c, ok := s["const"]; ok {
		return c, true
	}
	e, ok := s["enum"].([]any)
	if !ok || len(e) == 0 {
		return nil, false
	}
	var vals []any
	for _, v := range e {
		if v != nil {
			vals = append(vals, v)
		}
	}
	if len(vals) == 0 {
		return nil, true
	}
	return vals[g.rng.Intn(len(vals))], true
}

// schemaType is the first non-null type, inferred from properties or items
// when the schema names none.
func schemaType(s map[string]any) string {
	for _, x := range types(s) {
		if x != "null" {
			return x
		}
	}
	if _, ok := s["properties"]; ok {
		return "object"
	}
	if _, ok := s["items"]; ok {
		return "array"
	}
	return ""
}

func (g *gen) array(s map[string]any, depth int) any {
	n := g.rng.Intn(g.o.MaxArray + 1)
	if g.chance(0.2) {
		n = 0
	}
	items, _ := s["items"].(map[string]any)
	out := make([]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, g.value(items, depth+1))
	}
	return out
}

func (g *gen) object(s map[string]any, depth int) any {
	out := map[string]any{}
	req := map[string]bool{}
	if r, ok := s["required"].([]any); ok {
		for _, x := range r {
			if k, ok := x.(string); ok {
				req[k] = true
			}
		}
	}
	props, _ := s["properties"].(map[string]any)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !req[k] && g.chance(0.3) {
			continue
		}
		ps, _ := props[k].(map[string]any)
		out[k] = g.value(ps, depth+1)
	}
	if ap, ok := s["additionalProperties"].(map[string]any); ok && len(props) == 0 {
		for i := 0; i < g.rng.Intn(3); i++ {
			out[fmt.Sprintf("dynamic_key_%d", i)] = g.value(ap, depth+1)
		}
	}
	if g.chance(0.2) {
		out["x_unknown_extra_field"] = "unknown"
	}
	return out
}

func num(s map[string]any, k string) (float64, bool) {
	switch v := s[k].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	return 0, false
}

func (g *gen) integer(s map[string]any) any {
	lo, hasLo := num(s, "minimum")
	hi, hasHi := num(s, "maximum")
	if !hasLo {
		lo = 0
	}
	if !hasHi || hi > MaxSafeInteger {
		hi = MaxSafeInteger
	}
	if hi < lo {
		hi = lo
	}
	switch g.rng.Intn(5) {
	case 0:
		return int64(lo)
	case 1:
		return int64(hi)
	default:
		span := hi - lo
		if span > 1e6 {
			span = 1e6
		}
		return int64(lo) + g.rng.Int63n(int64(span)+1)
	}
}

func (g *gen) number(s map[string]any) any {
	lo, hasLo := num(s, "minimum")
	hi, hasHi := num(s, "maximum")
	if !hasLo {
		lo = 0
	}
	if !hasHi {
		hi = 1e15
	}
	switch g.rng.Intn(4) {
	case 0:
		return lo
	case 1:
		return hi
	default:
		return math.Round((lo+g.rng.Float64()*math.Min(hi-lo, 1e6))*100) / 100
	}
}

var timestamps = []string{
	"2026-01-01T00:00:00Z",      // UTC midnight
	"2026-03-08T07:00:00Z",      // US DST start instant (02:00 America/New_York)
	"2026-11-01T05:59:59Z",      // just before US DST end
	"2026-03-29T01:00:00Z",      // EU DST start instant
	"2026-12-31T23:59:59.999Z",  // year end with fraction
	"2026-07-01T12:00:00+05:30", // non-UTC offset
}

func (g *gen) str(s map[string]any) any {
	switch s["format"] {
	case "date-time":
		return timestamps[g.rng.Intn(len(timestamps))]
	case "date":
		return []string{"2026-01-01", "2026-03-08", "2026-11-01", "2026-12-31"}[g.rng.Intn(4)]
	case "uri", "url":
		return "https://example.com/synthetic"
	case "email":
		return "member@example.com"
	}
	pool := []string{"", "synthetic", "unicode é中", strings.Repeat("x", 256)}
	return pool[g.rng.Intn(len(pool))]
}

// CheckDeclaredFields decodes raw into target (a pointer to a contract type)
// and reports every field the type declares whose decoded value differs from
// the source: present-and-different, or present-but-dropped. A source null or
// an absent member must decode to the field's zero value. Unknown members are
// ignored (lenient parsing, plan 12A.5). It returns the decode error, if any.
func CheckDeclaredFields(raw []byte, target any) ([]string, error) {
	if reflect.ValueOf(target).Kind() != reflect.Pointer {
		return nil, fmt.Errorf("vendormock.CheckDeclaredFields: target must be a pointer")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return nil, err
	}
	src, err := decodeJSON(raw)
	if err != nil {
		return nil, err
	}
	back, err := json.Marshal(target)
	if err != nil {
		return nil, err
	}
	got, err := decodeJSON(back)
	if err != nil {
		return nil, err
	}
	var losses []string
	compare("$", src, got, &losses)
	return losses, nil
}

// compare walks the re-marshalled contract value (only declared fields) and
// checks each against the source.
func compare(path string, src, got any, losses *[]string) {
	switch g := got.(type) {
	case map[string]any:
		sm, _ := src.(map[string]any)
		keys := make([]string, 0, len(g))
		for k := range g {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			var sv any
			if sm != nil {
				sv = sm[k]
			}
			compare(path+"."+k, sv, g[k], losses)
		}
	case []any:
		sa, _ := src.([]any)
		if len(sa) != len(g) {
			*losses = append(*losses, fmt.Sprintf("%s: %d elements, source has %d", path, len(g), len(sa)))
			return
		}
		for i := range g {
			compare(fmt.Sprintf("%s[%d]", path, i), sa[i], g[i], losses)
		}
	default:
		if src == nil {
			if !isZero(got) {
				*losses = append(*losses, fmt.Sprintf("%s: source null or absent, decoded %v", path, got))
			}
			return
		}
		if !scalarEqual(src, got) {
			*losses = append(*losses, fmt.Sprintf("%s: source %v, decoded %v", path, src, got))
		}
	}
}

func isZero(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case json.Number:
		f, _ := new(big.Float).SetString(x.String())
		return f != nil && f.Sign() == 0
	}
	return false
}

func scalarEqual(a, b any) bool {
	an, aok := a.(json.Number)
	bn, bok := b.(json.Number)
	if aok && bok {
		af, _ := new(big.Float).SetString(an.String())
		bf, _ := new(big.Float).SetString(bn.String())
		if af == nil || bf == nil {
			return an == bn
		}
		if af.Cmp(bf) == 0 {
			return true
		}
		// A float64 round trip may print a different shortest form; compare
		// at float64 precision.
		x, _ := af.Float64()
		y, _ := bf.Float64()
		return x == y
	}
	return reflect.DeepEqual(a, b) || bytes.Equal(mustJSON(a, nil), mustJSON(b, nil))
}
