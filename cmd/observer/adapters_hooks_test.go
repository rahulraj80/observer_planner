package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/controlcoverage"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestHookParityGolden_AdaptersHooksMatrix pins the rendered `observer
// adapters --hooks` matrix (Lane G). It is generated from the registry's
// hook bindings, so a lane that adds or wires a binding regenerates it with
// -hookparity-update and names the expected diff.
func TestHookParityGolden_AdaptersHooksMatrix(t *testing.T) {
	var buf bytes.Buffer
	if err := renderHookMatrixCmd(&buf, false); err != nil {
		t.Fatal(err)
	}
	checkHookParityGolden(t, "adapters_hooks_matrix.txt", buf.String())
}

// TestAdaptersHooksJSON: --hooks --json is the controlcoverage view, one row
// per registry adapter, every row carrying one cell per column.
func TestAdaptersHooksJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := renderHookMatrixCmd(&buf, true); err != nil {
		t.Fatal(err)
	}
	var v controlcoverage.MatrixView
	if err := json.Unmarshal(buf.Bytes(), &v); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(v.Rows) != len(integration.Capabilities()) || len(v.Groups) == 0 || len(v.Summaries) != len(integration.HookCaps()) {
		t.Fatalf("rows %d groups %d summaries %d", len(v.Rows), len(v.Groups), len(v.Summaries))
	}
	for _, r := range v.Rows {
		if len(r.Cells) != len(v.Groups) {
			t.Errorf("%s: %d cells for %d groups", r.Tool, len(r.Cells), len(v.Groups))
		}
	}
	// The cobra flag is wired.
	cmd := newAdaptersCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--hooks"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "TOOL GATE") || !strings.Contains(out.String(), "per capability") {
		t.Errorf("--hooks output lacks the matrix header: %.200s", out.String())
	}
}
