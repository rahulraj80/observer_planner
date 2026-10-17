package vendorsurface

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// HostTuple is one exact (scheme, host, port) a surface's operations may be
// sent to (plan 6.2: one URL policy, exact scheme, host and port tuple from
// the surface). The egress seam allows a request only to a tuple the catalog
// row declares; a connection may pick one of them (a residency variant) but
// never add one.
type HostTuple struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

// String renders the tuple as scheme://host:port.
func (h HostTuple) String() string {
	return h.Scheme + "://" + net.JoinHostPort(h.Host, strconv.Itoa(h.Port))
}

// hostnameRE is a lower-case DNS name of at least two labels. IP literals,
// upper case, trailing dots and userinfo never match.
var hostnameRE = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidateHostTuple checks one catalog host tuple: https only, a lower-case
// DNS name (never an IP literal, never a single label such as localhost),
// and a port in range.
func ValidateHostTuple(h HostTuple) error {
	switch {
	case h.Scheme != "https":
		return fmt.Errorf("host tuple %q: scheme must be https", h.String())
	case net.ParseIP(strings.Trim(h.Host, "[]")) != nil:
		return fmt.Errorf("host tuple %q: an IP literal is never a catalog host", h.String())
	case !hostnameRE.MatchString(h.Host):
		return fmt.Errorf("host tuple %q: host must be a lower-case DNS name", h.String())
	case h.Port < 1 || h.Port > 65535:
		return fmt.Errorf("host tuple %q: port out of range", h.String())
	}
	return nil
}

// ParseHostTuple parses "scheme://host[:port]" (the form a connection's
// host_tuple column stores). The default port of https is 443.
func ParseHostTuple(s string) (HostTuple, error) {
	scheme, rest, ok := strings.Cut(s, "://")
	if !ok || scheme == "" || rest == "" || strings.ContainsAny(rest, "/?#@") {
		return HostTuple{}, fmt.Errorf("host tuple %q: want scheme://host[:port]", s)
	}
	h := HostTuple{Scheme: scheme, Host: rest, Port: 443}
	if host, port, err := net.SplitHostPort(rest); err == nil {
		p, perr := strconv.Atoi(port)
		if perr != nil {
			return HostTuple{}, fmt.Errorf("host tuple %q: bad port", s)
		}
		h.Host, h.Port = host, p
	}
	return h, nil
}

// TenantLabel is the one placeholder label of a residency host template.
const TenantLabel = "{tenant}"

// HostTemplate is a residency host family (lane L8): exactly one DNS label
// is TenantLabel, standing for the customer's own subdomain, and at least two
// fixed labels follow it (GitHub GHE.com: "api.{tenant}.ghe.com"). A
// connection may name one expansion; nothing else about the policy changes.
type HostTemplate struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

// String renders the template as scheme://host:port.
func (t HostTemplate) String() string {
	return t.Scheme + "://" + net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

// tenantRE is one lower-case DNS label (a GHE.com enterprise subdomain).
var tenantRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidTenant reports whether s may stand for a template's tenant label.
func ValidTenant(s string) bool { return tenantRE.MatchString(s) }

// ValidateHostTemplate checks one template: https only, a port in range,
// exactly one tenant label that is a whole label and is followed by at least
// two fixed labels, and an expansion that is a valid catalog host.
func ValidateHostTemplate(t HostTemplate) error {
	labels := strings.Split(t.Host, ".")
	at := -1
	for i, l := range labels {
		if l == TenantLabel {
			if at >= 0 {
				return fmt.Errorf("host template %q: more than one tenant label", t.String())
			}
			at = i
		}
	}
	switch {
	case t.Scheme != "https":
		return fmt.Errorf("host template %q: scheme must be https", t.String())
	case at < 0:
		return fmt.Errorf("host template %q: no %s label", t.String(), TenantLabel)
	case len(labels)-at-1 < 2:
		return fmt.Errorf("host template %q: the tenant label needs at least two fixed labels after it", t.String())
	case strings.Count(t.Host, "{") != 1 || strings.Count(t.Host, "}") != 1:
		return fmt.Errorf("host template %q: only the tenant label may use braces", t.String())
	case t.Port < 1 || t.Port > 65535:
		return fmt.Errorf("host template %q: port out of range", t.String())
	}
	h, ok := t.Expand("x")
	if !ok {
		return fmt.Errorf("host template %q: expansion is not a valid catalog host", t.String())
	}
	return ValidateHostTuple(h)
}

// Expand returns the tuple of the template for one tenant label.
func (t HostTemplate) Expand(tenant string) (HostTuple, bool) {
	if !ValidTenant(tenant) || strings.Count(t.Host, TenantLabel) != 1 {
		return HostTuple{}, false
	}
	return HostTuple{Scheme: t.Scheme, Host: strings.Replace(t.Host, TenantLabel, tenant, 1), Port: t.Port}, true
}

// Match returns the tenant label a tuple expands the template with.
func (t HostTemplate) Match(h HostTuple) (string, bool) {
	if h.Scheme != t.Scheme || h.Port != t.Port {
		return "", false
	}
	prefix, suffix, ok := strings.Cut(t.Host, TenantLabel)
	if !ok || !strings.HasPrefix(h.Host, prefix) || !strings.HasSuffix(h.Host, suffix) ||
		len(h.Host) <= len(prefix)+len(suffix) {
		return "", false
	}
	tenant := h.Host[len(prefix) : len(h.Host)-len(suffix)]
	if !ValidTenant(tenant) {
		return "", false
	}
	return tenant, true
}

// builtRule is one row of the Built-row table: a surface may move to Built
// only when every row holds (lane F7, D-402).
type builtRule struct {
	name string
	ok   func(Surface) bool
}

var builtRules = []builtRule{
	{"a built pull or invoke surface declares at least one host tuple (or is a customer_host row)", func(s Surface) bool {
		if s.Reach != ReachPullAPI && s.Reach != ReachInvokeAPI {
			return true
		}
		return len(s.Hosts) > 0 || s.CustomerHost
	}},
	{"a built surface has a reviewed secret class (never unclassified)", func(s Surface) bool {
		return s.SecretClass != SecretUnclassified
	}},
	{"a built surface's auth contract is not keyword-derived", func(s Surface) bool {
		return !strings.HasPrefix(s.Derivation.AuthRule, "keyword:")
	}},
	{"a built surface's auth kind is classified", func(s Surface) bool {
		return s.Auth.Kind != AuthUnclassified
	}},
	{"a built OAuth client-credentials surface declares its token host", func(s Surface) bool {
		return s.Auth.Kind != AuthOAuthClientCredentials || len(s.TokenHosts) > 0
	}},
}

// validateCustomerHost: a customer_host row (lane ENTAPI-L16C, T-710) names no
// host at all, the connection declares its own tuple. Declaring one too would
// make the catalog's tuple and the customer's compete, so a customer_host row
// carries no API, template, download or token host.
func validateCustomerHost(s Surface) []error {
	if s.CustomerHost && (len(s.Hosts) > 0 || len(s.HostTemplates) > 0 || len(s.DownloadHosts) > 0 ||
		len(s.DownloadHostTemplates) > 0 || len(s.TokenHosts) > 0) {
		return []error{fmt.Errorf("%s: a customer_host row declares no host tuple, template, download or token host", s.ID)}
	}
	return nil
}

// validateHosts checks the declared host tuples of any row and the Built-row
// table of a built row.
func validateHosts(s Surface) []error {
	errs := validateCustomerHost(s)
	seen := map[HostTuple]bool{}
	for _, h := range s.Hosts {
		if err := ValidateHostTuple(h); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.ID, err))
		}
		if seen[h] {
			errs = append(errs, fmt.Errorf("%s: host tuple %q listed twice", s.ID, h.String()))
		}
		seen[h] = true
	}
	for _, h := range s.DownloadHosts {
		if err := ValidateHostTuple(h); err != nil {
			errs = append(errs, fmt.Errorf("%s: download %w", s.ID, err))
		}
		if seen[h] {
			errs = append(errs, fmt.Errorf("%s: download host tuple %q listed twice or also an API host", s.ID, h.String()))
		}
		seen[h] = true
	}
	tseen := map[HostTemplate]bool{}
	for _, list := range [][]HostTemplate{s.HostTemplates, s.DownloadHostTemplates} {
		for _, t := range list {
			if err := ValidateHostTemplate(t); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", s.ID, err))
			}
			if tseen[t] {
				errs = append(errs, fmt.Errorf("%s: host template %q listed twice", s.ID, t.String()))
			}
			tseen[t] = true
		}
	}
	for _, h := range s.TokenHosts {
		if err := ValidateHostTuple(h); err != nil {
			errs = append(errs, fmt.Errorf("%s: token %w", s.ID, err))
		}
		if seen[h] {
			errs = append(errs, fmt.Errorf("%s: token host tuple %q listed twice or also an API or download host", s.ID, h.String()))
		}
		seen[h] = true
	}
	// A cloud_iam surface may declare one too: the Google service account
	// JWT bearer token step (lane ENTAPI-L16C, T-710).
	if len(s.TokenHosts) > 0 && s.Auth.Kind != AuthOAuthClientCredentials && s.Auth.Kind != AuthCloudIAM {
		errs = append(errs, fmt.Errorf("%s: token hosts are declared only by an oauth_client_credentials or cloud_iam surface", s.ID))
	}
	if len(s.DownloadHostTemplates) > 0 && len(s.HostTemplates) == 0 {
		errs = append(errs, fmt.Errorf("%s: a download host template needs an API host template to bind its tenant", s.ID))
	}
	if (len(s.HostTemplates) > 0 || len(s.DownloadHosts) > 0) && len(s.Hosts) == 0 {
		errs = append(errs, fmt.Errorf("%s: host templates and download hosts need at least one exact API host tuple", s.ID))
	}
	if s.Build != BuildBuilt {
		return errs
	}
	for _, r := range builtRules {
		if !r.ok(s) {
			errs = append(errs, fmt.Errorf("%s: %s", s.ID, r.name))
		}
	}
	return errs
}
