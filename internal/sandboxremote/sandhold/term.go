package sandhold

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
)

// term.go runs a command on the node's terminal WebSocket
// (GET /v1/sandboxes/{id}/pty?command=, api.go pty). It carries two kinds of
// process:
//
//   - a terminal (ExecSpec.PTY): the agent, whose output passes through as
//     it is; binary frames are the terminal's input and output, a text frame
//     {"type":"resize","cols","rows"} resizes it (contract vector terminal);
//   - a pipe (ExecSpec.Stdin without PTY): the capture relay. It asks for
//     mode=pipe (contract vector terminal-pipe): binary frames in are stdin
//     and {"type":"eof"} ends it (CloseWrite), binary frames out are 0x01 +
//     stdout or 0x02 + stderr, exact bytes either way. A node from before the
//     pipe mode ignores it and gives a terminal, and a Shared sandbox refuses
//     it (pipe_unsupported, then the client asks again for a terminal): the
//     launch script then puts the terminal in raw mode with no echo before
//     the command starts, and the client discards everything before a ready
//     line and sends nothing until it has seen it, so the relay's base64
//     lines cross unchanged. The node's first text frame,
//     {"type":"connected","mode":"pipe"}, tells the two apart.
//
// What the command needs (its environment, which may hold a model key under
// in-box custody, its directory and argv) is written first as an owner-only
// launch script, so nothing but the script's path is in the address the
// node logs. The node's last text frame before the close is the exit
// {"type":"exit","exitCode","signaled"}; a node from before it sends none,
// so the script also writes the command's exit code to a file the client
// reads once the socket closes. No exit frame and no file means the session
// ended before the command did (a cut).

// The terminal's bounds (variables so tests shorten them).
var (
	// termDialWait bounds the WebSocket handshake.
	termDialWait = time.Minute
	// termReadyWait bounds the wait for a pipe's ready line.
	termReadyWait = time.Minute
)

// maxFrame is the most one input frame carries (the node reads at most
// 1 MiB a frame).
const maxFrame = 256 << 10

// termDir is where the launch script and the exit file are written.
const termDir = "/tmp"

// launchScript is the script a terminal session runs. exitFile receives the
// command's exit code; ready, when not empty, is the pipe's ready line.
func launchScript(spec sandboxremote.ExecSpec, exitFile, ready string) string {
	var b strings.Builder
	b.WriteString("rm -f -- \"$0\"\n")
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		// A name that is not a shell identifier would be read as shell.
		if !envName.MatchString(k) {
			continue
		}
		b.WriteString("export " + k + "=" + shellQuote(spec.Env[k]) + "\n")
	}
	ef := shellQuote(exitFile)
	if spec.Cwd != "" {
		b.WriteString("cd -- " + shellQuote(spec.Cwd) + " || { printf 126 > " + ef + "; exit 126; }\n")
	}
	cmd := shellJoin(spec.Argv)
	if ready != "" {
		// On a terminal (a node without the pipe mode) the terminal is made
		// a clean byte pipe and the ready line marks where the command's
		// output starts; on the node's pipe (no terminal) neither is needed,
		// and the command's stderr is kept apart from its stdout.
		b.WriteString("if [ -t 0 ]; then\n")
		b.WriteString("stty raw -echo -iexten 2>/dev/null || { printf 125 > " + ef + "; exit 125; }\n")
		b.WriteString("printf '%s\\n' " + shellQuote(ready) + "\n")
		// The relay's stderr would mix into its stream on the terminal.
		b.WriteString(cmd + " 2>/dev/null\n")
		b.WriteString("else\n" + cmd + "\nfi\n")
	} else {
		b.WriteString(cmd + "\n")
	}
	b.WriteString("c=$?\nprintf '%s' \"$c\" > " + ef + "\nexit \"$c\"\n")
	return b.String()
}

// termProcess is one terminal session.
type termProcess struct {
	b       *Box
	id      string
	ws      *websocket.Conn
	ctx     context.Context
	cancel  context.CancelFunc
	out     *outBuf
	exit    string
	pid     string
	readyCh chan struct{}
	pipe    bool
	done    chan struct{}
	// stderr receives a pipe-mode command's stderr (nil discards it).
	stderr io.Writer
	// pipeMode: the node runs the command on its pipe (mode=pipe), not a
	// terminal. Set before readyCh closes.
	pipeMode bool
	// exitSeen, exitCode and signaled are the node's exit frame, set
	// before done closes.
	exitSeen bool
	exitCode int
	signaled bool

	once sync.Once
	code int
	err  error
}

// wsURL is the terminal's address on the node: wss for an https node, ws
// only for this machine's own (NodeURL already refused plain http
// elsewhere). The key never enters it.
func (b *Box) wsURL(id, command string, pipe bool) (string, error) {
	sp, err := sandboxPath(id)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(b.api.URL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return "", errors.New("sandhold: a terminal over plain http is for localhost only")
		}
		u.Scheme = "ws"
	default:
		return "", errors.New("sandhold: the node address is not https")
	}
	u.Path = strings.TrimRight(u.Path, "/") + sp + "/pty"
	q := url.Values{"command": {command}}
	if pipe {
		q.Set("mode", "pipe")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (b *Box) startTerm(ctx context.Context, id string, spec sandboxremote.ExecSpec) (sandboxremote.Process, error) {
	n := nonce()
	script := termDir + "/.observer-launch-" + n + ".sh"
	exitFile := termDir + "/.observer-exit-" + n
	ready := ""
	pipe := !spec.PTY
	if pipe {
		ready = "observer-pipe-ready-" + n
	}
	body := launchScript(spec, exitFile, ready)
	if err := b.putFile(ctx, id, script, 0o700, int64(len(body)), strings.NewReader(body)); err != nil {
		return nil, fmt.Errorf("sandhold: write the launch script: %w", err)
	}
	askPipe := pipe
	if v, ok := b.noPipe.Load(id); ok && v.(bool) {
		askPipe = false
	}
	ws, err := b.dialTerm(ctx, id, "/bin/sh "+script, askPipe)
	if askPipe && isPipeUnsupported(err) {
		// A Shared sandbox has no pipe: the terminal with the ready line.
		b.noPipe.Store(id, true)
		ws, err = b.dialTerm(ctx, id, "/bin/sh "+script, false)
	}
	if err != nil {
		b.removeQuiet(id, script)
		return nil, err
	}
	ws.SetReadLimit(4 << 20)
	// The session outlives the call that started it (the relay's stream is
	// ended explicitly); Kill and Close end it.
	pctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	p := &termProcess{b: b, id: id, ws: ws, ctx: pctx, cancel: cancel, out: newOutBuf(), exit: exitFile, pid: n, pipe: pipe, readyCh: make(chan struct{}), done: make(chan struct{}), stderr: spec.Stderr}
	if spec.PTY && spec.Rows > 0 && spec.Cols > 0 {
		_ = p.Resize(pctx, spec.Rows, spec.Cols)
	}
	go p.read([]byte(ready))
	if pipe {
		select {
		case <-p.readyCh:
		case <-p.done:
			code, werr := p.Wait(ctx)
			if werr == nil {
				werr = fmt.Errorf("it exited %d first", code)
			}
			return nil, fmt.Errorf("sandhold: the terminal pipe did not come up: %w", werr)
		case <-time.After(termReadyWait):
			_ = p.Close()
			return nil, errors.New("sandhold: the terminal pipe did not come up in time")
		case <-ctx.Done():
			_ = p.Close()
			return nil, ctx.Err()
		}
		if p.pipeMode {
			return &pipeProcess{p}, nil
		}
	} else {
		close(p.readyCh)
	}
	return p, nil
}

// dialTerm opens the terminal WebSocket for command (mode=pipe when pipe).
func (b *Box) dialTerm(ctx context.Context, id, command string, pipe bool) (*websocket.Conn, error) {
	u, err := b.wsURL(id, command, pipe)
	if err != nil {
		return nil, err
	}
	hdr := http.Header{}
	hdr.Set("User-Agent", UserAgent)
	sp, _ := sandboxPath(id) // wsURL checked it
	if err := b.api.authorize(ctx, http.MethodGet, b.api.URL+sp+"/pty", hdr); err != nil {
		return nil, err
	}
	dctx, dcancel := context.WithTimeout(ctx, termDialWait)
	ws, res, err := websocket.Dial(dctx, u, &websocket.DialOptions{HTTPClient: b.api.httpClient(), HTTPHeader: hdr})
	dcancel()
	if err != nil {
		return nil, b.dialRefusal(res, err)
	}
	return ws, nil
}

// isPipeUnsupported reports the node's refusal of the pipe mode for this
// sandbox (the Shared tier has none).
func isPipeUnsupported(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && (ae.Code == "pipe_unsupported" || (ae.Code == "" && ae.Status == http.StatusNotImplemented))
}

// dialRefusal is a failed handshake in the node's own words.
func (b *Box) dialRefusal(res *http.Response, err error) error {
	if res != nil && res.StatusCode != http.StatusSwitchingProtocols {
		if res.StatusCode/100 == 3 {
			return fmt.Errorf("%w: %s answered %d pointing to %s; not following it with your key", ErrRedirect, b.api.URL, res.StatusCode, Plain(res.Header.Get("Location")))
		}
		var body []byte
		if res.Body != nil {
			body, _ = io.ReadAll(io.LimitReader(res.Body, 1024))
		}
		return notFound(fmt.Errorf("sandhold: the terminal: %w", refusal(res, body)))
	}
	return fmt.Errorf("%w: %s: the terminal: %s", ErrUnreachable, b.api.URL, PlainLine(rootCause(err).Error()))
}

// removeQuiet removes a file in the box, best effort.
func (b *Box) removeQuiet(id, p string) {
	ctx, cancel := context.WithTimeout(context.Background(), callWait)
	defer cancel()
	_, _, _ = b.run(ctx, id, []string{"rm", "-f", "--", p}, 256)
}

// read copies the terminal's output until the socket closes. A pipe's
// output before its ready line is discarded; on the node's pipe mode the
// output is tagged by stream and there is no ready line.
func (p *termProcess) read(ready []byte) {
	defer close(p.done)
	defer p.out.close()
	var head []byte
	waiting := len(ready) > 0
	for {
		typ, data, err := p.ws.Read(p.ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary {
			if p.control(data) && waiting {
				// The node runs the command on its pipe: no ready line.
				waiting = false
				close(p.readyCh)
			}
			continue
		}
		if p.pipeMode {
			if len(data) == 0 {
				continue
			}
			switch data[0] {
			case 0x01:
				p.out.write(data[1:])
			case 0x02:
				if p.stderr != nil {
					_, _ = p.stderr.Write(data[1:])
				}
			}
			continue
		}
		if !waiting {
			p.out.write(data)
			continue
		}
		head = append(head, data...)
		i := bytes.Index(head, ready)
		if i < 0 {
			if len(head) > 1<<20 {
				head = head[len(head)-len(ready):]
			}
			continue
		}
		rest := head[i+len(ready):]
		j := bytes.IndexByte(rest, '\n')
		if j < 0 {
			continue // the line's end has not arrived yet
		}
		waiting = false
		close(p.readyCh)
		p.out.write(rest[j+1:])
		head = nil
	}
}

// control reads one of the node's text frames: the connected frame (true
// when it says the pipe mode, for a pipe that asked for it) and the exit.
func (p *termProcess) control(data []byte) bool {
	var f struct {
		Type     string `json:"type"`
		Mode     string `json:"mode"`
		ExitCode *int   `json:"exitCode"`
		Signaled bool   `json:"signaled"`
	}
	if json.Unmarshal(data, &f) != nil {
		return false
	}
	switch f.Type {
	case "connected":
		if p.pipe && f.Mode == "pipe" && !p.pipeMode {
			p.pipeMode = true
			return true
		}
	case "exit":
		if f.ExitCode != nil {
			p.exitSeen, p.exitCode, p.signaled = true, *f.ExitCode, f.Signaled
		}
	}
	return false
}

func (p *termProcess) Read(b []byte) (int, error) { return p.out.Read(b) }

// Write sends input, in frames the node accepts; a pipe's input waits for
// its ready line.
func (p *termProcess) Write(b []byte) (int, error) {
	select {
	case <-p.readyCh:
	case <-p.done:
		return 0, io.ErrClosedPipe
	}
	n := 0
	for len(b) > 0 {
		c := min(len(b), maxFrame)
		if err := p.ws.Write(p.ctx, websocket.MessageBinary, b[:c]); err != nil {
			return n, err
		}
		n += c
		b = b[c:]
	}
	return n, nil
}

// PID is the session's own name (the node has no process id to give).
func (p *termProcess) PID() string { return p.pid }

// Resize sends the node's resize control frame.
func (p *termProcess) Resize(ctx context.Context, rows, cols uint16) error {
	msg, _ := json.Marshal(map[string]any{"type": "resize", "cols": cols, "rows": rows})
	return p.ws.Write(ctx, websocket.MessageText, msg)
}

// Wait waits for the socket to close, then takes the exit code from the
// node's exit frame, or (a node without it) from the file the script wrote.
// Neither: the session ended before the command did.
func (p *termProcess) Wait(ctx context.Context) (int, error) {
	select {
	case <-p.done:
	case <-ctx.Done():
		return -1, ctx.Err()
	}
	p.once.Do(func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callWait)
		defer cancel()
		if p.exitSeen {
			p.code = p.exitCode
			if p.signaled || p.exitCode < 0 {
				p.code, p.err = -1, errors.New("the command was ended by a signal before it exited")
			}
			// The script wrote the file too, for a node without the frame.
			p.b.removeQuiet(p.id, p.exit)
			return
		}
		b, err := p.b.ReadFile(rctx, p.id, p.exit, 32)
		if err != nil {
			p.code, p.err = -1, errors.New("the terminal session ended before its command did (the connection was cut, or the command was stopped)")
			return
		}
		code, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			p.code, p.err = -1, errors.New("the command's exit code does not parse")
			return
		}
		p.code = code
		p.b.removeQuiet(p.id, p.exit)
	})
	return p.code, p.err
}

// Kill ends the session: the node ends the command's terminal when its
// socket goes.
func (p *termProcess) Kill(context.Context) error {
	_ = p.ws.Close(websocket.StatusNormalClosure, "ended by Observer")
	p.cancel()
	return nil
}

// Close releases the socket (the node then ends the session).
func (p *termProcess) Close() error {
	_ = p.ws.CloseNow()
	p.cancel()
	return nil
}

// pipeProcess is a command on the node's pipe mode: its stdin can be ended
// on its own (sandboxremote.HalfCloser), so the relay's end is clean.
type pipeProcess struct{ *termProcess }

// CloseWrite ends the command's stdin ({"type":"eof"}); the output and the
// exit still arrive.
func (p *pipeProcess) CloseWrite() error {
	select {
	case <-p.done:
		return nil
	default:
	}
	return p.ws.Write(p.ctx, websocket.MessageText, []byte(`{"type":"eof"}`))
}

var _ sandboxremote.HalfCloser = (*pipeProcess)(nil)
