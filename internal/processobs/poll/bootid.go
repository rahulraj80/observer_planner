package poll

import "github.com/marmutapp/superbased-observer/internal/bootid"

// platformBootID is the machine's boot id (internal/bootid, the one owner;
// split out in modular packaging phase PB so intervention reads the same
// value without importing this backend).
func platformBootID() string { return bootid.Get() }
