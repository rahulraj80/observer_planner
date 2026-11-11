package sandhold

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// TestLiveNode runs Observer's SandHold client against a real node: what
// the launch does, step by step, on every isolation the node offers. It is
// skipped unless SANDHOLD_LIVE_URL and SANDHOLD_LIVE_KEY are set (a test
// org's key on a development node; never a person's own key).
//
//	SANDHOLD_LIVE_URL=https://node:48000 SANDHOLD_LIVE_KEY=... \
//	  go test ./internal/sandboxremote/sandhold -run TestLiveNode -v
//
// SANDHOLD_LIVE_CLASSES narrows the classes (comma-separated, default
// container,userspace_kernel,hw_vm).
func TestLiveNode(t *testing.T) {
	node, key := os.Getenv("SANDHOLD_LIVE_URL"), os.Getenv("SANDHOLD_LIVE_KEY")
	if node == "" || key == "" {
		t.Skip("set SANDHOLD_LIVE_URL and SANDHOLD_LIVE_KEY to run against a live node")
	}
	classes := []vocab.IsoClass{vocab.ClassContainer, vocab.ClassUserspaceKernel, vocab.ClassHWVM}
	if s := os.Getenv("SANDHOLD_LIVE_CLASSES"); s != "" {
		classes = nil
		for _, c := range strings.Split(s, ",") {
			classes = append(classes, vocab.IsoClass(strings.TrimSpace(c)))
		}
	}
	b, err := NewBox(node, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range classes {
		t.Run(string(class), func(t *testing.T) { liveRun(t, b, class) })
	}
}

func liveRun(t *testing.T, b *Box, class vocab.IsoClass) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	run := fmt.Sprintf("live-%s-%d", strings.ReplaceAll(string(class), "_", "-"), time.Now().Unix())

	// Create: the node says what the sandbox got; less than asked is refused.
	start := time.Now()
	info, err := b.Create(ctx, sandboxremote.BoxSpec{RunID: run, Labels: map[string]string{sandboxremote.LabelKey: run},
		TimeoutSeconds: 600, Tier: vocab.TierNone, Isolation: class})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Logf("created %s in %s: %s", info.ID, time.Since(start).Round(time.Millisecond), info.Described)
	defer func() {
		dctx, dcancel := context.WithTimeout(context.Background(), time.Minute)
		defer dcancel()
		if err := b.Delete(dctx, info.ID); err != nil {
			t.Errorf("delete: %v", err)
		}
		if _, err := b.Get(dctx, info.ID); err == nil {
			t.Errorf("the sandbox is still there after delete")
		}
	}()
	if info.Described == "" {
		t.Errorf("the node did not say how the sandbox runs")
	}

	// Found again by the run's label (a lost create answer).
	found, err := b.ListByLabel(ctx, sandboxremote.LabelKey, run)
	if err != nil || len(found) != 1 || found[0].ID != info.ID {
		t.Errorf("list by label: %v %+v", err, found)
	}

	// Upload into /workspace (a tar batch when the box has tar).
	files := map[string]string{"/workspace/a.txt": "hello\n", "/workspace/dir/b.sh": "#!/bin/sh\necho from-b\n"}
	var up []sandboxremote.UploadFile
	for p, body := range files {
		body := body
		mode := uint32(0o644)
		if strings.HasSuffix(p, ".sh") {
			mode = 0o755
		}
		up = append(up, sandboxremote.UploadFile{Path: p, Mode: mode, Size: int64(len(body)),
			Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body)), nil }})
	}
	if err := b.WriteFiles(ctx, info.ID, up); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if got, err := b.ReadFile(ctx, info.ID, "/workspace/a.txt", 1<<10); err != nil || string(got) != "hello\n" {
		t.Errorf("read back: %q %v", got, err)
	}

	// Streamed exec: output, environment, directory, exit code.
	p, err := b.Exec(ctx, info.ID, sandboxremote.ExecSpec{Argv: []string{"sh", "-c", `echo "$GREETING"; pwd; ./dir/b.sh; exit 3`},
		Env: map[string]string{"GREETING": "hi there"}, Cwd: "/workspace"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	out, _ := io.ReadAll(p)
	code, err := p.Wait(ctx)
	if err != nil || code != 3 {
		t.Errorf("exec exit = %d %v", code, err)
	}
	for _, want := range []string{"hi there", "/workspace", "from-b"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("exec output lacks %q: %q", want, out)
		}
	}

	// The terminal: the owner-only launch script in /tmp, the exit file.
	tp, err := b.Exec(ctx, info.ID, sandboxremote.ExecSpec{Argv: []string{"sh", "-c", `echo "term $SECRET_NAME"; exit 7`},
		Env: map[string]string{"SECRET_NAME": "never-in-the-url"}, Cwd: "/workspace", PTY: true, Rows: 30, Cols: 100})
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	tout, _ := io.ReadAll(tp)
	tcode, err := tp.Wait(ctx)
	if err != nil || tcode != 7 {
		t.Errorf("terminal exit = %d %v (output %q)", tcode, err, tout)
	}
	if !bytes.Contains(tout, []byte("term never-in-the-url")) {
		t.Errorf("terminal output lacks the command's line: %q", tout)
	}
	// The launch script deleted itself.
	if p, err := b.Exec(ctx, info.ID, sandboxremote.ExecSpec{Argv: []string{"sh", "-c", "ls -a /tmp"}}); err == nil {
		left, _ := io.ReadAll(p)
		_, _ = p.Wait(ctx)
		if bytes.Contains(left, []byte("never-in-the-url")) || bytes.Contains(left, []byte(".observer-launch-")) {
			t.Errorf("the launch script is still in /tmp: %q", left)
		}
		t.Logf("/tmp after the terminal: %q", strings.Join(strings.Fields(string(left)), " "))
	}

	// Pipe mode (what the capture relay uses): stdin reaches the command.
	sp, err := b.Exec(ctx, info.ID, sandboxremote.ExecSpec{Argv: []string{"sh", "-c", `read line; echo "got:$line"`}, Stdin: true})
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := sp.Write([]byte("ping\n")); err != nil {
		t.Errorf("pipe write: %v", err)
	}
	sout, _ := io.ReadAll(sp)
	if scode, err := sp.Wait(ctx); err != nil || scode != 0 || !bytes.Contains(sout, []byte("got:ping")) {
		t.Errorf("pipe: exit %d %v output %q", scode, err, sout)
	}
}
