package vendorapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// Shape is the set of schema property paths and JSON types one or more
// responses showed (plan 12A.7 shape digests). A path uses the fixture field
// ledger's notation (seats[].assignee.login). An object member whose path the
// driver does not declare in KnownPaths, and every map key, is replaced by
// the typed wildcard "*" before it is recorded, so a raw unexpected field
// name, an email used as a key or any value never reaches the set or the
// digest.
type Shape map[string]bool

// AddNDJSON folds every line of a newline-delimited JSON body (a two-step
// report file) into the shape, each line walked from the root like a body.
// A line that does not decode is recorded as "$:invalid".
func (s Shape) AddNDJSON(body []byte, known map[string]bool) {
	for _, line := range bytes.Split(body, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		s.AddBody(line, known)
	}
}

// AddBody folds one JSON body into the shape. A body that does not decode is
// recorded as the single entry "$:invalid".
func (s Shape) AddBody(body []byte, known map[string]bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		s["$:invalid"] = true
		return
	}
	s.walk("", v, known)
}

func jsonType(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case json.Number:
		return "number"
	case bool:
		return "boolean"
	}
	return "null"
}

func (s Shape) walk(path string, v any, known map[string]bool) {
	label := path
	if label == "" {
		label = "$"
	}
	s[label+":"+jsonType(v)] = true
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if !known[p] {
				p = path + ".*"
				if path == "" {
					p = "*"
				}
			}
			s.walk(p, child, known)
		}
	case []any:
		for _, child := range x {
			s.walk(path+"[]", child, known)
		}
	}
}

// Digest is the sha256 of the sorted path:type entries, hex encoded; "" for
// an empty shape.
func (s Shape) Digest() string {
	if len(s) == 0 {
		return ""
	}
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:])
}

// KnownSet turns a driver's KnownPaths into a lookup set.
func KnownSet(paths []string) map[string]bool {
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		out[p] = true
	}
	return out
}
