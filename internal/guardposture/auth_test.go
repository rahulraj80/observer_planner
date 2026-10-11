package guardposture

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testKey = bytes.Repeat([]byte{7}, keyBytes)

// TestLoadStates pins Sol A2-02's reader table: only a posture that
// verifies under its key is trusted; every other combination of present /
// missing / edited files is tampered, except "nothing at all" (no daemon
// ever published), which is absent.
func TestLoadStates(t *testing.T) {
	enforced := Posture{Enforced: true, Source: SourceOrg, Generation: "3", GateCaps: []string{"tool_gate"}, WrittenAt: time.Unix(5, 0)}
	forge := func(t *testing.T, dir string, p Posture) {
		t.Helper()
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, PostureFilename), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  State
	}{
		{"nothing", func(*testing.T, string) {}, StateAbsent},
		{"signed by the daemon", func(t *testing.T, dir string) {
			mustKey(t, dir, testKey)
			if _, err := Write(filepath.Join(dir, PostureFilename), enforced, testKey); err != nil {
				t.Fatal(err)
			}
		}, StateValid},
		{"forged lowering, no MAC", func(t *testing.T, dir string) {
			mustKey(t, dir, testKey)
			forge(t, dir, Posture{Schema: Schema, Enforced: false, Source: SourceLocal})
		}, StateTampered},
		{"forged with a copied MAC from another posture", func(t *testing.T, dir string) {
			mustKey(t, dir, testKey)
			signed := Sign(enforced, testKey)
			signed.Enforced = false
			signed.GateCaps = nil
			forge(t, dir, signed)
		}, StateTampered},
		{"signed under a different key", func(t *testing.T, dir string) {
			mustKey(t, dir, testKey)
			forge(t, dir, Sign(Posture{Source: SourceLocal}, bytes.Repeat([]byte{9}, keyBytes)))
		}, StateTampered},
		{"posture deleted, key kept", func(t *testing.T, dir string) { mustKey(t, dir, testKey) }, StateTampered},
		{"key deleted, posture kept", func(t *testing.T, dir string) {
			if _, err := Write(filepath.Join(dir, PostureFilename), enforced, testKey); err != nil {
				t.Fatal(err)
			}
		}, StateTampered},
		{"garbage posture", func(t *testing.T, dir string) {
			mustKey(t, dir, testKey)
			if err := os.WriteFile(filepath.Join(dir, PostureFilename), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, StateTampered},
		{"garbage key", func(t *testing.T, dir string) {
			if _, err := Write(filepath.Join(dir, PostureFilename), enforced, testKey); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, KeyFilename), []byte("nope"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, StateTampered},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			got := Load(filepath.Join(dir, PostureFilename), filepath.Join(dir, KeyFilename))
			if got.State != tc.want {
				t.Fatalf("state = %s (%s), want %s", got.State, got.Detail, tc.want)
			}
			if got.State == StateValid && (got.Posture == nil || !got.Posture.FailsClosed("tool_gate")) {
				t.Errorf("valid posture = %+v", got.Posture)
			}
			if got.State == StateTampered && got.Detail == "" {
				t.Error("a tampered load carries no detail")
			}
		})
	}
}

func mustKey(t *testing.T, dir string, key []byte) {
	t.Helper()
	if err := WriteKey(filepath.Join(dir, KeyFilename), key); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOrCreateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), KeyFilename)
	k1, err := LoadOrCreateKey(path)
	if err != nil || len(k1) != keyBytes {
		t.Fatalf("create: %v len %d", err, len(k1))
	}
	k2, err := LoadOrCreateKey(path)
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatalf("reload changed the key: %v", err)
	}
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	k3, err := LoadOrCreateKey(path)
	if err != nil || bytes.Equal(k1, k3) || len(k3) != keyBytes {
		t.Fatalf("a malformed key was not replaced: %v", err)
	}
}
