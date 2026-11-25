//go:build linux && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
)

// TestHostedHostDropsNULFromTheBoxTerminal (live run 2026-11-11: a NUL
// came before the in-box Observer's first line): the box terminal's output
// reaches the helper's terminal without NUL bytes; the input side is left
// as it is.
func TestHostedHostDropsNULFromTheBoxTerminal(t *testing.T) {
	hh := newHostedHarness(t, sandboxremote.VendorE2B)
	code := make(chan int, 1)
	go func() { code <- hh.run.drive(context.Background()) }()
	waitFor(t, func() bool { hh.mu.Lock(); defer hh.mu.Unlock(); return len(hh.ptys) > 0 })
	_, _ = hh.termIn.Write([]byte("\x00observer: attach is not available inside the sandbox"))
	waitFor(t, func() bool { return strings.Contains(hh.out.String(), "attach is not available") })
	_, _ = hh.termIn.Write([]byte("exit"))
	select {
	case <-code:
	case <-time.After(20 * time.Second):
		t.Fatal("the run did not end")
	}
	if out := hh.out.String(); strings.ContainsRune(out, 0) {
		t.Fatalf("a NUL reached the terminal: %q", out)
	}
}
