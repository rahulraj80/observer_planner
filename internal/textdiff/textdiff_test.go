package textdiff

import (
	"math/rand"
	"strings"
	"testing"
)

func apply(a []string, script []Line) []string {
	var out []string
	ai := 0
	for _, l := range script {
		switch l.Op {
		case OpEqual:
			out = append(out, a[ai])
			ai++
		case OpDelete:
			ai++
		case OpInsert:
			out = append(out, l.Text)
		}
	}
	return out
}

func TestComputeProducesB(t *testing.T) {
	cases := []struct{ name, a, b string }{
		{"identical", "a\nb\nc\n", "a\nb\nc\n"},
		{"empty to text", "", "x\ny\n"},
		{"text to empty", "x\ny\n", ""},
		{"middle change", "a\nb\nc\nd\n", "a\nB\nc\nd\n"},
		{"insert", "a\nc\n", "a\nb\nc\n"},
		{"delete", "a\nb\nc\n", "a\nc\n"},
		{"crlf", "a\r\nb\r\n", "a\nb\n"},
		{"reorder", "a\nb\nc\nd\ne\n", "e\nd\nc\nb\na\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := Lines(c.a), Lines(c.b)
			got := apply(a, Compute(a, b))
			if strings.Join(got, "\n") != strings.Join(b, "\n") {
				t.Fatalf("apply(script) = %q, want %q", got, b)
			}
		})
	}
}

func TestComputeRandom(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	words := []string{"a", "b", "c", "d", "e"}
	for i := 0; i < 300; i++ {
		mk := func() []string {
			n := r.Intn(12)
			out := make([]string, n)
			for j := range out {
				out[j] = words[r.Intn(len(words))]
			}
			return out
		}
		a, b := mk(), mk()
		got := apply(a, Compute(a, b))
		if strings.Join(got, "|") != strings.Join(b, "|") {
			t.Fatalf("case %d: %q -> %q, got %q", i, a, b, got)
		}
	}
}

func TestUnified(t *testing.T) {
	a := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
	b := "one\ntwo\nTHREE\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\n"
	got, stats := Unified("before", "after", a, b, 1)
	if stats.Added != 2 || stats.Removed != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	want := "--- before\n+++ after\n@@ -2,3 +2,3 @@\n two\n-three\n+THREE\n four\n@@ -10,1 +10,2 @@\n ten\n+eleven\n"
	if got != want {
		t.Fatalf("unified:\n%s\nwant:\n%s", got, want)
	}
	if d, _ := Unified("a", "b", a, a, 3); d != "" {
		t.Fatalf("identical texts gave a diff: %q", d)
	}
	if d, s := Unified("a", "b", "", "x\n", 3); s.Added != 1 || !strings.Contains(d, "@@ -0,0 +1,1 @@") {
		t.Fatalf("new file diff = %q", d)
	}
}
