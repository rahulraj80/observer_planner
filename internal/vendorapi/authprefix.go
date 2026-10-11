package vendorapi

import "regexp"

// SchemePrefixed sends "<Prefix> <secret>" as the whole value of Header (lane
// ENTAPI-L16C, T-710): an Authorization scheme other than Bearer that a vendor
// documents literally, such as the JetBrains IDE Services automation token
// ("Authorization: Automation <token>"). The prefix is part of the driver's
// fixed contract, never connection or caller data.
const SchemePrefixed AuthScheme = "prefixed"

// prefixRE is the closed shape of an auth prefix: one HTTP auth-scheme token
// (letters first, then letters, digits or hyphens; at most 32 bytes).
var prefixRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,31}$`)

// ValidAuthPrefix reports whether p may lead a SchemePrefixed credential. The
// egress seam refuses (sends nothing) for any other prefix, including empty.
func ValidAuthPrefix(p string) bool { return prefixRE.MatchString(p) }
