package orgclient

import (
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestPushIntervalGovernanceCeiling walks the live push-interval composition
// (2026-10-02 budget-latency arc). The org's signed ceiling lowers the node's
// own interval and can be RAISED or WITHDRAWN again without a restart,
// because the client composes against the node's pre-governance value rather
// than the overlaid one the config carried at startup.
func TestPushIntervalGovernanceCeiling(t *testing.T) {
	cases := []struct {
		name    string
		cfgSecs int
		govern  bool
		local   int64
		ceiling int64
		want    time.Duration
	}{
		{name: "ungoverned default", cfgSecs: 0, want: 120 * time.Second},
		{name: "ungoverned enrol-written 900", cfgSecs: 900, want: 900 * time.Second},
		{name: "org lowers 900 to 120", cfgSecs: 120, govern: true, local: 900, ceiling: 120, want: 120 * time.Second},
		{name: "org raises its ceiling from 60 to 300 against a 900 local", cfgSecs: 60, govern: true, local: 900, ceiling: 300, want: 300 * time.Second},
		{name: "ceiling withdrawn returns the node's own value", cfgSecs: 120, govern: true, local: 900, ceiling: 0, want: 900 * time.Second},
		{name: "faster local survives", cfgSecs: 45, govern: true, local: 45, ceiling: 120, want: 45 * time.Second},
		{name: "unset local with a ceiling above the default keeps the default", cfgSecs: 120, govern: true, local: 0, ceiling: 600, want: 120 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{cfg: config.OrgClientConfig{PushIntervalSeconds: tc.cfgSecs}}
			if tc.govern {
				ceiling := tc.ceiling
				c.SetPushIntervalGovernance(tc.local, func() int64 { return ceiling })
			}
			if got := c.pushInterval(); got != tc.want {
				t.Fatalf("pushInterval = %v, want %v", got, tc.want)
			}
		})
	}
}
