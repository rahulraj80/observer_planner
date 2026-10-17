// The extension's component gate (T-670): reads GET /api/components/state
// when the daemon comes up and every minute after, and mirrors it into the
// context keys package.json's `when` clauses read. Pure decisions live in
// components-internals.ts.

import * as vscode from 'vscode';
import type { DaemonManager } from './daemon';
import { output } from './output';
import {
  contextValues,
  isComponentInactive,
  isRouteInactive,
  type ComponentsStateLite,
} from './components-internals';

const POLL_MS = 60_000;

/** ComponentGate answers "is this surface's component inactive here?". */
export interface ComponentGate extends vscode.Disposable {
  /** True only when the daemon said the component is not active. */
  componentInactive(id: string): boolean;
  /** True only when the daemon listed path under an inactive component. */
  routeInactive(path: string): boolean;
  /** Re-reads the state now. */
  refresh(): Promise<void>;
}

/** The gate before (or without) a state read: everything shows. */
let current: ComponentsStateLite | undefined;

export function createComponentGate(daemon: DaemonManager): ComponentGate {
  let timer: NodeJS.Timeout | undefined;
  let disposed = false;
  let lastKey = '';

  const apply = async (st: ComponentsStateLite | undefined): Promise<void> => {
    current = st;
    const values = contextValues(st);
    const key = JSON.stringify(values);
    if (key === lastKey) return;
    lastKey = key;
    for (const [k, v] of Object.entries(values)) {
      await vscode.commands.executeCommand('setContext', k, v);
    }
  };

  const refresh = async (): Promise<void> => {
    if (disposed) return;
    if (daemon.getState().status === 'idle') {
      await apply(undefined);
      return;
    }
    try {
      await apply(await daemon.getClient().componentsState());
    } catch (err) {
      // An older daemon without the route, or a failed read: fail open.
      output.appendLine(`Component state read failed (showing everything): ${(err as Error).message}`);
      await apply(undefined);
    }
  };

  const sub = daemon.onDidChangeState(() => void refresh());
  const tick = (): void => {
    timer = setTimeout(async () => {
      await refresh();
      if (!disposed) tick();
    }, POLL_MS);
  };
  void refresh();
  tick();

  return {
    componentInactive: (id) => isComponentInactive(current, id),
    routeInactive: (path) => isRouteInactive(current, path),
    refresh,
    dispose: () => {
      disposed = true;
      if (timer) clearTimeout(timer);
      sub.dispose();
    },
  };
}

/** componentInactiveNow reads the last state without a gate handle. */
export function componentInactiveNow(id: string): boolean {
  return isComponentInactive(current, id);
}
