package guard

import (
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/marmutapp/superbased-observer/internal/guardfallback"
)

const fallbackBundle = `[[override]]
rule = "R-110"
decision = "deny"

[fallbacks]
user_policy_invalid = "fail_closed"
sandbox_unavailable = "org_disallow"
`

// TestOrgFallbacksPerTenancy pins the one tenancy gate (D-221): the org's
// [fallbacks] choices apply on a managed node and on a process whose tenancy
// is unresolved (the D-194 conservative reading), never on an individual
// node, and an absent or unverifiable bundle means the defaults.
func TestOrgFallbacksPerTenancy(t *testing.T) {
	t.Parallel()
	b := func(v bool) *bool { return &v }
	strict := guardfallback.Fallbacks{
		UserPolicyInvalid:  guardfallback.UserPolicyFailClosed,
		SandboxUnavailable: guardfallback.SandboxOrgDisallow,
	}
	cases := []struct {
		name    string
		body    string
		badPin  bool
		tenancy *bool
		want    guardfallback.Fallbacks
	}{
		{"managed", fallbackBundle, false, b(true), strict},
		{"unresolved tenancy", fallbackBundle, false, nil, strict},
		{"individual", fallbackBundle, false, b(false), guardfallback.Fallbacks{}},
		{"managed, no table", "[[override]]\nrule = \"R-110\"\ndecision = \"deny\"\n", false, b(true), guardfallback.Fallbacks{}},
		{"managed, bundle does not verify", fallbackBundle, true, b(true), guardfallback.Fallbacks{}},
		{"managed, no bundle", "", false, b(true), guardfallback.Fallbacks{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			pin := ""
			if tc.body != "" {
				env, p := signedEnvelope(t, 9, tc.body)
				files[orgBundlePath] = env
				pin = p
				if tc.badPin {
					pin = "0000"
				}
			}
			opts := Options{Config: guardCfgOrg(), Home: "/home/u", ReadFile: fsMap(files), OrgKeyPinHash: pin}
			if tc.tenancy != nil {
				v := *tc.tenancy
				opts.ManagedTenancy = func() bool { return v }
			}
			g, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := g.OrgFallbacks(); got != tc.want.Normalize() {
				t.Errorf("OrgFallbacks = %+v, want %+v (issues %v)", got, tc.want.Normalize(), g.LoadIssues())
			}
		})
	}
}

// TestGateEnforcementCarriesUserPolicyMode pins that the fail-closed MODE
// rides the one gate-enforcement derivation (D-194) and is only ever set
// while some gate family is enforced: "fail closed under enforcement".
func TestGateEnforcementCarriesUserPolicyMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mode    string
		prompt  bool
		managed bool
		want    bool
	}{
		{"managed enforce", "enforce", true, true, true},
		{"managed observe, prompt lane enforces", "observe", true, true, true},
		{"managed observe, nothing enforced", "observe", false, true, false},
		{"individual enforce", "enforce", true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, pin := signedEnvelope(t, 3, "[fallbacks]\nuser_policy_invalid = \"fail_closed\"\n")
			cfg := guardCfgOrg()
			cfg.Mode = tc.mode
			if tc.prompt {
				// The prompt lane enforcing on its own (Sol A2-01): hook lane
				// on, independent enforcement, a mode that can stop a prompt.
				cfg.Prompt.Enabled, cfg.Prompt.HookLane, cfg.Prompt.EnforceIndependent = true, true, true
				cfg.Prompt.Mode = string(PromptModeBlock)
			}
			managed := tc.managed
			g, err := New(Options{
				Config: cfg, Home: "/home/u", ReadFile: fsMap(map[string]string{orgBundlePath: env}),
				OrgKeyPinHash: pin, ManagedTenancy: func() bool { return managed },
			})
			if err != nil {
				t.Fatal(err)
			}
			e := g.GateEnforcement()
			if e.UserPolicyFailClosed != tc.want {
				t.Errorf("UserPolicyFailClosed = %v, want %v (enforcement %+v)", e.UserPolicyFailClosed, tc.want, e)
			}
		})
	}
}

// TestFallbacksTableIsOrgOnly pins that a developer cannot author the org's
// choice: [fallbacks] in a user or project file is an unknown-key error, and
// a user file carrying it is dropped like any other broken user policy.
func TestFallbacksTableIsOrgOnly(t *testing.T) {
	t.Parallel()
	body := "[fallbacks]\nuser_policy_invalid = \"loud\"\n"
	for _, layer := range []string{"user", "project", "trusted_project"} {
		if probs := Lint([]byte(body), layer); len(probs) == 0 || !strings.Contains(probs[0], "fallbacks") {
			t.Errorf("%s layer accepted [fallbacks]: %v", layer, probs)
		}
	}
	if probs := Lint([]byte(body), "org"); len(probs) != 0 {
		t.Errorf("org layer refused a valid table: %v", probs)
	}
}

// TestFallbacksAuthoringStrictConsumerTolerant pins GUARD-FWD-1 for the new
// table: the publish gate refuses an unknown key or value, while a node
// accepts the same signed bundle, notes the entry and keeps the default for
// that key (a newer server may know a value this agent does not).
func TestFallbacksAuthoringStrictConsumerTolerant(t *testing.T) {
	t.Parallel()
	body := "[[override]]\nrule = \"R-110\"\ndecision = \"deny\"\n\n[fallbacks]\nuser_policy_invalid = \"fail_closed_twice\"\nsandbox_unavailable = \"org_disallow\"\nfuture_knob = \"on\"\n"
	if probs := Lint([]byte(body), "org"); len(probs) != 2 {
		t.Fatalf("authoring gate problems = %v, want the bad value and the unknown key", probs)
	}
	problems, notes := AcceptOrgBundleTOML([]byte(body))
	if len(problems) != 0 {
		t.Fatalf("consumer gate refused a bundle from a newer server: %v", problems)
	}
	if len(notes) != 2 {
		t.Fatalf("notes = %v", notes)
	}
	env, pin := signedEnvelope(t, 4, body)
	g, err := New(Options{
		Config: guardCfgOrg(), Home: "/home/u", ReadFile: fsMap(map[string]string{orgBundlePath: env}),
		OrgKeyPinHash: pin, ManagedTenancy: func() bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	got := g.OrgFallbacks()
	if got.UserPolicyInvalid != guardfallback.UserPolicyLoud || got.SandboxUnavailable != guardfallback.SandboxOrgDisallow {
		t.Errorf("OrgFallbacks = %+v, want the unknown value ignored and the known one kept", got)
	}
}

// preD221PolicyFile is the org-bundle on-disk shape as it was BEFORE the
// [fallbacks] table existed (frozen copy of tomlPolicyFile at 7d0c35e2d). It
// stands in for an older agent's decoder.
type preD221PolicyFile struct {
	Rule     []tomlRule     `toml:"rule"`
	Override []tomlOverride `toml:"override"`
	Disable  []string       `toml:"disable"`
}

// TestOlderAgentIgnoresFallbacksTable is the older-agent compatibility pin:
// an agent that predates D-221 decodes a bundle carrying [fallbacks] with
// every unknown key confined to that table, and its org loader (GUARD-FWD-1,
// pinned by orgcompat_test.go's "unknown top-level table" case) IGNORES and
// notes undecoded org keys rather than rejecting the bundle. So an older
// agent keeps the org floor and runs both defaults (loud, recorded
// acceptance), which is exactly what it did before the table existed.
func TestOlderAgentIgnoresFallbacksTable(t *testing.T) {
	t.Parallel()
	var old preD221PolicyFile
	meta, err := toml.Decode(fallbackBundle, &old)
	if err != nil {
		t.Fatalf("an older agent could not even parse the bundle: %v", err)
	}
	var undecoded []string
	for _, k := range meta.Undecoded() {
		undecoded = append(undecoded, k.String())
	}
	sort.Strings(undecoded)
	want := []string{"fallbacks", "fallbacks.sandbox_unavailable", "fallbacks.user_policy_invalid"}
	if strings.Join(undecoded, ",") != strings.Join(want, ",") {
		t.Fatalf("older agent's undecoded keys = %v, want exactly the table %v", undecoded, want)
	}
	if len(old.Override) != 1 || old.Override[0].Rule != "R-110" {
		t.Fatalf("older agent lost the rules: %+v", old.Override)
	}
	// And the default render keeps an org that never sets a fallback
	// byte-identical for every agent generation.
	if guardfallback.Render(guardfallback.Fallbacks{}) != "" {
		t.Fatal("default fallbacks must not add a table")
	}
}

func TestUserPolicyBlockReasonNamesFileAndLine(t *testing.T) {
	t.Parallel()
	r := UserPolicyBlockReason(UserPolicyStatus{Path: "/home/u/.observer/guard-policy.toml", Present: true, Line: 7, Error: "parse: bad"})
	for _, want := range []string{"/home/u/.observer/guard-policy.toml line 7", "parse: bad", "fail closed", "observer guard lint"} {
		if !strings.Contains(r, want) {
			t.Errorf("reason %q lacks %q", r, want)
		}
	}
	if strings.Contains(r, "—") {
		t.Error("no em dashes in developer copy")
	}
}
