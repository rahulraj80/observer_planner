//go:build sbo_omit_adapter_codex || sbo_omit_adapter_open_interpreter || sbo_omit_adapter_cursor

package main

import "os"

// handleOmittedAdapterHook (built only with an omit tag of an adapter that
// needs it) is the receiver a build registers for an adapter
// it compiled out whose hook mechanism serves its own gates (modular
// packaging P5, lane MP-L1D, T-598). The L2 adapter gate
// (hook_components.go) answers every hook of a compiled-out adapter with
// the universal allow and records nothing, EXCEPT a hook whose plan fails
// closed (a wired gate while guard enforcement is on): there the guard keeps
// deciding, because a missing component never weakens enforcement (ledger
// MODPKG-11's direction). The full receiver needs the adapter's package;
// the guard does not. So this answers exactly the two gates through the
// shared paths every receiverless tool uses: a served tool gate through
// runToolGate, a prompt gate through the prompt-submit guard. Nothing else
// is captured: no action row, no account, no pidbridge seed.
func handleOmittedAdapterHook(tool, label, promptDialect, promptEvent, event, configPath string) {
	if b, ok := resolveHookInvocationBinding(tool, event); ok && toolGateServed(b) {
		full := label
		if event != "" {
			full = label + ":" + event
		}
		handleToolGateHook(tool, full, b, os.Stdin, configPath)
		return
	}
	handlePromptSubmitOnlyHook(tool, label, promptDialect, promptEvent, event, configPath)
}
