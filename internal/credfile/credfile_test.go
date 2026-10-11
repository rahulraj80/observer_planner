package credfile

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

//nolint:gosec // G101: a fake fixture value, not a credential.
const fake = "sk-fake-credfile-not-real"

func TestSetObjectMember(t *testing.T) {
	orig := []byte("{\n  \"b\": {\"k\": \"" + fake + "\"},\n  \"a\": 1\n}\n")
	cases := []struct {
		name    string
		key     string
		value   string
		present bool
		want    string
	}{
		{"add keeps order and bytes", "c", `{"x":1}`, true, "{\n  \"b\": {\"k\": \"" + fake + "\"},\n  \"a\": 1,\n  \"c\": {\n    \"x\": 1\n  }\n}\n"},
		{"replace only that member", "a", `2`, true, "{\n  \"b\": {\"k\": \"" + fake + "\"},\n  \"a\": 2\n}\n"},
		{"remove only that member", "a", ``, false, "{\n  \"b\": {\"k\": \"" + fake + "\"}\n}\n"},
		{"equal value is a no-op", "a", `1`, true, string(orig)},
		{"absent removal is a no-op", "zz", ``, false, string(orig)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SetObjectMember(orig, tc.key, json.RawMessage(tc.value), tc.present)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
	if got, err := SetObjectMember(nil, "o", json.RawMessage(`{}`), true); err != nil || !JSONEqual(got, json.RawMessage(`{"o":{}}`)) {
		t.Errorf("empty source = %s, %v", got, err)
	}
	if _, err := SetObjectMember([]byte("[1]"), "o", json.RawMessage(`1`), true); !errors.Is(err, ErrPatch) {
		t.Errorf("non-object = %v, want ErrPatch", err)
	}
}

func TestSetTOMLTable(t *testing.T) {
	orig := "# keep\nmodel = \"o3\"\napi_key = \"" + fake + "\"\n\n[mcp_servers.other]\ncommand = \"npx\"\n\n[z]\nq = 1\n"
	entry := map[string]any{"command": "/opt/observer", "args": []string{"serve"}}
	added, err := SetTOMLTable([]byte(orig), []string{"mcp_servers", "observer"}, entry, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(added), orig) || !strings.Contains(string(added), "[mcp_servers.observer]\nargs = [\"serve\"]\ncommand = \"/opt/observer\"\n") {
		t.Errorf("add:\n%s", added)
	}
	entry["args"] = []string{"serve", "--config", "/c.toml"}
	replaced, err := SetTOMLTable(added, []string{"mcp_servers", "observer"}, entry, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(replaced), "[mcp_servers.observer]") != 1 || !strings.Contains(string(replaced), "--config") || !strings.HasPrefix(string(replaced), orig) {
		t.Errorf("replace:\n%s", replaced)
	}
	removed, err := SetTOMLTable(replaced, []string{"mcp_servers", "observer"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimRight(string(removed), "\n") != strings.TrimRight(orig, "\n") {
		t.Errorf("remove:\n%s", removed)
	}
	// A table in the middle is replaced in place; its sub-tables go with it.
	mid := "[mcp_servers.observer]\ncommand = \"old\"\n[mcp_servers.observer.env]\nX = \"1\"\n\n[z]\nq = 1\n"
	got, err := SetTOMLTable([]byte(mid), []string{"mcp_servers", "observer"}, map[string]any{"command": "new"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "[mcp_servers.observer]\ncommand = \"new\"\n[z]\nq = 1\n" {
		t.Errorf("in place:\n%s", got)
	}
	// An inline definition is refused rather than duplicated.
	inline := "[mcp_servers]\nobserver = { command = \"" + fake + "\" }\n"
	if _, err := SetTOMLTable([]byte(inline), []string{"mcp_servers", "observer"}, entry, true); !errors.Is(err, ErrPatch) {
		t.Errorf("inline table = %v, want ErrPatch", err)
	}
}

func TestWithhold(t *testing.T) {
	var decodeErr error = &json.SyntaxError{}
	err := Withhold("mcp.register x", "/p", decodeErr)
	if !errors.Is(err, ErrDetailWithheld) || strings.Contains(err.Error(), "syntax") {
		t.Errorf("decoder error = %v, want withheld", err)
	}
	safe := SafeErrorf("refused %s", "/p")
	if got := Withhold("w", "/p", safe); !errors.Is(got, safe) || got.Error() != safe.Error() {
		t.Error("a safe error was replaced")
	}
	if Withhold("w", "/p", nil) != nil {
		t.Error("nil error was not nil")
	}
}
