//go:build unix

package keystore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"time"
)

// openNoFollow is the O_NOFOLLOW open flag: the kernel itself refuses a
// symlinked secret file even if it is swapped in after the Lstat check.
const openNoFollow = syscall.O_NOFOLLOW

// checkOwner refuses a path owned by anyone other than this process user
// (root is accepted, since a root-owned 0600 file is readable only by a
// process already running as root).
func checkOwner(path string, fi fs.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil // unknown stat shape: the mode check is the guarantee
	}
	uid := os.Getuid()
	if uid < 0 {
		return nil
	}
	if int(st.Uid) != uid && st.Uid != 0 {
		return fmt.Errorf("keystore: %s is owned by uid %d, not this process's uid %d", path, st.Uid, uid)
	}
	return nil
}

// checkOwnerFile re-verifies ownership on the OPEN handle (fstat).
func checkOwnerFile(path string, _ *os.File, fi fs.FileInfo) error {
	return checkOwner(path, fi)
}

// FileFallbackSecure reports whether this platform build's file fallback has
// verified owner and symlink protections sufficient to hold secrets when the
// OS keychain is unavailable. Unix does: O_NOFOLLOW closes the
// Lstat-to-open window at the kernel level and POSIX ownership is verified
// on both the path and the open handle.
func FileFallbackSecure() bool { return true }

// WithLock runs fn while holding an exclusive flock on
// <dir>/<service>.coordination-lock (0600, owner-checked), waiting at most 5
// seconds. It serialises read-modify-write sequences across processes.
func WithLock(dir, service string, fn func() error) error {
	b := FileBackend{Dir: dir, Service: service}
	if err := b.EnsureDir(); err != nil {
		return err
	}
	path := b.Path("coordination-lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|openNoFollow, FileMode)
	if err != nil {
		return fmt.Errorf("keystore: open coordination lock: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != FileMode {
		return errors.New("keystore: insecure coordination lock")
	}
	if err := checkOwnerFile(path, f, info); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			return fmt.Errorf("keystore: acquire coordination lock: %w", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}
