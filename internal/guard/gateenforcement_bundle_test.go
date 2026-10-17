package guard

import (
	"testing"
)

// TestGateEnforcementOrgBundle pins the org half of the gate posture (Sol
// A2-02 follow-up): an org bundle that enforces closes the tool gate; a
// bundle that is present but does not verify counts as enforcing on a
// managed node (and on one whose tenancy is unresolved) and as absent on
// an individual one.
func TestGateEnforcementOrgBundle(t *testing.T) {
	t.Parallel()
	managed := func(v bool) *bool { return &v }
	cases := []struct {
		name     string
		bundle   func(t *testing.T) (string, string)
		tenancy  *bool
		wantTool bool
	}{
		{"enforcing bundle", func(t *testing.T) (string, string) {
			return signedEnvelope(t, 1, "[[override]]\nrule = \"R-101\"\nenforce = true\n")
		}, managed(false), true},
		{"non-enforcing bundle", func(t *testing.T) (string, string) {
			return signedEnvelope(t, 2, "[[override]]\nrule = \"R-172\"\noverridable = true\n")
		}, managed(true), false},
		{"pin mismatch, managed", func(t *testing.T) (string, string) {
			env, _ := signedEnvelope(t, 3, "")
			return env, "0000"
		}, managed(true), true},
		{"pin mismatch, tenancy unresolved", func(t *testing.T) (string, string) {
			env, _ := signedEnvelope(t, 4, "")
			return env, "0000"
		}, nil, true},
		{"pin mismatch, individual", func(t *testing.T) (string, string) {
			env, _ := signedEnvelope(t, 5, "")
			return env, "0000"
		}, managed(false), false},
		{"corrupt envelope, managed", func(*testing.T) (string, string) { return "{not json", "" }, managed(true), true},
		{"no bundle, managed", func(*testing.T) (string, string) { return "", "" }, managed(true), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, pin := tc.bundle(t)
			files := map[string]string{}
			if env != "" {
				files[orgBundlePath] = env
			}
			cfg := guardCfgOrg()
			cfg.Mode = "observe"
			cfg.Prompt.Enabled = false
			opts := Options{Config: cfg, Home: "/home/u", ReadFile: fsMap(files), OrgKeyPinHash: pin}
			if tc.tenancy != nil {
				v := *tc.tenancy
				opts.ManagedTenancy = func() bool { return v }
			}
			g, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := g.GateEnforcement().Tool; got != tc.wantTool {
				t.Errorf("tool gate = %v, want %v (issues %v)", got, tc.wantTool, g.LoadIssues())
			}
		})
	}
}
