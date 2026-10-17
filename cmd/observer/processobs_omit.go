//go:build sbo_omit_processobs

package main

import (
	"context"
	"io"

	"github.com/spf13/cobra"
)

// processobs_omit.go is the off side of the process-observation wiring
// (component processobs, modular packaging plan 5.1, lane MP-L1B). A
// `-tags sbo_omit_processobs` build drops processobs.go, process_cmd.go and
// the process_bridge*.go files, so internal/processobs/*,
// internal/processbridge/setup and github.com/cilium/ebpf leave the binary.
// These stubs keep the call sites in start.go, main.go and init*.go
// identical in every build.

// runProcessObserver starts nothing: there is no capture backend.
func runProcessObserver(_ context.Context, _ string) error { return nil }

// initProcessBridgeTaskStep reports nothing: the Windows ETW bridge task is
// part of process observation.
func initProcessBridgeTaskStep(_ context.Context, _ io.Writer, _ string) {}

// newProcessCmd and newProcessBridgeCmd keep the verbs registered so the
// component verb gate refuses them with exit code 3 and the "not in this
// build" line (components_gate.go), like every other inactive component.
func newProcessCmd() *cobra.Command {
	return compiledOutVerb("process", "Inspect captured OS process activity (not in this build)", "processobs")
}

func newProcessBridgeCmd() *cobra.Command {
	return compiledOutVerb("process-bridge", "Windows process bridge (not in this build)", "processobs")
}
