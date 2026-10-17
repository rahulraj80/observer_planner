package cfgfmt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// yamlDoc is the YAML engine: the file as physical lines plus the yaml.v3
// node tree (re-parsed after every edit) for positions. Edits are text
// splices located by node line / column: a scalar value is replaced over
// its exact span (its trailing comment kept), an entry is rewritten over
// its line extent, a new key is inserted after the last entry of its
// block mapping. Flow collections on the path fall back to editing the
// node tree and re-encoding it (comments kept by yaml.v3, indentation and
// blank lines normalized), which is disclosed.
type yamlDoc struct {
	bom   bool
	lines []line
	opts  Options
	discl []Disclosure
	doc   *yaml.Node // DocumentNode, nil for an empty document
	unit  int        // indentation step
}

var yamlLine = regexp.MustCompile(`line (\d+)`)

func parseYAML(bom bool, body []byte, opts Options) (Doc, error) {
	if err := yamlDepthCheck(body); err != nil {
		return nil, err
	}
	doc, err := decodeYAMLNode(body)
	if err != nil {
		return nil, err
	}
	d := &yamlDoc{bom: bom, lines: splitLines(string(body)), opts: opts, doc: doc}
	d.unit = d.detectUnit()
	if _, err := d.Value(); err != nil {
		return nil, err
	}
	return d, nil
}

func yamlDepthCheck(b []byte) error {
	depth := 0
	var quote byte
	inComment := false
	prev := byte('\n')
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case inComment:
			if c == '\n' {
				inComment = false
			}
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case (c == '"' || c == '\'') && (prev == ' ' || prev == '\n' || prev == '[' || prev == '{' || prev == ',' || prev == ':'):
			quote = c
		case c == '#' && (prev == ' ' || prev == '\t' || prev == '\n'):
			inComment = true
		case c == '[' || c == '{':
			depth++
			if depth > MaxDepth {
				return ErrTooDeep
			}
		case c == ']' || c == '}':
			if depth > 0 {
				depth--
			}
		}
		prev = c
	}
	// Block nesting is bounded by indentation: refuse absurd indents.
	for _, ln := range bytes.Split(b, []byte("\n")) {
		if len(ln)-len(bytes.TrimLeft(ln, " ")) > 4*MaxDepth {
			return ErrTooDeep
		}
	}
	return nil
}

// decodeYAMLNode parses exactly one document; nil for an empty one.
func decodeYAMLNode(b []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var n yaml.Node
	if err := dec.Decode(&n); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, yamlParseError(err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%w: multi-document YAML", ErrUnsupported)
	} else if !errors.Is(err, io.EOF) {
		return nil, yamlParseError(err)
	}
	return &n, nil
}

func yamlParseError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "already defined") {
		return fmt.Errorf("%w: %s", ErrDuplicateKey, msg)
	}
	pe := &ParseError{Format: FormatYAML, Msg: strings.TrimPrefix(msg, "yaml: ")}
	if m := yamlLine.FindStringSubmatch(msg); m != nil {
		pe.Line, _ = strconv.Atoi(m[1])
	}
	return pe
}

func (d *yamlDoc) Format() Format            { return FormatYAML }
func (d *yamlDoc) Disclosures() []Disclosure { return append([]Disclosure(nil), d.discl...) }
func (d *yamlDoc) Bytes() []byte             { return withBOM(d.bom, []byte(joinLines(d.lines))) }

func (d *yamlDoc) Value() (map[string]any, error) {
	return yamlTree(d.doc)
}

func yamlTree(doc *yaml.Node) (map[string]any, error) {
	if doc == nil || len(doc.Content) == 0 {
		return map[string]any{}, nil
	}
	var v any
	if err := doc.Content[0].Decode(&v); err != nil {
		return nil, yamlParseError(err)
	}
	n, err := normalize(v, 0)
	if err != nil {
		return nil, err
	}
	switch x := n.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return x, nil
	}
	return nil, fmt.Errorf("%w: document root is not a mapping", ErrTypeMismatch)
}

func (d *yamlDoc) Get(path Path) (any, bool, error) {
	tree, err := d.Value()
	if err != nil {
		return nil, false, err
	}
	return lookup(tree, path)
}

// rootMapping returns the root block / flow mapping, or nil for an empty
// (or null) document.
func (d *yamlDoc) rootMapping() (*yaml.Node, error) {
	if d.doc == nil || len(d.doc.Content) == 0 {
		return nil, nil
	}
	n := d.doc.Content[0]
	switch {
	case n.Kind == yaml.MappingNode:
		return n, nil
	case n.Kind == yaml.ScalarNode && n.Tag == "!!null":
		return nil, nil
	}
	return nil, fmt.Errorf("%w: document root is not a mapping", ErrTypeMismatch)
}

// pathStep is one mapping entry on the way to a target.
type pathStep struct {
	mapping  *yaml.Node
	key, val *yaml.Node
	index    int // index of key in mapping.Content
}

// walk resolves as much of path as exists. steps[i] is the entry for
// path[i]; the returned slice is shorter than path when a key is absent.
// It refuses aliases and anchors on the way (editing through them would
// change every alias of the anchored node).
func (d *yamlDoc) walk(path Path) ([]pathStep, *yaml.Node, error) {
	m, err := d.rootMapping()
	if err != nil || m == nil {
		return nil, nil, err
	}
	var steps []pathStep
	for i, seg := range path {
		found := false
		for k := 0; k+1 < len(m.Content); k += 2 {
			key := m.Content[k]
			if yamlKeyString(key) != seg {
				continue
			}
			val := m.Content[k+1]
			steps = append(steps, pathStep{mapping: m, key: key, val: val, index: k})
			found = true
			if val.Anchor != "" || key.Anchor != "" {
				return steps, m, fmt.Errorf("%w: %s is an anchored node", ErrUnsupported, path[:i+1])
			}
			if i < len(path)-1 {
				if val.Kind == yaml.AliasNode {
					return steps, m, fmt.Errorf("%w: %s is an alias", ErrUnsupported, path[:i+1])
				}
				if val.Kind == yaml.MappingNode {
					m = val
				}
			}
			break
		}
		if !found {
			return steps, m, nil
		}
		if i < len(path)-1 && steps[len(steps)-1].val.Kind != yaml.MappingNode {
			return steps, m, nil
		}
	}
	return steps, m, nil
}

// --- editing -------------------------------------------------------------

func (d *yamlDoc) Set(path Path, value any) error {
	if err := checkPath(path); err != nil {
		return err
	}
	nv, err := Normalize(value)
	if err != nil {
		return err
	}
	before, err := d.Value()
	if err != nil {
		return err
	}
	if cur, found, err := lookup(before, path); err != nil {
		return err
	} else if found && Equal(cur, nv) {
		return nil
	}
	expected, err := treeSet(before, path, nv)
	if err != nil {
		return err
	}
	steps, _, err := d.walk(path)
	if err != nil {
		return err
	}
	for _, lines := range d.surgicalSet(path, nv, steps) {
		if d.accept(lines, expected) {
			return nil
		}
	}
	return d.nodeFallback("set", path, expected, func(root *yaml.Node) error { return nodeSet(root, path, nv) })
}

func (d *yamlDoc) Delete(path Path) (bool, error) {
	if err := checkPath(path); err != nil {
		return false, err
	}
	before, err := d.Value()
	if err != nil {
		return false, err
	}
	if _, found, err := lookup(before, path); err != nil || !found {
		return false, err
	}
	expected := treeDelete(before, path)
	steps, _, err := d.walk(path)
	if err != nil {
		return true, err
	}
	if len(steps) != len(path) {
		// Present in the decoded tree but not as a written key: it comes
		// from a "<<" merge, and deleting the merge source would change
		// every mapping that merges it.
		return true, fmt.Errorf("%w: %s comes from a YAML merge key", ErrUnsupported, path)
	}
	for _, lines := range d.surgicalDelete(steps) {
		if d.accept(lines, expected) {
			return true, nil
		}
	}
	return true, d.nodeFallback("delete", path, expected, func(root *yaml.Node) error { return nodeDelete(root, path) })
}

func (d *yamlDoc) accept(lines []line, expected map[string]any) bool {
	doc, err := decodeYAMLNode([]byte(joinLines(lines)))
	if err != nil {
		return false
	}
	got, err := yamlTree(doc)
	if err != nil || !Equal(any(got), any(expected)) {
		return false
	}
	d.lines, d.doc = lines, doc
	return true
}

func (d *yamlDoc) surgicalSet(path Path, v any, steps []pathStep) [][]line {
	m, err := d.rootMapping()
	if err != nil {
		return nil
	}
	eol := dominantEOL(d.lines)
	if m == nil {
		// Empty / null document: the whole nested value as a root entry.
		entry := d.renderEntry(path[0], nestValue(path, v), "")
		at := len(d.lines)
		if d.doc != nil && len(d.doc.Content) > 0 && d.doc.Content[0].Tag == "!!null" && d.doc.Content[0].Value != "" {
			return nil // an explicit "null" / "~" root: leave it to the fallback
		}
		return [][]line{spliceLines(d.lines, at, at, textLines(entry, eol), eol)}
	}
	if len(steps) == len(path) {
		st := steps[len(steps)-1]
		return d.replaceEntry(st, v, eol)
	}
	// Some prefix exists. The deepest existing step decides.
	if n := len(steps); n > 0 {
		st := steps[n-1]
		if st.val.Kind == yaml.ScalarNode && st.val.Tag == "!!null" {
			return d.replaceEntry(st, nestValue(path[n-1:], v), eol)
		}
		if st.val.Kind != yaml.MappingNode {
			return nil // a type mismatch: lookup already refused it
		}
		if st.val.Style&yaml.FlowStyle != 0 {
			// Inserting into a flow mapping: rewrite the whole entry.
			cur, _ := yamlNodeValue(st.val)
			merged, err := treeSet(asMap(cur), path[n:], v)
			if err != nil {
				return nil
			}
			return d.replaceEntry(st, merged, eol)
		}
		return d.insertEntry(st.val, path[n], nestValue(path[n:], v), eol)
	}
	if m.Style&yaml.FlowStyle != 0 {
		return nil
	}
	return d.insertEntry(m, path[0], nestValue(path, v), eol)
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func yamlNodeValue(n *yaml.Node) (any, error) {
	var v any
	if err := n.Decode(&v); err != nil {
		return nil, err
	}
	return normalize(v, 0)
}

// replaceEntry returns candidates that replace the value of an existing
// entry: (A) an in-place scalar span replacement keeping the trailing
// comment, then (B) a rewrite of the entry's whole line extent.
func (d *yamlDoc) replaceEntry(st pathStep, v any, eol string) [][]line {
	var out [][]line
	if sc, ok := d.inlineText(v, st.val); ok {
		if l, s, e, ok := d.valueSpan(st.val); ok {
			text := d.lines[l].text
			repl := []line{{text: text[:s] + sc + text[e:], eol: d.lines[l].eol}}
			out = append(out, spliceLines(d.lines, l, l+1, repl, eol))
		}
	}
	keyLine := st.key.Line - 1
	indent, ok := d.keyIndent(st.key)
	if !ok {
		return out
	}
	end := d.extentEnd(st.key, st.val)
	entry := d.renderEntry(st.key.Value, v, indent)
	out = append(out, spliceLines(d.lines, keyLine, end+1, textLines(entry, eol), eol))
	return out
}

// insertEntry returns the candidate that appends key: v after the last
// entry of block mapping m.
func (d *yamlDoc) insertEntry(m *yaml.Node, key string, v any, eol string) [][]line {
	if m.Style&yaml.FlowStyle != 0 || len(m.Content) < 2 {
		return nil
	}
	firstKey := m.Content[0]
	indent, ok := d.keyIndent(firstKey)
	if !ok {
		return nil
	}
	lastKey, lastVal := m.Content[len(m.Content)-2], m.Content[len(m.Content)-1]
	at := d.extentEnd(lastKey, lastVal) + 1
	entry := d.renderEntry(key, v, indent)
	return [][]line{spliceLines(d.lines, at, at, textLines(entry, eol), eol)}
}

func (d *yamlDoc) surgicalDelete(steps []pathStep) [][]line {
	st := steps[len(steps)-1]
	if st.val.Anchor != "" {
		return nil
	}
	if st.mapping.Style&yaml.FlowStyle != 0 {
		return nil
	}
	eol := dominantEOL(d.lines)
	if len(st.mapping.Content) == 2 && len(steps) > 1 {
		// Removing the only entry of a nested mapping: the parent becomes
		// an empty mapping, written "{}" (a bare "key:" would be null).
		return d.replaceEntry(steps[len(steps)-2], map[string]any{}, eol)
	}
	keyLine := st.key.Line - 1
	if _, ok := d.keyIndent(st.key); !ok {
		return nil
	}
	end := d.extentEnd(st.key, st.val)
	from := keyLine
	for from > 0 && strings.HasPrefix(strings.TrimSpace(d.lines[from-1].text), "#") &&
		len(leadingWS(d.lines[from-1].text)) == len(leadingWS(d.lines[keyLine].text)) {
		from--
	}
	to := end + 1
	// Collapse the double blank line a removal between two blank lines
	// would leave behind.
	if from > 0 && isBlank(d.lines[from-1].text) && to < len(d.lines) && isBlank(d.lines[to].text) {
		to++
	}
	out := spliceLines(d.lines, from, to, nil, eol)
	if len(d.lines) > 0 && d.lines[len(d.lines)-1].eol == "" && to == len(d.lines) && len(out) > 0 {
		out[len(out)-1].eol = ""
	}
	return [][]line{out}
}

// keyIndent returns the whitespace before a key on its line; ok is false
// when something other than whitespace precedes it (a "- " sequence
// marker, a flow context).
func (d *yamlDoc) keyIndent(key *yaml.Node) (string, bool) {
	l := key.Line - 1
	if l < 0 || l >= len(d.lines) {
		return "", false
	}
	text := d.lines[l].text
	b := runeColToByte(text, key.Column-1)
	prefix := text[:b]
	if strings.TrimLeft(prefix, " ") != "" {
		return "", false
	}
	return prefix, true
}

// extentEnd returns the last line index of the entry key: val: the key
// line plus every following line indented deeper than the key (or, for a
// block sequence value, "- " items at the key's own indentation), ignoring
// trailing blank lines.
func (d *yamlDoc) extentEnd(key, val *yaml.Node) int {
	keyLine := key.Line - 1
	keyCol := len(leadingWS(d.lines[keyLine].text))
	blockSeq := val.Kind == yaml.SequenceNode && val.Style&yaml.FlowStyle == 0
	end := keyLine
	for l := keyLine + 1; l < len(d.lines); l++ {
		t := d.lines[l].text
		if isBlank(t) {
			continue
		}
		ind := len(leadingWS(t))
		tr := strings.TrimSpace(t)
		if ind > keyCol {
			end = l
			continue
		}
		if ind == keyCol && blockSeq && (strings.HasPrefix(tr, "- ") || tr == "-") {
			end = l
			continue
		}
		break
	}
	return end
}

// valueSpan locates a single-line scalar (or single-line flow collection)
// value: line index and byte range. ok is false for block scalars,
// multi-line values and nodes without a usable position.
func (d *yamlDoc) valueSpan(v *yaml.Node) (int, int, int, bool) {
	l := v.Line - 1
	if l < 0 || l >= len(d.lines) {
		return 0, 0, 0, false
	}
	text := d.lines[l].text
	s := runeColToByte(text, v.Column-1)
	if s >= len(text) {
		return 0, 0, 0, false
	}
	var end int
	switch {
	case v.Kind == yaml.ScalarNode && v.Style&yaml.DoubleQuotedStyle != 0:
		end = doubleQuotedEnd(text, s)
	case v.Kind == yaml.ScalarNode && v.Style&yaml.SingleQuotedStyle != 0:
		end = singleQuotedEnd(text, s)
	case v.Kind == yaml.ScalarNode && v.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		end = -1
	case v.Kind == yaml.ScalarNode:
		end = plainEnd(text, s, v.Tag == "!!null")
	case (v.Kind == yaml.MappingNode || v.Kind == yaml.SequenceNode) && v.Style&yaml.FlowStyle != 0:
		end = flowEnd(text, s)
	default:
		end = -1
	}
	if end < 0 {
		return 0, 0, 0, false
	}
	return l, s, end, true
}

// doubleQuotedEnd returns the end of a "..." scalar starting at s on the
// same line, or -1.
func doubleQuotedEnd(text string, s int) int {
	for j := s + 1; j < len(text); j++ {
		if text[j] == '\\' {
			j++
			continue
		}
		if text[j] == '"' {
			return j + 1
		}
	}
	return -1
}

// singleQuotedEnd returns the end of a '...' scalar (” escapes a quote).
func singleQuotedEnd(text string, s int) int {
	for j := s + 1; j < len(text); j++ {
		if text[j] != '\'' {
			continue
		}
		if j+1 < len(text) && text[j+1] == '\'' {
			j++
			continue
		}
		return j + 1
	}
	return -1
}

// plainEnd returns the end of a plain scalar: up to a " #" comment or the
// end of the line, trailing blanks excluded. An empty null value ("key:")
// has no span.
func plainEnd(text string, s int, isNull bool) int {
	if isNull && (text[s:] == "" || strings.HasPrefix(text[s:], "#")) {
		return -1
	}
	end := len(text)
	for j := s; j < len(text)-1; j++ {
		if (text[j] == ' ' || text[j] == '\t') && text[j+1] == '#' {
			end = j
			break
		}
	}
	return s + len(strings.TrimRight(text[s:end], " \t"))
}

// flowEnd returns the end of a one-line flow collection, or -1.
func flowEnd(text string, s int) int {
	depth := 0
	var quote byte
	for j := s; j < len(text); j++ {
		c := text[j]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				j++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
			if depth == 0 {
				return j + 1
			}
		}
	}
	return -1
}

// inlineText renders v for an in-place replacement of old: scalars always;
// a collection only when old was itself a flow collection (JSON is valid
// YAML flow syntax).
func (d *yamlDoc) inlineText(v any, old *yaml.Node) (string, bool) {
	switch x := v.(type) {
	case []any:
		if len(x) > 0 && old.Style&yaml.FlowStyle == 0 {
			return "", false
		}
		b, err := json.Marshal(jsonable(v))
		return string(b), err == nil
	case map[string]any:
		if len(x) > 0 && old.Style&yaml.FlowStyle == 0 {
			return "", false
		}
		b, err := json.Marshal(jsonable(v))
		return string(b), err == nil
	}
	// Keep the old scalar's quoting style for a string replacement.
	if str, ok := v.(string); ok && old.Kind == yaml.ScalarNode && !strings.ContainsAny(str, "\n\r") {
		switch {
		case old.Style&yaml.DoubleQuotedStyle != 0:
			q, err := json.Marshal(str)
			return string(q), err == nil
		case old.Style&yaml.SingleQuotedStyle != 0:
			return "'" + strings.ReplaceAll(str, "'", "''") + "'", true
		}
	}
	s, err := yamlScalar(v)
	return s, err == nil
}

// yamlScalar renders a scalar on one line.
func yamlScalar(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case []any:
		if len(x) == 0 {
			return "[]", nil
		}
	case map[string]any:
		if len(x) == 0 {
			return "{}", nil
		}
	}
	b, err := yaml.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrValueType, err)
	}
	s := strings.TrimSuffix(string(b), "\n")
	if strings.Contains(s, "\n") {
		if str, ok := v.(string); ok {
			q, _ := json.Marshal(str)
			return string(q), nil
		}
		return "", fmt.Errorf("%w: multi-line scalar", ErrValueType)
	}
	return s, nil
}

func yamlKey(k string) string {
	s, err := yamlScalar(k)
	if err != nil {
		q, _ := json.Marshal(k)
		return string(q)
	}
	return s
}

// renderEntry renders "key: value" at indent, with a block body for a
// non-empty collection.
func (d *yamlDoc) renderEntry(key string, v any, indent string) string {
	head := indent + yamlKey(key) + ":"
	switch x := v.(type) {
	case []any:
		if len(x) == 0 {
			return head + " []"
		}
	case map[string]any:
		if len(x) == 0 {
			return head + " {}"
		}
	default:
		s, err := yamlScalar(v)
		if err != nil {
			s = "null"
		}
		return head + " " + s
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(d.unit)
	_ = enc.Encode(v)
	_ = enc.Close()
	body := strings.TrimSuffix(buf.String(), "\n")
	child := indent + strings.Repeat(" ", d.unit)
	var b strings.Builder
	b.WriteString(head)
	for _, ln := range strings.Split(body, "\n") {
		b.WriteString("\n")
		if ln != "" {
			b.WriteString(child)
		}
		b.WriteString(ln)
	}
	return b.String()
}

// detectUnit returns the smallest positive indentation step between a
// mapping key and its first nested key (default 2).
func (d *yamlDoc) detectUnit() int {
	best := 0
	var visit func(n *yaml.Node, parentCol int)
	visit = func(n *yaml.Node, parentCol int) {
		if n == nil {
			return
		}
		if n.Kind == yaml.MappingNode && n.Style&yaml.FlowStyle == 0 {
			for k := 0; k+1 < len(n.Content); k += 2 {
				key := n.Content[k]
				if parentCol >= 0 && key.Column-1 > parentCol {
					step := key.Column - 1 - parentCol
					if best == 0 || step < best {
						best = step
					}
				}
				visit(n.Content[k+1], key.Column-1)
			}
			return
		}
		for _, c := range n.Content {
			visit(c, -1)
		}
	}
	if d.doc != nil {
		for _, c := range d.doc.Content {
			visit(c, -1)
		}
	}
	if best <= 0 || best > 8 {
		return 2
	}
	return best
}

func runeColToByte(s string, col int) int {
	if col <= 0 {
		return 0
	}
	b := 0
	for i := 0; i < col && b < len(s); i++ {
		_, w := utf8.DecodeRuneInString(s[b:])
		b += w
	}
	return b
}

// --- node fallback -----------------------------------------------------------

func (d *yamlDoc) nodeFallback(op string, path Path, expected map[string]any, mutate func(root *yaml.Node) error) error {
	if d.opts.ForbidReserialize {
		return fmt.Errorf("%w: %s %s", ErrNeedsReserialize, op, path)
	}
	doc := d.doc
	if doc == nil {
		doc = &yaml.Node{Kind: yaml.DocumentNode}
	}
	if len(doc.Content) == 0 || (doc.Content[0].Kind == yaml.ScalarNode && doc.Content[0].Tag == "!!null") {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	if err := mutate(doc.Content[0]); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(d.unit)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("%w: re-encode: %w", ErrValueType, err)
	}
	_ = enc.Close()
	eol := dominantEOL(d.lines)
	lines := textLines(buf.String(), eol)
	if !d.accept(lines, expected) {
		return fmt.Errorf("%w: re-encoded document differs", ErrVerify)
	}
	d.discl = append(d.discl, Disclosure{
		Op: op, Path: append(Path(nil), path...),
		Reason: "the edit goes through a flow collection, so the file was re-encoded: comments were kept, indentation and blank lines were normalized",
	})
	return nil
}

func nodeSet(m *yaml.Node, path Path, v any) error {
	for i, seg := range path {
		if m.Kind != yaml.MappingNode {
			return fmt.Errorf("%w: %s", ErrTypeMismatch, path[:i])
		}
		var val *yaml.Node
		for k := 0; k+1 < len(m.Content); k += 2 {
			if yamlKeyString(m.Content[k]) == seg {
				val = m.Content[k+1]
				break
			}
		}
		if i == len(path)-1 || val == nil || (val.Kind == yaml.ScalarNode && val.Tag == "!!null") {
			var nn yaml.Node
			if err := nn.Encode(nestValue(path[i:], v)); err != nil {
				return fmt.Errorf("%w: %w", ErrValueType, err)
			}
			if val != nil {
				*val = nn
				return nil
			}
			var kn yaml.Node
			_ = kn.Encode(seg)
			m.Content = append(m.Content, &kn, &nn)
			return nil
		}
		m = val
	}
	return nil
}

func nodeDelete(m *yaml.Node, path Path) error {
	for i, seg := range path {
		for k := 0; k+1 < len(m.Content); k += 2 {
			if yamlKeyString(m.Content[k]) != seg {
				continue
			}
			if i == len(path)-1 {
				m.Content = append(m.Content[:k], m.Content[k+2:]...)
				return nil
			}
			m = m.Content[k+1]
			break
		}
	}
	return nil
}

// yamlKeyString is a mapping key as the decoded tree spells it: a string
// key verbatim, any other scalar key (int, bool, ...) through its decoded
// value, so a path segment addresses the entry Get reports it under. A
// non-scalar key never matches.
func yamlKeyString(n *yaml.Node) string {
	if n.Kind != yaml.ScalarNode {
		return "\x00non-scalar-key"
	}
	if n.Tag == "!!str" || n.Tag == "" {
		return n.Value
	}
	var v any
	if err := n.Decode(&v); err != nil {
		return n.Value
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
