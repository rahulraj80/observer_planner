package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/marmutapp/superbased-observer/internal/controlcoverage"
)

// `observer adapters --hooks` (hook-parity Lane G): the audit's section 4
// matrix, rendered from internal/controlcoverage's HookMatrix, which reads
// the registry's hook bindings. Nothing here names a tool: a lane that adds
// or wires a binding changes this output with no edit to this file. --json
// emits controlcoverage.MatrixView, the same shape the node dashboard and
// the org Control coverage tab serve.

// renderHookMatrixCmd writes the matrix as a table, or as JSON.
func renderHookMatrixCmd(w io.Writer, jsonOut bool) error {
	if jsonOut {
		body, err := json.MarshalIndent(controlcoverage.HookMatrixView(), "", "  ")
		if err != nil {
			return fmt.Errorf("adapters --hooks: %w", err)
		}
		_, err = fmt.Fprintln(w, string(body))
		return err
	}
	renderHookMatrix(w, controlcoverage.HookMatrix(), controlcoverage.CapSummaries())
	return nil
}

// hookMatrixCell renders one cell as "<events> / <mark>" (the audit's
// "vendor / Observer" spelling); a column with no event is "-".
func hookMatrixCell(c controlcoverage.MatrixCell) string {
	if len(c.Events) == 0 {
		return "-"
	}
	return strings.Join(c.Events, ", ") + " / " + c.Mark()
}

// renderHookMatrix writes the per-adapter matrix, the per-capability
// summary (audit section 4.3) and the legend.
func renderHookMatrix(w io.Writer, rows []controlcoverage.MatrixRow, sums []controlcoverage.CapSummary) {
	groups := controlcoverage.CapGroups()
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	head := []string{"ADAPTER", "MECHANISM"}
	for _, g := range groups {
		head = append(head, strings.ToUpper(g.Label))
	}
	fmt.Fprintln(tw, strings.Join(head, "\t"))
	for _, r := range rows {
		mech := r.Mechanism
		switch {
		case mech == "":
			mech = "-"
		case !r.AutoWired:
			mech += "+manual"
		}
		cells := []string{r.Tool, mech}
		for _, c := range r.Cells {
			cells = append(cells, hookMatrixCell(c))
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	_ = tw.Flush()

	fmt.Fprintln(w)
	fmt.Fprintln(w, "per capability (rows whose vendor surface provides it / Observer wires it):")
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CAPABILITY\tPROVIDES\tWIRED\tREGISTERED ONLY\tPROBE\tGAP")
	for _, s := range sums {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\n", s.Cap, s.Provides, s.Wired, s.Registered, s.ProbeRequired, s.Gap())
	}
	_ = tw.Flush()

	fmt.Fprintln(w)
	fmt.Fprintln(w, "legend: a cell is \"<vendor event> <decisions> / <Observer>\". Decisions: D deny, A ask,")
	fmt.Fprintln(w, "M modify input, C inject context, U user-visible message, O observe only. Observer: W wired")
	fmt.Fprintln(w, "(registered and a feature acts on it), R registered and captured only, probe (documented,")
	fmt.Fprintln(w, "wire shape unverified), - not registered (vendor surface only). +manual = the mechanism is")
	fmt.Fprintln(w, "never auto-registered. Generated from the registry's hook bindings (internal/integration);")
	fmt.Fprintln(w, "`observer doctor --hooks <tool>` checks each binding on this machine (registered / trusted /")
	fmt.Fprintln(w, "probe), and `observer doctor --probe-hook <tool>` fires a synthetic call through each wired gate.")
}
