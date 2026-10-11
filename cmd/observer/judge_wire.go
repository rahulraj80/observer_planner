//go:build !sbo_omit_judge_client

package main

import (
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/judgeclient"
)

// judge_wire.go binds the judge client component (internal/judgeclient) to
// the short package-main names the obs and alignment wiring use. It carries no
// build tag: the alignment judge tier and the obs judges share one client, and
// neither the client nor these bindings import internal/obs (modular packaging
// phase PB, D-408).

// chatCompletionsJudge is the judge client.
type chatCompletionsJudge = judgeclient.Client

// judgeMaxPromptBytes caps the prompt sent to a remote judge.
const judgeMaxPromptBytes = judgeclient.RemoteMaxPromptBytes

// newChatCompletionsJudge is judgeclient.New.
func newChatCompletionsJudge(baseURL, hv string) chatCompletionsJudge {
	return judgeclient.New(baseURL, hv)
}

// secureJudgeEgress is judgeclient.SecureEgress.
func secureJudgeEgress(c chatCompletionsJudge, hosting string) chatCompletionsJudge {
	return judgeclient.SecureEgress(c, hosting)
}

// resolveJudgeConfig is judgeclient.ResolveConfig.
func resolveJudgeConfig(shared, override config.ObservabilityJudgeConfig) config.ObservabilityJudgeConfig {
	return judgeclient.ResolveConfig(shared, override)
}

// judgeHostingLabel is judgeclient.HostingLabel.
func judgeHostingLabel(baseURL, model string) string {
	return judgeclient.HostingLabel(baseURL, model)
}
