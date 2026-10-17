package cfgfmt

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Format names a file format an engine understands. The vocabulary is
// closed; Parse refuses anything else.
type Format string

// The closed Format vocabulary.
const (
	// FormatJSON is strict JSON (RFC 8259): a comment or a trailing comma
	// is a parse error, and edits never introduce one.
	FormatJSON Format = "json"
	// FormatJSONC is JSON with comments and trailing commas (VS Code
	// settings, OpenCode / Kilo jsonc, Zed).
	FormatJSONC Format = "jsonc"
	// FormatTOML is TOML 1.0 (Codex, Open Interpreter, Mistral vibe, ...).
	FormatTOML Format = "toml"
	// FormatYAML is a single-document YAML 1.2 file (goose, aider, hermes,
	// poolside).
	FormatYAML Format = "yaml"
	// FormatMarkdown is a Markdown instruction / skill / agent file with
	// optional YAML front matter.
	FormatMarkdown Format = "markdown"
	// FormatMDC is Cursor's .mdc rule dialect: structurally Markdown with
	// YAML front matter (its key vocabulary is its own, not the engine's
	// business).
	FormatMDC Format = "mdc"
)

// Formats returns the closed Format vocabulary in a stable order.
func Formats() []Format {
	return []Format{FormatJSON, FormatJSONC, FormatTOML, FormatYAML, FormatMarkdown, FormatMDC}
}

// Known reports whether f is in the closed vocabulary.
func (f Format) Known() bool {
	for _, k := range Formats() {
		if f == k {
			return true
		}
	}
	return false
}

// Path is a format-agnostic key path: one segment per nested table /
// object / mapping key. Array indexing is deliberately not expressible: a
// settings catalogue addresses keys, and an array is replaced as a whole
// value.
type Path []string

// String renders the path dotted, quoting a segment that itself contains a
// dot, a quote or a space, so the rendering is unambiguous in messages.
func (p Path) String() string {
	parts := make([]string, len(p))
	for i, s := range p {
		if s == "" || strings.ContainsAny(s, ". \"'\t") {
			parts[i] = fmt.Sprintf("%q", s)
		} else {
			parts[i] = s
		}
	}
	return strings.Join(parts, ".")
}

// Disclosure records an edit the engine could not perform surgically, so
// the caller's diff preview can name what the save will change beyond the
// edited key.
type Disclosure struct {
	// Op is "set" or "delete".
	Op string `json:"op"`
	// Path is the key the caller edited.
	Path Path `json:"path"`
	// Reason says why the surgical path was not taken and what the
	// re-serialization lost (comments, key order, blank lines).
	Reason string `json:"reason"`
}

// Doc is a parsed configuration document. Every engine implements it.
type Doc interface {
	// Format returns the engine's format.
	Format() Format
	// Get returns the decoded value at path. Values are normalized:
	// nil, bool, int64, float64, string, time.Time (TOML datetimes),
	// []any and map[string]any.
	Get(path Path) (value any, found bool, err error)
	// Set replaces (or creates) the value at path, creating intermediate
	// tables / objects / mappings. Setting a value equal to the current
	// one is a no-op that leaves the bytes untouched.
	Set(path Path, value any) error
	// Delete removes the key at path. found is false (and the bytes are
	// untouched) when the key does not exist.
	Delete(path Path) (found bool, err error)
	// Value decodes the whole document (normalized as for Get). An empty
	// document decodes to an empty map.
	Value() (map[string]any, error)
	// Bytes renders the document. Unedited, it is the parsed input byte
	// for byte.
	Bytes() []byte
	// Disclosures lists the edits that fell back to re-serialization.
	Disclosures() []Disclosure
}

// Options tunes Parse.
type Options struct {
	// ForbidReserialize makes an edit that cannot be done surgically fail
	// with ErrNeedsReserialize instead of re-serializing the document.
	ForbidReserialize bool
}

// Limits the engines enforce on every input (hostile-input guard).
const (
	// MaxBytes is the largest document Parse accepts. Vendor settings
	// files are kilobytes; anything near this is not a settings file.
	MaxBytes = 8 << 20
	// MaxDepth is the deepest structural nesting Parse accepts.
	MaxDepth = 200
)

// Sentinel errors. Match with errors.Is.
var (
	// ErrTooLarge: the input exceeds MaxBytes.
	ErrTooLarge = errors.New("cfgfmt: document too large")
	// ErrTooDeep: the input nests deeper than MaxDepth.
	ErrTooDeep = errors.New("cfgfmt: document nested too deeply")
	// ErrBinary: the input carries a NUL byte or invalid UTF-8.
	ErrBinary = errors.New("cfgfmt: document is not valid UTF-8 text")
	// ErrDuplicateKey: an object / table / mapping repeats a key, so
	// different readers would disagree on its value.
	ErrDuplicateKey = errors.New("cfgfmt: duplicate key")
	// ErrUnsupported: the document shape cannot be edited safely (multi-
	// document YAML, an anchored / aliased YAML target, a path through an
	// array).
	ErrUnsupported = errors.New("cfgfmt: unsupported document shape")
	// ErrTypeMismatch: the path crosses a value that is not a table /
	// object / mapping.
	ErrTypeMismatch = errors.New("cfgfmt: path crosses a non-object value")
	// ErrValueType: the value cannot be represented in the format (for
	// example nil in TOML, or a Go type outside the normalized set).
	ErrValueType = errors.New("cfgfmt: value type not representable")
	// ErrEmptyPath: Set / Delete need at least one path segment.
	ErrEmptyPath = errors.New("cfgfmt: empty path")
	// ErrNeedsReserialize: the edit is only possible by re-serializing the
	// document and Options.ForbidReserialize is set.
	ErrNeedsReserialize = errors.New("cfgfmt: edit needs a re-serialization")
	// ErrVerify: an edit did not produce the expected tree. It signals an
	// engine defect; the document is left unchanged.
	ErrVerify = errors.New("cfgfmt: edit verification failed")
	// ErrUnknownFormat: Parse was given a Format outside the vocabulary.
	ErrUnknownFormat = errors.New("cfgfmt: unknown format")
	// ErrMarker: a Markdown marked block is malformed (unterminated,
	// nested, duplicated) or new block content would forge a marker.
	ErrMarker = errors.New("cfgfmt: malformed observer block marker")
)

// ParseError is a syntax error with a 1-based position when the
// underlying parser reports one (Line or Column is 0 when unknown).
type ParseError struct {
	Format Format
	Line   int
	Column int
	Msg    string
}

// Error implements error.
func (e *ParseError) Error() string {
	switch {
	case e.Line > 0 && e.Column > 0:
		return fmt.Sprintf("cfgfmt: %s parse error at line %d, column %d: %s", e.Format, e.Line, e.Column, e.Msg)
	case e.Line > 0:
		return fmt.Sprintf("cfgfmt: %s parse error at line %d: %s", e.Format, e.Line, e.Msg)
	default:
		return fmt.Sprintf("cfgfmt: %s parse error: %s", e.Format, e.Msg)
	}
}

// Parse parses b as format f with default Options.
func Parse(f Format, b []byte) (Doc, error) {
	return ParseWith(f, b, Options{})
}

// ParseWith parses b as format f.
func ParseWith(f Format, b []byte, opts Options) (Doc, error) {
	if !f.Known() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownFormat, f)
	}
	if len(b) > MaxBytes {
		return nil, ErrTooLarge
	}
	bom, body := splitBOM(b)
	if bytes.IndexByte(body, 0) >= 0 || !utf8.Valid(body) {
		return nil, ErrBinary
	}
	switch f {
	case FormatJSON, FormatJSONC:
		return parseJSON(f, bom, body, opts)
	case FormatTOML:
		return parseTOML(bom, body, opts)
	case FormatYAML:
		return parseYAML(bom, body, opts)
	default: // FormatMarkdown, FormatMDC
		return parseMarkdown(f, bom, body, opts)
	}
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func splitBOM(b []byte) (bool, []byte) {
	if bytes.HasPrefix(b, utf8BOM) {
		return true, b[len(utf8BOM):]
	}
	return false, b
}

func withBOM(bom bool, body []byte) []byte {
	if !bom {
		return body
	}
	out := make([]byte, 0, len(body)+3)
	out = append(out, utf8BOM...)
	return append(out, body...)
}

// --- value normalization and tree operations (shared by every engine) ---

// Normalize converts a caller value into the engines' canonical value set
// (nil, bool, int64, float64, string, time.Time, []any, map[string]any).
// Go integer kinds widen to int64, float32 to float64, typed slices and
// string-keyed maps to their generic forms.
func Normalize(v any) (any, error) {
	return normalize(v, 0)
}

func normalize(v any, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, ErrTooDeep
	}
	switch x := v.(type) {
	case nil:
		return nil, nil
	case bool, string, int64, float64, time.Time:
		return x, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			n, err := normalize(e, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			n, err := normalize(e, depth+1)
			if err != nil {
				return nil, err
			}
			out[k] = n
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			ks, ok := k.(string)
			if !ok {
				ks = fmt.Sprint(k)
			}
			if _, dup := out[ks]; dup {
				// Two keys (say YAML 0 and "0") collapse to one string:
				// readers would disagree on which one a path means.
				return nil, fmt.Errorf("%w: %q (keys of different types)", ErrDuplicateKey, ks)
			}
			n, err := normalize(e, depth+1)
			if err != nil {
				return nil, err
			}
			out[ks] = n
		}
		return out, nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			n, err := normalize(rv.Index(i).Interface(), depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("%w: map key %s", ErrValueType, rv.Type().Key())
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			n, err := normalize(iter.Value().Interface(), depth+1)
			if err != nil {
				return nil, err
			}
			out[iter.Key().String()] = n
		}
		return out, nil
	}
	return normalizeScalarKind(rv, v)
}

// normalizeScalarKind widens Go numeric, string and bool kinds (named
// types included) to the canonical int64 / float64 / string / bool.
func normalizeScalarKind(rv reflect.Value, v any) (any, error) {
	switch rv.Kind() {
	case reflect.String:
		return rv.String(), nil
	case reflect.Bool:
		return rv.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := rv.Uint()
		if u > math.MaxInt64 {
			return nil, fmt.Errorf("%w: %d overflows int64", ErrValueType, u)
		}
		return int64(u), nil
	case reflect.Float32, reflect.Float64:
		return rv.Float(), nil
	}
	return nil, fmt.Errorf("%w: %T", ErrValueType, v)
}

// Equal reports whether two normalized values are the same configuration
// value. Numbers compare by value across int64 / float64 (JSON and YAML do
// not distinguish 2 from 2.0 the way a Go type switch does).
func Equal(a, b any) bool {
	switch x := a.(type) {
	case int64:
		switch y := b.(type) {
		case int64:
			return x == y
		case float64:
			return float64(x) == y
		}
		return false
	case float64:
		switch y := b.(type) {
		case int64:
			return x == float64(y)
		case float64:
			return x == y || (math.IsNaN(x) && math.IsNaN(y))
		}
		return false
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !Equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, xv := range x {
			yv, ok := y[k]
			if !ok || !Equal(xv, yv) {
				return false
			}
		}
		return true
	case time.Time:
		y, ok := b.(time.Time)
		return ok && x.Equal(y)
	}
	return reflect.DeepEqual(a, b)
}

// lookup walks a normalized tree.
func lookup(root map[string]any, path Path) (any, bool, error) {
	var cur any = root
	for i, seg := range path {
		if cur == nil && i > 0 {
			// A null intermediate ("key:" in YAML, null in JSON) is an
			// absent table, not a type mismatch: Set may fill it.
			return nil, false, nil
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("%w: %s", ErrTypeMismatch, path[:i])
		}
		v, ok := m[seg]
		if !ok {
			return nil, false, nil
		}
		cur = v
	}
	return cur, true, nil
}

// deepCopy copies a normalized tree.
func deepCopy(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = deepCopy(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = deepCopy(e)
		}
		return out
	}
	return v
}

// treeSet returns a copy of root with value set at path, creating maps.
func treeSet(root map[string]any, path Path, value any) (map[string]any, error) {
	out, _ := deepCopy(root).(map[string]any)
	if out == nil {
		out = map[string]any{}
	}
	cur := out
	for i, seg := range path[:len(path)-1] {
		next, ok := cur[seg]
		if !ok || next == nil {
			m := map[string]any{}
			cur[seg] = m
			cur = m
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrTypeMismatch, path[:i+1])
		}
		cur = m
	}
	cur[path[len(path)-1]] = deepCopy(value)
	return out, nil
}

// treeDelete returns a copy of root without the key at path.
func treeDelete(root map[string]any, path Path) map[string]any {
	out, _ := deepCopy(root).(map[string]any)
	if out == nil {
		return map[string]any{}
	}
	cur := out
	for _, seg := range path[:len(path)-1] {
		m, ok := cur[seg].(map[string]any)
		if !ok {
			return out
		}
		cur = m
	}
	delete(cur, path[len(path)-1])
	return out
}

// nestValue wraps value in maps for the path tail: nestValue({b,c}, v) is
// {"b": {"c": v}}.
func nestValue(tail Path, value any) any {
	v := value
	for i := len(tail) - 1; i >= 1; i-- {
		v = map[string]any{tail[i]: v}
	}
	return v
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func checkPath(path Path) error {
	if len(path) == 0 {
		return ErrEmptyPath
	}
	if len(path) > MaxDepth {
		return ErrTooDeep
	}
	return nil
}

// --- line model (TOML / YAML / Markdown keep each line's own terminator) ---

type line struct {
	text string // without the terminator
	eol  string // "\n", "\r\n", or "" for a final line with no newline
}

func splitLines(s string) []line {
	var out []line
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, line{text: s})
			break
		}
		t := s[:i]
		eol := "\n"
		if strings.HasSuffix(t, "\r") {
			t = t[:len(t)-1]
			eol = "\r\n"
		}
		out = append(out, line{text: t, eol: eol})
		s = s[i+1:]
	}
	return out
}

func joinLines(ls []line) string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(l.text)
		b.WriteString(l.eol)
	}
	return b.String()
}

// dominantEOL returns the most common terminator ("\n" when none).
func dominantEOL(ls []line) string {
	crlf, lf := 0, 0
	for _, l := range ls {
		switch l.eol {
		case "\r\n":
			crlf++
		case "\n":
			lf++
		}
	}
	if crlf > lf {
		return "\r\n"
	}
	return "\n"
}

// textLines turns rendered text (always "\n"-separated) into lines using
// eol, every line terminated.
func textLines(s, eol string) []line {
	s = strings.TrimSuffix(s, "\n")
	parts := strings.Split(s, "\n")
	out := make([]line, len(parts))
	for i, p := range parts {
		out[i] = line{text: p, eol: eol}
	}
	return out
}

// spliceLines replaces ls[from:to] with repl, terminating a previously
// final unterminated line when something now follows it.
func spliceLines(ls []line, from, to int, repl []line, eol string) []line {
	out := make([]line, 0, len(ls)-(to-from)+len(repl))
	out = append(out, ls[:from]...)
	if len(repl) > 0 && len(out) > 0 && out[len(out)-1].eol == "" {
		out[len(out)-1].eol = eol
	}
	out = append(out, repl...)
	rest := ls[to:]
	if len(rest) > 0 && len(out) > 0 && out[len(out)-1].eol == "" {
		out[len(out)-1].eol = eol
	}
	out = append(out, rest...)
	return out
}

func leadingWS(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }
