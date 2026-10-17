package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// clipicker: the interactive CLI pickers (D-571, lane PICKERS-2). Wherever a
// prompt's valid answers are a known set, the user picks from a numbered
// list instead of typing an id; a genuinely open value keeps a "type it"
// row. Two shapes:
//
//   - pickFromList: one value from a list (a model, a URL). Up to
//     pickMenuMax options are shown as a numbered menu; a longer list is
//     searched by typing part of a name, and the matches are shown numbered.
//   - pickChecklist: several rows at once (starter templates, feature
//     groups), each shown [x] or [ ], toggled by number, accepted with enter.

// pickMenuMax is the longest list shown whole as a numbered menu; a longer
// list is searched first.
const pickMenuMax = 12

// errStdinClosed is the one error every picker returns when input ends
// before an answer.
var errStdinClosed = fmt.Errorf("stdin closed - stopping; nothing was written")

// pickFromList asks for one value from options. current is the value in
// force (taken on enter). allowTyped keeps a "type another" row for an open
// value; without it only a listed option can be picked. With no options at
// all it falls back to a plain prompt.
func (p *setupPrompter) pickFromList(question string, options []string, current string, allowTyped bool) (string, error) {
	if len(options) == 0 {
		return p.prompt(question, current)
	}
	if len(options) <= pickMenuMax {
		menu := append([]string(nil), options...)
		if allowTyped {
			menu = append(menu, "another value (type it)")
		}
		def := 1
		for i, o := range options {
			if o == current {
				def = i + 1
			}
		}
		fmt.Fprintf(p.out, "  %s\n", strings.TrimSuffix(question, ":"))
		i, err := p.choose("pick:", menu, def)
		if err != nil {
			return "", err
		}
		if i < len(options) {
			return options[i], nil
		}
		return p.prompt("value:", current)
	}
	return p.searchList(question, options, current, allowTyped)
}

// searchList is pickFromList's long-list shape: the user types part of a
// name, the matches are shown numbered (or the count, when there are too
// many), and an exact id is taken at once.
func (p *setupPrompter) searchList(question string, options []string, current string, allowTyped bool) (string, error) {
	fmt.Fprintf(p.out, "  %s (%d known; type part of a name to list matches)\n", strings.TrimSuffix(question, ":"), len(options))
	for {
		q, err := p.prompt("search:", current)
		if err != nil {
			return "", err
		}
		q = strings.TrimSpace(q)
		if q == "" {
			fmt.Fprintln(p.out, "  type part of a name")
			continue
		}
		for _, o := range options {
			if o == q {
				return o, nil
			}
		}
		var hits []string
		for _, o := range options {
			if strings.Contains(strings.ToLower(o), strings.ToLower(q)) {
				hits = append(hits, o)
			}
		}
		switch {
		case len(hits) > pickMenuMax:
			fmt.Fprintf(p.out, "  %d match %q - type more of the name\n", len(hits), q)
			continue
		case len(hits) == 0 && !allowTyped:
			fmt.Fprintf(p.out, "  nothing matches %q - try another part of the name\n", q)
			continue
		}
		menu := append([]string(nil), hits...)
		menu = append(menu, "search again")
		if allowTyped {
			menu = append(menu, fmt.Sprintf("use %q as typed", q))
		}
		i, err := p.choose("pick:", menu, 1)
		if err != nil {
			return "", err
		}
		switch {
		case i < len(hits):
			return hits[i], nil
		case i == len(hits):
			continue
		default:
			return q, nil
		}
	}
}

// pickRow is one row of a numbered checklist.
type pickRow struct {
	Label string
	// Note is an optional second line under the label.
	Note string
	// On is the row's starting state.
	On bool
}

// pickChecklist shows rows as a numbered checklist and returns each row's
// final state. Answers: numbers toggle rows ("2 5" or "2,5") and the list is
// shown again; enter keeps the list as shown; "a" (or "y") turns every row on
// and "n" turns every row off, both accepting at once; "b" steps back when
// allowBack (back = true, states unchanged).
func pickChecklist(in *bufio.Reader, out io.Writer, rows []pickRow, allowBack bool) (states []bool, back bool, err error) {
	states = make([]bool, len(rows))
	for i, r := range rows {
		states[i] = r.On
	}
	hint := "numbers toggle, enter keeps as shown, a = all, n = none"
	if allowBack {
		hint += ", b = back"
	}
	show := true
	for {
		if show {
			for i, r := range rows {
				mark := " "
				if states[i] {
					mark = "x"
				}
				fmt.Fprintf(out, "  %d) [%s] %s\n", i+1, mark, r.Label)
				if r.Note != "" {
					fmt.Fprintf(out, "         %s\n", r.Note)
				}
			}
		}
		show = false
		fmt.Fprintf(out, "  (%s) ", hint)
		line, rerr := in.ReadString('\n')
		a := strings.ToLower(strings.TrimSpace(line))
		if a == "" {
			if rerr != nil {
				return states, false, errStdinClosed
			}
			return states, false, nil
		}
		switch a {
		case "a", "all", "y", "yes":
			for i := range states {
				states[i] = true
			}
			return states, false, nil
		case "n", "no", "none":
			for i := range states {
				states[i] = false
			}
			return states, false, nil
		case "b", "back":
			if allowBack {
				return states, true, nil
			}
		}
		nums, ok := parseRowNumbers(a, len(rows))
		if !ok {
			fmt.Fprintf(out, "  please answer row numbers from 1 to %d, or enter\n", len(rows))
			if rerr != nil {
				return states, false, errStdinClosed
			}
			continue
		}
		for _, n := range nums {
			states[n-1] = !states[n-1]
		}
		show = true
		if rerr != nil {
			return states, false, nil
		}
	}
}

// parseRowNumbers reads "2 5" / "2,5" into 1-based row numbers, all in range.
func parseRowNumbers(s string, n int) ([]int, bool) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' })
	if len(fields) == 0 {
		return nil, false
	}
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		v, err := strconv.Atoi(f)
		if err != nil || v < 1 || v > n {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}
