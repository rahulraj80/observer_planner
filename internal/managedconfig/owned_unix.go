//go:build unix

package managedconfig

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// trustRoot is where the parent-directory walk stops (inclusive), and
// requiredOwner the uid every checked path must have. Fixed in production;
// in-package tests point them at a temporary tree.
var (
	trustRoot     = "/"
	requiredOwner = uint32(0)
)

// readOwnedFile reads path only when it and every parent directory up to
// trustRoot are owned by requiredOwner and not group or world writable, and
// path itself is a regular file opened without following a symlink.
func readOwnedFile(path, format string) Probe {
	p := Probe{Source: path, Format: format}
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return p // no source
		}
		p.Found, p.FailedCheck = true, "stat"
		return p
	}
	p.Found = true
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if fc := checkOwned(dir, true); fc != "" {
			p.FailedCheck = fc + ": " + dir
			return p
		}
		if dir == trustRoot || dir == filepath.Dir(dir) {
			break
		}
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			p.FailedCheck = "symlink"
		} else {
			p.FailedCheck = "open"
		}
		return p
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() { _ = f.Close() }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		p.FailedCheck = "stat"
		return p
	}
	if fc := modeCheck(st, false); fc != "" {
		p.FailedCheck = fc
		return p
	}
	if st.Size > MaxBytes {
		p.FailedCheck = "too_large"
		return p
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil || len(data) > MaxBytes {
		p.FailedCheck = "read"
		return p
	}
	p.Data = data
	return p
}

// checkOwned lstat-checks one path.
func checkOwned(path string, wantDir bool) string {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return "stat"
	}
	return modeCheck(st, wantDir)
}

func modeCheck(st unix.Stat_t, wantDir bool) string {
	typ := uint32(st.Mode) & unix.S_IFMT //nolint:unconvert // Mode is uint16 on darwin
	switch {
	case wantDir && typ != unix.S_IFDIR:
		return "parent_not_a_directory"
	case !wantDir && typ != unix.S_IFREG:
		return "not_a_regular_file"
	case st.Uid != requiredOwner:
		if wantDir {
			return "parent_not_root_owned"
		}
		return "not_root_owned"
	case uint32(st.Mode)&0o022 != 0: //nolint:unconvert // Mode is uint16 on darwin
		if wantDir {
			return "parent_group_or_world_writable"
		}
		return "group_or_world_writable"
	}
	return ""
}
