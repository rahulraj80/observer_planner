package main

import (
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// guardHookFailureSeam answers the Security page's "what does a failing gate
// hook do here" (T-346) with the hook's OWN per-family answer,
// hookPostureFailsClosed: the daemon's signed, fresh posture, else the
// D-194 derivation from the config and the cached org bundle. One owner, so
// the tile can never say fail-open while the hooks fail closed (the text used
// to come from [guard].strict, which does not decide it).
func guardHookFailureSeam(configPath string) func() dashapi.GuardHookFailure {
	return func() dashapi.GuardHookFailure {
		resolved, err := config.ResolveGlobalPath(configPath)
		if err != nil {
			return dashapi.GuardHookFailure{}
		}
		return dashapi.GuardHookFailure{
			ToolGateClosed:   hookPostureFailsClosed(resolved, configPath, integration.HookCapToolGate).FailsClosed,
			PromptGateClosed: hookPostureFailsClosed(resolved, configPath, integration.HookCapPromptGate).FailsClosed,
		}
	}
}
