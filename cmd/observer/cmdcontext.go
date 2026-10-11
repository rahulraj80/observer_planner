package main

import (
	"context"

	"github.com/spf13/cobra"
)

// cmdContext returns the command's context, or context.Background when it has none.
func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
