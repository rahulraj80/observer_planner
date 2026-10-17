package microsoft

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
)

const (
	userA = "254efcd0-5094-4fc5-b2b3-1637629393e1"
	userB = "4db02e4b-d144-400e-b194-53253a34c5be"
)

func interactionsPath(u string) string {
	return "/v1.0/copilot/users/" + u + "/interactionHistory/getAllEnterpriseInteractions"
}

// TestInteractionsParsesCorpusFixture (lane L15): the reference example
// lands as one prompt content item keyed by the queried user, never as a
// usage, fact or graph landing.
func TestInteractionsParsesCorpusFixture(t *testing.T) {
	r := resp(interactionsPath(userA), fixture(t, SurfaceInteractions, "normal_page.json"))
	r.Params = vendorapi.Params{"user_ids": userA}
	p, err := parseInteractions(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Content) != 1 || p.HasActivity() || len(p.Resources) != 0 || p.NextCursor != "" {
		t.Fatalf("page: %+v", p)
	}
	c := p.Content[0]
	if c.ItemID != userA+":1764008994427" || c.Role != "prompt" || c.IdentityKind != "user_id" || c.Identity != userA ||
		c.Text != "What are the key action items from my recent meetings?" ||
		!c.At.Equal(time.Date(2025, 11, 24, 18, 29, 54, 427e6, time.UTC)) || !strings.HasPrefix(c.SessionRef, "19:") {
		t.Fatalf("item: %+v", c)
	}
	if err := vendorapi.ValidateContent(p); err != nil {
		t.Fatal(err)
	}
}

// TestInteractionsFansOutOverUsersAndFollowsOnlyTheirNextLink: the first
// request reads the first user with both createdDateTime bounds; a next link
// stays on that user; the end of a user moves to the next one; a cursor or
// next link naming anything else is refused.
func TestInteractionsFansOutOverUsersAndFollowsOnlyTheirNextLink(t *testing.T) {
	d := Interactions()
	p := vendorapi.Params{"user_ids": userA + "," + userB}
	w := vendorapi.Window{From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}
	req, err := d.Build(p, w, "")
	if err != nil || req.Path != interactionsPath(userA) || req.Query["$top"][0] != "100" ||
		req.Query["$filter"][0] != "createdDateTime gt 2026-10-01T00:00:00Z and createdDateTime lt 2026-10-02T00:00:00Z" {
		t.Fatalf("first request: %+v, %v", req, err)
	}
	body := []byte(`{"value":[],"@odata.nextLink":"https://graph.microsoft.com/v1.0/copilot/users/` + userA +
		`/interactionHistory/getAllEnterpriseInteractions?$top=100&$skiptoken=abc"}`)
	r := resp(interactionsPath(userA), body)
	r.Params = p
	pg, err := parseInteractions(r)
	if err != nil || !strings.HasPrefix(pg.NextCursor, "0|") {
		t.Fatalf("next link: %q, %v", pg.NextCursor, err)
	}
	if req, err = d.Build(p, w, pg.NextCursor); err != nil || req.Query["$skiptoken"][0] != "abc" {
		t.Fatalf("follow: %+v, %v", req, err)
	}
	r = resp(interactionsPath(userA), []byte(`{"value":[{"id":"1","createdDateTime":"2026-10-01T01:00:00Z","interactionType":"unknownFutureValue"}]}`))
	r.Params, r.Cursor = p, "0"
	if pg, err = parseInteractions(r); err != nil || pg.NextCursor != "1" || len(pg.Content) != 0 {
		t.Fatalf("end of user A: %+v, %v", pg, err)
	}
	if req, err = d.Build(p, w, "1"); err != nil || req.Path != interactionsPath(userB) {
		t.Fatalf("user B: %+v, %v", req, err)
	}
	for name, c := range map[string]string{
		"no such user": "2", "foreign path": "0|/v1.0/users?$skiptoken=x",
		"undocumented member": "0|" + interactionsPath(userA) + "?$skiptoken=x&$select=body",
	} {
		if _, err := d.Build(p, w, c); err == nil {
			t.Errorf("%s: built", name)
		}
	}
	for name, ids := range map[string]string{
		"none": "", "not a guid": "alice", "upper case": strings.ToUpper(userA),
		"repeat": userA + "," + userA,
	} {
		if _, err := d.Build(vendorapi.Params{"user_ids": ids}, w, ""); err == nil {
			t.Errorf("user_ids %s: built", name)
		}
	}
	r = resp(interactionsPath(userA), []byte(`{"value":[{"id":"1","interactionType":"userPrompt"}]}`))
	r.Params = p
	if _, err := parseInteractions(r); !errors.Is(err, vendorapi.ErrSchemaDrift) {
		t.Fatalf("missing createdDateTime: %v", err)
	}
	if d.Supplies[0] != "Content" || !d.ClosedDTO || d.LegacyPoller != LegacyInteractions {
		t.Fatalf("driver: %+v", d)
	}
}
