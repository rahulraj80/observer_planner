// copilotcli_route_test.go — the conditional BYOK route decision of
// `observer copilot-cli` (fix/copilot-cli-unrouted-launch). A recording stub
// stands in for `copilot`; no real binary is spawned.

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// copilotGateEnv is every env var the copilot-cli route decision reads.
var copilotGateEnv = []string{
	"COPILOT_PROVIDER_API_KEY", "COPILOT_PROVIDER_API_KEY_COMMAND", "COPILOT_PROVIDER_BEARER_TOKEN",
	"COPILOT_MODEL", "COPILOT_PROVIDER_MODEL_ID", "COPILOT_PROVIDER_BASE_URL", "COPILOT_PROVIDER_TYPE",
}

// unsetEnvForTest removes keys from the process env for this test and
// restores their previous values afterwards (t.Setenv can only set, and an
// empty value would still be PRESENT in the child env).
func unsetEnvForTest(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		prev, had := os.LookupEnv(k)
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unsetenv %s: %v", k, err)
		}
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(k, prev)
			} else {
				_ = os.Unsetenv(k)
			}
		})
	}
}

// TestCopilotCLIRouteDecision runs the real launcher against a recording stub
// for every row of the gate: subscription (no key) launches UNROUTED with no
// COPILOT_PROVIDER_* variable; a key plus a model routes; a key without a
// model is refused before copilot starts; the developer's own endpoint is
// left alone.
func TestCopilotCLIRouteDecision(t *testing.T) {
	const fakeKey = "fake-test-key-not-a-secret"
	cases := []struct {
		name        string
		env         map[string]string
		args        []string // launcher argv after --config/--copilot-path
		wantErr     string   // substring of a refusal; "" = launch runs
		wantRouted  bool
		wantModel   string // COPILOT_MODEL in the child ("" = absent)
		wantNotice  string // substring expected in stderr
		forbidInErr string // substring that must NOT appear in stderr
	}{
		{
			name:        "subscription: no key, no model",
			wantNotice:  "not routed through the proxy: Copilot is using your GitHub sign-in; set COPILOT_PROVIDER_API_KEY and a model (--model) to route it",
			forbidInErr: "routing via",
		},
		{
			name:        "subscription: no key, --model still selects the agent model",
			args:        []string{"--model", "gpt-5.4"},
			wantModel:   "gpt-5.4",
			wantNotice:  "not routed through the proxy",
			forbidInErr: "routing via",
		},
		{
			name:       "BYOK: key and --model route through the proxy",
			env:        map[string]string{"COPILOT_PROVIDER_API_KEY": fakeKey},
			args:       []string{"--model", "gpt-4o"},
			wantRouted: true,
			wantModel:  "gpt-4o",
		},
		{
			name:       "BYOK: key and an exported COPILOT_MODEL route",
			env:        map[string]string{"COPILOT_PROVIDER_API_KEY": fakeKey, "COPILOT_MODEL": "gpt-4o"},
			wantRouted: true,
			wantModel:  "gpt-4o",
		},
		{
			name:       "BYOK: bearer token counts as the credential",
			env:        map[string]string{"COPILOT_PROVIDER_BEARER_TOKEN": fakeKey},
			args:       []string{"--model", "gpt-4o"},
			wantRouted: true,
			wantModel:  "gpt-4o",
		},
		{
			name:    "BYOK: key without a model is refused with the fix named",
			env:     map[string]string{"COPILOT_PROVIDER_API_KEY": fakeKey},
			wantErr: "COPILOT_PROVIDER_API_KEY is set, so this launch routes your own provider through the proxy, and that needs an explicit model: pass --model <id> or set COPILOT_MODEL",
		},
		{
			// pass-2 review C156-19: an Anthropic key must not ride the
			// proxy's OpenAI lane (it would reach api.openai.com).
			name:        "BYOK: a non-openai provider type stays unrouted",
			env:         map[string]string{"COPILOT_PROVIDER_API_KEY": fakeKey, "COPILOT_PROVIDER_TYPE": "anthropic", "COPILOT_MODEL": "claude-x"},
			wantModel:   "claude-x",
			wantNotice:  "not routed through the proxy: COPILOT_PROVIDER_TYPE selects a provider this route cannot carry",
			forbidInErr: "routing via",
		},
		{
			name:        "BYOK: azure provider type without a model is unrouted, not refused",
			env:         map[string]string{"COPILOT_PROVIDER_API_KEY": fakeKey, "COPILOT_PROVIDER_TYPE": "azure"},
			wantNotice:  "COPILOT_PROVIDER_TYPE selects a provider this route cannot carry",
			forbidInErr: "routing via",
		},
		{
			name:       "BYOK: an explicit openai provider type routes",
			env:        map[string]string{"COPILOT_PROVIDER_API_KEY": fakeKey, "COPILOT_PROVIDER_TYPE": "openai", "COPILOT_MODEL": "gpt-4o"},
			wantRouted: true,
			wantModel:  "gpt-4o",
		},
		{
			name:        "own endpoint: left alone, not claimed",
			env:         map[string]string{"COPILOT_PROVIDER_API_KEY": fakeKey, "COPILOT_PROVIDER_BASE_URL": "https://my-api.example.com/v1", "COPILOT_MODEL": "m"},
			wantModel:   "m",
			wantNotice:  "COPILOT_PROVIDER_BASE_URL is already set to your own endpoint",
			forbidInErr: "routing via",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantRouted {
				requireComponents(t, "capture.proxy")
			}
			unsetEnvForTest(t, copilotGateEnv...)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			dir := t.TempDir()
			cfgPath := writeCopilotCLITestConfig(t, dir)
			argvPath := filepath.Join(dir, "argv.out")
			envPath := filepath.Join(dir, "env.out")
			stub := writeArgvEnvRecorderStub(t, dir, argvPath, envPath)

			cmd := newCopilotCLICmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(append([]string{"--config", cfgPath, "--copilot-path", stub}, tc.args...))
			err := cmd.Execute()

			if tc.wantErr != "" {
				var visible launcherMessageError
				if err == nil || !errors.As(err, &visible) || !strings.Contains(visible.launcherMessage(), tc.wantErr) {
					t.Fatalf("err = %v, want a visible refusal containing %q (stderr=%q)", err, tc.wantErr, out.String())
				}
				if _, statErr := os.Stat(envPath); statErr == nil {
					t.Fatal("copilot ran despite the refusal")
				}
				if strings.Contains(visible.launcherMessage(), fakeKey) {
					t.Fatal("refusal message leaks the key value")
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute: %v (stderr=%q)", err, out.String())
			}
			baseURL, _ := readRecordedEnvValue(t, envPath, "COPILOT_PROVIDER_BASE_URL")
			ptype, _ := readRecordedEnvValue(t, envPath, "COPILOT_PROVIDER_TYPE")
			routedToProxy := strings.HasPrefix(baseURL, "http://127.0.0.1:") && strings.HasSuffix(baseURL, "/v1")
			if routedToProxy != tc.wantRouted {
				t.Fatalf("routed = %v (COPILOT_PROVIDER_BASE_URL=%q), want %v", routedToProxy, baseURL, tc.wantRouted)
			}
			if tc.wantRouted && ptype != "openai" {
				t.Fatalf("COPILOT_PROVIDER_TYPE = %q, want openai on a routed launch", ptype)
			}
			if !tc.wantRouted && tc.env["COPILOT_PROVIDER_BASE_URL"] == "" && (baseURL != "" || ptype != tc.env["COPILOT_PROVIDER_TYPE"]) {
				t.Fatalf("unrouted launch set BYOK env: BASE_URL=%q TYPE=%q", baseURL, ptype)
			}
			if own := tc.env["COPILOT_PROVIDER_BASE_URL"]; own != "" && baseURL != own {
				t.Fatalf("own endpoint overwritten: %q, want %q", baseURL, own)
			}
			model, _ := readRecordedEnvValue(t, envPath, "COPILOT_MODEL")
			if model != tc.wantModel {
				t.Fatalf("COPILOT_MODEL = %q, want %q", model, tc.wantModel)
			}
			if tc.wantNotice != "" && !strings.Contains(out.String(), tc.wantNotice) {
				t.Fatalf("stderr %q missing %q", out.String(), tc.wantNotice)
			}
			if tc.forbidInErr != "" && strings.Contains(out.String(), tc.forbidInErr) {
				t.Fatalf("stderr %q must not contain %q", out.String(), tc.forbidInErr)
			}
			if strings.Contains(out.String(), "BYOK mode needs it") {
				t.Fatalf("stderr %q still prints the old BYOK warning", out.String())
			}
		})
	}
}

// TestAttachForwardsGateInputs: under attach the daemon-spawned launcher makes
// the gate decision, so every name the gate reads crosses the socket.
func TestAttachForwardsGateInputs(t *testing.T) {
	capab, _ := integration.For("copilot-cli")
	keys := mergeAuthKeys(capab.AuthEnv, attachExtraEnvKeys(capab, nil))
	for _, k := range []string{"COPILOT_PROVIDER_API_KEY", "COPILOT_PROVIDER_BEARER_TOKEN", "COPILOT_MODEL", "COPILOT_PROVIDER_MODEL_ID", "COPILOT_PROVIDER_BASE_URL", "COPILOT_PROVIDER_TYPE"} {
		if !slices.Contains(keys, k) {
			t.Errorf("attach does not forward %s", k)
		}
	}
	// An ungated launcher route adds nothing beyond its own extras.
	opencode, _ := integration.For("opencode")
	if got := attachExtraEnvKeys(opencode, []string{"X"}); !slices.Equal(got, []string{"X"}) {
		t.Errorf("ungated extras = %v, want [X]", got)
	}
}
