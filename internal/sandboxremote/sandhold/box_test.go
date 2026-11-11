package sandhold

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// box_test.go tests the calls the customer contract has no vector for yet
// (files and the terminal WebSocket) against an httptest fake that mirrors
// the node's handlers (control/internal/api/api.go putFile, getFile, pty and
// exec_stream.go): PUT and GET /v1/sandboxes/{id}/files?path=[&mode=], and
// GET /v1/sandboxes/{id}/pty?command= with binary frames for the terminal
// and a resize text frame.

const boxKey = "sandhold_0123456789abcdef0123456789abcdef01234567"

// boxNode is a node with one sandbox, a file store and a terminal that
// stands in for the launch script.
type boxNode struct {
	t   *testing.T
	srv *httptest.Server

	mu      sync.Mutex
	files   map[string][]byte
	modes   map[string]string
	tar     bool
	calls   []string
	keys    []string
	ptyURLs []string
	resizes []string
	input   bytes.Buffer
	// redirectPTY answers the terminal's handshake with a redirect.
	redirectPTY bool
}

var (
	readyRE    = regexp.MustCompile(`printf '%s\\n' '(observer-pipe-ready-[0-9a-f]+)'`)
	exitFileRE = regexp.MustCompile(`> '(/tmp/\.observer-exit-[0-9a-f]+)'`)
)

func newBoxNode(t *testing.T) *boxNode {
	f := &boxNode{t: t, files: map[string][]byte{}, modes: map[string]string{}, tar: true}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *boxNode) box(t *testing.T) *Box {
	b, err := NewBox(f.srv.URL, boxKey)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (f *boxNode) refuse(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (f *boxNode) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	f.keys = append(f.keys, r.Header.Get("X-API-Key"))
	f.mu.Unlock()
	if r.Header.Get("X-API-Key") != boxKey {
		f.refuse(w, http.StatusUnauthorized, "authentication required")
		return
	}
	switch {
	case r.Method == http.MethodPut && r.URL.Path == "/v1/sandboxes/sb1/files":
		p := r.URL.Query().Get("path")
		if p == "" {
			f.refuse(w, http.StatusBadRequest, "path is required")
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.files[p], f.modes[p] = b, r.URL.Query().Get("mode")
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/sandboxes/sb1/files":
		f.mu.Lock()
		b, ok := f.files[r.URL.Query().Get("path")]
		f.mu.Unlock()
		if !ok {
			f.refuse(w, http.StatusNotFound, "not found in sandbox: no such file")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(b)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/sandboxes/sb1/exec":
		f.exec(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/sandboxes/sb1/pty":
		f.pty(w, r)
	default:
		f.refuse(w, http.StatusNotFound, "sandbox not found")
	}
}

// exec answers the few commands the client runs itself.
func (f *boxNode) exec(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if r.Header.Get("Accept") != "text/event-stream" {
		f.refuse(w, http.StatusBadRequest, "this fake streams only")
		return
	}
	out, code := "", 0
	words := strings.Fields(req.Command)
	switch {
	case strings.Contains(req.Command, "command -v tar"):
		if f.tar {
			out = "yes\n"
		}
	case strings.Contains(req.Command, "tar -xf"):
		batch := strings.Trim(words[len(words)-1], "'")
		f.mu.Lock()
		data := f.files[batch]
		delete(f.files, batch)
		tr := tar.NewReader(bytes.NewReader(data))
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(tr)
			f.files["/"+h.Name], f.modes["/"+h.Name] = b, fmt.Sprintf("%o", h.Mode)
		}
		f.mu.Unlock()
	case len(words) == 4 && words[0] == "'rm'":
		f.mu.Lock()
		delete(f.files, strings.Trim(words[3], "'"))
		f.mu.Unlock()
	default:
		out = "ran " + req.Command + "\n"
	}
	w.Header().Set("Content-Type", "text/event-stream")
	ev := func(name string, v any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
	}
	_, _ = io.WriteString(w, ": waiting\n\n")
	if out != "" {
		ev("stdout", map[string]string{"text": out})
	}
	ev("stderr", map[string]string{"text": "a warning\n"})
	ev("exit", map[string]any{"exitCode": code, "durationMs": 1})
}

// pty stands in for the launch script: a pipe prints junk, then its ready
// line, then echoes its input ("quit\n" exits 0); a terminal greets, keeps
// its resizes, and exits 3 on "x".
func (f *boxNode) pty(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.ptyURLs = append(f.ptyURLs, r.URL.String())
	f.mu.Unlock()
	if f.redirectPTY {
		http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
		return
	}
	cmd := r.URL.Query().Get("command")
	script := strings.TrimPrefix(cmd, "/bin/sh ")
	f.mu.Lock()
	body, ok := f.files[script]
	f.mu.Unlock()
	if !ok {
		f.refuse(w, http.StatusBadRequest, "no such launch script")
		return
	}
	exitFile := exitFileRE.FindStringSubmatch(string(body))[1]
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := r.Context()
	setExit := func(code string) {
		f.mu.Lock()
		f.files[exitFile] = []byte(code)
		f.mu.Unlock()
	}
	if m := readyRE.FindStringSubmatch(string(body)); m != nil {
		_ = c.Write(ctx, websocket.MessageBinary, []byte("bash: some startup noise\r\n"+m[1]+"\n"))
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageBinary {
				continue
			}
			if string(data) == "quit\n" {
				setExit("0")
				_ = c.Close(websocket.StatusNormalClosure, "session ended")
				return
			}
			f.mu.Lock()
			f.input.Write(data)
			f.mu.Unlock()
			_ = c.Write(ctx, websocket.MessageBinary, data)
		}
	}
	_ = c.Write(ctx, websocket.MessageBinary, []byte("welcome\r\n"))
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageText {
			f.mu.Lock()
			f.resizes = append(f.resizes, string(data))
			f.mu.Unlock()
			continue
		}
		if string(data) == "x" {
			setExit("3")
			_ = c.Write(ctx, websocket.MessageBinary, []byte("bye\r\n"))
			_ = c.Close(websocket.StatusNormalClosure, "session ended")
			return
		}
	}
}

func TestBoxWriteAndReadFiles(t *testing.T) {
	f := newBoxNode(t)
	b := f.box(t)
	ctx := context.Background()
	one := func(p, s string, mode uint32) sandboxremote.UploadFile {
		return sandboxremote.UploadFile{Path: p, Mode: mode, Size: int64(len(s)), Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(s)), nil }}
	}
	if err := b.WriteFiles(ctx, "sb1", []sandboxremote.UploadFile{one("/tmp/observer-home/.observer-box/run/relay.x", "secret", 0o600)}); err != nil {
		t.Fatal(err)
	}
	if string(f.files["/tmp/observer-home/.observer-box/run/relay.x"]) != "secret" || f.modes["/tmp/observer-home/.observer-box/run/relay.x"] != "600" {
		t.Errorf("single file: %q mode %q", f.files["/tmp/observer-home/.observer-box/run/relay.x"], f.modes["/tmp/observer-home/.observer-box/run/relay.x"])
	}
	// Several files go as one tar batch: one PUT, one unpack.
	before := len(f.calls)
	if err := b.WriteFiles(ctx, "sb1", []sandboxremote.UploadFile{one("/workspace/a.txt", "a", 0o644), one("/workspace/bin/run", "#!/bin/sh", 0o755)}); err != nil {
		t.Fatal(err)
	}
	puts := 0
	for _, c := range f.calls[before:] {
		if strings.HasPrefix(c, "PUT ") {
			puts++
		}
	}
	if string(f.files["/workspace/a.txt"]) != "a" || string(f.files["/workspace/bin/run"]) != "#!/bin/sh" || f.modes["/workspace/bin/run"] != "755" || puts != 1 {
		t.Errorf("tar batch: files %v modes %v, %d PUTs", f.files, f.modes, puts)
	}
	for p := range f.files {
		if strings.HasPrefix(p, "/tmp/.observer-upload-") {
			t.Errorf("the batch %s was left in the box", p)
		}
	}
	// No tar in the box: one PUT a file.
	f.tar = false
	nb := f.box(t)
	if err := nb.WriteFiles(ctx, "sb1", []sandboxremote.UploadFile{one("/workspace/c", "c", 0o644), one("/workspace/d", "d", 0o644)}); err != nil || string(f.files["/workspace/d"]) != "d" {
		t.Errorf("without tar: %v", err)
	}
	got, err := b.ReadFile(ctx, "sb1", "/workspace/a.txt", 10)
	if err != nil || string(got) != "a" {
		t.Errorf("read: %q %v", got, err)
	}
	if _, err := b.ReadFile(ctx, "sb1", "/workspace/bin/run", 3); err == nil {
		t.Error("a file over the cap was read")
	}
	if _, err := b.ReadFile(ctx, "sb1", "/nope", 10); !errors.Is(err, sandboxremote.ErrNotFound) {
		t.Errorf("missing file: %v", err)
	}
	for _, k := range f.keys {
		if k != boxKey {
			t.Errorf("a call carried key %q", k)
		}
	}
}

func TestBoxExecStreamStderrAndRoot(t *testing.T) {
	f := newBoxNode(t)
	b := f.box(t)
	var stderr bytes.Buffer
	p, err := b.Exec(context.Background(), "sb1", sandboxremote.ExecSpec{Argv: []string{"echo", "it's"}, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(p)
	code, err := p.Wait(context.Background())
	if string(out) != `ran 'echo' 'it'\''s'`+"\n" || code != 0 || err != nil || stderr.String() != "a warning\n" {
		t.Errorf("got %q %d %v, stderr %q", out, code, err, stderr.String())
	}
	if _, err := b.Exec(context.Background(), "sb1", sandboxremote.ExecSpec{Argv: []string{"id"}, AsRoot: true}); !errors.Is(err, ErrNoRootExec) {
		t.Errorf("a root exec: %v", err)
	}
	if _, err := b.Exec(context.Background(), "sb1", sandboxremote.ExecSpec{Argv: []string{"id"}, Env: map[string]string{"A B": "x"}}); err == nil {
		t.Error("a bad environment name was sent")
	}
}

// TestBoxTerminal: the agent's terminal carries its output as it is, sends
// its size first, resizes and input, reports the command's exit code, and
// never puts the key or the environment in the address.
func TestBoxTerminal(t *testing.T) {
	f := newBoxNode(t)
	b := f.box(t)
	ctx := context.Background()
	p, err := b.Exec(ctx, "sb1", sandboxremote.ExecSpec{
		Argv: []string{"claude", "--model", "x y"}, Env: map[string]string{"ANTHROPIC_API_KEY": "sk-ant-very-secret"},
		Cwd: "/workspace", PTY: true, Stdin: true, Rows: 24, Cols: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Resize(ctx, 40, 120); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(p)
	code, err := p.Wait(ctx)
	if string(out) != "welcome\r\nbye\r\n" || code != 3 || err != nil {
		t.Errorf("got %q exit %d %v", out, code, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.resizes) != 2 || f.resizes[0] != `{"cols":80,"rows":24,"type":"resize"}` || f.resizes[1] != `{"cols":120,"rows":40,"type":"resize"}` {
		t.Errorf("resizes %v", f.resizes)
	}
	for _, u := range f.ptyURLs {
		if strings.Contains(u, "sk-ant") || strings.Contains(u, boxKey) || strings.Contains(u, "claude") {
			t.Errorf("the terminal's address carries what it must not: %s", u)
		}
	}
	var script string
	for path, b := range f.files {
		if strings.HasPrefix(path, "/tmp/.observer-launch-") {
			script = string(b)
			if f.modes[path] != "700" {
				t.Errorf("launch script mode %s", f.modes[path])
			}
		}
		if strings.HasPrefix(path, "/tmp/.observer-exit-") {
			t.Errorf("the exit file %s was left in the box", path)
		}
	}
	if !strings.Contains(script, "export ANTHROPIC_API_KEY='sk-ant-very-secret'") || !strings.Contains(script, "cd -- '/workspace'") || !strings.Contains(script, `'claude' '--model' 'x y'`) || strings.Contains(script, "stty") {
		t.Errorf("launch script:\n%s", script)
	}
}

// TestBoxPipe: the relay's pipe drops what the terminal printed before its
// ready line, sends nothing before it, then carries bytes both ways; a cut
// session is an error, never an exit code.
func TestBoxPipe(t *testing.T) {
	f := newBoxNode(t)
	b := f.box(t)
	ctx := context.Background()
	p, err := b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"/tmp/observer-home/.observer-box/bin/observer", "sandbox-guest", "relay-stdio"}, Stdin: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write([]byte("QUJD\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(p, buf); err != nil || string(buf) != "QUJD\n" {
		t.Fatalf("got %q %v, want only the echoed line (no startup noise, no ready line)", buf, err)
	}
	if err := p.Kill(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Wait(ctx); err == nil || !strings.Contains(err.Error(), "ended before its command did") {
		t.Errorf("a cut pipe: %v", err)
	}
	// A clean end reports its code.
	p, err = b.Exec(ctx, "sb1", sandboxremote.ExecSpec{Argv: []string{"relay"}, Stdin: true})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Write([]byte("quit\n"))
	_, _ = io.ReadAll(p)
	if code, err := p.Wait(ctx); code != 0 || err != nil {
		t.Errorf("clean end: %d %v", code, err)
	}
	var script string
	for path, b := range f.files {
		if strings.HasPrefix(path, "/tmp/.observer-launch-") && strings.Contains(string(b), "'relay' 2>/dev/null") {
			script = string(b)
		}
	}
	if !strings.Contains(script, "stty raw -echo -iexten") {
		t.Errorf("pipe script:\n%s", script)
	}
}

// TestBoxTerminalNeverFollowsARedirect: a redirect on the terminal's
// handshake is refused, and the key is never sent elsewhere.
func TestBoxTerminalNeverFollowsARedirect(t *testing.T) {
	f := newBoxNode(t)
	f.redirectPTY = true
	_, err := f.box(t).Exec(context.Background(), "sb1", sandboxremote.ExecSpec{Argv: []string{"sh"}, PTY: true, Stdin: true})
	if !errors.Is(err, ErrRedirect) {
		t.Fatalf("got %v, want ErrRedirect", err)
	}
	if strings.Contains(err.Error(), boxKey) {
		t.Error("the refusal carries the key")
	}
}

func TestBoxAddressRules(t *testing.T) {
	if _, err := NewBox("http://node.example", "k"); err == nil {
		t.Error("plain http to another machine was accepted")
	}
	if _, err := NewBox("https://node.example", ""); err == nil {
		t.Error("no key was accepted")
	}
	b, err := NewBox("https://node.example:48000", "k")
	if err != nil {
		t.Fatal(err)
	}
	u, err := b.wsURL("sb1", "/bin/sh /tmp/x")
	if err != nil || !strings.HasPrefix(u, "wss://node.example:48000/v1/sandboxes/sb1/pty?command=") {
		t.Errorf("terminal address %q %v", u, err)
	}
	if _, err := b.wsURL("../x", "sh"); err == nil {
		t.Error("a bad sandbox id became part of an address")
	}
	if s := fmt.Sprintf("%v %+v %s", b, b, b); strings.Contains(s, "k ") || strings.Contains(s, "Key") {
		t.Errorf("the client prints its key: %s", s)
	}
}

// TestBoxNodeWordsArePlain: what the node says about a sandbox reaches the
// terminal without control characters.
func TestBoxNodeWordsArePlain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"sb1","state":"Running","isolation":"vault","isolationMeans":"its own \u001b[31mkernel\u0007","network":"none","runsOn":"kata\u009b"}`)
	}))
	defer srv.Close()
	b, _ := NewBox(srv.URL, "k")
	got, err := b.Create(context.Background(), sandboxremote.BoxSpec{Tier: vocab.TierNone, Isolation: vocab.ClassHWVM, TimeoutSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(got.Described, "\x1b\x07\u009b") || got.Described != "Vault (its own [31mkernel), network none, runs on kata" {
		t.Errorf("described as %q", got.Described)
	}
}

// TestBoxCreateLostAnswer: a create whose answer never came is ErrCreateUnknown
// (the node may have made the box); one that never reached the node is not.
func TestBoxCreateLostAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, _ := w.(http.Hijacker)
		c, _, _ := hj.Hijack()
		_ = c.Close()
	}))
	b, _ := NewBox(srv.URL, "k")
	if _, err := b.Create(context.Background(), sandboxremote.BoxSpec{Tier: vocab.TierNone, TimeoutSeconds: 60}); !errors.Is(err, sandboxremote.ErrCreateUnknown) {
		t.Errorf("lost answer: %v", err)
	}
	srv.Close()
	if _, err := b.Create(context.Background(), sandboxremote.BoxSpec{Tier: vocab.TierNone, TimeoutSeconds: 60}); errors.Is(err, sandboxremote.ErrCreateUnknown) || !errors.Is(err, ErrNotSent) {
		t.Errorf("never sent: %v", err)
	}
}

// TestLaunchScriptRunsInSh runs the terminal's launch script in a real sh
// (no terminal, so the agent form): the environment, the directory, the
// argv as given and the exit code in its file.
func TestLaunchScriptRunsInSh(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	exitFile := filepath.Join(dir, "exit")
	script := filepath.Join(dir, "launch.sh")
	body := launchScript(sandboxremote.ExecSpec{
		Argv: []string{"sh", "-c", `printf '%s|%s|%s' "$GREETING" "$(pwd)" "$1"; exit 7`, "sh", "it's a b"},
		Env:  map[string]string{"GREETING": "hello 'world'"}, Cwd: dir,
	}, exitFile, "")
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	out, _ := cmd.Output()
	want := "hello 'world'|" + dir + "|it's a b"
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		want = "hello 'world'|" + real + "|it's a b"
	}
	if string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
	if code, _ := os.ReadFile(exitFile); string(code) != "7" {
		t.Errorf("exit file %q", code)
	}
	if _, err := os.Stat(script); !errors.Is(err, os.ErrNotExist) {
		t.Error("the launch script did not remove itself")
	}
	// Without a terminal the pipe form's stty fails, and says so in its code.
	body = launchScript(sandboxremote.ExecSpec{Argv: []string{"true"}}, exitFile, "observer-pipe-ready-1")
	_ = os.WriteFile(script, []byte(body), 0o700)
	c := exec.Command("sh", script)
	c.Stdin = strings.NewReader("")
	_ = c.Run()
	if code, _ := os.ReadFile(exitFile); string(code) != "125" {
		t.Errorf("pipe without a terminal: exit file %q", code)
	}
}
