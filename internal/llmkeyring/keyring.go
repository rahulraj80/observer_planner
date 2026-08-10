// Package llmkeyring stores LLM analysis API keys OUTSIDE config.toml:
// the [llm.connections.*] block holds only a LABEL (api_key_keychain),
// never the key. The store mirrors the org bearer_store posture — an
// OS-keychain-backed backend when available, a 0600-mode file fallback
// otherwise — so key-save works on headless machines too.
//
// The label IS the connection id (a slug, e.g. "freellmapi"), which
// makes the config ref self-describing: api_key_keychain = "freellmapi"
// resolves through Store.Load("freellmapi").
package llmkeyring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	keyring "github.com/zalando/go-keyring"
)

// Service is the keyring service namespace holding LLM analysis keys.
const Service = "superbased-observer-llm"

// probeRecord is a throwaway availability-probe record (mirrors the
// org bearer probe).
const probeRecord = "__sbo_llm_probe__"

// ErrNoKey is returned by Load when nothing has been stored for the
// label yet. Callers treat it as "no key configured", not a failure.
var ErrNoKey = errors.New("llmkeyring: no key stored for label")

// Store persists LLM API keys under per-connection labels.
type Store struct {
	dir     string
	backend string
}

// Open probes the OS keychain once; a successful round-trip selects
// the keychain backend, otherwise a 0600 file store rooted at dir.
// dir must be a writable directory (the caller passes the config dir).
func Open(dir string) *Store {
	if keychainUsable() {
		return &Store{backend: "keychain"}
	}
	return &Store{dir: dir, backend: "file"}
}

// OpenDir forces the 0600-file backend rooted at dir. It exists for
// tests (a deterministic store that never touches the real OS
// keychain) and for callers that want an explicit backend.
func OpenDir(dir string) *Store {
	return &Store{dir: dir, backend: "file"}
}

// Backend names the active backend for diagnostics ("keychain" or "file").
func (s *Store) Backend() string { return s.backend }

// keychainUsable probes the keyring with a throwaway record. A full
// set/get/delete cycle is the only reliable cross-platform availability
// test (ErrUnsupportedPlatform alone misses headless Linux without a
// Secret Service). The probe record is always deleted.
func keychainUsable() bool {
	if err := keyring.Set(Service, probeRecord, "1"); err != nil {
		return false
	}
	got, err := keyring.Get(Service, probeRecord)
	_ = keyring.Delete(Service, probeRecord)
	return err == nil && got == "1"
}

// Save stores key under label (the connection id).
func (s *Store) Save(label, key string) error {
	if label == "" {
		return fmt.Errorf("llmkeyring: label must not be empty")
	}
	if s.backend == "keychain" {
		if err := keyring.Set(Service, label, key); err != nil {
			return fmt.Errorf("llmkeyring.Save: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("llmkeyring.Save: mkdir: %w", err)
	}
	if err := os.WriteFile(s.path(label), []byte(key), 0o600); err != nil {
		return fmt.Errorf("llmkeyring.Save: write %s: %w", label, err)
	}
	return nil
}

// Load returns the key stored for label. ErrNoKey when absent.
func (s *Store) Load(label string) (string, error) {
	if s.backend == "keychain" {
		v, err := keyring.Get(Service, label)
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNoKey
		}
		if err != nil {
			return "", fmt.Errorf("llmkeyring.Load: %w", err)
		}
		return v, nil
	}
	b, err := os.ReadFile(s.path(label))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoKey
	}
	if err != nil {
		return "", fmt.Errorf("llmkeyring.Load: read %s: %w", label, err)
	}
	return string(b), nil
}

// Delete removes the key for label. Absence is not an error.
func (s *Store) Delete(label string) error {
	if s.backend == "keychain" {
		if err := keyring.Delete(Service, label); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("llmkeyring.Delete: %w", err)
		}
		return nil
	}
	if err := os.Remove(s.path(label)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("llmkeyring.Delete: %w", err)
	}
	return nil
}

// path is the 0600-file location for label.
func (s *Store) path(label string) string {
	safe := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(label)
	return filepath.Join(s.dir, "llm-"+safe+".key")
}
