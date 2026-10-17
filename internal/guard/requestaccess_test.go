package guard

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/policy"
)

// longSession is the longest session id shape the hook lanes carry (a
// UUID); the deny line must still fit the dashboard's 200-byte error
// display with it.
const longSession = "0f6b9d2e-6c1a-4f53-9a8e-2b7d1c4e9f10"

// TestRequestAccessLine pins the G4 sentence (design §1.3): an org-locked
// block names the rule and offers the node request form, prefilled with the
// rule and the session, or the CLI command; never more than 200 bytes (the
// node dashboard shows the first 200 of an error), one line, hyphens only,
// US spelling.
func TestRequestAccessLine(t *testing.T) {
	t.Parallel()
	base := "http://127.0.0.1:8081"
	cases := []struct {
		name       string
		av         ActionVerdict
		session    string
		want       []string
		wantAbsent []string
	}{
		{
			name:    "link with rule and session, and the command",
			av:      ActionVerdict{Kind: policy.KindShellExec, OrgLocked: true, RequestBase: base, Verdict: policy.Verdict{RuleID: "R-171"}},
			session: "s-1",
			want: []string{
				"observer: R-171 blocked this command; your organization locks this rule.",
				"Request access: " + base + "/requests?rule=R-171&session=s-1",
				"observer request access --rule R-171",
			},
		},
		{
			name:    "a long session id keeps the link, drops the command",
			av:      ActionVerdict{Kind: policy.KindConfigChange, OrgLocked: true, RequestBase: base, Verdict: policy.Verdict{RuleID: "R-171"}},
			session: longSession,
			want:    []string{"/requests?rule=R-171&session=" + longSession},
		},
		{
			name:       "no session id: the link carries the rule only",
			av:         ActionVerdict{Kind: policy.KindAPIRequest, OrgLocked: true, RequestBase: base, Verdict: policy.Verdict{RuleID: "R-172"}},
			want:       []string{base + "/requests?rule=R-172", "observer request access --rule R-172"},
			wantAbsent: []string{"session="},
		},
		{
			name:       "no dashboard origin: the command only",
			av:         ActionVerdict{Kind: policy.KindFileAccess, OrgLocked: true, Verdict: policy.Verdict{RuleID: "R-152"}},
			session:    "s-2",
			want:       []string{"Request access: run `observer request access --rule R-152"},
			wantAbsent: []string{"http"},
		},
		{
			name:    "a hostile rule id is escaped in the link",
			av:      ActionVerdict{Kind: policy.KindShellExec, OrgLocked: true, RequestBase: base, Verdict: policy.Verdict{RuleID: "R-1&x=<y>"}},
			session: "s 3",
			want:    []string{"rule=R-1%26x%3D%3Cy%3E", "session=s+3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := HumanBlockLine(tc.av, tc.session)
			if len(got) > RequestAccessMaxBytes {
				t.Fatalf("line is %d bytes, over %d: %q", len(got), RequestAccessMaxBytes, got)
			}
			if strings.ContainsAny(got, "\n\r—–") || strings.Contains(got, "organisation") || strings.Contains(got, "ask your admin") {
				t.Fatalf("copy rule broken: %q", got)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("line %q missing %q", got, w)
				}
			}
			for _, a := range tc.wantAbsent {
				if strings.Contains(got, a) {
					t.Errorf("line %q must not contain %q", got, a)
				}
			}
		})
	}
}

// TestRequestAccessLineFitsEveryKind: whatever the event kind and with the
// longest session id, the line fits and still offers a way to ask.
func TestRequestAccessLineFitsEveryKind(t *testing.T) {
	t.Parallel()
	kinds := []policy.EventKind{policy.EventKind("brand_new_kind")}
	for k := range blockedWhatByKind {
		kinds = append(kinds, k)
	}
	for _, k := range kinds {
		for _, base := range []string{"", "http://127.0.0.1:8081", "http://[::1]:65535"} {
			av := ActionVerdict{Kind: k, OrgLocked: true, RequestBase: base, Verdict: policy.Verdict{RuleID: "R-1234"}}
			got := HumanBlockLine(av, longSession)
			if len(got) > RequestAccessMaxBytes || !strings.Contains(got, "Request access") {
				t.Errorf("kind %s base %q: %d bytes %q", k, base, len(got), got)
			}
		}
	}
}

// TestRequestAccessStampedByTheGuard: the guard stamps its configured
// dashboard origin on every verdict it builds, so the hook reply, the proxy
// deny and the desktop toast all link the same request form.
func TestRequestAccessStampedByTheGuard(t *testing.T) {
	t.Parallel()
	env, pin := signedEnvelope(t, 11, orgOverridableBundleTOML)
	cfg := guardCfgOrg()
	cfg.Mode = "enforce"
	g, err := New(Options{
		Config: cfg, Home: "/home/u", KnownProjectRoots: []string{"/home/u/proj"},
		ReadFile: fsMap(map[string]string{orgBundlePath: env}), OrgKeyPinHash: pin,
	})
	if err != nil {
		t.Fatal(err)
	}
	g.SetRequestAccessBase("http://127.0.0.1:9999")
	av, _ := g.EvaluateHook(policy.Event{
		Kind: policy.KindShellExec, ActionType: "run_command", Target: "rm -rf ~", SessionID: "sA",
		ProjectRoot: "/home/u/proj", Caps: policy.Capabilities{PreExecution: true, CanBlock: true, CanAsk: true},
	})
	if av.RequestBase != "http://127.0.0.1:9999" {
		t.Fatalf("RequestBase = %q", av.RequestBase)
	}
	em := HumanizeEmission(Emission{Permission: "deny", Enforced: true, Reason: "rule reason"}, av, "sA")
	if !strings.HasPrefix(em.Reason, "observer: ") || !strings.Contains(em.Reason, "http://127.0.0.1:9999/requests?rule=") {
		t.Fatalf("emission does not lead with the request line: %q", em.Reason)
	}
}

// TestPromptRequestAccessHint: an org-locked prompt block offers the same
// request path (the prompt gate's own house message appends it).
func TestPromptRequestAccessHint(t *testing.T) {
	t.Parallel()
	pv := PromptVerdict{
		Verdict: policy.Verdict{RuleID: "R-172", Decision: policy.DecisionDeny},
		Outcome: PromptOutcomeBlocked, OrgLocked: true, RequestBase: "http://127.0.0.1:8081",
	}
	got := PromptRequestAccessHint(pv, "s-9")
	if !strings.Contains(got, "/requests?rule=R-172&session=s-9") || len(got) > RequestAccessMaxBytes {
		t.Fatalf("hint %q", got)
	}
	pv.OrgLocked = false
	if PromptRequestAccessHint(pv, "s-9") != "" {
		t.Fatal("an unlocked prompt block must not offer a request")
	}
}

// TestProxyPromptHouseMessage_OrgLockedOffersRequestAccess: the proxy
// lane's prompt block offers the same request line as the hook lane, inside
// the first 200 bytes.
func TestProxyPromptHouseMessage_OrgLockedOffersRequestAccess(t *testing.T) {
	t.Parallel()
	pv := PromptVerdict{
		Verdict:   policy.Verdict{RuleID: "R-172", Decision: policy.DecisionDeny, Reason: "secret in prompt"},
		Outcome:   PromptOutcomeBlocked,
		Detectors: "github_pat", OrgLocked: true, RequestBase: "http://127.0.0.1:8081",
	}
	got := proxyPromptHouseMessage(pv, "s9")
	link := "Request access: http://127.0.0.1:8081/requests?rule=R-172&session=s9"
	i := strings.Index(got, link)
	if i < 0 || i+len(link) > RequestAccessMaxBytes || !strings.Contains(got, "secret in prompt") {
		t.Fatalf("message %q", got)
	}
}
