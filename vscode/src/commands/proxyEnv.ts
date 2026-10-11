// observer.copyProxyEnv — writes the proxy env triple to the clipboard
// in the user's shell syntax. Replaces the M0 placeholder.

import * as vscode from 'vscode';
import type { DaemonManager } from '../daemon';
import { detectShell, formatEnv, proxyEnvFor } from '../terminal/envFormat';
import { componentInactiveNow } from '../components';

export function registerCopyProxyEnv(
  ctx: vscode.ExtensionContext,
  daemon: DaemonManager,
): void {
  ctx.subscriptions.push(
    vscode.commands.registerCommand('observer.copyProxyEnv', async () => {
      if (componentInactiveNow('capture.proxy')) {
        void vscode.window.showWarningMessage(
          'SuperBased: the API proxy component is not active on this machine; nothing listens on the proxy port. Turn it on in Settings > Components.',
        );
        return;
      }
      const env = proxyEnvFor(daemon.getState().proxyPort);
      const shell = detectShell(vscode.env.shell);
      const text = formatEnv(env, shell);
      await vscode.env.clipboard.writeText(text);
      void vscode.window.showInformationMessage(
        `SuperBased: proxy env vars copied (${shell} syntax). Paste into your terminal to route AI CLIs through the proxy.`,
      );
    }),
  );
}
