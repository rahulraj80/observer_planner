package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/telemetry"
)

// telemetryNudgeRulesFile holds this node's local additions to the segment
// classifier (telemetry plan Step 5.6): markup tag names, harness phrases and
// prompt HASHES only, never prompt text. Next to the store, 0600.
const telemetryNudgeRulesFile = "telemetry_nudge_rules.json"

func telemetryNudgeRulesPath(cfg config.Config) string {
	return filepath.Join(filepath.Dir(cfg.Observer.DBPath), telemetryNudgeRulesFile)
}

// loadTelemetryNudgeRules is the defaults plus the local file; a broken file
// falls back to the defaults (the rail must not stop over it).
func loadTelemetryNudgeRules(cfg config.Config) telemetry.NudgeRules {
	r, err := telemetry.LoadNudgeRules(telemetryNudgeRulesPath(cfg))
	if err != nil {
		return telemetry.DefaultNudgeRules()
	}
	return r
}

// newTelemetryNudgeScanCmd is the classifier's local improvement loop: a
// pareto over this machine's prompts that prints COUNTS ONLY (segment totals,
// unrecognized leading markup tags, verbatim-repeated prompts by hash prefix
// and length) and, with --apply, adds the suggestions to the local rules file.
// Nothing is sent anywhere and no prompt text is printed.
func newTelemetryNudgeScanCmd() *cobra.Command {
	var (
		days     int
		minCount int
		minLen   int
		apply    bool
	)
	cmd := &cobra.Command{
		Use:   "nudge-scan",
		Short: "Scan local prompts for system-nudge patterns (counts only; --apply saves suggestions)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, database, cleanup, err := openTelemetryCollectorDB()
			if err != nil {
				return err
			}
			defer cleanup()
			rules := loadTelemetryNudgeRules(cfg)
			since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
			rows, err := database.QueryContext(context.Background(), `
				SELECT COALESCE(tool, ''), COALESCE(NULLIF(raw_tool_input, ''), target, '')
				FROM actions WHERE action_type = 'user_prompt' AND timestamp >= ?`, since)
			if err != nil {
				return fmt.Errorf("scan prompts: %w", err)
			}
			defer rows.Close()

			type key struct{ tool, seg string }
			totals := map[key]int{}
			tags := map[string]int{}
			type rep struct {
				n, length int
			}
			repeats := map[string]*rep{}
			known := map[string]bool{}
			for _, t := range rules.MarkupTags {
				known[t] = true
			}
			for rows.Next() {
				var tool, text string
				if err := rows.Scan(&tool, &text); err != nil {
					return err
				}
				seg := rules.Classify(text)
				totals[key{tool, seg}]++
				if seg != telemetry.SegmentUserPrompt {
					continue
				}
				if tag := telemetry.LeadingTag(text); tag != "" && !known[tag] {
					tags[tag]++
				}
				if n := len(strings.TrimSpace(text)); n >= minLen {
					h := telemetry.TextHash(text)
					if repeats[h] == nil {
						repeats[h] = &rep{length: n}
					}
					repeats[h].n++
				}
			}
			if err := rows.Err(); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Prompts in the last %d days, by tool and segment (local classification):\n", days)
			var ks []key
			for k := range totals {
				ks = append(ks, k)
			}
			sort.Slice(ks, func(i, j int) bool {
				if ks[i].tool != ks[j].tool {
					return ks[i].tool < ks[j].tool
				}
				return ks[i].seg < ks[j].seg
			})
			for _, k := range ks {
				fmt.Fprintf(out, "  %-14s %-13s %d\n", k.tool, k.seg, totals[k])
			}

			var add telemetry.NudgeRules
			fmt.Fprintf(out, "\nUnrecognized leading markup tags in user prompts (>= %d):\n", minCount)
			for tag, n := range tags {
				if n >= minCount {
					fmt.Fprintf(out, "  <%s>  %d\n", tag, n)
					add.MarkupTags = append(add.MarkupTags, tag)
				}
			}
			fmt.Fprintf(out, "\nVerbatim-repeated prompts (>= %d times, >= %d chars), by hash:\n", minCount, minLen)
			for h, r := range repeats {
				if r.n >= minCount {
					fmt.Fprintf(out, "  %s…  len %d  x%d\n", h[:12], r.length, r.n)
					add.TextHashes = append(add.TextHashes, h)
				}
			}
			if len(add.MarkupTags)+len(add.TextHashes) == 0 {
				fmt.Fprintln(out, "\nNo new suggestions.")
				return nil
			}
			if !apply {
				fmt.Fprintln(out, "\nRe-run with --apply to classify these as system nudges from now on (saved locally as tag names and hashes, never text).")
				return nil
			}
			path := telemetryNudgeRulesPath(cfg)
			local, err := telemetry.LoadNudgeRules(path)
			if err != nil {
				return err
			}
			// Persist only the local additions on top of what the file had.
			merged := subtractDefaults(local.Merge(add))
			if err := telemetry.SaveNudgeRules(path, merged); err != nil {
				return fmt.Errorf("save nudge rules: %w", err)
			}
			fmt.Fprintf(out, "\nSaved %d tag(s) and %d hash(es) to %s. The daemon picks them up on its next start.\n",
				len(add.MarkupTags), len(add.TextHashes), path)
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "look back this many days")
	cmd.Flags().IntVar(&minCount, "min-count", 3, "minimum occurrences to suggest a signature")
	cmd.Flags().IntVar(&minLen, "min-len", 40, "minimum length for a verbatim-repeat signature (short commands like \"continue\" are left as user prompts)")
	cmd.Flags().BoolVar(&apply, "apply", false, "save the suggestions to the local rules file")
	return cmd
}

// subtractDefaults keeps only entries that are not built-in defaults, so the
// local file stays a list of this node's own additions.
func subtractDefaults(r telemetry.NudgeRules) telemetry.NudgeRules {
	d := telemetry.DefaultNudgeRules()
	drop := func(have, def []string) []string {
		skip := map[string]bool{}
		for _, s := range def {
			skip[s] = true
		}
		var out []string
		for _, s := range have {
			if !skip[s] {
				out = append(out, s)
			}
		}
		return out
	}
	return telemetry.NudgeRules{
		MarkupTags: drop(r.MarkupTags, d.MarkupTags),
		Prefixes:   drop(r.Prefixes, d.Prefixes),
		TextHashes: drop(r.TextHashes, d.TextHashes),
	}
}
