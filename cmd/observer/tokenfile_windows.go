//go:build windows

package main

import "os"

// ensureTokenFilePrivate is the Windows half: the token lives in the user's
// own profile directory, whose ACL is the access control there (POSIX mode
// bits carry no meaning on Windows). The symlink / non-regular refusal in
// readLocalTokenFile still applies.
func ensureTokenFilePrivate(string, os.FileInfo) error { return nil }
