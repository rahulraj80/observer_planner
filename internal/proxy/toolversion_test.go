package proxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestToolVersionFromHeaders is the UA rule table for T-784: one case per
// versioned signature row plus the shapes that must NOT yield a version.
func TestToolVersionFromHeaders(t *testing.T) {
	cases := []struct {
		name     string
		headers  map[string]string
		wantTool string
		wantVer  string
		wantOK   bool
	}{
		{"claude-cli", map[string]string{"User-Agent": "claude-cli/2.1.3 (external, cli)"}, models.ToolClaudeCode, "2.1.3", true},
		{"codex_cli_rs", map[string]string{"User-Agent": "codex_cli_rs/0.133.0 (Ubuntu 20.4.0; x86_64) xterm-256color", "Originator": "codex_cli_rs"}, models.ToolCodex, "0.133.0", true},
		{"kilo UA", map[string]string{"User-Agent": "Kilo-Code/7.3.40"}, models.ToolKiloCodeCLI, "7.3.40", true},
		{"opencode UA", map[string]string{"User-Agent": "opencode/1.14.29"}, models.ToolOpenCode, "1.14.29", true},
		{"case-insensitive prefix", map[string]string{"User-Agent": "Claude-CLI/2.1.3"}, models.ToolClaudeCode, "2.1.3", true},
		{"version cut at semicolon", map[string]string{"User-Agent": "opencode/1.14.29;bun"}, models.ToolOpenCode, "1.14.29", true},
		// Kilo attributed by X-Title but carrying an inherited opencode UA:
		// the opencode token must NOT become a Kilo version.
		{"kilo title with opencode UA", map[string]string{"X-Title": "Kilo Code", "User-Agent": "opencode/1.14.29"}, "", "", false},
		{"codex originator only", map[string]string{"Originator": "codex_cli_rs"}, "", "", false},
		{"opencode title only", map[string]string{"X-Title": "opencode"}, "", "", false},
		{"bare prefix no version", map[string]string{"User-Agent": "claude-cli/"}, "", "", false},
		{"not version-shaped", map[string]string{"User-Agent": "claude-cli/latest"}, "", "", false},
		{"no dot", map[string]string{"User-Agent": "codex_cli_rs/133"}, "", "", false},
		{"stock sdk UA", map[string]string{"User-Agent": "anthropic-sdk-typescript/0.40.0"}, "", "", false},
		{"no headers", map[string]string{}, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.headers {
				h.Set(k, v)
			}
			tool, ver, ok := toolVersionFromHeaders(h)
			if tool != tc.wantTool || ver != tc.wantVer || ok != tc.wantOK {
				t.Errorf("toolVersionFromHeaders = (%q, %q, %v), want (%q, %q, %v)", tool, ver, ok, tc.wantTool, tc.wantVer, tc.wantOK)
			}
		})
	}
}

type fakeToolVersionSink struct {
	mu      sync.Mutex
	calls   []string
	settled bool
	done    chan struct{}
}

func (f *fakeToolVersionSink) StampSessionToolVersion(_ context.Context, sessionID, tool, version string) (bool, error) {
	f.mu.Lock()
	f.calls = append(f.calls, sessionID+"|"+tool+"|"+version)
	settled := f.settled
	f.mu.Unlock()
	f.done <- struct{}{}
	return settled, nil
}

func (f *fakeToolVersionSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func waitSink(t *testing.T, f *fakeToolVersionSink) {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("sink not called")
	}
}

// TestCaptureToolVersionSessionBoundOnly pins the proxy seam: an unbound
// request never reaches the sink, a settled session is offered once, and
// an unsettled one (session row not ingested yet) is retried.
func TestCaptureToolVersionSessionBoundOnly(t *testing.T) {
	h := http.Header{}
	h.Set("User-Agent", "claude-cli/2.1.3 (external, cli)")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	sink := &fakeToolVersionSink{settled: true, done: make(chan struct{}, 4)}
	p := &Proxy{toolVersionSink: sink, logger: logger}

	p.captureToolVersion(h, "") // unbound: no call
	p.captureToolVersion(h, "sess-1")
	waitSink(t, sink)
	p.captureToolVersion(h, "sess-1") // settled: no second call
	time.Sleep(50 * time.Millisecond)
	if n := sink.count(); n != 1 {
		t.Fatalf("sink calls = %d (%v), want 1", n, sink.calls)
	}
	if sink.calls[0] != "sess-1|"+models.ToolClaudeCode+"|2.1.3" {
		t.Errorf("call = %q", sink.calls[0])
	}

	retry := &fakeToolVersionSink{settled: false, done: make(chan struct{}, 4)}
	p2 := &Proxy{toolVersionSink: retry, logger: logger}
	p2.captureToolVersion(h, "sess-2")
	waitSink(t, retry)
	// The goroutine clears the seen marker after the sink returns; poll
	// until a retry is accepted.
	deadline := time.Now().Add(5 * time.Second)
	for retry.count() < 2 && time.Now().Before(deadline) {
		p2.captureToolVersion(h, "sess-2")
		select {
		case <-retry.done:
		case <-time.After(20 * time.Millisecond):
		}
	}
	if retry.count() < 2 {
		t.Fatalf("unsettled session was not retried: calls=%v", retry.calls)
	}

	// No sink wired: a no-op, never a panic.
	(&Proxy{logger: logger}).captureToolVersion(h, "sess-3")
}

// TestCaptureToolVersionOffersAnUpgrade pins the (session, version) key: a
// session already settled on one build is offered again when its client
// comes back on a NEWER build (a mid-session upgrade, T-784 part B), and
// only once per build.
func TestCaptureToolVersionOffersAnUpgrade(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sink := &fakeToolVersionSink{settled: true, done: make(chan struct{}, 4)}
	p := &Proxy{toolVersionSink: sink, logger: logger}
	oldUA, newUA := http.Header{}, http.Header{}
	oldUA.Set("User-Agent", "claude-cli/2.1.3 (external, cli)")
	newUA.Set("User-Agent", "claude-cli/2.2.0 (external, cli)")

	p.captureToolVersion(oldUA, "sess-up")
	waitSink(t, sink)
	p.captureToolVersion(newUA, "sess-up")
	waitSink(t, sink)
	p.captureToolVersion(newUA, "sess-up") // same build again: no call
	time.Sleep(50 * time.Millisecond)
	if n := sink.count(); n != 2 {
		t.Fatalf("sink calls = %d (%v), want 2 (one per build)", n, sink.calls)
	}
	if sink.calls[1] != "sess-up|"+models.ToolClaudeCode+"|2.2.0" {
		t.Errorf("upgrade call = %q", sink.calls[1])
	}
}
