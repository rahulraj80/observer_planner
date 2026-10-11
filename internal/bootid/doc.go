// Package bootid returns a per-boot identifier for this machine: the Linux
// kernel boot_id, a boot-instant string on Windows, empty elsewhere. It is
// stamped into every process key (sha256(boot_id:pid:start_time)) so a pid
// reused across reboots never collides.
//
// It is a neutral core package, split out of the process capture backend
// internal/processobs/poll in modular packaging phase PB (rev F1), so the
// intervention feature can stamp the same boot id without importing the
// processobs component. The poll backend reads it here too, so the two keep
// one owner and one value.
package bootid
