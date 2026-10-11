package guardposture

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Posture authentication (Sol A2-02).
//
// The posture file is read by the hook process, which runs as the same OS
// user as the coding agent it guards, so file permissions cannot stop that
// agent from rewriting the file. What this layer buys, honestly:
//
//   - A posture that was hand-edited, replaced by a generic JSON writer,
//     truncated or deleted no longer verifies. The hook treats an
//     unverifiable posture as TAMPERED and IGNORES it (D-188 / D-193): it
//     computes enforcement itself from the config and the signed org bundle,
//     exactly as when no daemon ever published. So a forged "not enforced"
//     posture cannot open a gate the config or the org enforces, and a
//     forged "enforced" posture cannot close one on a node that does not
//     enforce. (An earlier design closed every wired gate on a tamper; that
//     was reversed because it blocked non-enforcing nodes.)
//   - The daemon, the one writer, re-checks the file and the key on every
//     publish interval, repairs a mismatch and records it as a guard event
//     that reaches the org.
//
// What it does NOT buy: the HMAC key sits beside the posture, readable by
// the same user, because the hook must verify without a daemon round trip.
// A same-user process that reads the key can sign any posture; the daemon
// repairs that on its next interval (docs/security.md HOOKFAIL-2 names the
// window). Under tool-gate enforcement the integrity rule R-160 also
// denies agent writes under ~/.observer, where both files live.

// KeyFilename is the posture HMAC key's basename, beside the posture.
const KeyFilename = "guard-posture.key"

// macDomain separates this MAC from any other use of the key.
const macDomain = "sbo-guard-posture-v1"

// keyBytes is the key length.
const keyBytes = 32

// KeyPath returns the key file beside the resolved global config path.
func KeyPath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), KeyFilename)
}

// ReadKey loads the key file. A missing file is (nil, nil); a malformed one
// is an error.
func ReadKey(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("guardposture.ReadKey: %w", err)
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != keyBytes {
		return nil, errors.New("guardposture.ReadKey: malformed key file")
	}
	return key, nil
}

// LoadOrCreateKey returns the key at path, creating a fresh random one when
// the file is missing or malformed. Daemon only (the one writer).
func LoadOrCreateKey(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("guardposture.LoadOrCreateKey: empty path")
	}
	if key, err := ReadKey(path); err == nil && key != nil {
		return key, nil
	}
	return WriteNewKey(path)
}

// WriteNewKey writes a fresh random key to path (atomic, 0600) and returns
// it.
func WriteNewKey(path string) ([]byte, error) {
	key := make([]byte, keyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("guardposture.WriteNewKey: %w", err)
	}
	if err := WriteKey(path, key); err != nil {
		return nil, err
	}
	return key, nil
}

// WriteKey writes key to path (atomic, 0600).
func WriteKey(path string, key []byte) error {
	if len(key) != keyBytes {
		return errors.New("guardposture.WriteKey: bad key length")
	}
	if err := writeAtomic(path, []byte(hex.EncodeToString(key)+"\n"), ".guard-posture-key-*.tmp"); err != nil {
		return fmt.Errorf("guardposture.WriteKey: %w", err)
	}
	return nil
}

// canonical is the MAC input: every field of the normalized posture except
// the MAC itself, in a fixed order and spelling.
func canonical(p Posture) []byte {
	p = normalize(p)
	var b bytes.Buffer
	b.WriteString(macDomain)
	b.WriteByte('\n')
	b.WriteString(strconv.Itoa(p.Schema))
	b.WriteByte('\n')
	b.WriteString(strconv.FormatBool(p.Enforced))
	b.WriteByte('\n')
	b.WriteString(strconv.Quote(p.Source))
	b.WriteByte('\n')
	b.WriteString(strconv.Quote(p.Generation))
	b.WriteByte('\n')
	for _, c := range p.GateCaps {
		b.WriteString(strconv.Quote(c))
		b.WriteByte(',')
	}
	b.WriteByte('\n')
	b.WriteString(p.WrittenAt.UTC().Format(time.RFC3339Nano))
	// Fields added after schema 1 are appended ONLY when set, so a posture
	// that does not use them keeps the exact MAC an older binary computes
	// (a mixed-version hook and daemon still verify each other's files).
	if p.UserPolicyFailClosed {
		b.WriteString("\nuser_policy_fail_closed")
	}
	return b.Bytes()
}

func mac(p Posture, key []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write(canonical(p))
	return hex.EncodeToString(h.Sum(nil))
}

// Sign returns p normalized and carrying its MAC under key.
func Sign(p Posture, key []byte) Posture {
	p = normalize(p)
	p.MAC = mac(p, key)
	return p
}

// Verify reports whether p carries a valid MAC under key.
func Verify(p *Posture, key []byte) bool {
	if p == nil || len(key) == 0 || p.MAC == "" {
		return false
	}
	want, err := hex.DecodeString(p.MAC)
	if err != nil {
		return false
	}
	got, _ := hex.DecodeString(mac(*p, key))
	return hmac.Equal(want, got)
}

// State is the outcome of loading the posture for a decision.
type State int

const (
	// StateAbsent: no posture and no key. No daemon has published on this
	// node; the reader falls back to its own configuration.
	StateAbsent State = iota
	// StateValid: the posture verifies under the key.
	StateValid
	// StateTampered: a posture or key exists but the pair does not verify
	// (edited, replaced, truncated, deleted, or written by a binary that
	// predates authentication). Readers ignore it and fall back to the
	// config, as for StateAbsent; the daemon repairs it and reports the
	// tamper.
	StateTampered
)

// String names the state for logs.
func (s State) String() string {
	switch s {
	case StateValid:
		return "valid"
	case StateTampered:
		return "tampered"
	default:
		return "absent"
	}
}

// Loaded is a posture load result.
type Loaded struct {
	State   State
	Posture *Posture
	// Detail says why a posture is tampered ("" otherwise).
	Detail string
}

// Load reads and authenticates the posture at posturePath with the key at
// keyPath.
func Load(posturePath, keyPath string) Loaded {
	key, kerr := ReadKey(keyPath)
	p, perr := Read(posturePath)
	switch {
	case perr != nil:
		return Loaded{State: StateTampered, Detail: "posture unreadable: " + perr.Error()}
	case kerr != nil:
		return Loaded{State: StateTampered, Detail: "key unreadable: " + kerr.Error()}
	case p == nil && key == nil:
		return Loaded{State: StateAbsent}
	case p == nil:
		return Loaded{State: StateTampered, Detail: "posture missing while its key exists"}
	case key == nil:
		return Loaded{State: StateTampered, Detail: "posture present without its key"}
	case !Verify(p, key):
		return Loaded{State: StateTampered, Detail: "posture does not verify under its key"}
	default:
		return Loaded{State: StateValid, Posture: p}
	}
}
