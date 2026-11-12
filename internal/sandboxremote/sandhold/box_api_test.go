package sandhold

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
)

// box_api_test.go tests the client on a node with the 2026-11-11 API
// (SandHold 6e3b45840): exec stdin with exact bytes, the terminal's pipe
// mode, and the connected and exit frames; and that a Shared sandbox (no
// pipe) and an older node still work.

// execModern is the node's exec with stdin and exact output (exec_pipe.go):
// the command is cat-like, its stdout the stdin given.
func (f *boxNode) execModern(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var req struct {
		StdinBase64 *string `json:"stdinBase64"`
		Output      string  `json:"output"`
	}
	_ = json.Unmarshal(raw, &req)
	if req.StdinBase64 == nil && req.Output == "" {
		r.Body = io.NopCloser(bytes.NewReader(raw))
		f.exec(w, r)
		return
	}
	if f.shared {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = io.WriteString(w, `{"code":"pipe_unsupported","error":"this sandbox's isolation (the Shared tier) has no pipe mode","fix":"start the sandbox with isolation shielded"}`)
		return
	}
	in, _ := base64.StdEncoding.DecodeString(*req.StdinBase64)
	w.Header().Set("Content-Type", "text/event-stream")
	ev := func(name string, v any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
	}
	if req.Output == "base64" {
		ev("stdout", map[string]string{"base64": base64.StdEncoding.EncodeToString(in)})
		ev("stderr", map[string]string{"base64": base64.StdEncoding.EncodeToString([]byte{0xfe, '\n'})})
	} else {
		ev("stdout", map[string]string{"text": string(in)})
	}
	ev("exit", map[string]any{"exitCode": 0, "durationMs": 1})
}

// ptyModern is the node's terminal with the connected and exit frames, and
// its pipe mode (exec_pipe.go ptyPipe). The pipe echoes stdin on stdout,
// says "warn" on stderr first, and exits 0 at the eof frame. The terminal
// exits 3 on "x" and on a signal on "k". Neither writes the exit file: the
// exit frame is the only exit status.
func (f *boxNode) ptyModern(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.ptyURLs = append(f.ptyURLs, r.URL.String())
	f.mu.Unlock()
	script := strings.TrimPrefix(r.URL.Query().Get("command"), "/bin/sh ")
	f.mu.Lock()
	body, ok := f.files[script]
	f.mu.Unlock()
	if !ok {
		f.refuse(w, http.StatusBadRequest, "no such launch script")
		return
	}
	pipe := r.URL.Query().Get("mode") == "pipe"
	if pipe && f.shared {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = io.WriteString(w, `{"code":"pipe_unsupported","error":"this sandbox's isolation (the Shared tier) has no pipe mode","fix":"start the sandbox with isolation shielded"}`)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := r.Context()
	exit := func(code int) {
		b, _ := json.Marshal(map[string]any{"type": "exit", "exitCode": code, "exit_code": code, "signaled": code < 0})
		_ = c.Write(ctx, websocket.MessageText, b)
		_ = c.Close(websocket.StatusNormalClosure, "process exited")
	}
	if pipe {
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"mode":"pipe","type":"connected"}`))
		_ = c.Write(ctx, websocket.MessageBinary, []byte("\x02warn\n"))
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageText {
				if string(data) == `{"type":"eof"}` {
					exit(0)
					return
				}
				continue
			}
			f.mu.Lock()
			f.pipeIn.Write(data)
			f.mu.Unlock()
			_ = c.Write(ctx, websocket.MessageBinary, append([]byte{0x01}, data...))
		}
	}
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"connected"}`))
	if m := readyRE.FindStringSubmatch(string(body)); m != nil {
		// A pipe on a terminal (the Shared fallback): the ready line.
		_ = c.Write(ctx, websocket.MessageBinary, []byte("bash: noise\r\n"+m[1]+"\n"))
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageBinary {
				continue
			}
			if string(data) == "quit\n" {
				exit(0)
				return
			}
			_ = c.Write(ctx, websocket.MessageBinary, data)
		}
	}
	_ = c.Write(ctx, websocket.MessageBinary, []byte("welcome\r\n"))
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary {
			continue
		}
		switch string(data) {
		case "x":
			_ = c.Write(ctx, websocket.MessageBinary, []byte("bye\r\n"))
			exit(3)
			return
		case "k":
			exit(-1)
			return
		}
	}
}

func (f *boxNode) callsTo(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// TestBoxPipeMode: the relay asks for the pipe mode; its bytes cross
// exactly both ways, its stderr reaches the relay log, its stdin is ended
// on its own (HalfCloser), and its exit status is the node's exit frame.
func TestBoxPipeMode(t *testing.T) {
	f := newBoxNode(t)
	f.modern = true
	b := f.box(t)
	ctx := context.Background()
	var stderr bytes.Buffer
	p, err := b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"relay"}, Stdin: true, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	hc, ok := p.(sandboxremote.HalfCloser)
	if !ok {
		t.Fatalf("a pipe-mode process %T cannot end its stdin on its own", p)
	}
	in := "\x00\xff\r\nQUJD\n"
	if _, err := p.Write([]byte(in)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(in))
	if _, err := io.ReadFull(p, buf); err != nil || string(buf) != in {
		t.Fatalf("got %q %v, want the exact bytes back", buf, err)
	}
	if err := hc.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(p)
	if code, err := p.Wait(ctx); code != 0 || err != nil {
		t.Errorf("exit: %d %v", code, err)
	}
	if stderr.String() != "warn\n" {
		t.Errorf("stderr %q, want the node's stderr frame", stderr.String())
	}
	if !strings.Contains(f.ptyURLs[0], "mode=pipe") {
		t.Errorf("the relay did not ask for the pipe mode: %s", f.ptyURLs[0])
	}
	if n := f.callsTo("GET /v1/sandboxes/sb1/files"); n != 0 {
		t.Errorf("the exit status was read from a file (%d reads) although the node sent the exit frame", n)
	}
	for path := range f.files {
		if strings.HasPrefix(path, "/tmp/.observer-exit-") {
			t.Errorf("the exit file %s was left in the box", path)
		}
	}
}

// TestBoxPipeModeSharedFallsBack: a Shared sandbox refuses the pipe mode;
// the relay runs on a terminal with the ready line, as on an older node,
// and the next one on that sandbox does not ask again.
func TestBoxPipeModeSharedFallsBack(t *testing.T) {
	f := newBoxNode(t)
	f.modern, f.shared = true, true
	b := f.box(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		p, err := b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"relay"}, Stdin: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := p.(sandboxremote.HalfCloser); ok {
			t.Error("a process on a terminal claims a stdin it can end on its own")
		}
		if _, err := p.Write([]byte("QUJD\n")); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 5)
		if _, err := io.ReadFull(p, buf); err != nil || string(buf) != "QUJD\n" {
			t.Fatalf("got %q %v", buf, err)
		}
		_, _ = p.Write([]byte("quit\n"))
		_, _ = io.ReadAll(p)
		if code, err := p.Wait(ctx); code != 0 || err != nil {
			t.Errorf("exit: %d %v", code, err)
		}
	}
	pipes := 0
	for _, u := range f.ptyURLs {
		if strings.Contains(u, "mode=pipe") {
			pipes++
		}
	}
	if len(f.ptyURLs) != 3 || pipes != 1 {
		t.Errorf("terminal handshakes %v: want one pipe ask, refused, then terminals", f.ptyURLs)
	}
}

// TestBoxTerminalExitFrame: the agent's exit status is the node's exit
// frame (no exit file read); a command ended by a signal is an error,
// never an exit code.
func TestBoxTerminalExitFrame(t *testing.T) {
	f := newBoxNode(t)
	f.modern = true
	b := f.box(t)
	ctx := context.Background()
	for _, tc := range []struct {
		key  string
		code int
		ok   bool
	}{{"x", 3, true}, {"k", -1, false}} {
		p, err := b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"claude"}, PTY: true, Stdin: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := p.(sandboxremote.HalfCloser); ok {
			t.Error("a terminal claims a stdin it can end on its own")
		}
		_, _ = p.Write([]byte(tc.key))
		_, _ = io.ReadAll(p)
		code, err := p.Wait(ctx)
		if code != tc.code || (err == nil) != tc.ok {
			t.Errorf("%s: exit %d %v, want %d", tc.key, code, err, tc.code)
		}
		if !tc.ok && !strings.Contains(fmt.Sprint(err), "signal") {
			t.Errorf("a signal reads as %v", err)
		}
	}
	if n := f.callsTo("GET /v1/sandboxes/sb1/files"); n != 0 {
		t.Errorf("the exit status was read from a file (%d reads)", n)
	}
	if strings.Contains(strings.Join(f.ptyURLs, " "), "mode=") {
		t.Errorf("a terminal asked for a mode: %v", f.ptyURLs)
	}
}

// TestBoxExecOneShotStdin: a one-shot stdin goes as stdinBase64 and the
// output comes back as its exact bytes; a node or a sandbox that cannot
// take it says so as not supported, in its words.
func TestBoxExecOneShotStdin(t *testing.T) {
	f := newBoxNode(t)
	f.modern = true
	b := f.box(t)
	ctx := context.Background()
	in := []byte{0x00, 0xff, '\n', 'h', 'i'}
	var stderr bytes.Buffer
	p, err := b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"cat"}, StdinData: in, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(p)
	if code, err := p.Wait(ctx); code != 0 || err != nil || !bytes.Equal(out, in) || stderr.String() != "\xfe\n" {
		t.Errorf("got %q (stderr %q) exit %d %v, want the exact bytes", out, stderr.String(), code, err)
	}
	// A plain command still goes as text, without the new fields (an older
	// node refuses an unknown field).
	p, err = b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"echo"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(p)
	if code, err := p.Wait(ctx); code != 0 || err != nil {
		t.Errorf("plain exec: %d %v", code, err)
	}
	f.shared = true
	if _, err := b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"cat"}, StdinData: in}); !errors.Is(err, sandboxremote.ErrNotSupported) || !strings.Contains(err.Error(), "Shared tier") {
		t.Errorf("a Shared sandbox: %v, want ErrNotSupported in the node's words", err)
	}
	old := newBoxNode(t)
	if _, err := old.box(t).Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"cat"}, StdinData: in}); !errors.Is(err, sandboxremote.ErrNotSupported) {
		t.Errorf("an older node: %v, want ErrNotSupported", err)
	}
	if _, err := b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"cat"}, StdinData: make([]byte, MaxOneShotStdin+1)}); err == nil {
		t.Error("a one-shot stdin over the node's body limit was sent")
	}
	if _, err := b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"cat"}, StdinData: in, Stdin: true}); err == nil {
		t.Error("a one-shot stdin was taken with a streaming one")
	}
}
