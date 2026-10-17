// Command catgen generates the schema tier of the adapter settings
// catalogue (docs/plans/adapter-config-and-common-vocabulary-plan-2026-10-03.md
// section 2.2, phase P1): one internal/agentconfig/catalog_<tool>.gen.go per
// manifest target, plus catalog_index.gen.go (the row index and the schema
// pins).
//
// # Inputs
//
// testdata/agentconfig/schemas/manifest.json (the one owner of the schema
// list) and the pinned vendor JSON schema files it names. The pinned copies
// are refreshed by scripts/agentconfig-schema-refresh.sh, the only place
// that touches the network.
//
// # Projection
//
// A walk over each schema's properties (following $ref / allOf, unwrapping
// nullable anyOf / oneOf) emits one row per leaf setting with the schema's
// type, enum, bounds, default and description. Objects with fixed
// properties are descended; maps (additionalProperties / patternProperties)
// become one string_map / object row; a union of a scalar and an object
// yields the scalar row AND the object's property rows. Key names that are
// credentials by shape (api_key, ..._token, secret, password) are marked
// Secret. Everything a schema cannot know (owner, danger, scope narrowing,
// env-backed keys) is the hand-written overlay's job, not the generator's.
//
// # Determinism
//
// No clock, no network, no environment, no map-order dependence (rows are
// sorted by id, schema keys walked sorted). The byte-diff drift gate
// `make verify-agentconfig-catalog` depends on it.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
)

type manifest struct {
	FetchedOn string `json:"fetched_on"`
	Schemas   []struct {
		File    string `json:"file"`
		URL     string `json:"url"`
		Targets []struct {
			Tool  string `json:"tool"`
			Shape string `json:"shape"`
		} `json:"targets"`
	} `json:"schemas"`
}

func main() {
	root := flag.String("root", ".", "repository root")
	schemas := flag.String("schemas", "testdata/agentconfig/schemas", "pinned schema directory (relative to -root)")
	out := flag.String("out", "internal/agentconfig", "output directory (relative to -root unless absolute)")
	flag.Parse()
	outDir := *out
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(*root, outDir)
	}
	files, err := generate(filepath.Join(*root, *schemas))
	if err != nil {
		log.Fatalf("catgen: %v", err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(outDir, name), files[name], 0o644); err != nil { //nolint:gosec // G306: generated source artifact, readable like the rest of the tree.
			log.Fatalf("catgen: %v", err)
		}
	}
	fmt.Printf("catgen: wrote %d files to %s\n", len(files), outDir)
}

type pin struct{ tool, shape, file, url, sha string }

// generate returns file name -> formatted Go source.
func generate(schemaDir string) (map[string][]byte, error) {
	raw, err := os.ReadFile(filepath.Join(schemaDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	files := map[string][]byte{}
	var pins []pin
	for _, s := range m.Schemas {
		b, err := os.ReadFile(filepath.Join(schemaDir, s.File))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		sha := hex.EncodeToString(sum[:])
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			return nil, fmt.Errorf("%s: %w", s.File, err)
		}
		leaves := (&walker{doc: doc}).walkRoot()
		for _, t := range s.Targets {
			src, err := renderTool(t.Tool, t.Shape, s.File, s.URL, sha, leaves)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", t.Tool, err)
			}
			files["catalog_"+strings.ReplaceAll(t.Tool, "-", "_")+".gen.go"] = src
			pins = append(pins, pin{t.Tool, t.Shape, s.File, s.URL, sha})
		}
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].tool < pins[j].tool })
	src, err := renderIndex(pins, m.FetchedOn)
	if err != nil {
		return nil, err
	}
	files["catalog_index.gen.go"] = src
	return files, nil
}

func renderIndex(pins []pin, fetchedOn string) ([]byte, error) {
	var b strings.Builder
	b.WriteString(header("testdata/agentconfig/schemas/manifest.json"))
	b.WriteString("// generatedRows indexes the schema tier of the catalogue by tool.\n")
	b.WriteString("var generatedRows = map[string][]SettingRow{\n")
	for _, p := range pins {
		fmt.Fprintf(&b, "\t%q: %s,\n", p.tool, varName(p.tool))
	}
	b.WriteString("}\n\n")
	b.WriteString("// schemaPins records which pinned schema each generated row set came from.\n")
	b.WriteString("var schemaPins = []SchemaPin{\n")
	for _, p := range pins {
		fmt.Fprintf(&b, "\t{Tool: %q, Shape: %q, File: %q, URL: %q, SHA256: %q, FetchedOn: %q},\n",
			p.tool, p.shape, p.file, p.url, p.sha, fetchedOn)
	}
	b.WriteString("}\n")
	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	return src, nil
}

func header(source string) string {
	return "// Code generated by internal/agentconfig/catgen; DO NOT EDIT.\n" +
		"// Source: " + source + "\n" +
		"// Regenerate: make agentconfig-catalog-build\n\npackage agentconfig\n\n"
}

func varName(tool string) string {
	var b strings.Builder
	b.WriteString("gen")
	for _, part := range strings.FieldsFunc(tool, func(r rune) bool { return r == '-' || r == '_' }) {
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// --- schema walk -------------------------------------------------------------

// leaf is one generated setting.
type leaf struct {
	path         []string
	typ          string // agentconfig ValueType constant suffix
	enum         []string
	min, max     *float64
	exMin, exMax bool
	def          string
	desc         string
	pointer      string
	secret       bool
	deprecated   bool
}

type walker struct {
	doc    map[string]any
	leaves []leaf
}

// maxWalkDepth bounds descent into nested objects; deeper objects become
// one object row (still editable through the raw view).
const maxWalkDepth = 8

func (w *walker) walkRoot() []leaf {
	w.object(w.resolve(w.doc, 0), nil, "#", 0)
	sort.SliceStable(w.leaves, func(i, j int) bool { return pathKey(w.leaves[i].path) < pathKey(w.leaves[j].path) })
	out := w.leaves[:0]
	seen := map[string]bool{}
	for _, l := range w.leaves {
		k := pathKey(l.path)
		if seen[k] {
			continue // a union reached the same path twice: the first wins
		}
		seen[k] = true
		out = append(out, l)
	}
	return out
}

func pathKey(p []string) string { return strings.Join(p, "\x00") }

// resolve follows $ref and merges allOf into one schema map; keys written
// beside a $ref (a property's own description / default) win.
func (w *walker) resolve(n map[string]any, depth int) map[string]any {
	if depth > 16 || n == nil {
		return n
	}
	out := map[string]any{}
	if ref, ok := n["$ref"].(string); ok {
		if target := w.lookupRef(ref); target != nil {
			for k, v := range w.resolve(target, depth+1) {
				out[k] = v
			}
		}
	}
	if all, ok := n["allOf"].([]any); ok {
		for _, a := range all {
			am, ok := a.(map[string]any)
			if !ok {
				continue
			}
			for k, v := range w.resolve(am, depth+1) {
				if k == "properties" {
					out[k] = mergeProps(out[k], v)
				} else {
					out[k] = v
				}
			}
		}
	}
	for k, v := range n {
		switch k {
		case "$ref", "allOf":
		case "properties":
			out[k] = mergeProps(out[k], v)
		default:
			out[k] = v
		}
	}
	return out
}

func mergeProps(a, b any) any {
	out := map[string]any{}
	if am, ok := a.(map[string]any); ok {
		for k, v := range am {
			out[k] = v
		}
	}
	if bm, ok := b.(map[string]any); ok {
		for k, v := range bm {
			out[k] = v
		}
	}
	return out
}

func (w *walker) lookupRef(ref string) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	var cur any = w.doc
	for _, seg := range strings.Split(ref[2:], "/") {
		seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[seg]
	}
	m, _ := cur.(map[string]any)
	return m
}

// object descends an object schema's fixed properties.
func (w *walker) object(n map[string]any, path []string, ptr string, depth int) {
	props, _ := n["properties"].(map[string]any)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "$schema" {
			continue
		}
		pm, ok := props[k].(map[string]any)
		if !ok {
			continue
		}
		w.property(pm, appendPath(path, k), ptr+"/properties/"+escapePtr(k), depth+1)
	}
}

func appendPath(p []string, k string) []string {
	return append(append([]string{}, p...), k)
}

func escapePtr(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// property classifies one property schema and emits rows.
func (w *walker) property(raw map[string]any, path []string, ptr string, depth int) {
	n := w.resolve(raw, 0)
	desc := description(raw, n)
	alts := w.branches(n)
	switch {
	case len(alts) == 1:
		// A nullable wrapper: unwrap, keeping the wrapper's own keys.
		m := map[string]any{}
		for k, v := range alts[0] {
			m[k] = v
		}
		for k, v := range n {
			if k == "anyOf" || k == "oneOf" {
				continue
			}
			if _, has := m[k]; !has || k == "description" || k == "default" {
				m[k] = v
			}
		}
		w.classify(m, path, ptr, depth, desc)
	case len(alts) > 1:
		w.union(n, alts, path, ptr, depth, desc)
	default:
		w.classify(n, path, ptr, depth, desc)
	}
}

func (w *walker) classify(n map[string]any, path []string, ptr string, depth int, desc string) {
	if schemaType(n) == "object" && hasProps(n) && depth < maxWalkDepth {
		w.object(n, path, ptr, depth)
		return
	}
	w.emit(n, path, ptr, desc, w.valueType(n), enumOf(w, n))
}

// branches returns the non-null anyOf / oneOf alternatives (resolved).
func (w *walker) branches(n map[string]any) []map[string]any {
	var out []map[string]any
	for _, key := range []string{"anyOf", "oneOf"} {
		alts, ok := n[key].([]any)
		if !ok {
			continue
		}
		for _, a := range alts {
			am, ok := a.(map[string]any)
			if !ok {
				continue
			}
			r := w.resolve(am, 0)
			if schemaType(r) == "null" {
				continue
			}
			out = append(out, r)
		}
	}
	return out
}

// union handles a multi-alternative anyOf / oneOf.
func (w *walker) union(n map[string]any, alts []map[string]any, path []string, ptr string, depth int, desc string) {
	var objAlt map[string]any
	var scalars []map[string]any
	for _, a := range alts {
		switch {
		case schemaType(a) == "object" && hasProps(a):
			if objAlt == nil {
				objAlt = a
			}
		default:
			scalars = append(scalars, a)
		}
	}
	if objAlt != nil && depth < maxWalkDepth {
		w.object(objAlt, path, ptr, depth)
		if len(scalars) == 0 {
			return
		}
		// The scalar form is a second way to write the same key
		// (opencode `permission: "allow"` vs `permission: {bash: ...}`).
		typ, enum := scalarUnion(w, scalars)
		w.emit(n, path, ptr, desc+" (also accepts an object form)", typ, enum)
		return
	}
	if objAlt != nil {
		scalars = append(scalars, objAlt)
	}
	typ, enum := scalarUnion(w, scalars)
	w.emit(n, path, ptr, desc, typ, enum)
}

// scalarUnion folds alternatives into one type: all strings -> enum (or
// string when one alternative is free text), all one other type -> that
// type, else object.
func scalarUnion(w *walker, alts []map[string]any) (string, []string) {
	allString, free := true, false
	var enum []string
	types := map[string]bool{}
	for _, a := range alts {
		t := w.valueType(a)
		types[t] = true
		switch t {
		case "Enum":
			enum = append(enum, enumOf(w, a)...)
		case "String":
			free = true
		default:
			allString = false
		}
	}
	if allString {
		if free {
			return "String", dedupe(enum)
		}
		return "Enum", dedupe(enum)
	}
	if len(types) == 1 {
		for t := range types {
			return t, nil
		}
	}
	return "Object", nil
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// schemaType returns the JSON-schema type ("null" filtered out of a type
// list; "mixed" for several), inferred when absent.
func schemaType(n map[string]any) string {
	switch t := n["type"].(type) {
	case string:
		return t
	case []any:
		var kept []string
		for _, x := range t {
			if s, ok := x.(string); ok && s != "null" {
				kept = append(kept, s)
			}
		}
		switch len(kept) {
		case 0:
			return "null"
		case 1:
			return kept[0]
		}
		return "mixed"
	}
	switch {
	case n["enum"] != nil || n["const"] != nil:
		if allStringValues(n) {
			return "string"
		}
		return "mixed"
	case n["properties"] != nil || n["additionalProperties"] != nil || n["patternProperties"] != nil:
		return "object"
	case n["items"] != nil:
		return "array"
	}
	return ""
}

func allStringValues(n map[string]any) bool {
	if c, ok := n["const"]; ok {
		_, isStr := c.(string)
		return isStr
	}
	vals, _ := n["enum"].([]any)
	for _, v := range vals {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return len(vals) > 0
}

func hasProps(n map[string]any) bool {
	p, ok := n["properties"].(map[string]any)
	return ok && len(p) > 0
}

// valueType maps a (resolved) schema onto the ValueType constant suffix.
func (w *walker) valueType(n map[string]any) string {
	switch schemaType(n) {
	case "string":
		if n["enum"] != nil || n["const"] != nil {
			return "Enum"
		}
		return "String"
	case "integer":
		return "Int"
	case "number":
		return "Float"
	case "boolean":
		return "Bool"
	case "array":
		items, _ := n["items"].(map[string]any)
		if items != nil && schemaType(w.resolve(items, 0)) == "string" {
			return "StringList"
		}
		return "Object"
	case "object":
		if ap, ok := n["additionalProperties"].(map[string]any); ok && !hasProps(n) {
			if schemaType(w.resolve(ap, 0)) == "string" {
				return "StringMap"
			}
		}
		return "Object"
	}
	return "Object"
}

// enumOf returns the vendor values of an enum / const (or of a string
// array's items).
func enumOf(w *walker, n map[string]any) []string {
	src := n
	if schemaType(n) == "array" {
		if items, ok := n["items"].(map[string]any); ok {
			src = w.resolve(items, 0)
		}
	}
	var out []string
	if c, ok := src["const"]; ok {
		out = append(out, valueText(c))
	}
	if vals, ok := src["enum"].([]any); ok {
		for _, v := range vals {
			if v == nil {
				continue
			}
			out = append(out, valueText(v))
		}
	}
	return dedupe(out)
}

func valueText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func description(raw, n map[string]any) string {
	for _, m := range []map[string]any{raw, n} {
		for _, k := range []string{"description", "markdownDescription", "title"} {
			if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

const (
	maxDescRunes    = 500
	maxDefaultBytes = 240
)

func (w *walker) emit(n map[string]any, path []string, ptr, desc, typ string, enum []string) {
	l := leaf{path: path, typ: typ, pointer: ptr}
	if typ == "Enum" || typ == "String" || typ == "StringList" || typ == "Int" || typ == "Float" {
		l.enum = enum
	}
	l.min, l.exMin = bound(n, "minimum", "exclusiveMinimum")
	l.max, l.exMax = bound(n, "maximum", "exclusiveMaximum")
	if d, ok := n["default"]; ok && d != nil {
		if b, err := json.Marshal(d); err == nil && len(b) <= maxDefaultBytes {
			l.def = string(b)
		}
	}
	l.desc = cleanDesc(desc)
	l.deprecated = n["deprecated"] == true || strings.HasPrefix(strings.ToLower(strings.TrimSpace(desc)), "deprecated")
	l.secret = secretName(path[len(path)-1])
	if l.secret {
		l.def = "" // a credential never carries a value, default included
	}
	w.leaves = append(w.leaves, l)
}

// bound reads an inclusive bound, else an exclusive (draft 2019+ numeric)
// one.
func bound(n map[string]any, incl, excl string) (*float64, bool) {
	if v, ok := number(n[incl]); ok {
		return &v, false
	}
	if v, ok := number(n[excl]); ok {
		return &v, true
	}
	return nil, false
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	}
	return 0, false
}

func cleanDesc(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxDescRunes {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:maxDescRunes])) + "..."
	}
	return s
}

// secretName reports a key whose value is a credential by shape.
func secretName(key string) bool {
	k := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	if k == "token" || k == "secret" || k == "password" || k == "apikey" {
		return true
	}
	// "bearertoken" anywhere (AWS_BEARER_TOKEN_BEDROCK); the rest as a
	// suffix only, so apiKeyHelper (a command) and env_key (a variable
	// NAME) stay plain.
	if strings.Contains(k, "bearertoken") {
		return true
	}
	for _, suffix := range []string{"apikey", "secret", "password", "passwd", "passphrase", "authtoken", "accesstoken", "refreshtoken", "clientsecret", "sessiontoken"} {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	return false
}

// --- rendering --------------------------------------------------------------

func renderTool(tool, shape, schemaFile, url, sha string, leaves []leaf) ([]byte, error) {
	var b strings.Builder
	b.WriteString(header("testdata/agentconfig/schemas/" + schemaFile + " (sha256 " + sha[:16] + ")"))
	fmt.Fprintf(&b, "// %s is the schema tier of the %s catalogue (%d rows).\n", varName(tool), tool, len(leaves))
	fmt.Fprintf(&b, "var %s = []SettingRow{\n", varName(tool))
	for _, l := range leaves {
		id := tool + "/" + cfgfmt.Path(l.path).String()
		b.WriteString("\t{\n")
		fmt.Fprintf(&b, "\t\tID: %s,\n", strconv.Quote(id))
		fmt.Fprintf(&b, "\t\tTool: %s,\n", strconv.Quote(tool))
		fmt.Fprintf(&b, "\t\tFileID: %s,\n", strconv.Quote(shape))
		b.WriteString("\t\tPath: KeyPath{")
		for i, s := range l.path {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(s))
		}
		b.WriteString("},\n")
		fmt.Fprintf(&b, "\t\tType: Type%s,\n", l.typ)
		if len(l.enum) > 0 {
			b.WriteString("\t\tEnum: []string{")
			for i, e := range l.enum {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(strconv.Quote(e))
			}
			b.WriteString("},\n")
		}
		if l.min != nil {
			fmt.Fprintf(&b, "\t\tMin: f64(%s),\n", strconv.FormatFloat(*l.min, 'g', -1, 64))
		}
		if l.max != nil {
			fmt.Fprintf(&b, "\t\tMax: f64(%s),\n", strconv.FormatFloat(*l.max, 'g', -1, 64))
		}
		if l.exMin {
			b.WriteString("\t\tExclusiveMin: true,\n")
		}
		if l.exMax {
			b.WriteString("\t\tExclusiveMax: true,\n")
		}
		if l.def != "" {
			fmt.Fprintf(&b, "\t\tDefault: %s,\n", strconv.Quote(l.def))
		}
		if l.desc != "" {
			fmt.Fprintf(&b, "\t\tDesc: %s,\n", strconv.Quote(l.desc))
		}
		fmt.Fprintf(&b, "\t\tSource: %s,\n", strconv.Quote(url+l.pointer))
		b.WriteString("\t\tVerified: VerifiedSchema,\n")
		b.WriteString("\t\tOwner: OwnerUser,\n")
		if l.secret {
			b.WriteString("\t\tSecret: true,\n")
		}
		if l.deprecated {
			b.WriteString("\t\tDeprecated: true,\n")
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")
	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, err
	}
	return src, nil
}
