package main

import (
	"strings"
	"testing"
)

// TestNoToolsLineWithAll pins D-553: `observer uninstall --all` with nothing
// installed no longer tells the operator to pass --all.
func TestNoToolsLineWithAll(t *testing.T) {
	if got := noToolsLine("remove Observer from", true, "--all"); strings.Contains(got, "pass") {
		t.Fatalf("--all line asks for flags: %q", got)
	}
	if got := noToolsLine("wire", false, "--all"); !strings.Contains(got, "pass --all") {
		t.Fatalf("no-flag line lost its hint: %q", got)
	}
}
