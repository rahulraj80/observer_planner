//go:build !sbo_omit_demo

package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/demo"
)

func newDemoSeedCmd() *cobra.Command {
	var configPath string
	var days int
	var future int
	var users int

	cmd := &cobra.Command{
		Use:   "demoseed",
		Short: "Seed days of realistic multi-user sample activity (demo)",
		Long: "Writes a deterministic block of synthetic multi-user activity " +
			"(projects, sessions, api_turns, token usage, actions, and routing " +
			"advice) into the local database so the dashboard and Daily Briefing " +
			"show realistic content. 75% of each user's workday is agentic coding " +
			"across the top harnesses; the rest is assistant/chat load, with " +
			"cache, long-context, and model-routing patterns included. Completed " +
			"days end at yesterday so the briefing's default window is full; " +
			"--future dates from today forward so the demo keeps working without " +
			"re-seeding. Re-running purges only previously seeded rows.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			_, database, cleanup, err := loadConfigAndDB(ctx, configPath)
			if err != nil {
				return err
			}
			defer cleanup()

			start := time.Now()
			res, err := demo.Seed(ctx, database, demo.SeedOptions{Days: days, FutureDays: future, Users: users})
			if err != nil {
				return fmt.Errorf("demoseed: %w", err)
			}
			blob, _ := json.MarshalIndent(res, "", "  ")
			fmt.Printf("seeded %d past + %d future days for %d users in %s\n", res.Days, res.FutureDays, res.Users, time.Since(start).Round(time.Millisecond))
			fmt.Println(string(blob))
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config.toml (defaults to ~/.observer/config.toml)")
	cmd.Flags().IntVarP(&days, "days", "d", 5, "Number of completed local days to generate (ends at yesterday)")
	cmd.Flags().IntVarP(&future, "future", "f", 20, "Future-dated demo days to generate from today forward")
	cmd.Flags().IntVarP(&users, "users", "u", 30, "Number of synthetic users (max 30)")
	return cmd
}
