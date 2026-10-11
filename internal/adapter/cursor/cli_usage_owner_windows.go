//go:build windows

package cursor

// ownedByCurrentUser is always true on Windows: the native temp directory
// (%LOCALAPPDATA%\Temp) is the current user's own, not shared.
func ownedByCurrentUser(string) bool { return true }
