package sandhold

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
)

// contract_api_test.go replays the contract vectors of the node's
// 2026-11-11 API that the client uses: exec-bytes-stream (a one-shot stdin
// and exact bytes), sandbox-create-open-not-granted (the network note),
// the coded refusals (exec-stdin-refused, sandbox-create-open-strict), and
// the terminal's frames (terminal, terminal-pipe: the connected, eof and
// exit frames, and the pipe's tagged output).

// TestContractBoxExecBytes: a one-shot stdin is sent as the contract's
// request sends it (stdinBase64, output base64) and the exact bytes come
// back.
func TestContractBoxExecBytes(t *testing.T) {
	v := loadVector(t, "exec-bytes-stream")
	var req struct {
		StdinBase64 []byte `json:"stdinBase64"`
	}
	_ = json.Unmarshal(v.Request, &req)
	b := contractBox(t, v)
	p, err := b.Exec(context.Background(), contractID, sandboxremote.ExecSpec{Argv: []string{"cat"}, StdinData: req.StdinBase64})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	out, _ := io.ReadAll(p)
	code, err := p.Wait(context.Background())
	if !bytes.Equal(out, req.StdinBase64) || code != 0 || err != nil {
		t.Errorf("got %q exit %d %v, want the bytes given back exactly", out, code, err)
	}
}

// TestContractCodedRefusals: a refusal's code reaches the client.
func TestContractCodedRefusals(t *testing.T) {
	for name, code := range map[string]string{"exec-stdin-refused": "stdin_twice", "sandbox-create-open-strict": NetworkOpenNotGranted} {
		v := loadVector(t, name)
		var in any
		_ = json.Unmarshal(v.Request, &in)
		err := contractNode(t, v).Do(context.Background(), v.Method, strings.ReplaceAll(v.Path, "<id>", contractID), in, nil)
		var ae *APIError
		if !errors.As(err, &ae) || ae.Code != code || ae.Status != v.Status || ae.Msg == "" || ae.Fix == "" {
			t.Errorf("%s: %#v, want code %s with the node's words", name, err, code)
		}
	}
}

// TestContractBoxCreateOpenNotGranted: an open network asked for and not
// given is accepted on the internet tier (the box is deny-by-default, less
// than asked, never more) and the run says why, with the note's code.
func TestContractBoxCreateOpenNotGranted(t *testing.T) {
	v := loadVector(t, "sandbox-create-open-not-granted")
	b := contractBox(t, v)
	got, err := b.Create(context.Background(), sandboxremote.BoxSpec{TimeoutSeconds: 600, Tier: vocab.TierInternet})
	if err != nil {
		t.Fatal(err)
	}
	if got.Extra["network"] != "none" || got.Extra["network_note"] != NetworkOpenNotGranted {
		t.Errorf("extra %v", got.Extra)
	}
	want := "Shielded (a hardened container on the host's kernel), network none, runs on runc; network open was asked for, got none (network_open_not_granted): network.open was asked for and not given: runc enforces"
	if !strings.HasPrefix(got.Described, want) {
		t.Errorf("described as %q, want it to start %q", got.Described, want)
	}
	// A box with no note says nothing more.
	plain, err := contractBox(t, loadVector(t, "sandbox-create")).Create(context.Background(), sandboxremote.BoxSpec{Template: "py-tests", TimeoutSeconds: 600, Labels: map[string]string{sandboxremote.LabelKey: "run1"}, Tier: vocab.TierInternet})
	if err != nil || strings.Contains(plain.Described, "asked for") || plain.Extra["network_note"] != "" {
		t.Errorf("no note: %+v %v", plain, err)
	}
}

// frameNode replays a terminal vector's frames on the pty, after taking
// the launch script's upload; its exec answers the exit file's removal.
func frameNode(t *testing.T, v vector, wantMode string) *Box {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/files"):
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/exec"):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: exit\ndata: {\"exitCode\":0,\"durationMs\":0}\n\n")
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sandboxes/"+contractID+"/pty":
			if got := r.URL.Query().Get("mode"); got != wantMode {
				t.Errorf("%s: mode %q, the contract's call sends %q", v.Name, got, wantMode)
			}
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer c.CloseNow()
			ctx := r.Context()
			for i, fr := range v.Frames {
				switch {
				case fr.From == "node" && fr.Close != 0:
					_ = c.Close(websocket.StatusCode(fr.Close), fr.Reason)
					return
				case fr.From == "node" && fr.JSON != nil:
					_ = c.Write(ctx, websocket.MessageText, fr.JSON)
				case fr.From == "node":
					_ = c.Write(ctx, websocket.MessageBinary, fr.Binary)
				default:
					typ, data, err := c.Read(ctx)
					if err != nil {
						t.Errorf("%s frame %d: %v", v.Name, i, err)
						return
					}
					if fr.JSON != nil {
						var want, got any
						_ = json.Unmarshal(fr.JSON, &want)
						if typ != websocket.MessageText || json.Unmarshal(data, &got) != nil || !reflect.DeepEqual(got, want) {
							t.Errorf("%s frame %d: the client sent %q, the contract's client sends %s", v.Name, i, data, fr.JSON)
						}
					} else if typ != websocket.MessageBinary || !bytes.Equal(data, fr.Binary) {
						t.Errorf("%s frame %d: the client sent %q, the contract's client sends %q", v.Name, i, data, fr.Binary)
					}
				}
			}
		default:
			t.Errorf("%s: unexpected call %s %s", v.Name, r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(srv.Close)
	b, err := NewBox(srv.URL, "k")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestContractTerminalFrames: the agent's terminal sends the contract's
// resize and keystrokes, passes the output through, and takes its exit
// status from the exit frame.
func TestContractTerminalFrames(t *testing.T) {
	v := loadVector(t, "terminal")
	b := frameNode(t, v, "")
	ctx := context.Background()
	p, err := b.Exec(ctx, contractID, sandboxremote.ExecSpec{Argv: []string{"/bin/bash"}, PTY: true, Stdin: true})
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	for _, fr := range v.Frames {
		if fr.From == "node" && fr.Binary != nil {
			want.Write(fr.Binary)
		}
	}
	if err := p.Resize(ctx, 40, 120); err != nil {
		t.Fatal(err)
	}
	_, _ = p.Write([]byte("echo hi\r"))
	_, _ = p.Write([]byte("exit 3\r"))
	out, _ := io.ReadAll(p)
	code, err := p.Wait(ctx)
	if !bytes.Equal(out, want.Bytes()) || code != 3 || err != nil {
		t.Errorf("got %q exit %d %v, want %q exit 3", out, code, err, want.String())
	}
}

// TestContractTerminalPipeFrames: the relay's pipe asks for mode=pipe,
// sends stdin as binary frames and ends it with the eof frame, reads the
// tagged stdout as its exact bytes, and takes the exit frame's status.
func TestContractTerminalPipeFrames(t *testing.T) {
	v := loadVector(t, "terminal-pipe")
	b := frameNode(t, v, "pipe")
	ctx := context.Background()
	p, err := b.Exec(ctx, contractID, sandboxremote.ExecSpec{Argv: []string{"cat"}, Stdin: true})
	if err != nil {
		t.Fatal(err)
	}
	hc, ok := p.(sandboxremote.HalfCloser)
	if !ok {
		t.Fatalf("%T has no stdin close", p)
	}
	var want bytes.Buffer
	for _, fr := range v.Frames {
		switch {
		case fr.From == "client" && fr.Binary != nil:
			if _, err := p.Write(fr.Binary); err != nil {
				t.Fatal(err)
			}
		case fr.From == "node" && len(fr.Binary) > 0:
			want.Write(fr.Binary[1:])
		}
	}
	if err := hc.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(p)
	code, err := p.Wait(ctx)
	if !bytes.Equal(out, want.Bytes()) || code != 0 || err != nil {
		t.Errorf("got %q exit %d %v, want %q exit 0", out, code, err, want.Bytes())
	}
}
