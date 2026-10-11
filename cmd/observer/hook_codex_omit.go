//go:build sbo_omit_adapter_codex || sbo_omit_adapter_open_interpreter

package main

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/hook"
	"github.com/marmutapp/superbased-observer/internal/models"
)

// A build without adapter.codex keeps only the guard half of the codex
// receiver (see handleOmittedAdapterHook): a leftover codex hook reaches it
// only when its plan fails closed, so enforcement keeps deciding.
func init() {
	hookReceivers["codex"] = func(_ context.Context, event, configPath string) {
		handleOmittedAdapterHook(models.ToolCodex, "codex", hook.PromptDialectTopLevelBlock, "UserPromptSubmit", event, configPath)
	}
}
