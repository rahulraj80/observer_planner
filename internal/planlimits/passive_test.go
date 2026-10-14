package planlimits

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fp(v float64) *float64 { return &v }
func ip(v int64) *int64     { return &v }

func TestFromLimitRow(t *testing.T) {
	r := LimitRow{
		ScopeHash: "default", Provider: "anthropic", SessionID: "s1", ObservedAt: time.Unix(1781900000, 0),
		Window5hUtil: fp(0.52), Window5hReset: ip(1781916000), Window7dUtil: fp(0.21), Window7dReset: ip(1782345600), Status: "allowed",
	}
	s, ok := FromLimitRow(r)
	if !ok {
		t.Fatal("want ok")
	}
	mustValid(t, s)
	if s.Provider != ProviderClaude || s.Rung != RungOfficial || s.Confidence != ConfPercentOnly {
		t.Fatalf("header %+v", s)
	}
	if w, _ := FindWindow(s, "5h"); *w.UsedFrac != 0.52 || w.ResetsAt.Unix() != 1781916000 {
		t.Fatalf("5h %+v", w)
	}
	if _, ok := FromLimitRow(LimitRow{Provider: "anthropic", ObservedAt: time.Unix(1, 0)}); ok {
		t.Error("no windows must be not ok")
	}
	if _, ok := FromLimitRow(LimitRow{Provider: "openai", Window5hUtil: fp(0.1)}); ok {
		t.Error("openai per-minute counters are not a plan window")
	}
	r.Status = "rejected"
	if s, _ := FromLimitRow(r); s.Status != StatusThrottled {
		t.Error("rejected must map to throttled")
	}
}

func TestFromLogRowFixtures(t *testing.T) {
	cases := []struct {
		file          string
		want5h, wantW *float64
		plan          string
		credits       int64
	}{
		{"codex_plus_both.json", fp(0.12), fp(0.44), "plus", -1},
		{"codex_prolite_weekly_only.json", nil, fp(0.31), "prolite", -1},
		{"codex_credits_throttled.json", fp(1.0), fp(0.97), "pro", 25},
	}
	for _, c := range cases {
		s, ok := FromLogRow(LogRow{Tool: "codex", ObservedAt: t0, Raw: fixture(t, c.file)})
		if !ok {
			t.Fatalf("%s: not ok", c.file)
		}
		mustValid(t, s)
		if s.PlanCode != c.plan {
			t.Errorf("%s: plan %q", c.file, s.PlanCode)
		}
		for key, want := range map[string]*float64{"5h": c.want5h, "weekly": c.wantW} {
			w, ok := FindWindow(s, key)
			if (want == nil) == ok {
				t.Errorf("%s: window %s presence mismatch", c.file, key)
				continue
			}
			if ok && !approx(*w.UsedFrac, *want) {
				t.Errorf("%s: %s = %v want %v", c.file, key, *w.UsedFrac, *want)
			}
		}
		b, ok := FindBalance(s, "credits")
		if (c.credits < 0) == ok || (ok && b.Amount != c.credits) {
			t.Errorf("%s: credits %+v", c.file, b)
		}
	}
	if _, ok := FromLogRow(LogRow{Tool: "cowork", Raw: fixture(t, "codex_plus_both.json")}); ok {
		t.Error("only the tools in PassiveLogTools map")
	}
}

func TestStatusLineFeed(t *testing.T) {
	f, err := ExtractStatusLineForward(fixture(t, "statusline_max_full.json"), time.Unix(1738420000, 0))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(f)
	for _, banned := range []string{"cwd", "transcript", "model", "cost", "/current", "workspace", "Opus"} {
		if strings.Contains(string(b), banned) {
			t.Errorf("forward record leaks %q: %s", banned, b)
		}
	}
	s, err := FromStatusLine(f)
	if err != nil {
		t.Fatal(err)
	}
	mustValid(t, s)
	if w5, _ := FindWindow(s, "5h"); !approx(*w5.UsedFrac, 0.235) || w5.ResetsAt.Unix() != 1738425600 {
		t.Errorf("5h %+v", w5)
	}
	if sl, _ := FindWindow(s, "spend_limit"); *sl.Used != 314_120_000 || *sl.Limit != 500_000_000 || *sl.Length != 30*24*time.Hour {
		t.Errorf("spend %+v", sl)
	}
	for name, p := range map[string]string{
		"api key user":      `{"session_id":"x","model":{"id":"m"}}`,
		"empty rate_limits": `{"session_id":"x","rate_limits":{}}`,
	} {
		if _, err := ExtractStatusLineForward([]byte(p), t0); !errors.Is(err, ErrNoRateLimits) {
			t.Errorf("%s: %v", name, err)
		}
	}
	f, _ = ExtractStatusLineForward([]byte(`{"session_id":"x","rate_limits":{"seven_day":{"used_percentage":5,"resets_at":2}}}`), t0)
	if s, _ := FromStatusLine(f); len(s.Windows) != 1 {
		t.Error("an absent 5h window must not be synthesized")
	}
	a, _ := ExtractStatusLineForward([]byte(`{"session_id":"x","rate_limits":{"five_hour":{"used_percentage":5,"resets_at":2}}}`), t0)
	b2, _ := ExtractStatusLineForward([]byte(`{"session_id":"x","rate_limits":{"five_hour":{"used_percentage":5,"resets_at":2}}}`), t0.Add(time.Second))
	c, _ := ExtractStatusLineForward([]byte(`{"session_id":"x","rate_limits":{"five_hour":{"used_percentage":6,"resets_at":2}}}`), t0)
	if StatusLineChanged(a, b2) || !StatusLineChanged(a, c) {
		t.Error("change-only dedup")
	}
}

func TestResolveScopeTable(t *testing.T) {
	obs := []AccountObs{
		{SessionID: "a", Tool: "claude-code", AccountKey: "k1", Label: "me@example.test"},
		{SessionID: "a", Tool: "claude-code", AccountKey: "k1"},
		{SessionID: "b", Tool: "claude-code", AccountKey: "k2"},
		{SessionID: "c", Tool: "claude-code", AccountKey: "k1"},
		{SessionID: "c", Tool: "claude-code", AccountKey: "k2"},
		{SessionID: "d", Tool: "codex", AccountKey: "k9"},
	}
	cases := []struct{ sid, tool, scope, reason string }{
		{"", "claude-code", DefaultScope, ScopeNoSession},
		{"zzz", "claude-code", DefaultScope, ScopeNoObservation},
		{"a", "claude-code", "acct:k1", ScopeBound},
		{"b", "claude-code", "acct:k2", ScopeBound},
		{"c", "claude-code", DefaultScope, ScopeConflict},
		{"d", "claude-code", DefaultScope, ScopeNoObservation}, // another tool's login is never borrowed
	}
	for _, c := range cases {
		got := ResolveScope(c.sid, c.tool, obs)
		if got.Scope != c.scope || got.Reason != c.reason {
			t.Errorf("%s/%s: %+v", c.sid, c.tool, got)
		}
	}
	if got := ScopeFor("acct:explicit", "a", "claude-code", obs); got.Scope != "acct:explicit" || got.Reason != ScopeExplicit {
		t.Errorf("explicit scope must be honoured: %+v", got)
	}
	if ScopeLabel("acct:k1", obs) != "me@example.test" || ScopeLabel(DefaultScope, obs) != "" {
		t.Error("scope label")
	}
}

// fakeAppServer answers like `codex app-server`: it enforces the handshake
// order, interleaves a notification and replies by method.
func fakeAppServer(t *testing.T, in io.Reader, out io.WriteCloser, results map[string]string, seen *[]string) {
	defer func() { _ = out.Close() }()
	sc := bufio.NewScanner(in)
	initialized := false
	for sc.Scan() {
		var m struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Errorf("non-JSON line %q", sc.Text())
			return
		}
		if strings.Contains(sc.Text(), `"jsonrpc"`) {
			t.Error("the jsonrpc member must be omitted on the wire")
		}
		*seen = append(*seen, m.Method+string(m.Params))
		if m.ID == nil {
			initialized = initialized || m.Method == "initialized"
			continue
		}
		id := strconv.FormatInt(*m.ID, 10)
		if m.Method != "initialize" && !initialized {
			_, _ = io.WriteString(out, `{"id":`+id+`,"error":{"code":-32600,"message":"not initialized"}}`+"\n")
			continue
		}
		_, _ = io.WriteString(out, `{"method":"account/rateLimits/updated","params":{}}`+"\n")
		res, ok := results[m.Method]
		if !ok {
			_, _ = io.WriteString(out, `{"id":`+id+`,"error":{"code":-32601,"message":"method not found"}}`+"\n")
			continue
		}
		var compact bytes.Buffer
		_ = json.Compact(&compact, []byte(res))
		_, _ = io.WriteString(out, `{"id":`+id+`,"result":`+compact.String()+`}`+"\n")
	}
}

func runAppServer(t *testing.T, results map[string]string) (Snapshot, []string, error) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	var seen []string
	done := make(chan struct{})
	go func() { fakeAppServer(t, sr, sw, results, &seen); close(done) }()
	snap, err := Drivers[SourceCodexAppServer].Converse(cw, cr, PlanInput{Now: time.Unix(1766940000, 0), Params: map[string]string{"observer_version": "test"}})
	_ = cw.Close()
	<-done
	return snap, seen, err
}

const acctChatGPT = `{"account":{"type":"chatgpt","email":"x@example.test","planType":"pro"}}`

func TestAppServer(t *testing.T) {
	s, seen, err := runAppServer(t, map[string]string{
		"initialize":   `{}`,
		"account/read": acctChatGPT,
		"account/rateLimits/read": `{"rateLimits":{"limitId":"codex",
		    "primary":{"usedPercent":12,"windowDurationMins":300,"resetsAt":1766948068},
		    "secondary":{"usedPercent":43,"windowDurationMins":10080,"resetsAt":1767407914},
		    "credits":{"hasCredits":true,"unlimited":false,"balance":"7"}},
		  "rateLimitsByLimitId":{"codex":{"limitId":"codex"},
		    "codex_spark":{"limitId":"codex_spark","primary":{"usedPercent":30,"windowDurationMins":300,"resetsAt":1766948068},
		      "secondary":{"usedPercent":100,"windowDurationMins":10080,"resetsAt":1767407914}}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustValid(t, s)
	for k, v := range map[string]float64{"5h": 0.12, "weekly": 0.43, "model:codex_spark:5h": 0.30, "model:codex_spark:weekly": 1.0} {
		if w, ok := FindWindow(s, k); !ok || !approx(*w.UsedFrac, v) {
			t.Errorf("%s: %+v", k, w)
		}
	}
	if s.PlanCode != "pro" || s.Rung != RungOfficial {
		t.Errorf("header %+v", s)
	}
	if b, ok := FindBalance(s, "credits"); !ok || b.Amount != 7 {
		t.Errorf("credits %+v", b)
	}
	for _, m := range seen {
		if strings.HasPrefix(m, "account/read") && !strings.Contains(m, `"refreshToken":false`) {
			t.Errorf("account/read must never ask for a refresh: %s", m)
		}
		if strings.HasPrefix(m, "initialize{") && !strings.Contains(m, AppServerClientName) {
			t.Errorf("client name must be honest: %s", m)
		}
	}
	if !strings.HasPrefix(seen[0], "initialize") || !strings.HasPrefix(seen[1], "initialized") {
		t.Errorf("handshake order %v", seen)
	}
	_, _, err = runAppServer(t, map[string]string{"initialize": `{}`, "account/read": `{"account":{"type":"apiKey"}}`})
	if !errors.Is(err, ErrNotApplicable) {
		t.Errorf("api key login: %v", err)
	}
	_, _, err = runAppServer(t, map[string]string{"initialize": `{}`, "account/read": acctChatGPT})
	if !errors.Is(err, ErrShape) {
		t.Errorf("method not found: %v", err)
	}
	if strings.Join(Drivers[SourceCodexAppServer].Argv, " ") != "codex -s read-only -a never app-server" {
		t.Error("app-server argv must stay read-only")
	}
}
