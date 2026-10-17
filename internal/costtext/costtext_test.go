package costtext

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestTokens(t *testing.T) {
	rows := []struct {
		mode string
		want bool
	}{
		{ModeUSD, false},
		{"", false},
		{ModeTokens, true},
		{"percent-of-cap", true}, // unknown: token usage, never dollars (D-179)
	}
	for _, r := range rows {
		if got := Tokens(r.mode); got != r.want {
			t.Errorf("Tokens(%q) = %v, want %v", r.mode, got, r.want)
		}
	}
}

// TestMaskDollars mirrors shared/lib/format.ts maskDollarAmounts' table row
// for row (with Hidden in place of the web's placeholder), plus the aligned
// table cases only a CLI meets.
func TestMaskDollars(t *testing.T) {
	rows := []struct{ in, want string }{
		{"$4.20 fast-tier premium across 3 turns", "[hidden] fast-tier premium across 3 turns"},
		{"$1,234 of spend isn't routed through the proxy", "[hidden] of spend isn't routed through the proxy"},
		{"save ~$0.0040 per turn (net of -$3.10 forfeit)", "save ~[hidden] per turn (net of -[hidden] forfeit)"},
		{"$12k a month", "[hidden] a month"},
		{"no money here, 40% and 3 turns", "no money here, 40% and 3 turns"},
		{"regex anchors like ^/health$ stay", "regex anchors like ^/health$ stay"},
		{"export KEY=$HOME/bin", "export KEY=$HOME/bin"},
		// A tabwriter column: the padding absorbs the longer placeholder so the
		// next column still starts at the same offset.
		// The run keeps one space, so a cell three wider than its padding
		// allows still reads as two cells.
		{"opus  $0.12   10", "opus  [hidden] 10"},
		{"opus  $1,234.5678   10", "opus  [hidden]      10"},
		{"TOTAL $12.3456    ok", "TOTAL [hidden]    ok"},
	}
	for _, r := range rows {
		if got := MaskDollars(ModeTokens, r.in); got != r.want {
			t.Errorf("MaskDollars(%q)\n got %q\nwant %q", r.in, got, r.want)
		}
		if got := MaskDollars(ModeUSD, r.in); got != r.in {
			t.Errorf("dollar mode changed %q to %q", r.in, got)
		}
	}
}

func TestMaskKeepsColumnOffsets(t *testing.T) {
	in := "KEY   COST_USD   TURNS\nopus  $0.1234    12\nhaiku $12.5000   3\n"
	out := MaskDollars(ModeTokens, in)
	lines := strings.Split(out, "\n")
	want := strings.Index(lines[0], "TURNS")
	for _, l := range lines[1:3] {
		if got := strings.IndexAny(l, "0123456789"); got != want {
			t.Errorf("column moved: %q (TURNS at %d, digits at %d)", l, want, got)
		}
	}
}

func TestIsDollarKey(t *testing.T) {
	rows := []struct {
		key  string
		want bool
	}{
		{"cost_usd", true},
		{"ai_cost_usd", true},
		{"total_fast_cost_usd", true},
		{"usd", true},
		{"cost", true},
		{"usd_per_turn", true},
		{"spend_today_usd", true},
		{"baseline_cost_per_turn_usd", true},
		{"TotalUSD", true},
		{"costUsd", true},
		{"cost_reliability", false},
		{"cost_source", false},
		{"cost_tokens", false},
		{"input_tokens", false},
		{"usage", false},
		{"status", false},
		{"UserID", false},
	}
	for _, r := range rows {
		if got := IsDollarKey(r.key); got != r.want {
			t.Errorf("IsDollarKey(%q) = %v, want %v", r.key, got, r.want)
		}
	}
}

// TestTokenFigure mirrors the web's compact token figure (shared/lib/format.ts
// fmtTok): one decimal, trailing ".0" dropped.
func TestTokenFigure(t *testing.T) {
	rows := map[int64]string{
		0: "0 tok", 950: "950 tok", 1000: "1K tok", 212_400: "212.4K tok",
		1_500_000: "1.5M tok", 2_000_000_000: "2B tok", -1500: "-1.5K tok",
	}
	for in, want := range rows {
		if got := TokenFigure(in); got != want {
			t.Errorf("TokenFigure(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestKeepColumns(t *testing.T) {
	h := []string{"id", "input_tokens", "cost_usd", "stop_reason"}
	if got := KeepColumns(ModeTokens, h); strings.Join(got, ",") != "id,input_tokens,stop_reason" {
		t.Errorf("tokens = %v", got)
	}
	if got := KeepColumns(ModeUSD, h); strings.Join(got, ",") != strings.Join(h, ",") {
		t.Errorf("usd = %v", got)
	}
}

func TestStripDollarFields(t *testing.T) {
	in := map[string]any{
		"model":    "opus",
		"cost_usd": 1.5,
		"rows": []any{
			map[string]any{"key": "a", "ai_cost_usd": 1.0, "input_tokens": json.Number("12")},
		},
		"note": "saves about $3.10 a day",
	}
	got := StripDollarFields(in).(map[string]any)
	if _, ok := got["cost_usd"]; ok {
		t.Error("cost_usd kept")
	}
	row := got["rows"].([]any)[0].(map[string]any)
	if _, ok := row["ai_cost_usd"]; ok || row["input_tokens"] != json.Number("12") {
		t.Errorf("row = %v", row)
	}
	if got["note"] != "saves about [hidden] a day" {
		t.Errorf("note = %v", got["note"])
	}
	if in["cost_usd"] != 1.5 {
		t.Error("the input was mutated")
	}
}

func TestWriterDollarModeIsPassThrough(t *testing.T) {
	var b bytes.Buffer
	w := NewWriter(&b, ModeUSD)
	_, _ = w.Write([]byte(`{"cost_usd": 1}` + "\n"))
	_ = w.Close()
	if b.String() != `{"cost_usd": 1}`+"\n" {
		t.Errorf("usd mode changed output: %q", b.String())
	}
}

func TestWriterTokenMode(t *testing.T) {
	rows := []struct {
		name   string
		writes []string
		want   string
	}{
		{
			name:   "indented JSON document",
			writes: []string{"{\n  \"cost_usd\": 1.25,\n", "  \"tokens\": 40\n}\n"},
			want:   "{\n  \"cost_display\": \"tokens\",\n  \"tokens\": 40\n}\n",
		},
		{
			name:   "JSON lines",
			writes: []string{`{"id":1,"cost_usd":0.5}` + "\n" + `{"id":2,"cost_usd":0.7}` + "\n"},
			want:   `{"cost_display":"tokens","id":1}` + "\n" + `{"cost_display":"tokens","id":2}` + "\n",
		},
		{
			name:   "array document",
			writes: []string{`[{"usd":1,"n":2}]`},
			want:   `[{"n":2}]`,
		},
		{
			name:   "text streamed by line",
			writes: []string{"window: 7 day(s)  total avoidable: $12.40\n", "done, no money"},
			want:   "window: 7 day(s)  total avoidable: [hidden]\ndone, no money",
		},
		{
			name:   "bracketed text that is not JSON",
			writes: []string{"[warn] spend $3.00 today\n"},
			want:   "[warn] spend [hidden] today\n",
		},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			var b bytes.Buffer
			w := NewWriter(&b, ModeTokens)
			for _, s := range r.writes {
				if _, err := w.Write([]byte(s)); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if b.String() != r.want {
				t.Errorf("\n got %q\nwant %q", b.String(), r.want)
			}
		})
	}
}

// TestWriterStreamsTextBeforeClose: a long-running command's lines reach the
// terminal as they are written, not only at Close.
func TestWriterStreamsTextBeforeClose(t *testing.T) {
	var b bytes.Buffer
	w := NewWriter(&b, ModeTokens)
	_, _ = w.Write([]byte("cell 1 $0.0100 ok\ncell 2"))
	if b.String() != "cell 1 [hidden] ok\n" {
		t.Errorf("before Close: %q", b.String())
	}
	_ = w.Close()
	if b.String() != "cell 1 [hidden] ok\ncell 2" {
		t.Errorf("after Close: %q", b.String())
	}
}
