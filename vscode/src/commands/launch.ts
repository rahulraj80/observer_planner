import * as vscode from 'vscode';
import type { ResolvedBinary } from '../binary';
import { componentInactiveNow } from '../components';

// "Launch <tool> (proxied)" — L2a of the usability arc (P4.5): an
// integrated terminal running the observer wrapper, which sets
// ANTHROPIC_BASE_URL (and re-exports a FRESH Pro/Max OAuth token —
// stale tokens are deliberately skipped since D13, so the session
// never 401s on a dead re-export). The terminal is the honest
// mechanism: the daemon can't host an interactive session, and the
// user sees exactly what ran.
export function registerLaunch(
  ctx: vscode.ExtensionContext,
  bin: ResolvedBinary,
): void {
  const quoted = bin.path.includes(' ') ? `"${bin.path}"` : bin.path;
  const launch = (id: string, name: string, sub: string) =>
    vscode.commands.registerCommand(id, () => {
      // Hidden from the palette while the proxy component is not active
      // (T-670); a keybinding still lands here, so say why instead.
      if (componentInactiveNow('capture.proxy')) {
        void vscode.window.showWarningMessage(
          'SuperBased: the API proxy component is not active on this machine, so a proxied launch would not be routed. Turn it on in Settings > Components.',
        );
        return;
      }
      const term = vscode.window.createTerminal({
        name,
        cwd: vscode.workspace.workspaceFolders?.[0]?.uri.fsPath,
      });
      term.sendText(`${quoted} ${sub}`);
      term.show();
      ctx.subscriptions.push(term);
    });
  ctx.subscriptions.push(
    launch('observer.launchClaude', 'Claude Code (proxied)', 'claude'),
    launch('observer.launchCodex', 'Codex (proxied)', 'codex'),
  );
}
