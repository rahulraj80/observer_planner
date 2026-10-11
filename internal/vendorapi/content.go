package vendorapi

import (
	"fmt"
	"time"
	"unicode/utf8"
)

// Content landing (enterprise vendor API plan lane L15 = F17, group G8,
// T-618). A content surface (catalog sensitivity "content": prompts,
// responses, transcripts, code a vendor API exposes) fills Page.Content. A
// content item is the ONE landing type whose Text may carry free prose; every
// other field is metadata like the activity landings. The runner fetches a
// content surface only while the connection has content enabled with a
// recorded consent (decide.MayRun), the sanitizer drops every item unless the
// caller passes a content policy (vendorsanitize.PageWith), scrubs the text
// and caps it, and the store lands items only under that policy, stamped with
// their own retention_until. Nothing reads a content item on a spend, budget,
// alert or enforcement path (invariant I4).

// ContentItem is one vendor content record (G8 org_vendor_content): one
// prompt, response or transcript entry. ItemID is the vendor's own id of the
// record (unique per connection); Role is one of ContentRoles; Identity names
// the person the record belongs to (resolved to a member at landing, else
// held); SessionRef groups the items of one conversation.
type ContentItem struct {
	ItemID       string
	At           time.Time
	IdentityKind string
	Identity     string
	SessionRef   string
	Role         string
	// Text is the content. Before the sanitizer it is the vendor's text;
	// after it, the scrubbed and capped text.
	Text string
	// Truncated is set by the sanitizer when Text was cut at MaxContentBytes.
	Truncated bool
}

// MaxContentBytes caps one item's stored text (UTF-8 bytes). Longer text is
// cut at a rune boundary and marked Truncated, never refused.
const MaxContentBytes = 32 << 10

// MaxContentItems caps the content items one page may land.
const MaxContentItems = 1000

// ContentRoles is the closed role vocabulary a content item may carry.
var ContentRoles = map[string]bool{
	"prompt":     true, // what a person asked
	"response":   true, // what the vendor's assistant answered
	"transcript": true, // a whole-conversation record with no turn split
	"code":       true, // a code artifact the vendor stores
}

// HasContent reports whether a page carries any content item.
func (p Page) HasContent() bool { return len(p.Content) > 0 }

var contentRules = []activityRule[ContentItem]{
	{"content item id is bounded", func(c ContentItem) bool { return boundedID(c.ItemID) }},
	{"content time is set", func(c ContentItem) bool { return !c.At.IsZero() }},
	{"content role is a known role", func(c ContentItem) bool { return ContentRoles[c.Role] }},
	{"content identity kind names its identity", func(c ContentItem) bool {
		return (c.IdentityKind == "") == (c.Identity == "") && (c.IdentityKind == "" || tokenRE.MatchString(c.IdentityKind)) &&
			len(c.Identity) <= MaxIDLen
	}},
	{"content session is bounded", func(c ContentItem) bool { return len(c.SessionRef) <= MaxIDLen }},
	{"content text is valid UTF-8 within the cap", func(c ContentItem) bool {
		return len(c.Text) <= MaxContentBytes && utf8.ValidString(c.Text)
	}},
}

// ValidateContent is the structural gate content items pass before the store
// lands them. An item id repeated inside one page is refused.
func ValidateContent(p Page) error {
	if len(p.Content) > MaxContentItems {
		return fmt.Errorf("%w: %d content items exceed the per-page cap", ErrInvalidPage, len(p.Content))
	}
	return walk("content item", p.Content, contentRules, func(c ContentItem) string { return c.ItemID })
}
