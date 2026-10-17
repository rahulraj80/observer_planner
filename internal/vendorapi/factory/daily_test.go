package factory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/vendorapi"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
)

func dailyFixture(t *testing.T, s vs.SurfaceID) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "vendorapi", "factory", string(s), "normal_page.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestDailyAnalytics (lane ENTAPI-L16C, T-710): each documented ungrouped
// example lands its scalar metrics per UTC day, the per-client and per-tool
// breakdowns carry their key as Feature and are never added to the total,
// the derived ratios, trailing-window counts and organization-authored names
// never land, and every field-policy row is schema drift.
func TestDailyAnalytics(t *testing.T) {
	day := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		drv  vendorapi.Driver
		want map[string]int64 // metric/feature -> whole value
	}{
		{Activity(), map[string]int64{
			"daily_active_users/": 128, "sessions/": 890, "messages/": 12500, "user_messages/": 4200,
			"daily_active_users/terminal-ui": 95, "daily_active_users/web": 42, "daily_active_users/non-interactive-cli": 18,
		}},
		{Productivity(), map[string]int64{
			"files_created/": 245, "files_edited/": 1820, "git_commits/": 156, "git_prs_created/": 42,
		}},
		{Tools(), map[string]int64{
			"tool_calls/": 45000, "skills_invocations/": 320, "slash_commands_invocations/": 1500,
			"hooks_invocations/": 2800, "mcp_users/": 42, "web_users/": 42, "tool_calls/Read": 12500,
			"tool_calls/Edit": 8200, "tool_calls/Execute": 6100,
		}},
	} {
		p, err := c.drv.ParseResponse(vendorapi.Response{Status: 200, Body: dailyFixture(t, c.drv.Surface)})
		if err != nil {
			t.Fatalf("%s: %v", c.drv.Surface, err)
		}
		if err := vendorapi.Validate(p); err != nil {
			t.Fatalf("%s: %v", c.drv.Surface, err)
		}
		got := map[string]int64{}
		for _, f := range p.Facts {
			if !f.IntervalStart.Equal(day) || !f.IntervalEnd.Equal(day.Add(24*time.Hour)) || f.CoverageDomain != DailyDomain ||
				f.IdentityKind != "" || f.ModelRaw != "" {
				t.Fatalf("%s fact %+v", c.drv.Surface, f)
			}
			got[f.Metric+"/"+f.Feature] = f.ValueMicros / 1_000_000
		}
		if len(got) != len(c.want) {
			t.Errorf("%s facts %v, want %v", c.drv.Surface, got, c.want)
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s %s = %d, want %d", c.drv.Surface, k, got[k], v)
			}
		}
		req, err := c.drv.Build(nil, vendorapi.Window{From: day.Add(26 * time.Hour), To: day.Add(50 * time.Hour)}, "")
		if err != nil || req.Query["startDate"][0] != "2026-01-15" || req.Query["endDate"][0] != "2026-01-16" ||
			req.Query["group_by"] != nil {
			t.Errorf("%s request %+v %v", c.drv.Surface, req, err)
		}
		if _, err := c.drv.Build(nil, vendorapi.Window{From: day, To: day.Add(time.Hour)}, "x"); err == nil {
			t.Errorf("%s accepted a cursor", c.drv.Surface)
		}
	}
	if !Tools().ClosedDTO || Activity().ClosedDTO {
		t.Error("tools must be closed-DTO (hooks_by_event names a command)")
	}
	for name, c := range map[string]struct {
		drv  vendorapi.Driver
		body string
	}{
		"data missing":    {Activity(), `{"meta":{}}`},
		"date missing":    {Activity(), `{"data":[{"daily_active_users":1,"sessions":1,"messages":1,"user_messages":1}]}`},
		"date format":     {Productivity(), `{"data":[{"date":"15/01/2026","files_created":1,"files_edited":1,"git_commits":1,"git_prs_created":1}]}`},
		"scalar missing":  {Productivity(), `{"data":[{"date":"2026-01-15","files_created":1,"files_edited":1,"git_commits":1}]}`},
		"quoted number":   {Productivity(), `{"data":[{"date":"2026-01-15","files_created":"1","files_edited":1,"git_commits":1,"git_prs_created":1}]}`},
		"negative":        {Productivity(), `{"data":[{"date":"2026-01-15","files_created":-1,"files_edited":1,"git_commits":1,"git_prs_created":1}]}`},
		"repeated day":    {Productivity(), `{"data":[{"date":"2026-01-15","files_created":1,"files_edited":1,"git_commits":1,"git_prs_created":1},{"date":"2026-01-15","files_created":1,"files_edited":1,"git_commits":1,"git_prs_created":1}]}`},
		"grouped row":     {Activity(), `{"data":[{"date":"2026-01-15","group_key":"web","daily_active_users":1,"sessions":1,"messages":1,"user_messages":1}]}`},
		"client value":    {Activity(), `{"data":[{"date":"2026-01-15","daily_active_users":1,"sessions":1,"messages":1,"user_messages":1,"daily_active_users_by_client":{"web":"x"}}]}`},
		"repeated tool":   {Tools(), `{"data":[{"date":"2026-01-15","tool_calls":1,"skills_invocations":1,"slash_commands_invocations":1,"hooks_invocations":1,"mcp_users_with_mcp":1,"web_users":1,"by_tool":[{"tool":"Read","invocations":1},{"tool":"Read","invocations":1}]}]}`},
		"tool name shape": {Tools(), `{"data":[{"date":"2026-01-15","tool_calls":1,"skills_invocations":1,"slash_commands_invocations":1,"hooks_invocations":1,"mcp_users_with_mcp":1,"web_users":1,"by_tool":[{"tool":"rm -rf /","invocations":1}]}]}`},
		"status":          {Tools(), ``},
	} {
		status := 200
		if name == "status" {
			status = 202
		}
		if _, err := c.drv.ParseResponse(vendorapi.Response{Status: status, Body: []byte(c.body)}); !errors.Is(err, vendorapi.ErrSchemaDrift) {
			t.Errorf("%s: err %v, want schema drift", name, err)
		}
	}
	// A hook command never reaches a page: the closed DTO does not decode it.
	p, err := Tools().ParseResponse(vendorapi.Response{Status: 200, Body: []byte(`{"data":[{"date":"2026-01-15","tool_calls":1,
		"skills_invocations":1,"slash_commands_invocations":1,"hooks_invocations":1,"mcp_users_with_mcp":1,"web_users":1,
		"hooks_by_event":[{"event":"PostToolUse","command":"curl -H 'Authorization: Bearer sk-canary-hook'","count":1}]}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range p.Facts {
		if f.Feature != "" {
			t.Errorf("a hook value landed as a dimension: %+v", f)
		}
	}
}
