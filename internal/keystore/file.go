package keystore

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FileMode is the ONLY acceptable permission set for a secret file.
const FileMode fs.FileMode = 0o600

// DirMode is the required mode for the private secret directory.
const DirMode fs.FileMode = 0o700

// FileBackend is the hardened 0600 file primitive set: one file per record
// at <Dir>/<Service>.<rec>. Every read and write validates the directory,
// refuses symlinks (Lstat plus O_NOFOLLOW), verifies ownership and (for
// writes) installs atomically. Record names are validated with ValidName.
type FileBackend struct {
	// Dir is the private directory (created 0700 on first write).
	Dir string
	// Service prefixes every file name, so one directory never mixes two
	// services' records.
	Service string
}

// Path returns the file path of rec. It does not validate rec.
func (b FileBackend) Path(rec string) string {
	return filepath.Join(b.Dir, b.Service+"."+rec)
}

// EnsureDir creates (0700) and validates the private directory: it must be a
// real directory (not a symlink), owner-only, and owned by this user.
func (b FileBackend) EnsureDir() error {
	if err := os.MkdirAll(b.Dir, DirMode); err != nil {
		return fmt.Errorf("keystore: create credential dir: %w", err)
	}
	li, err := os.Lstat(b.Dir)
	if err != nil {
		return fmt.Errorf("keystore: stat credential dir: %w", err)
	}
	if li.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("keystore: credential dir %s is a symlink; refusing", b.Dir)
	}
	if !li.IsDir() {
		return fmt.Errorf("keystore: credential path %s is not a directory", b.Dir)
	}
	if perm := li.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("keystore: credential dir %s is mode %#o; must be 0700", b.Dir, perm)
	}
	return checkOwner(b.Dir, li)
}

func randomSuffix() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate temp suffix: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// Write installs val at rec atomically: a fresh O_EXCL temp file in the same
// directory, fsync, rename over the destination, directory fsync. A crash at
// any point leaves the old file or the new one, never a truncated one. A
// destination that is a symlink, not a regular file, or foreign-owned is
// refused.
func (b FileBackend) Write(rec string, val []byte) error {
	if err := checkName(rec); err != nil {
		return err
	}
	if len(val) > MaxValueBytes {
		return ErrValueTooLarge
	}
	if err := b.EnsureDir(); err != nil {
		return err
	}
	dst := b.Path(rec)
	if err := validateExistingTarget(dst); err != nil {
		return err
	}
	suffix, err := randomSuffix()
	if err != nil {
		return fmt.Errorf("keystore: %w", err)
	}
	tmp := dst + ".tmp." + suffix
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|openNoFollow, FileMode)
	if err != nil {
		return fmt.Errorf("keystore: create temp credential file: %w", err)
	}
	if _, err := f.Write(val); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("keystore: write temp credential file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("keystore: fsync temp credential file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("keystore: close temp credential file: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("keystore: install credential file: %w", err)
	}
	b.syncDir()
	return nil
}

func validateExistingTarget(dst string) error {
	li, err := os.Lstat(dst)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("keystore: stat credential file: %w", err)
	}
	if li.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("keystore: credential file %s is a symlink; refusing to replace it", dst)
	}
	if !li.Mode().IsRegular() {
		return fmt.Errorf("keystore: credential file %s is not a regular file", dst)
	}
	return checkOwner(dst, li)
}

func (b FileBackend) syncDir() {
	d, err := os.Open(b.Dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// Read validates and returns the bytes at rec, or ErrNotFound when absent.
// It refuses a symlink, a group/world-accessible mode, a foreign owner, a
// non-regular file and a file larger than MaxValueBytes, and re-checks the
// OPEN handle to close the Lstat-to-open window.
func (b FileBackend) Read(rec string) ([]byte, error) {
	if err := checkName(rec); err != nil {
		return nil, err
	}
	path := b.Path(rec)
	li, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("keystore: stat credential file: %w", err)
	}
	if li.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("keystore: credential file %s is a symlink; refusing to follow it", path)
	}
	if !li.Mode().IsRegular() {
		return nil, fmt.Errorf("keystore: credential file %s is not a regular file", path)
	}
	if perm := li.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("keystore: credential file %s is mode %#o; it must not be group- or world-accessible (chmod 600)", path, perm)
	}
	if err := checkOwner(path, li); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|openNoFollow, 0)
	if err != nil {
		return nil, fmt.Errorf("keystore: open credential file: %w", err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("keystore: stat open credential file: %w", err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("keystore: credential file %s changed under us; refusing to load it", path)
	}
	if err := checkOwnerFile(path, f, fi); err != nil {
		return nil, err
	}
	buf := make([]byte, MaxValueBytes+1)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, fmt.Errorf("keystore: read credential file: %w", err)
	}
	if n > MaxValueBytes {
		return nil, fmt.Errorf("keystore: credential file %s exceeds %d bytes", path, MaxValueBytes)
	}
	return buf[:n], nil
}

// Remove deletes rec. Absence is not an error.
func (b FileBackend) Remove(rec string) error {
	if err := checkName(rec); err != nil {
		return err
	}
	if err := os.Remove(b.Path(rec)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("keystore: remove credential file: %w", err)
	}
	return nil
}
