package orgenrol

import (
	"net/url"
	"regexp"
	"strings"
)

// orgIDShape matches an org id (32 hex characters). A grant row records the
// org id in its org_name column, so a name must be chosen, never read
// straight off the grant.
var orgIDShape = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// DisplayName is the ONE node-side owner of what a developer surface calls the
// organization (T-160): the enrolment page, `observer org` and the governance
// surfaces ("managed by <name>", the managed page, the read-only bar, the
// Privacy card, the API refusal body) all take their name from here. It picks
// the first candidate that is a real name (non-empty, not the org id, not
// id-shaped), else the org server's host, else "" (the web helper
// web/src/lib/orgName.ts then says "your organization"). An org id never
// reaches the page. Ordered rules, first match wins (CLAUDE.md #5).
func DisplayName(orgID, serverURL string, names ...string) string {
	for _, n := range names {
		name := strings.TrimSpace(n)
		if name == "" || name == orgID || orgIDShape.MatchString(name) {
			continue
		}
		return name
	}
	if u, err := url.Parse(strings.TrimSpace(serverURL)); err == nil && u.Host != "" && !orgIDShape.MatchString(u.Hostname()) {
		return u.Host
	}
	return ""
}
