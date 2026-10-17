package managedconfig

import "fmt"

// Pure Windows registry ACL rule (plan 6.2.2: only Administrators and SYSTEM
// can write the policy key). source_windows.go extracts the owner and the
// ACEs; this file decides, so the rule is tested on every platform.

// Well-known SIDs allowed to own or write the policy key.
const (
	sidAdministrators   = "S-1-5-32-544"
	sidSystem           = "S-1-5-18"
	sidTrustedInstaller = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
)

// trustedWriters are the SIDs that may hold write access to the key.
var trustedWriters = map[string]bool{
	sidAdministrators:   true,
	sidSystem:           true,
	sidTrustedInstaller: true,
}

// ACE type and flag values (winnt.h).
const (
	aceTypeAccessAllowed = 0x0
	aceFlagInheritOnly   = 0x8
)

// grantingACETypes are the OTHER ACE types that grant access (compound,
// object, callback, callback-object allowed). source_windows.go reads the
// SID only from the plain allowed layout, so one of these carrying a write
// right cannot be verified and fails closed (ledger MODPKG-9): skipping it
// would let a callback or object ACE hand an untrusted principal write
// access to the ceiling.
var grantingACETypes = map[uint8]bool{0x4: true, 0x5: true, 0x9: true, 0xB: true}

// keyWriteMask is every access right that lets a principal change the key's
// values or its security: KEY_SET_VALUE, KEY_CREATE_SUB_KEY, DELETE,
// WRITE_DAC, WRITE_OWNER, GENERIC_WRITE, GENERIC_ALL.
const keyWriteMask = 0x2 | 0x4 | 0x10000 | 0x40000 | 0x80000 | 0x40000000 | 0x10000000

// ACE is one access control entry, reduced to what the rule reads.
type ACE struct {
	Type  uint8
	Flags uint8
	Mask  uint32
	SID   string
}

// CheckKeySecurity returns the name of the failed check, or "" when only
// trusted principals own and can write the key. A nil DACL grants everyone
// full access and fails.
func CheckKeySecurity(owner string, daclPresent bool, aces []ACE) string {
	if !trustedWriters[owner] {
		return "registry_owner_not_admin_or_system"
	}
	if !daclPresent {
		return "registry_null_dacl"
	}
	for _, a := range aces {
		if a.Flags&aceFlagInheritOnly != 0 {
			continue
		}
		if grantingACETypes[a.Type] && a.Mask&keyWriteMask != 0 {
			return fmt.Sprintf("registry_unverifiable_allow_ace_type_%d", a.Type)
		}
		if a.Type != aceTypeAccessAllowed {
			continue
		}
		if a.Mask&keyWriteMask != 0 && !trustedWriters[a.SID] {
			return "registry_writable_by_" + a.SID
		}
	}
	return ""
}
