package telemetrywire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// jsonobj.go is a minimal byte-preserving editor for ONE JSON object: it
// finds each member's byte span so a single member can be replaced, added or
// removed while every other byte of the document stays exactly as it was
// (key order, spacing, line endings, and any credential the file holds).

// member is one key/value pair of an object with its byte offsets.
type member struct {
	key      string
	keyStart int // offset of the opening quote of the key
	valStart int // offset of the first byte of the value
	valEnd   int // offset just past the value
	raw      json.RawMessage
}

// object is one parsed object: its members and the offsets of its braces.
type object struct {
	open, close int // offsets of '{' and '}'
	members     []member
}

// errNotObject is returned when the document is not a JSON object.
var errNotObject = errors.New("not a JSON object")

// parseObject scans b, which must hold exactly one JSON object (surrounding
// whitespace allowed).
func parseObject(b []byte) (object, error) {
	var o object
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return o, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return o, errNotObject
	}
	o.open = int(dec.InputOffset()) - 1
	pos := o.open + 1
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return o, err
		}
		key, ok := kt.(string)
		if !ok {
			return o, errNotObject
		}
		keyStart := skipTo(b, pos, '"')
		if keyStart < 0 {
			return o, errNotObject
		}
		afterKey := int(dec.InputOffset())
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return o, err
		}
		valEnd := int(dec.InputOffset())
		valStart := afterKey
		for valStart < len(b) && (isSpace(b[valStart]) || b[valStart] == ':') {
			valStart++
		}
		o.members = append(o.members, member{key: key, keyStart: keyStart, valStart: valStart, valEnd: valEnd, raw: raw})
		pos = valEnd
	}
	if _, err := dec.Token(); err != nil {
		return o, err
	}
	o.close = int(dec.InputOffset()) - 1
	if o.close < 0 || o.close >= len(b) || b[o.close] != '}' {
		return o, errNotObject
	}
	// Nothing but whitespace may follow the object.
	for _, c := range b[o.close+1:] {
		if !isSpace(c) {
			return o, fmt.Errorf("trailing data after the JSON object")
		}
	}
	return o, nil
}

// find returns the member index for key, or -1.
func (o object) find(key string) int {
	for i, m := range o.members {
		if m.key == key {
			return i
		}
	}
	return -1
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// skipTo returns the index of the first c at or after from, skipping only
// whitespace and commas; -1 when another byte comes first.
func skipTo(b []byte, from int, c byte) int {
	for i := from; i < len(b); i++ {
		switch {
		case b[i] == c:
			return i
		case isSpace(b[i]) || b[i] == ',':
		default:
			return -1
		}
	}
	return -1
}

// newline returns the document's line ending ("\r\n" when it uses CRLF).
func newline(b []byte) string {
	if bytes.Contains(b, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// lineIndent returns the whitespace between the start of the line holding
// offset and offset itself.
func lineIndent(b []byte, offset int) string {
	start := offset
	for start > 0 && b[start-1] != '\n' {
		start--
	}
	ind := b[start:offset]
	for _, c := range ind {
		if c != ' ' && c != '\t' {
			return ""
		}
	}
	return string(ind)
}

// memberIndent is the indentation members of o use: the first member's, or
// fallback when o has no member (or its first member shares a line with the
// brace).
func memberIndent(b []byte, o object, fallback string) string {
	if len(o.members) > 0 {
		if ind := lineIndent(b, o.members[0].keyStart); ind != "" {
			return ind
		}
	}
	return fallback
}

// indentUnit guesses one indentation step from the document: the indent of
// the first top-level member, else two spaces.
func indentUnit(b []byte, top object) string {
	if len(top.members) > 0 {
		if ind := lineIndent(b, top.members[0].keyStart); ind != "" {
			return ind
		}
	}
	return "  "
}

// renderValue marshals v as an indented JSON value whose nested lines start
// at prefix, with nl line endings.
func renderValue(v any, prefix, unit, nl string) ([]byte, error) {
	out, err := json.MarshalIndent(v, prefix, unit)
	if err != nil {
		return nil, err
	}
	if nl != "\n" {
		out = bytes.ReplaceAll(out, []byte("\n"), []byte(nl))
	}
	return out, nil
}

// setMember returns b with o's member key set to value (raw JSON bytes,
// already indented for its position): replaced in place when present,
// appended as the last member otherwise. indent is the members' indentation
// and closeIndent the closing brace's (used only when o was empty).
func setMember(b []byte, o object, key string, value []byte, indent, closeIndent, nl string) []byte {
	if i := o.find(key); i >= 0 {
		m := o.members[i]
		return splice(b, m.valStart, m.valEnd, value)
	}
	kv := append(append([]byte(nil), quote(key)...), ": "...)
	kv = append(kv, value...)
	if len(o.members) == 0 {
		inner := []byte(nl + indent)
		inner = append(inner, kv...)
		inner = append(inner, (nl + closeIndent)...)
		return splice(b, o.open+1, o.close, inner)
	}
	last := o.members[len(o.members)-1]
	ins := []byte("," + nl + indent)
	ins = append(ins, kv...)
	return splice(b, last.valEnd, last.valEnd, ins)
}

// removeMember returns b without o's member key (b unchanged when absent).
func removeMember(b []byte, o object, key string) []byte {
	i := o.find(key)
	if i < 0 {
		return b
	}
	m := o.members[i]
	switch {
	case len(o.members) == 1:
		// The only member: keep the braces, drop what was between them,
		// keeping the closing brace on its own line when it was.
		return splice(b, o.open+1, o.close, nil)
	case i > 0:
		// Cut from the end of the previous value: its comma goes too.
		return splice(b, o.members[i-1].valEnd, m.valEnd, nil)
	default:
		// The first of several: cut up to the next key.
		return splice(b, m.keyStart, o.members[i+1].keyStart, nil)
	}
}

// splice replaces b[from:to] with with.
func splice(b []byte, from, to int, with []byte) []byte {
	out := make([]byte, 0, len(b)-(to-from)+len(with))
	out = append(out, b[:from]...)
	out = append(out, with...)
	return append(out, b[to:]...)
}

// quote renders s as a JSON string.
func quote(s string) []byte {
	out, _ := json.Marshal(s)
	return out
}
