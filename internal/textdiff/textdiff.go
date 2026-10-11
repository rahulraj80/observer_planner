package textdiff

import (
	"fmt"
	"strings"
)

// Op is one line operation of an edit script.
type Op byte

// The three line operations.
const (
	OpEqual  Op = ' '
	OpDelete Op = '-'
	OpInsert Op = '+'
)

// Line is one line of an edit script, without its terminator.
type Line struct {
	Op   Op
	Text string
}

// Stats counts the changed lines of a diff.
type Stats struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

// MaxEditCost bounds the Myers search (the D in O(ND)). Settings files
// are kilobytes; past this the diff degrades to "replace everything",
// which is still a correct (if unhelpful) script.
const MaxEditCost = 1000

// Lines splits text into lines without terminators. A trailing newline
// does not produce a final empty line; "\r\n" is treated as "\n".
func Lines(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

// Compute returns the line edit script turning a into b (Myers' greedy
// shortest-edit algorithm, bounded by MaxEditCost).
func Compute(a, b []string) []Line {
	// Trim the common prefix and suffix first: most settings edits touch a
	// handful of lines in the middle of a file.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	out := make([]Line, 0, len(a)+len(b))
	for _, s := range a[:pre] {
		out = append(out, Line{OpEqual, s})
	}
	out = append(out, myers(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, s := range a[len(a)-suf:] {
		out = append(out, Line{OpEqual, s})
	}
	return out
}

// myers is the core O(ND) search with a trace for backtracking.
func myers(a, b []string) []Line {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 || m == 0 {
		return replaceAll(a, b)
	}
	maxD := n + m
	if maxD > MaxEditCost {
		maxD = MaxEditCost
	}
	off := maxD + 1
	v := make([]int, 2*maxD+3)
	var trace [][]int
	found := false
	for d := 0; d <= maxD; d++ {
		snap := make([]int, len(v))
		copy(snap, v)
		trace = append(trace, snap)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return replaceAll(a, b)
	}
	return backtrack(a, b, trace, off)
}

// backtrack walks the Myers trace back from (len(a), len(b)) to (0, 0)
// and returns the edit script in forward order.
func backtrack(a, b []string, trace [][]int, off int) []Line {
	var rev []Line
	x, y := len(a), len(b)
	for d := len(trace) - 1; d >= 0 && (x > 0 || y > 0); d-- {
		vv := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && vv[off+k-1] < vv[off+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := vv[off+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
			rev = append(rev, Line{OpEqual, a[x]})
		}
		if d == 0 {
			break
		}
		if x == prevX {
			y--
			rev = append(rev, Line{OpInsert, b[y]})
		} else {
			x--
			rev = append(rev, Line{OpDelete, a[x]})
		}
	}
	for x > 0 && y > 0 {
		x--
		y--
		rev = append(rev, Line{OpEqual, a[x]})
	}
	out := make([]Line, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out
}

func replaceAll(a, b []string) []Line {
	out := make([]Line, 0, len(a)+len(b))
	for _, s := range a {
		out = append(out, Line{OpDelete, s})
	}
	for _, s := range b {
		out = append(out, Line{OpInsert, s})
	}
	return out
}

// Count returns the added / removed line counts of a script.
func Count(script []Line) Stats {
	var s Stats
	for _, l := range script {
		switch l.Op {
		case OpInsert:
			s.Added++
		case OpDelete:
			s.Removed++
		}
	}
	return s
}

// Unified renders a and b as a unified diff with ctx lines of context.
// The result is "" when the texts are line-identical. Names label the
// --- / +++ header lines.
func Unified(nameA, nameB, a, b string, ctx int) (string, Stats) {
	script := Compute(Lines(a), Lines(b))
	stats := Count(script)
	if stats.Added == 0 && stats.Removed == 0 {
		return "", stats
	}
	if ctx < 0 {
		ctx = 0
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", nameA, nameB)
	// Line numbers (1-based) of each script entry in a and b.
	type pos struct{ ai, bi int }
	positions := make([]pos, len(script))
	ai, bi := 1, 1
	for i, l := range script {
		positions[i] = pos{ai, bi}
		switch l.Op {
		case OpEqual:
			ai++
			bi++
		case OpDelete:
			ai++
		case OpInsert:
			bi++
		}
	}
	i := 0
	for i < len(script) {
		if script[i].Op == OpEqual {
			i++
			continue
		}
		start := i - ctx
		if start < 0 {
			start = 0
		}
		// Extend the hunk while changes are within 2*ctx of each other.
		end := i
		for end < len(script) {
			if script[end].Op != OpEqual {
				end++
				continue
			}
			run := end
			for run < len(script) && script[run].Op == OpEqual {
				run++
			}
			if run < len(script) && run-end <= 2*ctx {
				end = run
				continue
			}
			end += ctx
			if end > len(script) {
				end = len(script)
			}
			break
		}
		if end > len(script) {
			end = len(script)
		}
		la, lb := 0, 0
		for _, l := range script[start:end] {
			if l.Op != OpInsert {
				la++
			}
			if l.Op != OpDelete {
				lb++
			}
		}
		sa, sbb := positions[start].ai, positions[start].bi
		if la == 0 {
			sa--
		}
		if lb == 0 {
			sbb--
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", sa, la, sbb, lb)
		for _, l := range script[start:end] {
			sb.WriteByte(byte(l.Op))
			sb.WriteString(l.Text)
			sb.WriteByte('\n')
		}
		i = end
	}
	return sb.String(), stats
}
