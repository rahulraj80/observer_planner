package main

import (
	"strings"
	"testing"
)

// TestRemoveBrowserHostReportsWhatItCannotRemove pins the honest outcome:
// an empty home removes nothing, a Windows home without a registry writer
// is an error that says the keys were left in place, and the extension
// note is always present.
func TestRemoveBrowserHostReportsWhatItCannotRemove(t *testing.T) {
	home := t.TempDir()
	o := removeBrowserHost(browserHostRemoveOptions{HomeDir: home, GOOS: "linux"}, true)
	if o.Changed || o.Error != "" {
		t.Fatalf("empty home: %+v", o)
	}
	if !strings.Contains(strings.Join(o.NeedsAttention, "\n"), "extension itself keeps running") {
		t.Fatalf("missing extension note: %+v", o.NeedsAttention)
	}
	o = removeBrowserHost(browserHostRemoveOptions{HomeDir: home, GOOS: "linux", WindowsHomes: []string{t.TempDir()}}, true)
	if !strings.Contains(o.Error, "left in place") {
		t.Fatalf("no registry writer must be reported: %+v", o)
	}
}
