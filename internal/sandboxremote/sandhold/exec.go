package sandhold

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
)

// exec.go runs a command in a box. The node's exec has no stdin, so:
//
//   - a command with no stdin is POST /v1/sandboxes/{id}/exec with
//     Accept: text/event-stream (contract vector exec-stream): stdout and
//     stderr events as the output arrives, then the exit;
//   - a command with a terminal, or with a streaming stdin, runs on the
//     node's terminal WebSocket (term.go).

// ErrNoRootExec refuses an exec that asks for root: the node runs every
// command as its sandbox user and offers no root exec.
var ErrNoRootExec = errors.New("sandhold: the node runs every command as its sandbox user; a root exec is not offered")

// Exec starts a command.
func (b *Box) Exec(ctx context.Context, id string, spec sandboxremote.ExecSpec) (sandboxremote.Process, error) {
	if spec.AsRoot {
		return nil, ErrNoRootExec
	}
	if len(spec.Argv) == 0 {
		return nil, errors.New("sandhold: exec needs an argv")
	}
	if len(spec.StdinData) > 0 {
		return nil, fmt.Errorf("sandhold: a one-shot stdin: %w", sandboxremote.ErrNotSupported)
	}
	for k := range spec.Env {
		if !envName.MatchString(k) {
			return nil, fmt.Errorf("sandhold: %q is not an environment variable name", PlainLine(k))
		}
	}
	if spec.PTY || spec.Stdin {
		return b.startTerm(ctx, id, spec)
	}
	return b.startStream(ctx, id, spec)
}

// envName is the shape of an environment variable name.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// shellQuote quotes one word for sh.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// shellJoin quotes argv for sh, so the node's shell runs exactly that argv.
func shellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shellQuote(a)
	}
	return strings.Join(q, " ")
}

// streamProcess is a command on the node's streamed exec.
type streamProcess struct {
	cancel context.CancelFunc
	body   io.ReadCloser
	out    *outBuf
	stderr io.Writer
	done   chan struct{}
	code   int
	err    error
}

func (b *Box) startStream(ctx context.Context, id string, spec sandboxremote.ExecSpec) (sandboxremote.Process, error) {
	sp, err := sandboxPath(id)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"command": shellJoin(spec.Argv)}
	if spec.Cwd != "" {
		body["cwd"] = spec.Cwd
	}
	if len(spec.Env) > 0 {
		body["env"] = spec.Env
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithCancel(ctx)
	res, err := b.api.send(pctx, request{method: http.MethodPost, path: sp + "/exec", body: bytes.NewReader(raw), size: int64(len(raw)), contentType: "application/json", accept: "text/event-stream"})
	if err != nil {
		cancel()
		return nil, notFound(err)
	}
	p := &streamProcess{cancel: cancel, body: res.Body, out: newOutBuf(), stderr: spec.Stderr, done: make(chan struct{})}
	go p.read()
	return p, nil
}

// maxEventLine caps one event-stream line (a line of output can be long).
const maxEventLine = 16 << 20

// read parses the event stream until the exit event or the stream's end.
func (p *streamProcess) read() {
	defer close(p.done)
	defer p.out.close()
	r := bufio.NewReaderSize(p.body, 64<<10)
	event := ""
	var data strings.Builder
	exited := false
	for {
		line, err := readLine(r, maxEventLine)
		if err != nil {
			if !exited {
				p.code, p.err = -1, fmt.Errorf("the command's output stream ended before its exit: %s", PlainLine(err.Error()))
			}
			return
		}
		switch {
		case line == "":
			if event != "" || data.Len() > 0 {
				if p.dispatch(event, data.String()) {
					exited = true
					return
				}
			}
			event = ""
			data.Reset()
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
}

// dispatch handles one event; true at the exit.
func (p *streamProcess) dispatch(event, data string) bool {
	switch event {
	case "stdout", "stderr":
		var ev struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			return false
		}
		if event == "stdout" {
			// The user's own command's output, as it is.
			p.out.write([]byte(ev.Text))
		} else if p.stderr != nil {
			_, _ = io.WriteString(p.stderr, ev.Text)
		}
	case "exit":
		var ev struct {
			ExitCode int    `json:"exitCode"`
			Error    string `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			p.code, p.err = -1, errors.New("the node's exit event does not parse")
			return true
		}
		p.code = ev.ExitCode
		if ev.Error != "" && ev.ExitCode < 0 {
			p.err = errors.New("the node: " + PlainLine(ev.Error))
		}
		return true
	}
	return false
}

// readLine reads one line without its end, failing over max.
func readLine(r *bufio.Reader, max int) (string, error) {
	var b []byte
	for {
		chunk, err := r.ReadSlice('\n')
		b = append(b, chunk...)
		if len(b) > max {
			return "", errors.New("an event line is over the cap")
		}
		if err == nil {
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(b) == 0 || err != io.EOF {
			return "", err
		}
		return "", io.ErrUnexpectedEOF
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

func (p *streamProcess) Read(b []byte) (int, error) { return p.out.Read(b) }

func (p *streamProcess) Write([]byte) (int, error) {
	return 0, errors.New("sandhold: this command has no stdin")
}

func (p *streamProcess) PID() string { return "" }

func (p *streamProcess) Resize(context.Context, uint16, uint16) error {
	return fmt.Errorf("sandhold: a resize without a terminal: %w", sandboxremote.ErrNotSupported)
}

func (p *streamProcess) Wait(ctx context.Context) (int, error) {
	select {
	case <-p.done:
		return p.code, p.err
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

// Kill ends the call; whether the node then ends the command is a live
// check (the vendor table's Unverified).
func (p *streamProcess) Kill(context.Context) error {
	p.cancel()
	return nil
}

func (p *streamProcess) Close() error {
	p.cancel()
	err := p.body.Close()
	p.out.close()
	return err
}

// outBuf is a command's output between the reader of the node's stream and
// the Read of the caller: unbounded up to maxOutBuf, so a caller that reads
// only part of the output and then waits never stalls the stream (the rest
// is dropped, and the caller has stopped reading anyway).
type outBuf struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	closed bool
}

// maxOutBuf caps what is held for a slow reader.
const maxOutBuf = 64 << 20

func newOutBuf() *outBuf {
	o := &outBuf{}
	o.cond = sync.NewCond(&o.mu)
	return o
}

func (o *outBuf) write(b []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	if room := maxOutBuf - o.buf.Len(); room < len(b) {
		b = b[:max(room, 0)]
	}
	o.buf.Write(b)
	o.cond.Broadcast()
}

func (o *outBuf) close() {
	o.mu.Lock()
	o.closed = true
	o.cond.Broadcast()
	o.mu.Unlock()
}

func (o *outBuf) Read(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for o.buf.Len() == 0 && !o.closed {
		o.cond.Wait()
	}
	if o.buf.Len() == 0 {
		return 0, io.EOF
	}
	return o.buf.Read(p)
}
