package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/componentsvc"
)

// chooserTestService is a real componentsvc.Service over a throwaway
// config.toml with no teardown wired (nothing outside the temp dir).
func chooserTestService(t *testing.T, block string) (*componentsvc.Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(block), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := componentsvc.New(componentsvc.Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	return svc, path
}

func runChooser(t *testing.T, svc chooserService, input string, detected []string) string {
	t.Helper()
	var out bytes.Buffer
	p := &initPrompter{in: bufio.NewReader(strings.NewReader(input)), out: &out}
	if err := runComponentChooser(context.Background(), &out, p, svc, detected); err != nil {
		t.Fatalf("chooser: %v\n%s", err, out.String())
	}
	return out.String()
}

// TestComponentChooserWritesTheChoice: detected-only capture plus the
// default group answers writes a selection on the binary's own distro (the
// unstamped release build is full; it was a hard-coded developer before
// T-731, D-551) that removes the undetected adapters, after a summary and one
// confirm.
func TestComponentChooserWritesTheChoice(t *testing.T) {
	requireUnstampedBuild(t)
	requireAdapter(t, "codex")
	svc, path := chooserTestService(t, "")
	// step 1 yes, the step 2 checklist at its defaults (enter), confirm yes.
	out := runChooser(t, svc, "y\n\ny\n", []string{"claude-code"})
	for _, want := range []string{"1. Which AI tools", "detected: claude-code", "2. Which features?", "3. Summary", "selection written"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.Contains(s, `distro = "full"`) || !strings.Contains(s, "adapter.codex") || strings.Contains(s, "adapter.claude-code") {
		t.Fatalf("config:\n%s", s)
	}
	// A configured machine is never asked again.
	if again := runChooser(t, svc, "", nil); again != "" {
		t.Fatalf("chooser ran on a configured machine:\n%s", again)
	}
}

// TestComponentChooserDeclineWritesNothing: no to the confirm keeps today's
// everything-active behaviour and leaves the file untouched.
func TestComponentChooserDeclineWritesNothing(t *testing.T) {
	requireUnstampedBuild(t)
	svc, path := chooserTestService(t, "# mine\n")
	out := runChooser(t, svc, "n\n\nn\n", []string{"codex"})
	if !strings.Contains(out, "kept this build's defaults") {
		t.Fatalf("output:\n%s", out)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "# mine\n" {
		t.Fatalf("a declined chooser wrote: %q", raw)
	}
}

// TestComponentChooserCopyHasNoDashes guards the honest-copy rule.
func TestComponentChooserCopyHasNoDashes(t *testing.T) {
	svc, _ := chooserTestService(t, "")
	out := runChooser(t, svc, "y\n\nn\n", []string{"claude-code"})
	if strings.ContainsAny(out, "\u2013\u2014") {
		t.Fatalf("dash in chooser copy:\n%s", out)
	}
}
