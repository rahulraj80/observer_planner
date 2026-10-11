package credfile

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// bareTOMLKey matches a key TOML accepts unquoted.
var bareTOMLKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// SetTOMLTable sets the TOML table at path (for example
// ["mcp_servers", "observer"]) to value (present), or removes it
// (!present), editing orig line by line: the table's own block (its header
// through the line before the next header, plus any [path.sub] blocks) is
// replaced or cut, and every other line keeps its bytes. A new table is
// appended at the end. value holds scalars and arrays only (no nested
// tables). The result must decode to orig's values with only path changed
// (an empty parent table left behind by a removal is dropped), or ErrPatch
// is returned: a table defined some other way (an inline table, dotted
// keys) is refused rather than guessed at.
func SetTOMLTable(orig []byte, path []string, value map[string]any, present bool) ([]byte, error) {
	if len(path) == 0 {
		return nil, ErrPatch
	}
	before := map[string]any{}
	if len(bytes.TrimSpace(orig)) > 0 {
		if _, err := toml.Decode(string(orig), &before); err != nil {
			return nil, ErrPatch
		}
	}
	var block string
	if present {
		b, err := renderTOMLTable(path, value)
		if err != nil {
			return nil, err
		}
		block = b
	}

	lines := strings.SplitAfter(string(orig), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var out []string
	inserted := false
	owned := false
	for _, l := range lines {
		if keys, ok := tomlHeaderKeys(strings.TrimRight(l, "\r\n")); ok {
			owned = hasPrefix(keys, path)
			if owned && present && !inserted {
				out = append(out, block)
				inserted = true
			}
		}
		if owned {
			continue
		}
		out = append(out, l)
	}
	if present && !inserted {
		text := strings.Join(out, "")
		if text != "" && !strings.HasSuffix(text, "\n") {
			out = append(out, "\n")
		}
		if text != "" {
			out = append(out, "\n")
		}
		out = append(out, block)
	}
	result := strings.Join(out, "")

	after := map[string]any{}
	if _, err := toml.Decode(result, &after); err != nil {
		return nil, ErrPatch
	}
	want := cloneTOML(before)
	setTOMLPath(want, path, value, present)
	if !tomlValuesEqual(after, want) {
		return nil, ErrPatch
	}
	return []byte(result), nil
}

// renderTOMLTable renders [path] followed by value's keys, sorted.
func renderTOMLTable(path []string, value map[string]any) (string, error) {
	var head []string
	for _, k := range path {
		head = append(head, tomlKey(k))
	}
	var sb strings.Builder
	sb.WriteString("[" + strings.Join(head, ".") + "]\n")
	keys := make([]string, 0, len(value))
	for k := range value {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(map[string]any{"v": value[k]}); err != nil {
			return "", ErrPatch
		}
		line := strings.TrimSpace(buf.String())
		rest, ok := strings.CutPrefix(line, "v = ")
		if !ok || strings.Contains(rest, "\n") {
			return "", ErrPatch
		}
		sb.WriteString(tomlKey(k) + " = " + rest + "\n")
	}
	return sb.String(), nil
}

// tomlKey renders one key segment, quoted when it is not bare.
func tomlKey(k string) string {
	if bareTOMLKey.MatchString(k) {
		return k
	}
	b, _ := json.Marshal(k)
	return string(b)
}

// tomlHeaderKeys parses a [table] header line (not an [[array]] header)
// into its key segments. ok is false for any other line.
func tomlHeaderKeys(line string) ([]string, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "[") {
		return nil, false
	}
	if strings.HasPrefix(s, "[[") {
		// An array-of-tables header still ends the previous block.
		return []string{}, true
	}
	end := -1
	inStr := byte(0)
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr != 0:
			if c == '\\' && inStr == '"' {
				i++
			} else if c == inStr {
				inStr = 0
			}
		case c == '"' || c == '\'':
			inStr = c
		case c == ']':
			end = i
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil, false
	}
	var keys []string
	var cur strings.Builder
	inStr = 0
	body := s[1:end]
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case inStr != 0 && c == '\\' && inStr == '"' && i+1 < len(body):
			cur.WriteByte(body[i+1])
			i++
		case inStr != 0 && c == inStr:
			inStr = 0
		case inStr != 0:
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			inStr = c
		case c == '.':
			keys = append(keys, strings.TrimSpace(cur.String()))
			cur.Reset()
		case c == ' ' || c == '\t':
		default:
			cur.WriteByte(c)
		}
	}
	keys = append(keys, strings.TrimSpace(cur.String()))
	return keys, true
}

func hasPrefix(keys, path []string) bool {
	if len(keys) < len(path) {
		return false
	}
	for i := range path {
		if keys[i] != path[i] {
			return false
		}
	}
	return true
}

// setTOMLPath sets or removes path in m, dropping a parent table the
// removal leaves empty.
func setTOMLPath(m map[string]any, path []string, value map[string]any, present bool) {
	if len(path) == 1 {
		if present {
			m[path[0]] = cloneTOML(value)
		} else {
			delete(m, path[0])
		}
		return
	}
	child, _ := m[path[0]].(map[string]any)
	if child == nil {
		if !present {
			return
		}
		child = map[string]any{}
	}
	setTOMLPath(child, path[1:], value, present)
	if len(child) == 0 && !present {
		delete(m, path[0])
		return
	}
	m[path[0]] = child
}

// cloneTOML deep-copies the maps of a decoded TOML document.
func cloneTOML(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			out[k] = cloneTOML(sub)
			continue
		}
		out[k] = v
	}
	return out
}

// tomlValuesEqual compares two decoded documents through one JSON
// round-trip (so []string and []any arrays compare equal), treating an
// empty table as absent.
func tomlValuesEqual(a, b map[string]any) bool {
	ja, errA := json.Marshal(dropEmptyTables(a))
	jb, errB := json.Marshal(dropEmptyTables(b))
	if errA != nil || errB != nil {
		return false
	}
	var va, vb any
	if json.Unmarshal(ja, &va) != nil || json.Unmarshal(jb, &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

func dropEmptyTables(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			sub = dropEmptyTables(sub)
			if len(sub) == 0 {
				continue
			}
			out[k] = sub
			continue
		}
		out[k] = v
	}
	return out
}
