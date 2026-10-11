package github

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

// nextLink returns the URL of the rel="next" entry of an RFC 8288 Link
// header ("" when there is none). GitHub documents the header at
// https://docs.github.com/en/rest/using-the-rest-api/using-pagination-in-the-rest-api.
func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		segs := strings.Split(part, ";")
		target := strings.TrimSpace(segs[0])
		if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		for _, p := range segs[1:] {
			k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(k), "rel") {
				continue
			}
			for _, rel := range strings.Fields(strings.Trim(strings.TrimSpace(v), `"`)) {
				if strings.EqualFold(rel, "next") {
					return strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">")
				}
			}
		}
	}
	return ""
}

var defaultPorts = map[string]string{"https": "443", "http": "80"}

// origin canonicalises scheme://host[:port] with the scheme's default port
// made explicit, so "https://api.github.com" and
// "https://api.github.com:443" compare equal.
func origin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		port = defaultPorts[scheme]
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

// nextPage reads the page number the rel="next" link asks for. The link
// must point at the origin the request was sent to: a link to another host
// is a pagination-contract change and fails the page (plan 12A.5), never a
// request the runner follows.
func nextPage(r vendorapi.Response) (string, error) {
	link := nextLink(r.Header["link"])
	if link == "" {
		return "", nil
	}
	u, err := url.Parse(link)
	if err != nil || u.User != nil || u.Host == "" {
		return "", fmt.Errorf("%w: next link is not an absolute URL", vendorapi.ErrSchemaDrift)
	}
	want, err := url.Parse(r.Origin)
	if err != nil || origin(u) != origin(want) {
		return "", fmt.Errorf("%w: next link points at another origin", vendorapi.ErrSchemaDrift)
	}
	page := u.Query().Get("page")
	if n, err := strconv.Atoi(page); err != nil || n < 2 || strconv.Itoa(n) != page {
		return "", fmt.Errorf("%w: next link carries no page number", vendorapi.ErrSchemaDrift)
	}
	return page, nil
}
