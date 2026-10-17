package agentconfig

import (
	"errors"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

func TestCommonSpecsClosed(t *testing.T) {
	tiers := map[CommonTier]int{}
	seen := map[CommonKey]bool{}
	for _, s := range CommonSpecs() {
		if seen[s.Key] {
			t.Errorf("%s: duplicate", s.Key)
		}
		seen[s.Key] = true
		tiers[s.Tier]++
		if s.Desc == "" || s.Type == "" {
			t.Errorf("%s: missing desc / type", s.Key)
		}
		if s.Order == OrderEnum && len(s.Enum) == 0 {
			t.Errorf("%s: an ordered enum needs its values, strictest first", s.Key)
		}
		for _, d := range s.Danger {
			if !containsString(s.Enum, d) {
				t.Errorf("%s: danger value %q not in the enum", s.Key, d)
			}
		}
	}
	// Plan section 4: 13 Core keys (11 table rows), 6 Secondary, 2 read-only.
	if tiers[TierCore] != 13 || tiers[TierSecondary] != 6 || tiers[TierObserverOwned] != 2 || len(seen) != 21 {
		t.Errorf("tiers = %v (%d keys), want core 13 / secondary 6 / observer_owned 2", tiers, len(seen))
	}
	for _, k := range []CommonKey{KeyApprovalMode, KeySandboxMode, KeyDeniedTools, KeyAllowedTools, KeyTelemetryOff} {
		if s, _ := CommonSpecFor(k); s.Order == OrderNone {
			t.Errorf("%s is an ordered-danger key (section 3.2) but has no order", k)
		}
	}
}

// TestMapRowsGrounded: every mapping points at existing catalogue rows of
// its own tool and names its source (plan: "every common-vocabulary
// mapping points at an existing row").
func TestMapRowsGrounded(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range MapRows() {
		k := string(m.Key) + "\x00" + m.Tool
		if seen[k] {
			t.Errorf("%s/%s: two map rows", m.Tool, m.Key)
		}
		seen[k] = true
		if _, ok := CommonSpecFor(m.Key); !ok {
			t.Errorf("%s: unknown common key %q", m.Tool, m.Key)
		}
		if _, ok := integration.For(m.Tool); !ok {
			t.Errorf("%s: not a registry tool", m.Tool)
		}
		if m.Source == "" {
			t.Errorf("%s/%s: no Source", m.Tool, m.Key)
		}
		if _, ok := transformTable[m.Transform]; !ok {
			t.Errorf("%s/%s: unknown transform %q", m.Tool, m.Key, m.Transform)
		}
		if m.Key == KeyHooks {
			t.Errorf("%s: hooks is never a common-plane mapping", m.Tool)
		}
		for _, id := range m.Settings {
			r, ok := Setting(id)
			switch {
			case !ok:
				t.Errorf("%s/%s: setting %q not in the catalogue", m.Tool, m.Key, id)
			case r.Tool != m.Tool:
				t.Errorf("%s/%s: setting %q belongs to %s", m.Tool, m.Key, id, r.Tool)
			case r.Secret:
				t.Errorf("%s/%s: maps onto secret row %q", m.Tool, m.Key, id)
			}
		}
		if spec, _ := CommonSpecFor(m.Key); spec.Tier != TierObserverOwned {
			for _, id := range m.Settings {
				if r, ok := Setting(id); ok && r.Owner.IsObserver() {
					t.Errorf("%s/%s: a writable common key maps onto Observer-owned %q", m.Tool, m.Key, id)
				}
			}
		}
	}
	if len(MapRows()) == 0 {
		t.Fatal("no map rows")
	}
}

// samples returns canonical sample values for one key on one tool.
func samples(m MapRow) []any {
	spec, _ := CommonSpecFor(m.Key)
	switch spec.Type {
	case TypeEnum:
		out := make([]any, len(spec.Enum))
		for i, e := range spec.Enum {
			out[i] = e
		}
		return out
	case TypeBool:
		return []any{true, false}
	case TypeInt:
		return []any{int64(70), int64(200000)}
	case TypeFloat:
		return []any{0.5}
	case TypeStringList:
		return []any{[]any{"AGENTS.md", "docs/extra.md"}}
	case TypeStringMap:
		return []any{map[string]any{"FOO": "1", "BAR": "two"}}
	case TypeRuleList:
		var out []any
		for _, r := range []string{"shell", "shell:git status", "read", "read:./src/**", "edit", "write", "fetch", "fetch:example.com", "search", "mcp:srv", "mcp:srv/tool"} {
			out = append(out, []any{r})
		}
		return out
	case TypeMCPMap:
		return []any{
			map[string]any{"local": map[string]any{"command": "npx", "args": []any{"-y", "pkg"}, "env": map[string]any{"K": "V"}}},
			map[string]any{"remote": map[string]any{"type": "http", "url": "https://example.com/mcp"}},
			map[string]any{"bare": map[string]any{"command": "srv"}},
		}
	}
	if m.Key == KeyModel || m.Key == KeySmallModel {
		cat := modelCatalog(m.Tool)
		switch {
		case len(cat.Providers) > 0:
			return []any{cat.Providers[0] + "/sample-model"}
		case m.Params.Model.Qualified:
			return []any{"openrouter/sample-model"}
		}
		// No grounded catalogue: the canonical form is the bare id (a
		// provider prefix would not survive the bare spelling).
		return []any{"sample-model"}
	}
	return []any{"sample"}
}

// TestMapRowRoundTrip: for every map row and every canonical sample, a
// forward then reverse is the identity, or the row is marked Lossy, or the
// tool refuses the value. A row that refuses every sample is a dead row.
func TestMapRowRoundTrip(t *testing.T) {
	for _, m := range MapRows() {
		accepted := 0
		for _, v := range samples(m) {
			natives, err := m.Forward(v)
			if errors.Is(err, ErrRefused) {
				continue
			}
			if err != nil {
				t.Errorf("%s/%s forward %v: %v", m.Tool, m.Key, v, err)
				continue
			}
			accepted++
			got, present, err := m.Reverse(natives)
			if err != nil || !present {
				t.Errorf("%s/%s reverse %v (from %v): %v present=%v", m.Tool, m.Key, natives, v, err, present)
				continue
			}
			want, _ := cfgfmt.Normalize(v)
			if !cfgfmt.Equal(got, want) && !m.Lossy {
				t.Errorf("%s/%s: %v -> %v -> %v is not the identity; mark the row Lossy", m.Tool, m.Key, v, natives, got)
			}
		}
		if accepted == 0 {
			t.Errorf("%s/%s: every canonical sample is refused", m.Tool, m.Key)
		}
		if _, present, err := m.Reverse(make([]any, len(m.Settings))); present || err != nil {
			t.Errorf("%s/%s: all-absent natives must be absent, got present=%v err=%v", m.Tool, m.Key, present, err)
		}
	}
}

// TestTransforms is one table row per transform (plus the encodings and
// the refusal paths), independent of the shipped map rows.
func TestTransforms(t *testing.T) {
	approval := []EnumPair{{"auto_edits", []any{"on-request", "workspace-write"}}, {"full_auto", []any{"never", "danger-full-access"}}}
	cases := []struct {
		name    string
		m       MapRow
		in      any
		want    []any
		wantErr error
		back    any // reverse of want; nil = in
	}{
		{name: "identity", m: MapRow{Key: KeyContextLimit, Settings: []string{"x"}, Transform: TransformIdentity}, in: 128000, want: []any{int64(128000)}},
		{
			name: "identity list from scalar", m: MapRow{Key: KeyInstructionsFiles, Settings: []string{"x"}, Transform: TransformIdentity},
			in: []any{"A.md"}, want: []any{[]any{"A.md"}},
		},
		{
			name: "enum_map fan-out", m: MapRow{Key: KeyApprovalMode, Settings: []string{"a", "b"}, Transform: TransformEnumMap, Params: TransformParams{Enum: approval}},
			in: "auto_edits", want: []any{"on-request", "workspace-write"},
		},
		{
			name: "enum_map refused", m: MapRow{Key: KeyApprovalMode, Settings: []string{"a", "b"}, Transform: TransformEnumMap, Params: TransformParams{Enum: approval}},
			in: "plan", wantErr: ErrRefused,
		},
		{name: "bool_invert", m: MapRow{Key: KeyTelemetryOff, Settings: []string{"x"}, Transform: TransformBoolInvert}, in: true, want: []any{false}},
		{
			name: "bool_invert env", m: MapRow{Key: KeyAutoUpdate, Settings: []string{"x"}, Transform: TransformBoolInvert, Params: TransformParams{Encoding: EncEnvBool}},
			in: false, want: []any{"1"},
		},
		{
			name: "env_int", m: MapRow{Key: KeyMaxOutputTokens, Settings: []string{"x"}, Transform: TransformIdentity, Params: TransformParams{Encoding: EncEnvInt}},
			in: 32000, want: []any{"32000"},
		},
		{
			name: "scale percent to fraction", m: MapRow{Key: KeyAutoCompact, Settings: []string{"x"}, Transform: TransformScale, Params: TransformParams{Factor: 0.01}},
			in: 70, want: []any{0.7},
		},
		{
			name: "scale rounds", m: MapRow{Key: KeyAutoCompact, Settings: []string{"x"}, Transform: TransformScale, Params: TransformParams{Factor: 1000, Round: true}},
			in: 2, want: []any{int64(2000)},
		},
		{name: "effort_to_budget", m: MapRow{
			Key: KeyReasoningEffort, Settings: []string{"x"}, Transform: TransformEffortToBudget,
			Params: TransformParams{Budgets: []EffortBudget{{"low", 1024}, {"high", 16384}}},
		}, in: "high", want: []any{int64(16384)}},
		{name: "effort_to_budget refused", m: MapRow{
			Key: KeyReasoningEffort, Settings: []string{"x"}, Transform: TransformEffortToBudget,
			Params: TransformParams{Budgets: []EffortBudget{{"low", 1024}}},
		}, in: "max", wantErr: ErrRefused},
		{
			name: "list_join", m: MapRow{Key: KeyAdditionalDirs, Settings: []string{"x"}, Transform: TransformListJoin, Params: TransformParams{Sep: ","}},
			in: []any{"/a", "/b"}, want: []any{"/a,/b"},
		},
		{
			name: "rule_translate claude", m: MapRow{Key: KeyAllowedTools, Settings: []string{"x"}, Transform: TransformRuleTranslate, Params: TransformParams{Grammar: claudeRuleGrammar}},
			in:   []any{"shell:npm test", "fetch:example.com", "mcp:github/create_issue", "search"},
			want: []any{[]any{"Bash(npm test)", "WebFetch(domain:example.com)", "mcp__github__create_issue", "WebSearch"}},
		},
		{
			name: "rule_translate refuses a pattern on a bare tool", m: MapRow{Key: KeyAllowedTools, Settings: []string{"x"}, Transform: TransformRuleTranslate, Params: TransformParams{Grammar: crushAllowGrammar}},
			in: []any{"shell:git status"}, wantErr: ErrRefused,
		},
		{
			name: "rule_translate refuses an unknown kind", m: MapRow{Key: KeyDeniedTools, Settings: []string{"x"}, Transform: TransformRuleTranslate, Params: TransformParams{Grammar: geminiRuleGrammar}},
			in: []any{"mcp:srv"}, wantErr: ErrRefused,
		},
		{
			name: "model_alias bare", m: MapRow{Key: KeyModel, Tool: "claude-code", Settings: []string{"x"}, Transform: TransformModelAlias},
			in: "anthropic/claude-sonnet-4-6", want: []any{"claude-sonnet-4-6"},
		},
		{
			name: "model_alias refuses another vendor", m: MapRow{Key: KeyModel, Tool: "claude-code", Settings: []string{"x"}, Transform: TransformModelAlias},
			in: "openai/gpt-5", wantErr: ErrRefused,
		},
		{
			name: "model_alias qualified", m: MapRow{Key: KeyModel, Tool: "opencode", Settings: []string{"x"}, Transform: TransformModelAlias, Params: TransformParams{Model: ModelSpelling{Qualified: true}}},
			in: "openai/gpt-5", want: []any{"openai/gpt-5"},
		},
		{
			name: "mcp_shape opencode", m: MapRow{Key: KeyMCPServers, Settings: []string{"x"}, Transform: TransformMCPShape, Params: TransformParams{MCP: mcpOpenCode}},
			in:   map[string]any{"s": map[string]any{"command": "npx", "args": []any{"-y", "p"}, "env": map[string]any{"K": "V"}}},
			want: []any{map[string]any{"s": map[string]any{"type": "local", "command": []any{"npx", "-y", "p"}, "environment": map[string]any{"K": "V"}}}},
		},
		{
			name: "mcp_shape gemini remote", m: MapRow{Key: KeyMCPServers, Settings: []string{"x"}, Transform: TransformMCPShape, Params: TransformParams{MCP: mcpGemini}},
			in:   map[string]any{"r": map[string]any{"type": "http", "url": "https://x"}},
			want: []any{map[string]any{"r": map[string]any{"httpUrl": "https://x"}}},
		},
		{
			name: "mcp_shape refuses a transport", m: MapRow{Key: KeyMCPServers, Settings: []string{"x"}, Transform: TransformMCPShape, Params: TransformParams{MCP: mcpOpenCode}},
			in: map[string]any{"r": map[string]any{"type": "sse", "url": "https://x"}}, wantErr: ErrRefused,
		},
		{name: "bad map row: fan-out on identity", m: MapRow{Key: KeyModel, Settings: []string{"a", "b"}, Transform: TransformIdentity}, in: "x", wantErr: ErrBadMapRow},
		{name: "bad value", m: MapRow{Key: KeyTelemetryOff, Settings: []string{"x"}, Transform: TransformBoolInvert}, in: "yes", wantErr: ErrBadValue},
	}
	covered := map[TransformID]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.m.Forward(tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !cfgfmt.Equal(any(got), any(tc.want)) {
				t.Fatalf("forward = %#v, want %#v", got, tc.want)
			}
			back, present, err := tc.m.Reverse(got)
			if err != nil || !present {
				t.Fatalf("reverse: %v present=%v", err, present)
			}
			want := tc.back
			if want == nil {
				want, _ = cfgfmt.Normalize(tc.in)
			}
			if !cfgfmt.Equal(back, want) {
				t.Fatalf("reverse = %#v, want %#v", back, want)
			}
		})
		covered[tc.m.Transform] = true
	}
	for _, id := range Transforms() {
		if !covered[id] {
			t.Errorf("transform %s has no test row", id)
		}
	}
}

func TestReverseEdges(t *testing.T) {
	g := MapRow{Key: KeyAllowedTools, Settings: []string{"x"}, Transform: TransformRuleTranslate, Params: TransformParams{Grammar: claudeRuleGrammar}}
	v, _, err := g.Reverse([]any{[]any{"Bash", "NotebookEdit(x)"}})
	if err != nil || !cfgfmt.Equal(v, []any{"shell", "native:NotebookEdit(x)"}) {
		t.Errorf("unknown native rule: %v %v", v, err)
	}
	if _, err := g.Forward([]any{"native:NotebookEdit(x)"}); !errors.Is(err, ErrRefused) {
		t.Errorf("a native rule must not cross tools: %v", err)
	}
	e := MapRow{
		Key: KeyReasoningEffort, Settings: []string{"x"}, Transform: TransformEffortToBudget,
		Params: TransformParams{Budgets: []EffortBudget{{"low", 1000}, {"high", 10000}}},
	}
	if v, _, _ := e.Reverse([]any{int64(4000)}); v != "low" {
		t.Errorf("nearest budget = %v, want low (lossy)", v)
	}
	m := MapRow{Key: KeyModel, Tool: "claude-code", Settings: []string{"x"}, Transform: TransformModelAlias}
	if v, _, _ := m.Reverse([]any{"claude-opus-4-6"}); v != "anthropic/claude-opus-4-6" {
		t.Errorf("vendor tool reverse = %v", v)
	}
	b := MapRow{Key: KeyTelemetryOff, Settings: []string{"x"}, Transform: TransformIdentity, Params: TransformParams{Encoding: EncEnvBool}}
	if _, _, err := b.Reverse([]any{"maybe"}); !errors.Is(err, ErrNoCanonical) {
		t.Errorf("env bool garbage: %v", err)
	}
	en := MapRow{Key: KeyTheme, Settings: []string{"x"}, Transform: TransformEnumMap, Params: TransformParams{Enum: []EnumPair{{"dark", []any{"dark"}}}}}
	if _, _, err := en.Reverse([]any{"dark-ansi"}); !errors.Is(err, ErrNoCanonical) {
		t.Errorf("a named theme has no canonical form: %v", err)
	}
}
