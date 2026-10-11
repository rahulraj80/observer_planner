package main

import "github.com/spf13/cobra"

// launcherCommands is the launcher registry (modular packaging P5, lane
// MP-ADP): every per-tool launcher verb (`observer claude`, `observer
// gemini`, ...) is one row, built and added to the root by
// newRootCmdWith. The verb gate (components_gate.go) owns each verb through
// the integration registry's Handoff.Launch.Subcommand, so a launcher whose
// adapter is inactive or compiled out refuses with exit code 3 and the
// reason. TestLauncherRegistryCoversEveryLaunchSubcommand keeps this table
// and the registry in step.
var launcherCommands = []func() *cobra.Command{
	newClaudeCmd,
	newCodexCmd,
	newOpencodeCmd,
	newClineCLICmd,
	newCopilotCLICmd,
	newOpenclawCmd,
	newGeminiCmd,
	newPiCmd,
	newHermesCmd,
	newKiloCmd,
	newCursorCmd,
	newAntigravityCmd,
	newQwenCmd,
	newKiroCmd,
	newKimiCmd,
	newGrokCmd,
	newQoderCmd,
	newGooseCmd,
	newCrushCmd,
	newDevinCmd,
	newDroidCmd,
	newOpenInterpreterCmd,
	newCommandCodeCmd,
	newAiderCmd,
	newMuseCmd,
	newPrimeAgentCmd,
	newZcodeCmd,
	newVibeCmd,
	newFreebuffCmd,
	newJunieCmd,
	newPoolCmd,
}
