package feasibility

import (
	"strings"
	"testing"
)

var testRules = []Rule{
	{ID: "gw", Option: "AI gateway", Needs: []string{"org.aigateway"}, FixStep: "Settings > AI gateway"},
	{ID: "store", Option: "S3 store", Needs: []string{"org.sor.s3"}, Unsafe: "data would be written somewhere else", Fix: "Deploy the full image."},
	{ID: "sandbox", Option: "Sandbox", Needs: []string{"host.bwrap"}},
	{ID: "sandbox.clone", Option: "Clone workspace", Requires: []string{"sandbox"}},
	{ID: "a", Option: "A", Conflicts: []string{"b"}},
	{ID: "b", Option: "B"},
}

// TestEvaluateTable is one case per row of the check table and the effect
// table (CLAUDE.md module rule 5).
func TestEvaluateTable(t *testing.T) {
	cases := []struct {
		name       string
		in         Input
		id         string
		feasible   bool
		effect     Effect
		cause      string
		reasonPart string
		step       string
	}{
		{"feasible when nothing is missing", Input{Configured: map[string]bool{"gw": true}}, "gw", true, EffectNone, "", "", ""},
		{"configured but missing component is off", Input{Configured: map[string]bool{"gw": true}, Unavailable: map[string]string{"org.aigateway": "not in this image"}}, "gw", false, EffectOff, "org.aigateway", "not in this image", "Settings > AI gateway"},
		{"not configured and missing is unavailable", Input{Unavailable: map[string]string{"org.aigateway": "not in this image"}}, "gw", false, EffectUnavailable, "org.aigateway", "not in this image", "Settings > AI gateway"},
		{"unsafe option refuses", Input{Configured: map[string]bool{"store": true}, Unavailable: map[string]string{"org.sor.s3": "not in this image"}}, "store", false, EffectRefuse, "org.sor.s3", "not in this image", ""},
		{"required option off", Input{Configured: map[string]bool{"sandbox.clone": true}}, "sandbox.clone", false, EffectOff, "sandbox", "needs Sandbox to be on", ""},
		{"required option infeasible carries its reason", Input{Configured: map[string]bool{"sandbox": true, "sandbox.clone": true}, Unavailable: map[string]string{"host.bwrap": "bubblewrap is not installed"}}, "sandbox.clone", false, EffectOff, "sandbox", "bubblewrap is not installed", ""},
		{"conflict", Input{Configured: map[string]bool{"a": true, "b": true}}, "a", false, EffectOff, "b", "cannot be on together with B", ""},
		{"conflict partner not on", Input{Configured: map[string]bool{"a": true}}, "a", true, EffectNone, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := Evaluate(testRules, tc.in).Get(tc.id)
			if !ok {
				t.Fatalf("no verdict for %s", tc.id)
			}
			if v.Feasible != tc.feasible || v.Effect != tc.effect || v.Cause != tc.cause {
				t.Fatalf("got feasible=%v effect=%q cause=%q, want %v %q %q", v.Feasible, v.Effect, v.Cause, tc.feasible, tc.effect, tc.cause)
			}
			if tc.reasonPart != "" && !strings.Contains(v.Reason, tc.reasonPart) {
				t.Fatalf("reason %q lacks %q", v.Reason, tc.reasonPart)
			}
			if v.FixStep != tc.step {
				t.Fatalf("fix step %q, want %q", v.FixStep, tc.step)
			}
			if !v.Feasible && v.Fix == "" {
				t.Fatal("an infeasible verdict must carry a fix")
			}
		})
	}
}

func TestRefusalErrorNamesCauseAndFix(t *testing.T) {
	in := Input{Configured: map[string]bool{"store": true, "gw": true}, Unavailable: map[string]string{"org.sor.s3": "not in this image", "org.aigateway": "not in this image"}}
	rep := Evaluate(testRules, in)
	err := RefusalError(testRules, rep)
	if err == nil {
		t.Fatal("want a refusal for the unsafe option")
	}
	for _, want := range []string{"S3 store", "not in this image", "Deploy the full image", "data would be written somewhere else"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "AI gateway") {
		t.Errorf("an ordinary feature must never be part of the refusal: %q", err)
	}
	if len(rep.Off()) != 1 || rep.Off()[0].ID != "gw" {
		t.Errorf("want gw switched off, got %+v", rep.Off())
	}
}

func TestIgnoredProblemsAreOffNeverRefusals(t *testing.T) {
	vs := Ignored([]string{"b problem", "a problem"}, "[components]")
	if len(vs) != 2 || !strings.Contains(vs[0].Reason, "a problem") {
		t.Fatalf("want sorted verdicts, got %+v", vs)
	}
	for _, v := range vs {
		if v.Effect != EffectOff || v.Feasible {
			t.Errorf("an ignored delta is off, never a refusal: %+v", v)
		}
	}
}

func TestLineIsOneSentenceWithStep(t *testing.T) {
	rep := Evaluate(testRules, Input{Configured: map[string]bool{"gw": true}, Unavailable: map[string]string{"org.aigateway": "org.aigateway is not in this image"}})
	v, _ := rep.Get("gw")
	got := v.Line()
	for _, want := range []string{"AI gateway is off", "not in this image", "Fix:", "Settings > AI gateway"} {
		if !strings.Contains(got, want) {
			t.Errorf("line %q lacks %q", got, want)
		}
	}
}
