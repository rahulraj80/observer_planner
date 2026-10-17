package browserhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Native-host protocol version handshake (modular packaging plan 3.3.1,
// lane MP-ART, T-601).
//
// Protocol 1 is every extension that predates the handshake: it sends no
// version and no protocol. Protocol 2 adds them: the extension puts
// "protocol" and "extension_version" on every message (the config request,
// health beacons and captured turns), and the host answers the config
// request with the protocol range it supports. The host records the last
// config handshake (SeenFileName, node-local, beside the DB) so `observer
// components artifacts` and `observer doctor` can say whether the
// extension is connected, which version it runs and whether it is too old
// (stale) or too new (incompatible). An extension whose protocol is above
// ProtocolMax is refused with a reason: its config reply disables capture
// and its captured turns are dropped with a recorded reason, never
// half-understood.
//
// browser-extension/src/service-worker.js carries the twin of
// ProtocolCurrent (SBO_PROTOCOL); a test byte-checks the two agree.
const (
	// ProtocolMin is the oldest extension protocol this host accepts.
	ProtocolMin = 1
	// ProtocolMax is the newest extension protocol this host accepts.
	ProtocolMax = 2
	// ProtocolCurrent is the protocol this release's extension speaks.
	ProtocolCurrent = 2
	// SeenFileName is the node-local handshake record, next to the DB.
	SeenFileName = "browser-extension.json"
	// maxVersionLen bounds a reported version string.
	maxVersionLen = 64
)

// Hello is the handshake part of any extension message.
type Hello struct {
	// Protocol is the extension's protocol; 1 when the message has none.
	Protocol int
	// ExtensionVersion is the extension's manifest version, sanitized; ""
	// when the message has none or it was not a plain version string.
	ExtensionVersion string
}

// ParseHello reads the handshake fields from a message body. A missing,
// malformed or non-positive protocol reads as protocol 1 (an extension that
// predates the handshake); a malformed version reads as "".
func ParseHello(body []byte) Hello {
	var raw struct {
		Protocol         json.RawMessage `json:"protocol"`
		ExtensionVersion json.RawMessage `json:"extension_version"`
	}
	h := Hello{Protocol: 1}
	if json.Unmarshal(body, &raw) != nil {
		return h
	}
	if len(raw.Protocol) > 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(string(raw.Protocol))); err == nil && n > 0 {
			h.Protocol = n
		}
	}
	var v string
	if len(raw.ExtensionVersion) > 0 && json.Unmarshal(raw.ExtensionVersion, &v) == nil {
		h.ExtensionVersion = sanitizeVersion(v)
	}
	return h
}

// sanitizeVersion keeps a version string only when it is short and made of
// version characters, so a hostile extension cannot write arbitrary text
// into the record or the doctor output.
func sanitizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > maxVersionLen {
		return ""
	}
	for _, r := range v {
		ok := r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '.' || r == '-' || r == '+'
		if !ok {
			return ""
		}
	}
	return v
}

// Refusal returns the reason the host refuses protocol p, or "" when it is
// accepted. An older protocol is accepted while it is in range; a newer one
// is always refused (this binary cannot know what it means).
func Refusal(p int) string {
	switch {
	case p > ProtocolMax:
		return fmt.Sprintf("extension protocol %d is newer than this observer supports (%d to %d): update observer", p, ProtocolMin, ProtocolMax)
	case p < ProtocolMin:
		return fmt.Sprintf("extension protocol %d is older than this observer supports (%d to %d): update the extension", p, ProtocolMin, ProtocolMax)
	}
	return ""
}

// Seen is the recorded handshake.
type Seen struct {
	Version     string `json:"version,omitempty"`
	Protocol    int    `json:"protocol"`
	FirstSeenMs int64  `json:"first_seen_ms"`
	LastSeenMs  int64  `json:"last_seen_ms"`
	// Refused carries the refusal reason when the last handshake was
	// refused.
	Refused string `json:"refused,omitempty"`
}

// LoadSeen reads the handshake record in dir. A missing file returns
// (nil, nil); an unreadable or corrupt one returns an error.
func LoadSeen(dir string) (*Seen, error) {
	raw, err := os.ReadFile(filepath.Join(dir, SeenFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("browserhost.LoadSeen: %w", err)
	}
	var s Seen
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("browserhost.LoadSeen: %w", err)
	}
	s.Version = sanitizeVersion(s.Version)
	return &s, nil
}

// RecordSeen merges one handshake into the record in dir (first-seen kept,
// last-seen, version, protocol and refusal replaced) and writes it 0600
// through a temp file and rename, so a reader never sees a torn file. It is
// best-effort for the caller: the native-messaging port must never wait on
// it, and a failure only means the artifact report is older.
func RecordSeen(dir string, h Hello, nowMs int64) error {
	prev, _ := LoadSeen(dir)
	s := Seen{Version: h.ExtensionVersion, Protocol: h.Protocol, FirstSeenMs: nowMs, LastSeenMs: nowMs, Refused: Refusal(h.Protocol)}
	if prev != nil && prev.FirstSeenMs > 0 && prev.FirstSeenMs <= nowMs {
		s.FirstSeenMs = prev.FirstSeenMs
	}
	body, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("browserhost.RecordSeen: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("browserhost.RecordSeen: %w", err)
	}
	f, err := os.CreateTemp(dir, SeenFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("browserhost.RecordSeen: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return fmt.Errorf("browserhost.RecordSeen: %w", err)
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("browserhost.RecordSeen: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("browserhost.RecordSeen: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, SeenFileName)); err != nil {
		return fmt.Errorf("browserhost.RecordSeen: %w", err)
	}
	return nil
}
