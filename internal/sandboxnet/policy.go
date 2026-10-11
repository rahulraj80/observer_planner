package sandboxnet

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/marmutapp/superbased-observer/internal/sandboxcidr"
)

// Rule names used by DestinationAllowed. They appear in RefusedError.Rule and
// in the gateway's 403 body, so they are part of the operator-visible
// vocabulary and must stay stable.
const (
	RuleNil                     = "nil-address"
	RuleUnspecified             = "unspecified"
	RuleThisNetwork             = "this-network"
	RuleLoopback                = "loopback"
	RuleLinkLocalUnicast        = "link-local-unicast"
	RuleLinkLocalMulticast      = "link-local-multicast"
	RuleInterfaceLocalMulticast = "interface-local-multicast"
	RuleMulticast               = "multicast"
	RuleLimitedBroadcast        = "limited-broadcast"
	RuleIPv4Compatible          = "ipv4-compatible"
	RuleHostLocal               = "host-local-address"
	// The four allow-listable private-range rules are defined in
	// internal/sandboxcidr (the neutral owner of the ranges, shared with
	// config validation) and re-exported here.
	RulePrivate       = sandboxcidr.RulePrivate
	RuleSharedAddress = sandboxcidr.RuleSharedAddress
	RuleUniqueLocal   = sandboxcidr.RuleUniqueLocal
	RuleSiteLocal     = sandboxcidr.RuleSiteLocal
)

// AllowCIDRsConfigKey is the operator-facing config key that allow-lists
// private destinations. It is named in every refusal a private-range rule
// produces, so an operator who hits one knows exactly what to change.
const AllowCIDRsConfigKey = "[terminal.sandbox].egress_allow_cidrs"

// RefusedError is returned by DestinationAllowed when a rule refuses an
// address. Rule is one of the Rule* constants.
type RefusedError struct {
	// IP is the refused address as given (before normalisation).
	IP net.IP
	// Rule names the policy row that refused IP.
	Rule string
	// AllowListable reports whether the refusing row can be lifted for a
	// destination by listing its CIDR under AllowCIDRsConfigKey.
	AllowListable bool
}

// Error implements error.
func (e *RefusedError) Error() string {
	msg := fmt.Sprintf("sandboxnet: destination %s refused by rule %s", e.IP, e.Rule)
	if e.AllowListable {
		msg += " (to allow it, add its CIDR to " + AllowCIDRsConfigKey + ")"
	}
	return msg
}

// policyRule is one row of the egress policy: a named predicate over a
// normalised address. allowListable rows (the private ranges) are skipped
// for a destination inside a Policy.Allow prefix; every other row always
// applies.
type policyRule struct {
	name          string
	allowListable bool
	match         func(ip net.IP, local []net.IP) bool
}

// nat64Prefix is the RFC 6052 well-known NAT64 prefix 64:ff9b::/96; an
// address inside it carries an IPv4 destination in its last four bytes.
var nat64Prefix = net.IP{0x00, 0x64, 0xff, 0x9b, 0, 0, 0, 0, 0, 0, 0, 0}

// inRanges reports whether ip lies in any prefix of the named private rule.
func inRanges(rule string) func(ip net.IP, _ []net.IP) bool {
	return func(ip net.IP, _ []net.IP) bool {
		a, ok := toAddr(ip)
		if !ok {
			return false
		}
		for _, r := range sandboxcidr.PrivateRanges() {
			if r.Rule != rule {
				continue
			}
			for _, p := range r.Prefixes {
				if p.Contains(a) {
					return true
				}
			}
		}
		return false
	}
}

// policyRules is walked top-down; the first matching row refuses. Order only
// matters for which name is reported (0.0.0.0 reports unspecified, not
// this-network; a host interface address in 192.168/16 reports
// host-local-address, which no allow-list can lift).
var policyRules = []policyRule{
	{name: RuleUnspecified, match: func(ip net.IP, _ []net.IP) bool { return ip.IsUnspecified() }},
	{name: RuleThisNetwork, match: func(ip net.IP, _ []net.IP) bool {
		v4 := ip.To4()
		return v4 != nil && v4[0] == 0
	}},
	{name: RuleLoopback, match: func(ip net.IP, _ []net.IP) bool { return ip.IsLoopback() }},
	{name: RuleLinkLocalUnicast, match: func(ip net.IP, _ []net.IP) bool { return ip.IsLinkLocalUnicast() }},
	{name: RuleLinkLocalMulticast, match: func(ip net.IP, _ []net.IP) bool { return ip.IsLinkLocalMulticast() }},
	{name: RuleInterfaceLocalMulticast, match: func(ip net.IP, _ []net.IP) bool { return ip.IsInterfaceLocalMulticast() }},
	{name: RuleMulticast, match: func(ip net.IP, _ []net.IP) bool { return ip.IsMulticast() }},
	{name: RuleLimitedBroadcast, match: func(ip net.IP, _ []net.IP) bool { return ip.Equal(net.IPv4bcast) }},
	{name: RuleIPv4Compatible, match: func(ip net.IP, _ []net.IP) bool {
		// Deprecated IPv4-compatible IPv6 (::a.b.c.d, RFC 4291 2.5.5.1). :: and
		// ::1 are caught above; nothing legitimate lives in the rest of ::/96.
		if ip.To4() != nil || len(ip) != net.IPv6len {
			return false
		}
		for _, b := range ip[:12] {
			if b != 0 {
				return false
			}
		}
		return true
	}},
	{name: RuleHostLocal, match: func(ip net.IP, local []net.IP) bool {
		for _, l := range local {
			if l != nil && normalize(l).Equal(ip) {
				return true
			}
		}
		return false
	}},
	{name: RulePrivate, allowListable: true, match: inRanges(RulePrivate)},
	{name: RuleSharedAddress, allowListable: true, match: inRanges(RuleSharedAddress)},
	{name: RuleUniqueLocal, allowListable: true, match: inRanges(RuleUniqueLocal)},
	{name: RuleSiteLocal, allowListable: true, match: inRanges(RuleSiteLocal)},
}

// normalize returns the IPv4 form of an IPv4-mapped (::ffff:a.b.c.d) or
// NAT64 well-known-prefix (64:ff9b::a.b.c.d) address so neither can bypass an
// IPv4 rule; any other address is returned unchanged.
func normalize(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	if len(ip) == net.IPv6len && ip[:12].Equal(nat64Prefix) {
		return net.IPv4(ip[12], ip[13], ip[14], ip[15]).To4()
	}
	return ip
}

// toAddr converts a normalised net.IP to an unmapped netip.Addr.
func toAddr(ip net.IP) (netip.Addr, bool) {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

// Policy is the egress policy with its one operator knob: Allow lists the
// private prefixes a sandboxed agent may reach (corporate registries or git
// servers on private addresses). Allow only ever lifts the allow-listable
// rows (private-rfc1918, shared-address-cgnat, unique-local-ipv6,
// site-local-ipv6); loopback, link-local (including cloud metadata),
// multicast, broadcast and the host's own interface addresses stay refused
// whatever it says. The zero Policy allows no private destination. Build
// Allow with ParseAllowCIDRs.
type Policy struct {
	Allow []netip.Prefix
}

// allowed reports whether a (normalised) ip lies inside an Allow prefix.
func (p Policy) allowed(ip net.IP) bool {
	if len(p.Allow) == 0 {
		return false
	}
	a, ok := toAddr(ip)
	if !ok {
		return false
	}
	for _, pre := range p.Allow {
		if pre.Contains(a) {
			return true
		}
	}
	return false
}

// Check is the PURE egress policy: it returns nil when ip may be dialled by
// the gateway, or a *RefusedError naming the rule that refused it.
//
// Refused: a nil ip; unspecified (0.0.0.0, ::); the IPv4 "this network"
// block 0.0.0.0/8; loopback (127/8, ::1); link-local unicast (169.254/16,
// including cloud metadata 169.254.169.254, and fe80::/10); link-local,
// interface-local and all other multicast; the IPv4 limited broadcast
// 255.255.255.255; deprecated IPv4-compatible IPv6 (::/96); any address
// equal to one in local (the host's own interface addresses); and, unless
// the address lies inside p.Allow, the RFC 1918 private ranges, the RFC 6598
// shared address space 100.64.0.0/10, IPv6 unique local fc00::/7 and the
// deprecated site-local fec0::/10. IPv4-mapped (::ffff:a.b.c.d) and NAT64
// (64:ff9b::a.b.c.d) addresses are normalised to IPv4 first so they cannot
// bypass an IPv4 rule.
func (p Policy) Check(ip net.IP, local []net.IP) error {
	if len(ip) != net.IPv4len && len(ip) != net.IPv6len {
		return &RefusedError{IP: ip, Rule: RuleNil}
	}
	n := normalize(ip)
	for _, r := range policyRules {
		if !r.match(n, local) {
			continue
		}
		if r.allowListable && p.allowed(n) {
			continue
		}
		return &RefusedError{IP: ip, Rule: r.name, AllowListable: r.allowListable}
	}
	return nil
}

// DestinationAllowed is Policy{}.Check: the default policy with no
// allow-listed private prefix.
func DestinationAllowed(ip net.IP, local []net.IP) error {
	return Policy{}.Check(ip, local)
}

// ParseAllowCIDRs validates the operator's allow-list (AllowCIDRsConfigKey)
// and returns it as prefixes. The rules live in internal/sandboxcidr, the
// neutral owner shared with config validation (modular packaging phase PB);
// see sandboxcidr.ParseAllowCIDRs.
func ParseAllowCIDRs(entries []string) ([]netip.Prefix, error) {
	return sandboxcidr.ParseAllowCIDRs(entries)
}
