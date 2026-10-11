//go:build !linux && !windows

package bootid

// Get has no source on this OS yet; "" is acceptable because no process
// capture backend runs here either.
func Get() string { return "" }
