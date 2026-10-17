//go:build sbo_omit_adapter_cursor

package main

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/hook"
	"github.com/marmutapp/superbased-observer/internal/models"
)

// A build without adapter.cursor keeps only the guard half of the cursor
// receiver (see handleOmittedAdapterHook): a leftover cursor hook reaches it
// only when its plan fails closed, so enforcement keeps deciding.
func init() {
	hookReceivers["cursor"] = func(_ context.Context, event, configPath string) {
		handleOmittedAdapterHook(models.ToolCursor, "cursor", hook.PromptDialectCursor, "beforeSubmitPrompt", event, configPath)
	}
}
