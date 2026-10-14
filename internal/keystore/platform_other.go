//go:build !unix

package keystore

import (
	"io/fs"
	"os"
)

// openNoFollow is 0 on platforms without O_NOFOLLOW (Windows). The Lstat
// symlink refusal still applies, but nothing closes the Lstat-to-open race,
// which is why FileFallbackSecure is false here.
const openNoFollow = 0

// checkOwner is a documented no-op where POSIX ownership does not apply. A
// real check needs a current-user SID/DACL verification and a reparse-point
// refusal (golang.org/x/sys/windows), which this package does not ship, so it
// must never gate a plaintext-secret write: FileFallbackSecure is false and
// Open never selects the file store on this build.
func checkOwner(string, fs.FileInfo) error { return nil }

// checkOwnerFile is the open-handle counterpart of checkOwner; the same note
// applies.
func checkOwnerFile(string, *os.File, fs.FileInfo) error { return nil }

// FileFallbackSecure reports whether this platform build's file fallback has
// verified owner and symlink/reparse protections. It does not on this build,
// so Open fails closed instead of writing an unverified plaintext file.
func FileFallbackSecure() bool { return false }

// WithLock runs fn directly: there is no verified private coordination
// directory on this build, and keychain operations keep their own per-item
// atomicity.
func WithLock(_, _ string, fn func() error) error { return fn() }
