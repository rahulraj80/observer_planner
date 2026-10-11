// Contributed terminal profile that pre-exports the observer
// proxy env vars. Pairs with package.json's
// contributes.terminal.profiles entry under the same id.

import * as vscode from 'vscode';
import type { DaemonManager } from '../daemon';
import { proxyEnvFor } from './envFormat';
import { componentInactiveNow } from '../components';

const PROFILE_ID = 'observer.terminalProfile';

export function registerTerminalProfile(
  ctx: vscode.ExtensionContext,
  daemon: DaemonManager,
): void {
  const provider: vscode.TerminalProfileProvider = {
    provideTerminalProfile() {
      // A contributed profile cannot be hidden by a `when` clause; with the
      // proxy component inactive it opens a plain terminal and says so,
      // never one that points tools at a port nothing listens on (T-670).
      if (componentInactiveNow('capture.proxy')) {
        void vscode.window.showWarningMessage(
          'SuperBased: the API proxy component is not active on this machine, so this terminal is not routed through it.',
        );
        return new vscode.TerminalProfile({ name: 'AI Coding Tool (proxy inactive)' });
      }
      const env = proxyEnvFor(daemon.getState().proxyPort);
      return new vscode.TerminalProfile({
        name: 'AI Coding Tool (SuperBased-proxied)',
        env: {
          ANTHROPIC_BASE_URL: env.ANTHROPIC_BASE_URL,
          OPENAI_BASE_URL: env.OPENAI_BASE_URL,
          ENABLE_TOOL_SEARCH: env.ENABLE_TOOL_SEARCH,
        },
      });
    },
  };
  ctx.subscriptions.push(
    vscode.window.registerTerminalProfileProvider(PROFILE_ID, provider),
  );
}
