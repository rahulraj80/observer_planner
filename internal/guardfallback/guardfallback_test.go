package guardfallback

import (
	"strings"
	"testing"
)

func TestDefaultsAndGet(t *testing.T) {
	var zero Fallbacks
	if !zero.IsDefault() {
		t.Fatal("zero value must be all defaults")
	}
	if zero.Get(KeyUserPolicyInvalid) != UserPolicyLoud || zero.Get(KeySandboxUnavailable) != SandboxRecordedAcceptance {
		t.Fatalf("defaults = %+v", zero.Normalize())
	}
	if zero.FailClosedOnInvalidUserPolicy() || zero.DisallowWithoutSandbox() {
		t.Fatal("defaults must keep today's behaviour")
	}
	junk := Fallbacks{UserPolicyInvalid: "maybe"}
	if junk.Get(KeyUserPolicyInvalid) != UserPolicyLoud {
		t.Fatal("an unknown value must read as the default")
	}
	if zero.Get("nope") != "" {
		t.Fatal("unknown key must read empty")
	}
}

func TestDecode(t *testing.T) {
	cases := []struct {
		name     string
		raw      map[string]any
		want     Fallbacks
		problems int
	}{
		{"empty", map[string]any{}, Fallbacks{}, 0},
		{
			"both strict",
			map[string]any{KeyUserPolicyInvalid: "fail_closed", KeySandboxUnavailable: "org_disallow"},
			Fallbacks{UserPolicyFailClosed, SandboxOrgDisallow},
			0,
		},
		{
			"explicit defaults",
			map[string]any{KeyUserPolicyInvalid: "loud", KeySandboxUnavailable: "recorded_acceptance"},
			Fallbacks{UserPolicyLoud, SandboxRecordedAcceptance},
			0,
		},
		{"unknown value ignored", map[string]any{KeyUserPolicyInvalid: "fail_closed_harder"}, Fallbacks{}, 1},
		{
			"unknown key ignored, known kept",
			map[string]any{"future_key": "x", KeySandboxUnavailable: "org_disallow"},
			Fallbacks{SandboxUnavailable: SandboxOrgDisallow},
			1,
		},
		{"non-string", map[string]any{KeyUserPolicyInvalid: true}, Fallbacks{}, 1},
		{"unknown non-string", map[string]any{"x": int64(1)}, Fallbacks{}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, problems := Decode(c.raw)
			if got.Normalize() != c.want.Normalize() {
				t.Errorf("got %+v want %+v", got.Normalize(), c.want.Normalize())
			}
			if len(problems) != c.problems {
				t.Errorf("problems = %v, want %d", problems, c.problems)
			}
		})
	}
}

func TestStrictest(t *testing.T) {
	cases := []struct {
		name string
		in   []Fallbacks
		want Fallbacks
	}{
		{"none", nil, Fallbacks{}},
		{"one strict wins", []Fallbacks{{}, {UserPolicyInvalid: UserPolicyFailClosed}}, Fallbacks{UserPolicyInvalid: UserPolicyFailClosed}},
		{
			"per key",
			[]Fallbacks{{SandboxUnavailable: SandboxOrgDisallow}, {UserPolicyInvalid: UserPolicyFailClosed}},
			Fallbacks{UserPolicyFailClosed, SandboxOrgDisallow},
		},
		{
			"loud never loosens",
			[]Fallbacks{{UserPolicyInvalid: UserPolicyFailClosed}, {UserPolicyInvalid: UserPolicyLoud}},
			Fallbacks{UserPolicyInvalid: UserPolicyFailClosed},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Strictest(c.in...); got.Normalize() != c.want.Normalize() {
				t.Errorf("got %+v want %+v", got, c.want.Normalize())
			}
		})
	}
}

func TestEffectiveIsTenancyGated(t *testing.T) {
	org := Fallbacks{UserPolicyFailClosed, SandboxOrgDisallow}
	if got := Effective(org, true); got != org.Normalize() {
		t.Errorf("managed: got %+v", got)
	}
	if got := Effective(org, false); !got.IsDefault() {
		t.Errorf("individual must run the defaults, got %+v", got)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(KeyUserPolicyInvalid, UserPolicyFailClosed); err != nil {
		t.Fatal(err)
	}
	if err := Validate(KeyUserPolicyInvalid, "x"); err == nil {
		t.Fatal("bad value accepted")
	}
	if err := Validate("x", "loud"); err == nil {
		t.Fatal("bad key accepted")
	}
}

func TestRenderOmitsDefaults(t *testing.T) {
	if r := Render(Fallbacks{}); r != "" {
		t.Fatalf("default render = %q, want empty (older agents see byte-identical bundles)", r)
	}
	r := Render(Fallbacks{UserPolicyInvalid: UserPolicyFailClosed})
	if !strings.HasPrefix(r, "[fallbacks]\n") || !strings.Contains(r, `user_policy_invalid = "fail_closed"`) || strings.Contains(r, KeySandboxUnavailable) {
		t.Fatalf("render = %q", r)
	}
}

func TestSpliceRoundTrips(t *testing.T) {
	rules := "# my rules\n[[override]]\nrule = \"R-110\"\ndecision = \"deny\"\nenforce = true\n"
	strict := Fallbacks{UserPolicyFailClosed, SandboxOrgDisallow}
	cases := []struct {
		name string
		body string
		f    Fallbacks
	}{
		{"add to rules", rules, strict},
		{"replace existing", rules + "\n[fallbacks]\nuser_policy_invalid = \"loud\"\n", strict},
		{"existing in the middle", "[fallbacks]\nsandbox_unavailable = \"org_disallow\"\n\n" + rules, Fallbacks{UserPolicyInvalid: UserPolicyFailClosed}},
		{"reset to defaults removes table", rules + "\n[fallbacks]\nuser_policy_invalid = \"fail_closed\"\n", Fallbacks{}},
		{"empty body", "", strict},
		{"empty body defaults", "", Fallbacks{}},
		{"only table reset", "[fallbacks]\nuser_policy_invalid = \"fail_closed\"\n", Fallbacks{}},
		{"crlf", strings.ReplaceAll(rules+"[fallbacks]\r\nuser_policy_invalid = \"fail_closed\"\n", "\n", "\r\n"), Fallbacks{SandboxUnavailable: SandboxOrgDisallow}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := Splice(c.body, c.f)
			if err != nil {
				t.Fatalf("splice: %v", err)
			}
			got, present, problems, err := FromTOML(out)
			if err != nil || len(problems) > 0 {
				t.Fatalf("decode: %v %v\n%s", err, problems, out)
			}
			if got.Normalize() != c.f.Normalize() {
				t.Fatalf("got %+v want %+v\n%s", got.Normalize(), c.f.Normalize(), out)
			}
			if present != !c.f.IsDefault() {
				t.Errorf("present = %v for %+v\n%s", present, c.f, out)
			}
			if strings.Contains(c.body, "R-110") && !strings.Contains(out, "rule = \"R-110\"") {
				t.Errorf("rules lost:\n%s", out)
			}
			if strings.TrimSpace(out) == "" {
				t.Error("splice produced an empty bundle")
			}
		})
	}
}

func TestSpliceRefusesDottedForm(t *testing.T) {
	body := "fallbacks.user_policy_invalid = \"fail_closed\"\n"
	if _, err := Splice(body, Fallbacks{}); err == nil {
		t.Fatal("a dotted-key form was silently left in place")
	}
}

func TestPresent(t *testing.T) {
	for body, want := range map[string]bool{
		"":                               false,
		"[fallbacks]\n":                  true,
		"  [ fallbacks ]  # org\n":       true,
		"[[override]]\nrule = \"R-1\"\n": false,
		"# [fallbacks] in a comment\n":   false,
		"[fallbacks.extra]\nx = \"y\"\n": false,
		"[[rule]]\nid = \"fallbacks\"\n": false,
	} {
		if got := Present(body); got != want {
			t.Errorf("Present(%q) = %v, want %v", body, got, want)
		}
	}
}
