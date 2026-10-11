//go:build !windows

package cursor

import (
	"os"
	"syscall"
)

// ownedByCurrentUser reports whether path is owned by this process's user
// (U10: another user's cursor-agent-logs directory in a shared /tmp is never
// read).
func ownedByCurrentUser(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
