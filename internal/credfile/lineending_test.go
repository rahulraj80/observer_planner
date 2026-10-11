package credfile

import (
	"bytes"
	"encoding/json"
	"testing"
)

// T-407: a splice that changes nothing returns the input unchanged, an
// unchanged member that the caller re-encoded keeps its source bytes, and a
// changed member follows the file's line endings.

func TestSameJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", `{"a":1}`, `{"a":1}`, true},
		{"key order and spacing", "{\r\n  \"b\": [1, 2],\r\n  \"a\": {\"x\": true}\r\n}", `{"a":{"x":true},"b":[1,2]}`, true},
		{"array order matters", `[1,2]`, `[2,1]`, false},
		{"large integers differ", `{"n":9007199254740993}`, `{"n":9007199254740992}`, false},
		{"number spelling differs", `{"n":1.0}`, `{"n":1}`, false},
		{"value differs", `{"a":1}`, `{"a":2}`, false},
		{"invalid", `{"a":`, `{"a":1}`, false},
		{"trailing value", `{"a":1} {}`, `{"a":1}`, false},
	} {
		if got := SameJSON([]byte(tc.a), []byte(tc.b)); got != tc.want {
			t.Errorf("%s: SameJSON = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestLineEnding(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"", "\n"},
		{"{}", "\n"},
		{"{\n}\n", "\n"},
		{"{\r\n}\r\n", "\r\n"},
		{"{\r\n\"a\": 1\n}\r\n", "\r\n"},
		{"{\r\n\"a\": 1\n}\n", "\n"},
	} {
		if got := LineEnding([]byte(tc.in)); got != tc.want {
			t.Errorf("LineEnding(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWithLineEnding(t *testing.T) {
	body := []byte("{\n  \"a\": 1\n}\n")
	if got := WithLineEnding([]byte("{\r\n}\r\n"), body); !bytes.Equal(got, []byte("{\r\n  \"a\": 1\r\n}\r\n")) {
		t.Errorf("CRLF orig: %q", got)
	}
	if got := WithLineEnding([]byte("{\n}\n"), []byte("{\r\n}\r\n")); !bytes.Equal(got, []byte("{\n}\n")) {
		t.Errorf("LF orig: %q", got)
	}
}

func TestSpliceObjectUnchangedIsIdentity(t *testing.T) {
	orig := []byte("{\r\n  \"env\": {\r\n    \"K\": \"v\"\r\n  },\r\n  \"hooks\": {\r\n    \"Stop\": [\r\n      {\"matcher\": \"*\", \"hooks\": []}\r\n    ]\r\n  }\r\n}\r\n")
	next := map[string]json.RawMessage{}
	if err := json.Unmarshal(orig, &next); err != nil {
		t.Fatal(err)
	}
	// The caller re-encodes the member it manages (compact, sorted keys).
	next["hooks"] = json.RawMessage(`{"Stop":[{"hooks":[],"matcher":"*"}]}`)
	got, err := SpliceObject(orig, next)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, orig) {
		t.Errorf("unchanged splice rewrote the file:\nwant %q\ngot  %q", orig, got)
	}
}

func TestSpliceObjectChangedMemberFollowsCRLF(t *testing.T) {
	orig := []byte("{\r\n  \"env\": {\r\n    \"K\": \"v\"\r\n  }\r\n}\r\n")
	next := map[string]json.RawMessage{
		"env":   json.RawMessage(`{"K":"v"}`),
		"hooks": json.RawMessage(`{"Stop":[{"matcher":"*"}]}`),
	}
	got, err := SpliceObject(orig, next)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(got, []byte("\n")) != bytes.Count(got, []byte("\r\n")) {
		t.Errorf("bare LF in a CRLF file: %q", got)
	}
	if !bytes.HasPrefix(got, []byte("{\r\n  \"env\": {\r\n    \"K\": \"v\"\r\n  }")) {
		t.Errorf("unchanged env member lost its bytes: %q", got)
	}
}
