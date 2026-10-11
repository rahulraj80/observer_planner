package main

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestPickChecklist pins the checklist answers (D-571): enter keeps the rows
// as shown, numbers toggle and re-show, a / n set every row and accept, b
// steps back only where allowed, junk re-asks, and a closed stdin stops.
func TestPickChecklist(t *testing.T) {
	rows := []pickRow{{Label: "one", On: true}, {Label: "two"}, {Label: "three"}}
	cases := []struct {
		name, script string
		allowBack    bool
		want         []bool
		wantBack     bool
		wantErr      bool
	}{
		{"enter keeps defaults", "\n", false, []bool{true, false, false}, false, false},
		{"toggle then enter", "1 3\n\n", false, []bool{false, false, true}, false, false},
		{"comma toggle", "2,3\n\n", false, []bool{true, true, true}, false, false},
		{"all", "a\n", false, []bool{true, true, true}, false, false},
		{"yes means all", "y\n", false, []bool{true, true, true}, false, false},
		{"none", "n\n", false, []bool{false, false, false}, false, false},
		{"back", "b\n", true, []bool{true, false, false}, true, false},
		{"back not offered re-asks", "b\n\n", false, []bool{true, false, false}, false, false},
		{"out of range re-asks", "4\nx\n2\n\n", false, []bool{true, true, false}, false, false},
		{"closed", "", false, nil, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			got, back, err := pickChecklist(bufio.NewReader(strings.NewReader(tc.script)), &out, rows, tc.allowBack)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want err %v\n%s", err, tc.wantErr, out.String())
			}
			if tc.wantErr {
				return
			}
			if back != tc.wantBack || fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("got %v back=%v, want %v back=%v\n%s", got, back, tc.want, tc.wantBack, out.String())
			}
		})
	}
}

// TestPickFromList pins the one-value picker: a short list is a numbered
// menu with a "type it" row, a long list is searched, an exact id is taken
// at once, and no options at all falls back to a prompt.
func TestPickFromList(t *testing.T) {
	long := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		long = append(long, fmt.Sprintf("model-%02d", i))
	}
	long = append(long, "gpt-5-nano", "gpt-5-mini")
	cases := []struct {
		name, script, current string
		options               []string
		typed                 bool
		want                  string
	}{
		{"menu pick", "2\n", "", []string{"a", "b"}, true, "b"},
		{"menu default is current", "\n", "b", []string{"a", "b"}, true, "b"},
		{"menu type another", "3\nzz\n", "", []string{"a", "b"}, true, "zz"},
		{"no options prompts", "typed\n", "", nil, true, "typed"},
		{"search exact", "gpt-5-mini\n", "", long, true, "gpt-5-mini"},
		{"search narrows", "gpt\n2\n", "", long, true, "gpt-5-mini"},
		{"search too broad then narrow", "model\nmodel-1\n3\n", "", long, true, "model-12"},
		{"search keeps typed", "vendor/x\n2\n", "", long, true, "vendor/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			p := &setupPrompter{in: bufio.NewReader(strings.NewReader(tc.script)), out: &out}
			got, err := p.pickFromList("model:", tc.options, tc.current, tc.typed)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q\n%s", got, err, tc.want, out.String())
			}
		})
	}
}
