//go:build !windows

package antigravity

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDiscoverViaPowerShell_BoundedByContext pins T-557: process discovery
// shells out (powershell.exe on WSL2 / Windows, ps + lsof on macOS) and a hung
// child used to block the parse, and so daemon shutdown, for as long as the
// child ran. A fake powershell.exe that hangs (and leaves a grandchild holding
// its stdout, the WSL interop shape) must not hold discovery past the
// cancellation or the discovery bound.
func TestDiscoverViaPowerShell_BoundedByContext(t *testing.T) {
	bin := t.TempDir()
	fake := filepath.Join(bin, "powershell.exe")
	// No exec: sh stays the parent of sleep, so killing sh alone leaves
	// sleep holding the stdout pipe open.
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nsleep 20\necho never\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	t.Run("cancelled by the caller", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		start := time.Now()
		servers, err := discoverViaPowerShell(ctx)
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("discovery returned after %v, want it bounded by the 300ms context", elapsed)
		}
		if err == nil || len(servers) != 0 {
			t.Fatalf("got %d servers, err %v; want an error and nothing", len(servers), err)
		}
	})

	t.Run("bounded with no caller deadline", func(t *testing.T) {
		prev := discoveryTimeout
		discoveryTimeout = 300 * time.Millisecond
		defer func() { discoveryTimeout = prev }()
		start := time.Now()
		if _, err := discoverLanguageServersWith(context.Background(), discoverViaPowerShell); err == nil {
			t.Fatal("want an error from the timed-out discovery")
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("discovery returned after %v, want it bounded by discoveryTimeout", elapsed)
		}
	})
}
