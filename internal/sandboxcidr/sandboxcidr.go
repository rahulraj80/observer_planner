package sandboxcidr

import (
	"fmt"
	"net/netip"
	"strings"
)

// Rule names of the four allow-listable private ranges. They appear in
// sandboxnet.RefusedError.Rule and in the egress gateway's 403 body, so they
// are part of the operator-visible vocabulary and must stay stable.
const (
	// RulePrivate refuses the RFC 1918 private IPv4 ranges (10/8,
	// 172.16/12, 192.168/16): the WSL2 NAT gateway (the Windows host and
	// every service it listens on), Docker bridge containers and LAN hosts
	// all live there.
	RulePrivate = "private-rfc1918"
	// RuleSharedAddress refuses the RFC 6598 shared address space
	// 100.64.0.0/10 (carrier-grade NAT, and the tailnet addresses of a
	// Tailscale network).
	RuleSharedAddress = "shared-address-cgnat"
	// RuleUniqueLocal refuses IPv6 unique local addresses fc00::/7.
	RuleUniqueLocal = "unique-local-ipv6"
	// RuleSiteLocal refuses the deprecated IPv6 site-local range fec0::/10.
	RuleSiteLocal = "site-local-ipv6"
)

// Range is one allow-listable rule and the prefixes it refuses.
type Range struct {
	Rule     string
	Prefixes []netip.Prefix
}

// privateRanges maps each allow-listable rule to the prefixes it refuses.
// It is the one owner of those ranges: the sandboxnet policy rows and
// ParseAllowCIDRs both read it.
var privateRanges = []Range{
	{RulePrivate, []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
	}},
	{RuleSharedAddress, []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10")}},
	{RuleUniqueLocal, []netip.Prefix{netip.MustParsePrefix("fc00::/7")}},
	{RuleSiteLocal, []netip.Prefix{netip.MustParsePrefix("fec0::/10")}},
}

// PrivateRanges returns the allow-listable ranges in rule order. The slice is
// shared; callers must not modify it.
func PrivateRanges() []Range { return privateRanges }

// ParseAllowCIDRs validates the operator's allow-list
// ([terminal.sandbox].egress_allow_cidrs)
// and returns it as prefixes. It is the ONE owner of what an allow-list
// entry may be, shared by config validation, the daemon's runtime and the
// host-side helper. Each entry must be a canonical CIDR (no host bits set,
// IPv4 in dotted form rather than IPv4-mapped IPv6) lying entirely inside
// one of the ranges the allow-listable rules refuse: a public prefix would
// be a no-op (public addresses are already allowed) and a loopback,
// link-local or metadata prefix can never be allow-listed. An empty list is
// valid (the default).
func ParseAllowCIDRs(entries []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(entries))
	for _, raw := range entries {
		s := strings.TrimSpace(raw)
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("sandboxnet.ParseAllowCIDRs: %q is not a CIDR (for example 10.20.0.0/16): %w", raw, err)
		}
		if p.Addr().Is4In6() {
			return nil, fmt.Errorf("sandboxnet.ParseAllowCIDRs: %q is an IPv4-mapped IPv6 prefix; write the IPv4 form", raw)
		}
		if p.Addr().Zone() != "" {
			return nil, fmt.Errorf("sandboxnet.ParseAllowCIDRs: %q carries a zone", raw)
		}
		if m := p.Masked(); m != p {
			return nil, fmt.Errorf("sandboxnet.ParseAllowCIDRs: %q has host bits set; did you mean %s", raw, m)
		}
		if !insidePrivateRange(p) {
			return nil, fmt.Errorf("sandboxnet.ParseAllowCIDRs: %q is not inside a private range the egress gateway refuses (%s); public addresses are already allowed, and loopback, link-local and host addresses cannot be allow-listed", raw, privateRangeList())
		}
		out = append(out, p)
	}
	return out, nil
}

// insidePrivateRange reports whether p lies entirely inside one of the
// allow-listable ranges.
func insidePrivateRange(p netip.Prefix) bool {
	for _, r := range privateRanges {
		for _, rp := range r.Prefixes {
			if rp.Addr().Is4() == p.Addr().Is4() && rp.Bits() <= p.Bits() && rp.Contains(p.Addr()) {
				return true
			}
		}
	}
	return false
}

// privateRangeList renders every allow-listable prefix for an error message.
func privateRangeList() string {
	var parts []string
	for _, r := range privateRanges {
		for _, p := range r.Prefixes {
			parts = append(parts, p.String())
		}
	}
	return strings.Join(parts, ", ")
}
