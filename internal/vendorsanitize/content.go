package vendorsanitize

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/marmutapp/superbased-observer/internal/scrub"
	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

// Content boundary (enterprise vendor API plan lane L15 = F17, T-618, plan
// 6.5). Content items are dropped unless the caller passes a policy with
// Content set, which the runner does only for a connection whose content
// consent is recorded (decide.MayLandContent). With it, every item's
// metadata passes the same identity rules as an activity row (a bad key
// quarantines the item), the role must be one of vendorapi.ContentRoles and
// the text is scrubbed with the shared scrubber, stripped of control
// characters other than newline and tab, and cut at vendorapi.MaxContentBytes
// on a rune boundary (marked Truncated, never refused).

// Policy is what the caller allows a page to carry. The zero value allows
// no content.
type Policy struct {
	// Content lets content items through (scrubbed and capped).
	Content bool
}

// contentScrubber is the shared scrubber content text passes before it is
// stored (the same rules as every other captured body).
var contentScrubber = scrub.New()

// PageWith is Page under a policy: with Policy.Content false every content
// item is dropped and counted (ContentDropped); with it true each item is
// sanitized. The rest of the page is sanitized exactly as Page does.
func PageWith(p vendorapi.Page, pol Policy) (vendorapi.Page, Report, error) {
	out, rep, err := sanitizePage(p)
	if err != nil {
		return vendorapi.Page{}, Report{}, err
	}
	out.Content = nil
	if len(p.Content) == 0 {
		return out, rep, nil
	}
	if !pol.Content {
		rep.ContentDropped += len(p.Content)
		return out, rep, nil
	}
	if len(p.Content) > vendorapi.MaxContentItems {
		return vendorapi.Page{}, Report{}, fmt.Errorf("%w: %d content items exceed the per-page cap", ErrRefused, len(p.Content))
	}
	out.Content = make([]vendorapi.ContentItem, 0, len(p.Content))
	for _, c := range p.Content {
		if !vendorapi.ContentRoles[c.Role] || !allIdentity(c.ItemID, c.Identity, c.SessionRef) {
			rep.quarantine(false)
			continue
		}
		c.Text, c.Truncated = cleanContent(c.Text, &rep)
		out.Content = append(out.Content, c)
	}
	return out, rep, nil
}

// cleanContent scrubs, strips and caps one content text.
func cleanContent(v string, rep *Report) (string, bool) {
	if !utf8.ValidString(v) {
		v = strings.ToValidUTF8(v, "�")
	}
	v = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, v)
	if s := contentScrubber.String(v); s != v {
		rep.Redacted++
		v = s
	}
	if len(v) <= vendorapi.MaxContentBytes {
		return v, false
	}
	cut := vendorapi.MaxContentBytes
	for cut > 0 && !utf8.RuneStart(v[cut]) {
		cut--
	}
	rep.ContentTruncated++
	return v[:cut], true
}
