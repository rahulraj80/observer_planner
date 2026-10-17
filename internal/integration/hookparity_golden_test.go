package integration_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/controlcoverage"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// Hook-parity golden gate (docs/plans/hook-parity-lane0-brief.md section 2):
// the guard conformance matrix, the control-coverage rows and the per-row
// hook facts, generated on the PRE-CHANGE tree (Lane 0 step 1). Lane 0 steps
// 2 to 8 must leave every golden byte-identical. Regenerate only with
//
//	go test ./internal/integration/ -run TestHookParityGolden -hookparity-update
//
// and a commit message that names the expected diff.

var hookParityUpdate = flag.Bool("hookparity-update", false, "rewrite the hook-parity goldens under testdata/hookparity")

const hookParityGoldenDir = "../../testdata/hookparity"

func checkHookParityGolden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	got = append(got, '\n')
	path := filepath.Join(hookParityGoldenDir, name)
	if *hookParityUpdate {
		if err := os.MkdirAll(hookParityGoldenDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("hook-parity golden %s changed (Lane 0 golden-equality gate).\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// conformanceGoldenRow is every ConformanceEntry field, spelled out so a new
// field on the entry cannot silently escape the golden.
type conformanceGoldenRow struct {
	Client         string
	Channel        string
	PreExecution   bool
	CanBlock       bool
	CanAsk         bool
	AskOnceKeyless bool
	Notes          string
}

// TestHookParityGolden_ConformanceMatrix pins every field of every row of
// guard.ConformanceMatrix(), sorted, plus the returned ORDER separately:
// `observer guard status`, the guard export and /api/guard/conformance all
// render the matrix in returned order, so a generated matrix must preserve
// it.
func TestHookParityGolden_ConformanceMatrix(t *testing.T) {
	m := guard.ConformanceMatrix()
	rows := make([]conformanceGoldenRow, 0, len(m))
	order := make([]string, 0, len(m))
	for _, e := range m {
		rows = append(rows, conformanceGoldenRow{
			Client: e.Client, Channel: e.Channel,
			PreExecution: e.Caps.PreExecution, CanBlock: e.Caps.CanBlock, CanAsk: e.Caps.CanAsk,
			AskOnceKeyless: e.AskOnceKeyless, Notes: e.Notes,
		})
		order = append(order, e.Client+" "+e.Channel)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Client != rows[j].Client {
			return rows[i].Client < rows[j].Client
		}
		return rows[i].Channel < rows[j].Channel
	})
	checkHookParityGolden(t, "conformance_matrix.json", rows)
	checkHookParityGolden(t, "conformance_order.json", order)
}

// coverageGoldenRow is controlcoverage.Row's pre-Lane-0 fields, in their
// declaration order and spelling, so the step-1 golden stays byte-identical
// while Row gains the additive Caps map (pinned by its own golden).
type coverageGoldenRow struct {
	Tool                  string
	ToolCallChannel       integration.EnforcementChannel
	PromptSubmit          controlcoverage.PromptSubmitChannel
	BudgetChannel         integration.BudgetAdmissionChannel
	RouteProof            controlcoverage.RouteProofState
	Note                  string
	PromptWarnBeforeModel bool
	RouteProbeRequired    bool
	PromptHookScope       string
	PromptProxyScope      string
}

func projectCoverageRow(r controlcoverage.Row) coverageGoldenRow {
	return coverageGoldenRow{
		Tool: r.Tool, ToolCallChannel: r.ToolCallChannel, PromptSubmit: r.PromptSubmit,
		BudgetChannel: r.BudgetChannel, RouteProof: r.RouteProof, Note: r.Note,
		PromptWarnBeforeModel: r.PromptWarnBeforeModel, RouteProbeRequired: r.RouteProbeRequired,
		PromptHookScope: r.PromptHookScope, PromptProxyScope: r.PromptProxyScope,
	}
}

// TestHookParityGolden_ControlCoverage pins controlcoverage.Rows() and
// controlcoverage.For(tool) for every registry tool.
func TestHookParityGolden_ControlCoverage(t *testing.T) {
	var rows []coverageGoldenRow
	caps := map[string]any{}
	for _, r := range controlcoverage.Rows() {
		rows = append(rows, projectCoverageRow(r))
		caps[r.Tool] = r.Caps
	}
	checkHookParityGolden(t, "controlcoverage_rows.json", rows)
	// Lane 0 step 6: the additive per-capability view has its own golden.
	checkHookParityGolden(t, "controlcoverage_caps.json", caps)
	byTool := map[string]any{}
	for _, c := range integration.Capabilities() {
		row, ok := controlcoverage.For(c.Tool)
		byTool[c.Tool] = map[string]any{
			"ok": ok, "row": projectCoverageRow(row),
			"has_pre_send_prompt_seam": row.HasPreSendPromptSeam(),
		}
	}
	checkHookParityGolden(t, "controlcoverage_for.json", byTool)
	checkHookParityGolden(t, "controlcoverage_posthoc_only.json", controlcoverage.PostHocOnlyPromptTools())
}

// TestHookParityGolden_RegistryHookFacts pins the per-row hook facts the
// registry itself computes: the HookSpec scalar fields, PromptLane and the
// EnforcementChannel bucket.
func TestHookParityGolden_RegistryHookFacts(t *testing.T) {
	facts := map[string]any{}
	for _, c := range integration.Capabilities() {
		facts[c.Tool] = map[string]any{
			"mechanism":          string(c.Hook.Mechanism),
			"cross_os_bridge":    c.Hook.CrossOSBridge,
			"auto_wired":         c.Hook.AutoWired,
			"prompt_lane_only":   c.Hook.PromptLaneOnly,
			"prompt_warn_only":   c.Hook.PromptWarnOnly,
			"prompt_scope":       c.Hook.PromptScope,
			"prompt_lane":        string(c.PromptLane),
			"enforcement":        string(c.EnforcementChannel()),
			"enforcement_no_sbx": string(c.EffectiveEnforcement(false)),
		}
	}
	checkHookParityGolden(t, "registry_hook_facts.json", facts)
}
