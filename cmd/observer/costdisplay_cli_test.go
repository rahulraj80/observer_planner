package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/intelligence/cost"
)

// withCLICostMode pins the CLI's resolved developer cost display.
func withCLICostMode(t *testing.T, mode string) {
	t.Helper()
	old := cliCostDisplayMode
	cliCostDisplayMode = func(*cobra.Command) string { return mode }
	t.Cleanup(func() { cliCostDisplayMode = old })
}

// TestCostReportCommandsResolve: every costReportCommands row names a real
// command, so the table cannot silently stop covering one.
func TestCostReportCommandsResolve(t *testing.T) {
	root := newRootCmd()
	for _, row := range costReportCommands {
		parts := strings.Fields(row.path)
		c, _, err := root.Find(parts)
		if err != nil || c == nil || c.Name() != parts[len(parts)-1] {
			t.Errorf("costReportCommands row %q does not resolve to a command (got %v, %v)", row.path, c, err)
		}
		if len(row.why) < 10 {
			t.Errorf("row %q needs a reason", row.path)
		}
	}
}

// TestReportCommandsFollowCostDisplay pins T-204's CLI seam: a covered
// command's JSON loses its dollar fields and its text hides dollar amounts
// under the token display; dollars pass through unchanged; an uncovered
// command is never touched.
func TestReportCommandsFollowCostDisplay(t *testing.T) {
	build := func() (*cobra.Command, *bytes.Buffer) {
		root := &cobra.Command{Use: "observer"}
		root.AddCommand(&cobra.Command{Use: "cost", RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && args[0] == "text" {
				fmt.Fprintln(cmd.OutOrStdout(), "total avoidable: $12.40")
				return nil
			}
			fmt.Fprint(cmd.OutOrStdout(), `{"cost_usd": 1.5, "input_tokens": 40}`+"\n")
			return nil
		}})
		root.AddCommand(&cobra.Command{Use: "status", RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "cap $5.00")
			return nil
		}})
		installCostDisplay(root)
		var out bytes.Buffer
		root.SetOut(&out)
		return root, &out
	}
	rows := []struct {
		mode string
		args []string
		want string
	}{
		{"tokens", []string{"cost"}, `{"cost_display":"tokens","input_tokens":40}` + "\n"},
		{"tokens", []string{"cost", "text"}, "total avoidable: [hidden]\n"},
		{"usd", []string{"cost"}, `{"cost_usd": 1.5, "input_tokens": 40}` + "\n"},
		{"usd", []string{"cost", "text"}, "total avoidable: $12.40\n"},
		{"tokens", []string{"status"}, "cap $5.00\n"},
	}
	for _, r := range rows {
		withCLICostMode(t, r.mode)
		root, out := build()
		root.SetArgs(r.args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%s %v: %v", r.mode, r.args, err)
		}
		if out.String() != r.want {
			t.Errorf("%s %v:\n got %q\nwant %q", r.mode, r.args, out.String(), r.want)
		}
	}
}

// TestPrintCostSummaryTokenView: `observer cost` renders the token view
// natively, without the COST_USD column and with the note.
func TestPrintCostSummaryTokenView(t *testing.T) {
	s := cost.Summary{
		GroupBy: cost.GroupByModel, Source: cost.SourceAuto, Days: 7,
		Rows: []cost.Row{{
			Key: "claude-sonnet-4", Tokens: cost.TokenBundle{Input: 1_000_000, Output: 500_000},
			CostUSD: 10.5, TurnCount: 3, Source: "proxy", Reliability: "accurate",
		}},
		TotalTokens: cost.TokenBundle{Input: 1_000_000, Output: 500_000},
		TotalCost:   10.5, TurnCount: 3, Reliability: "accurate", UnknownModelCount: 1,
	}
	var tok, usd bytes.Buffer
	printCostSummaryAs(&tok, s, "tokens")
	printCostSummaryAs(&usd, s, "usd")
	if strings.Contains(tok.String(), "$") || strings.Contains(tok.String(), "COST_USD") || !strings.Contains(tok.String(), tokenViewNote) {
		t.Errorf("token view:\n%s", tok.String())
	}
	if !strings.Contains(usd.String(), "$10.5000") || !strings.Contains(usd.String(), "COST_USD") {
		t.Errorf("dollar view:\n%s", usd.String())
	}
}

// TestExportFollowsCostDisplay: `observer export` drops the dollar columns
// natively under the token display (the --out file path never passes the
// stdout writer), keeping the token columns.
func TestExportFollowsCostDisplay(t *testing.T) {
	dbPath := seedExport(t)
	database, err := dbtemplate.Open(context.Background(), db.Options{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, mode := range []string{"usd", "tokens"} {
		var buf bytes.Buffer
		if err := runExport(context.Background(), database, &buf, exportOptions{Format: "csv", Table: "api_turns", Mode: mode}); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		header, err := csv.NewReader(&buf).Read()
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		h := strings.Join(header, ",")
		if hasCost := strings.Contains(h, "cost_usd"); hasCost != (mode == "usd") {
			t.Errorf("%s header = %s", mode, h)
		}
		if !strings.Contains(h, "input_tokens") {
			t.Errorf("%s header lost the token columns: %s", mode, h)
		}
	}
}
