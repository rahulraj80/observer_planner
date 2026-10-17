package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func walk(t *testing.T, schema string) map[string]leaf {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(schema))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]leaf{}
	for _, l := range (&walker{doc: doc}).walkRoot() {
		out[strings.Join(l.path, ".")] = l
	}
	return out
}

// TestWalkerProjection: one row per schema construct the vendor schemas
// use.
func TestWalkerProjection(t *testing.T) {
	got := walk(t, `{
	  "$defs": {
	    "Mode": {"type": "string", "enum": ["ask", "allow", "deny"], "description": "from the def"},
	    "Obj": {"type": "object", "properties": {"inner": {"type": "integer", "minimum": 1, "maximum": 9}}}
	  },
	  "definitions": {"Effort": {"type": "string", "minLength": 1}},
	  "properties": {
	    "$schema": {"type": "string"},
	    "plain": {"type": "string", "description": "  spaced\n  text  ", "default": "x"},
	    "ref": {"$ref": "#/$defs/Mode", "description": "own description wins"},
	    "allof": {"allOf": [{"$ref": "#/definitions/Effort"}], "description": "wrapped"},
	    "nullable": {"anyOf": [{"type": "boolean"}, {"type": "null"}], "default": false},
	    "typelist": {"type": ["number", "null"], "exclusiveMinimum": 0},
	    "consts": {"oneOf": [{"const": "a"}, {"const": "b"}]},
	    "freeOrEnum": {"anyOf": [{"type": "string"}, {"enum": ["known"]}]},
	    "union": {"anyOf": [{"$ref": "#/$defs/Mode"}, {"$ref": "#/$defs/Obj"}]},
	    "nested": {"type": "object", "properties": {"deep": {"type": "object", "properties": {"leaf": {"type": "boolean"}}}}},
	    "strmap": {"type": "object", "additionalProperties": {"type": "string"}},
	    "objmap": {"type": "object", "additionalProperties": {"$ref": "#/$defs/Obj"}},
	    "list": {"type": "array", "items": {"type": "string", "enum": ["x", "y"]}},
	    "objlist": {"type": "array", "items": {"type": "object"}},
	    "mixed": {"anyOf": [{"type": "string"}, {"type": "integer"}]},
	    "old": {"type": "boolean", "deprecated": true},
	    "big": {"type": "object", "additionalProperties": true, "default": {"k": "`+strings.Repeat("v", 300)+`"}},
	    "api_key": {"type": "string", "default": "never-shipped"},
	    "apiKeyHelper": {"type": "string"},
	    "env_key": {"type": "string"}
	  }
	}`)
	want := map[string]struct {
		typ  string
		enum []string
	}{
		"plain":            {"String", nil},
		"ref":              {"Enum", []string{"ask", "allow", "deny"}},
		"allof":            {"String", nil},
		"nullable":         {"Bool", nil},
		"typelist":         {"Float", nil},
		"consts":           {"Enum", []string{"a", "b"}},
		"freeOrEnum":       {"String", []string{"known"}},
		"union":            {"Enum", []string{"ask", "allow", "deny"}},
		"union.inner":      {"Int", nil},
		"nested.deep.leaf": {"Bool", nil},
		"strmap":           {"StringMap", nil},
		"objmap":           {"Object", nil},
		"list":             {"StringList", []string{"x", "y"}},
		"objlist":          {"Object", nil},
		"mixed":            {"Object", nil},
		"old":              {"Bool", nil},
		"big":              {"Object", nil},
		"api_key":          {"String", nil},
		"apiKeyHelper":     {"String", nil},
		"env_key":          {"String", nil},
	}
	if len(got) != len(want) {
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		t.Fatalf("got %d rows %v, want %d", len(got), keys, len(want))
	}
	for k, w := range want {
		l, ok := got[k]
		if !ok {
			t.Errorf("missing %s", k)
			continue
		}
		if l.typ != w.typ || !reflect.DeepEqual(l.enum, w.enum) {
			t.Errorf("%s = %s %v, want %s %v", k, l.typ, l.enum, w.typ, w.enum)
		}
	}
	if got["plain"].desc != "spaced text" || got["plain"].def != `"x"` {
		t.Errorf("plain = %+v", got["plain"])
	}
	if got["ref"].desc != "own description wins" {
		t.Errorf("ref desc = %q", got["ref"].desc)
	}
	if got["nullable"].def != "false" {
		t.Errorf("nullable default = %q", got["nullable"].def)
	}
	if l := got["typelist"]; l.min == nil || *l.min != 0 || !l.exMin {
		t.Errorf("typelist bound = %+v", l)
	}
	if l := got["union.inner"]; *l.min != 1 || *l.max != 9 || l.exMin {
		t.Errorf("inner bounds = %+v", l)
	}
	if !got["old"].deprecated {
		t.Error("deprecated flag lost")
	}
	if got["big"].def != "" {
		t.Error("an oversized default must be dropped, not truncated")
	}
	if l := got["api_key"]; !l.secret || l.def != "" {
		t.Errorf("api_key = %+v (must be secret with no default)", l)
	}
	if got["apiKeyHelper"].secret || got["env_key"].secret {
		t.Error("a command / a variable name is not a credential")
	}
	if !strings.HasPrefix(got["union.inner"].pointer, "#/properties/union") {
		t.Errorf("pointer = %q", got["union.inner"].pointer)
	}
}

func TestSecretName(t *testing.T) {
	for k, want := range map[string]bool{
		"ANTHROPIC_API_KEY": true, "ANTHROPIC_AUTH_TOKEN": true, "AWS_BEARER_TOKEN_BEDROCK": true,
		"CLAUDE_CODE_CLIENT_KEY_PASSPHRASE": true, "MCP_CLIENT_SECRET": true, "bearer_token": true, "apiKey": true,
		"token": true, "password": true,
		"tool_output_token_limit": false, "max_tokens": false, "apiKeyHelper": false, "env_key": false,
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS": false, "tokenBudget": false,
	} {
		if got := secretName(k); got != want {
			t.Errorf("secretName(%q) = %v, want %v", k, got, want)
		}
	}
}

// TestGeneratedFilesUpToDate is the in-test half of the drift gate: a
// fresh generation from the pinned schemas must equal the committed
// catalog_*.gen.go files byte for byte.
func TestGeneratedFilesUpToDate(t *testing.T) {
	files, err := generate("../../../testdata/agentconfig/schemas")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 9 {
		t.Fatalf("generated %d files, want 8 tools + the index", len(files))
	}
	for name, src := range files {
		committed, err := os.ReadFile(filepath.Join("..", name))
		if err != nil {
			t.Fatalf("%s is not committed: run make agentconfig-catalog-build", name)
		}
		if !bytes.Equal(committed, src) {
			t.Errorf("%s drifted from a fresh catgen run: run make agentconfig-catalog-build", name)
		}
	}
	// No stale generated file left behind by a removed target.
	matches, _ := filepath.Glob(filepath.Join("..", "catalog_*.gen.go"))
	if len(matches) != len(files) {
		t.Errorf("%d committed catalog_*.gen.go files, a fresh run makes %d", len(matches), len(files))
	}
}

// TestDeterministic: two runs produce identical bytes.
func TestDeterministic(t *testing.T) {
	a, err := generate("../../../testdata/agentconfig/schemas")
	if err != nil {
		t.Fatal(err)
	}
	b, err := generate("../../../testdata/agentconfig/schemas")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("generation is not deterministic")
	}
}
