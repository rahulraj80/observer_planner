package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/browserhost"
	"github.com/marmutapp/superbased-observer/internal/config"
)

// runBrowserConfig feeds one config request to `observer browser hook
// config` and returns the parsed reply and the observer dir.
func runBrowserConfig(t *testing.T, request string) (browserConfigResponse, string) {
	t.Helper()
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	toml := "[observer]\ndb_path = \"" + filepath.ToSlash(filepath.Join(tmp, "observer.db")) + "\"\n[browser]\nenabled = true\n"
	if err := os.WriteFile(configPath, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = stdinW.Write([]byte(request)); _ = stdinW.Close() }()
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin, oldStdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = stdinR, stdoutW
	t.Cleanup(func() { os.Stdin, os.Stdout = oldStdin, oldStdout })
	handleBrowserHook(context.Background(), "config", configPath)
	_ = stdoutW.Close()
	os.Stdin, os.Stdout = oldStdin, oldStdout
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(stdoutR); err != nil {
		t.Fatal(err)
	}
	var resp browserConfigResponse
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &resp); err != nil {
		t.Fatalf("config reply %q: %v", buf.String(), err)
	}
	return resp, tmp
}

// TestBrowserConfigHandshake is lane MP-ART's protocol handshake (T-601):
// the config request's version and protocol are recorded node-local, the
// reply carries the supported range, and a protocol this observer does not
// support is refused with a reason and capture off.
func TestBrowserConfigHandshake(t *testing.T) {
	cases := []struct {
		name     string
		request  string
		refused  bool
		protocol int
		version  string
	}{
		{"legacy extension", `{"type":"config"}`, false, 1, ""},
		{"current extension", `{"type":"config","protocol":2,"extension_version":"1.36.0"}`, false, 2, "1.36.0"},
		{"newer extension refused", `{"type":"config","protocol":3,"extension_version":"1.40.0"}`, true, 3, "1.40.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, dir := runBrowserConfig(t, tc.request)
			if resp.Type != "config" || resp.ProtocolMin != browserhost.ProtocolMin || resp.ProtocolMax != browserhost.ProtocolMax {
				t.Fatalf("reply %+v does not carry the protocol range", resp)
			}
			if resp.Refused != tc.refused || resp.Enabled == tc.refused {
				t.Fatalf("refused %v enabled %v, want refused %v", resp.Refused, resp.Enabled, tc.refused)
			}
			if tc.refused && !strings.Contains(resp.Reason, "update observer") {
				t.Errorf("reason %q", resp.Reason)
			}
			seen, err := browserhost.LoadSeen(dir)
			if err != nil || seen == nil {
				t.Fatalf("handshake not recorded: %v %v", seen, err)
			}
			if seen.Protocol != tc.protocol || seen.Version != tc.version || (seen.Refused != "") != tc.refused {
				t.Fatalf("record %+v", seen)
			}
			if _, err := os.Stat(filepath.Join(dir, "observer.db")); !os.IsNotExist(err) {
				t.Errorf("the config event must not open the DB")
			}
		})
	}
}

// TestIngestBrowserTurnRefusesNewerProtocol: a captured turn from an
// extension whose protocol is above the supported range is dropped with a
// recorded reason before anything is normalized or stored (both transports
// land in ingestBrowserTurn).
func TestIngestBrowserTurnRefusesNewerProtocol(t *testing.T) {
	tmp := t.TempDir()
	healthPath := filepath.Join(tmp, browserHealthFileName)
	body := []byte(`{"site":"chatgpt-web","protocol":3,"extension_version":"1.40.0"}`)
	err := ingestBrowserTurn(context.Background(), nil, body, config.BrowserConfig{Enabled: true}, healthPath)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("want a refusal, got %v", err)
	}
	hf := loadBrowserHealthFile(healthPath)
	e := hf.Sites["chatgpt-web"]
	if e.Dropped != 1 || !strings.Contains(e.LastDropReason, "newer than this observer") {
		t.Fatalf("drop not recorded: %+v", e)
	}
}
