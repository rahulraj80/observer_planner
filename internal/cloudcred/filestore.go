package cloudcred

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/keystore"
)

// fileStore is the hardened 0600-file fallback. It delegates every read and
// write to the shared hardened primitives in internal/keystore (directory
// validation, Lstat + O_NOFOLLOW symlink refusal, ownership checks, atomic
// temp + fsync + rename), so a crash mid-write leaves the previous value
// intact.
//
// host scopes the API token and WorkOS refresh record NAMES (recordName /
// scopedRecords) — never the directory, so a host-scoped and a legacy
// unscoped store over the same dir see each other's device key file (shared)
// but distinct token/refresh files. host == "" reproduces the legacy
// single-slot behaviour.
type fileStore struct {
	dir  string
	host string
}

func (s *fileStore) Backend() string { return "file" }

// SecurityDiagnostic reports the degraded posture of the file fallback: the
// bytes are protected only by filesystem permissions, not OS-backed encryption.
func (s *fileStore) SecurityDiagnostic() string {
	return "cloud credentials are stored in 0600 files at " + s.dir +
		" WITHOUT OS-backed encryption (the OS keychain was unavailable); " +
		"protection relies on filesystem permissions and ownership only"
}

func (s *fileStore) path(rec string) string { return s.backend().Path(rec) }

func (s *fileStore) SaveWorkOSRefresh(refresh string) error {
	return withCredentialLock(s.dir, func() error { return s.write(recordName(recWorkOSRefresh, s.host), []byte(refresh)) })
}

func (s *fileStore) LoadWorkOSRefresh() (string, error) {
	b, err := s.readScoped(recWorkOSRefresh)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *fileStore) SaveAPIToken(token string) error {
	return withCredentialLock(s.dir, func() error { return s.write(recordName(recAPIToken, s.host), []byte(token)) })
}

func (s *fileStore) LoadAPIToken() (string, error) {
	b, err := s.readScoped(recAPIToken)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *fileStore) SaveDeviceKey(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return fmt.Errorf("cloudcred.SaveDeviceKey: key is %d bytes, want %d", len(key), ed25519.PrivateKeySize)
	}
	// recordName never scopes recDeviceKey: one device identity is shared
	// across every host.
	return s.write(recordName(recDeviceKey, s.host), []byte(base64.StdEncoding.EncodeToString(key)))
}

func (s *fileStore) LoadDeviceKey() (ed25519.PrivateKey, error) {
	b, err := s.read(recordName(recDeviceKey, s.host))
	if err != nil {
		return nil, err
	}
	return decodeDeviceKey(string(b))
}

// readScoped loads rec scoped to s.host, with the one-time legacy adoption
// documented on OpenForHost: when the scoped file is absent, a live legacy
// (unscoped) file is returned AND migrated — written under the scoped name,
// then the legacy file removed — so no later reader (a different host)
// adopts it again.
func (s *fileStore) readScoped(rec string) ([]byte, error) {
	scoped := recordName(rec, s.host)
	b, err := s.read(scoped)
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, ErrNotFound) || s.host == "" || !scopedRecords[rec] {
		return nil, err
	}
	err = adoptLegacyBundle(s.dir, s.host,
		func(rec string) (string, error) { b, err := s.read(rec); return string(b), err },
		func(rec, value string) error { return s.write(rec, []byte(value)) },
		func(rec string) error { return os.Remove(s.path(rec)) })
	if err != nil {
		return nil, err
	}
	return s.read(scoped)
}

// Clear removes this host's scoped API token and WorkOS refresh files, the
// legacy unscoped files if still present, and the device signing key file.
// The device key is shared across hosts, so Clear is a full device reset
// (logout / device-revoke) — it does not reference-count hosts, and clearing
// one host's store clears the device identity for every host.
func (s *fileStore) Clear() error {
	return withCredentialLock(s.dir, s.clearLocked)
}

func (s *fileStore) clearLocked() error {
	recs := []string{recDeviceKey, recWorkOSRefresh, recAPIToken}
	if s.host != "" {
		recs = append(recs, recordName(recWorkOSRefresh, s.host), recordName(recAPIToken, s.host))
	} else {
		// A host-less Clear (`observer cloud logout` with no base URL resolvable)
		// is the full local wipe: every host's scoped record goes too. The
		// directory is dedicated to this service, so every file carrying the
		// service prefix is ours to remove; the keychain-unavailable sentinel
		// has no such prefix and stays.
		if entries, err := os.ReadDir(s.dir); err == nil {
			for _, e := range entries {
				if name := e.Name(); strings.HasPrefix(name, ServiceName+".") && name != ServiceName+".coordination-lock" && name != ServiceName+"."+recLegacyHost {
					recs = append(recs, strings.TrimPrefix(name, ServiceName+"."))
				}
			}
		}
	}
	var errs []error
	for _, rec := range recs {
		if err := os.Remove(s.path(rec)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("cloudcred.fileStore.Clear: %w", errors.Join(errs...))
	}
	return nil
}

// backend is the shared hardened file primitive set (internal/keystore) over
// this store's directory, with files named <dir>/<ServiceName>.<rec>.
func (s *fileStore) backend() keystore.FileBackend {
	return keystore.FileBackend{Dir: s.dir, Service: ServiceName}
}

// ensureDir creates (0700) and validates the private credential directory
// (keystore.FileBackend.EnsureDir: a real directory, owner-only, owned by
// this user).
func (s *fileStore) ensureDir() error { return s.backend().EnsureDir() }

// write installs val at rec atomically through keystore.FileBackend.Write
// (temp + fsync + rename; symlink, non-regular and foreign-owned targets are
// refused).
func (s *fileStore) write(rec string, val []byte) error { return s.backend().Write(rec, val) }

// read validates and returns the bytes at rec through
// keystore.FileBackend.Read (symlink, permissive mode, foreign owner and
// oversized files are refused), mapping absence to ErrNotFound.
func (s *fileStore) read(rec string) ([]byte, error) {
	b, err := s.backend().Read(rec)
	if errors.Is(err, keystore.ErrNotFound) {
		return nil, ErrNotFound
	}
	return b, err
}
