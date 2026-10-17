package vendorsanitize

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

func contentPage(items ...vendorapi.ContentItem) vendorapi.Page {
	return vendorapi.Page{Content: items}
}

func item(id, text string) vendorapi.ContentItem {
	return vendorapi.ContentItem{ItemID: id, At: time.Unix(1700000000, 0).UTC(), IdentityKind: "user_id", Identity: "u1", Role: "prompt", Text: text}
}

// TestContentDroppedWithoutPolicy (lane L15, plan 6.5): the default
// boundary keeps no content item; only a content policy lets one through.
func TestContentDroppedWithoutPolicy(t *testing.T) {
	out, rep, err := Page(contentPage(item("a", "hello"), item("b", "world")))
	if err != nil || len(out.Content) != 0 || rep.ContentDropped != 2 {
		t.Fatalf("Page kept content: %+v %+v %v", out.Content, rep, err)
	}
	out, rep, err = PageWith(contentPage(item("a", "hello")), Policy{Content: true})
	if err != nil || len(out.Content) != 1 || out.Content[0].Text != "hello" || rep.ContentDropped != 0 {
		t.Fatalf("PageWith: %+v %+v %v", out.Content, rep, err)
	}
}

// TestContentIsScrubbedStrippedAndCapped: a canary secret in content never
// survives, control characters other than newline and tab are stripped, a
// long text is cut on a rune boundary and marked truncated, and an item
// whose key carries secret material or whose role is unknown is quarantined.
func TestContentIsScrubbedStrippedAndCapped(t *testing.T) {
	canary := "ghp_" + strings.Repeat("A1b2C3d4E5", 4)
	long := strings.Repeat("é", vendorapi.MaxContentBytes) // two bytes each
	bad := item("c", "x")
	bad.Role = "system"
	keyed := item(canary, "x")
	out, rep, err := PageWith(contentPage(item("a", "token "+canary+"\x07 ok\nnext\tcol"), item("b", long), bad, keyed),
		Policy{Content: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Content) != 2 || rep.Quarantined != 2 {
		t.Fatalf("kept %d, quarantined %d", len(out.Content), rep.Quarantined)
	}
	a := out.Content[0].Text
	if strings.Contains(a, canary) || rep.Redacted == 0 || strings.ContainsRune(a, 0x07) || !strings.Contains(a, "\nnext\tcol") {
		t.Fatalf("scrubbed text %q (redacted %d)", a, rep.Redacted)
	}
	b := out.Content[1]
	if !b.Truncated || len(b.Text) > vendorapi.MaxContentBytes || !utf8.ValidString(b.Text) || rep.ContentTruncated != 1 {
		t.Fatalf("cap: truncated %v len %d valid %v", b.Truncated, len(b.Text), utf8.ValidString(b.Text))
	}
	if err := vendorapi.Validate(out); err != nil {
		t.Fatalf("a sanitized content page must validate: %v", err)
	}
}
