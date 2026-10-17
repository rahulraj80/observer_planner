// rowActionsModel: the pure half of RowActions (docs/app-design-system.md
// "Row actions"). No React, no DOM: which actions stay visible on the row,
// which go in the overflow menu and in what order, whether an action needs
// the in-menu confirm step, and the confirm step itself as a reducer.
//
// The rule it enforces: a destructive action (Delete, Revoke, Deactivate)
// never renders as a button repeated on every row. It always lives in the
// menu, after the non-destructive actions, and it confirms before it fires
// unless its own dialog is the confirm step (`confirm: false`).

/** The fields the model reads from a RowAction. */
export type RowActionShape = {
  id: string;
  destructive?: boolean;
  /** Keep this action visible on the row. Ignored for a destructive action. */
  inline?: boolean;
  /** false: the action opens its own confirm dialog, so the menu fires it at once. */
  confirm?: false | object;
  disabled?: boolean;
};

export type RowActionSplit<T extends RowActionShape> = {
  /** Visible on the row, in the caller's order. */
  inline: T[];
  /** In the overflow menu: non-destructive first, then destructive. */
  menu: T[];
  /** Index in `menu` of the first destructive item (a separator goes before it), or -1. */
  firstDestructive: number;
};

/** splitRowActions partitions a row's actions into the visible ones and the overflow menu. */
export function splitRowActions<T extends RowActionShape>(actions: readonly T[]): RowActionSplit<T> {
  const inline: T[] = [];
  const safe: T[] = [];
  const destructive: T[] = [];
  for (const a of actions) {
    if (a.destructive) destructive.push(a);
    else if (a.inline) inline.push(a);
    else safe.push(a);
  }
  const menu = [...safe, ...destructive];
  return {
    inline,
    menu,
    firstDestructive: destructive.length > 0 && safe.length > 0 ? safe.length : destructive.length > 0 ? 0 : -1,
  };
}

/** needsConfirm reports whether choosing the action arms a confirm step first. */
export function needsConfirm(a: RowActionShape): boolean {
  return !!a.destructive && a.confirm !== false;
}

/** The confirm step's state: which menu item, if any, is armed. */
export type ConfirmState = { armed: string | null };

export type ConfirmEvent =
  | { type: "choose"; action: RowActionShape }
  | { type: "disarm" };

export type ConfirmResult = {
  state: ConfirmState;
  /** The action id to run now, or null. */
  fire: string | null;
  /** Close the menu after this event. */
  close: boolean;
};

/**
 * confirmStep is the menu's two-step confirm: choosing a destructive item
 * arms it (the menu stays open and shows its confirm label), choosing the
 * armed item again fires it, choosing anything else re-targets the arm or
 * fires a safe item. A disabled item does nothing.
 */
export function confirmStep(state: ConfirmState, ev: ConfirmEvent): ConfirmResult {
  if (ev.type === "disarm") return { state: { armed: null }, fire: null, close: false };
  const a = ev.action;
  if (a.disabled) return { state, fire: null, close: false };
  if (!needsConfirm(a)) return { state: { armed: null }, fire: a.id, close: true };
  if (state.armed === a.id) return { state: { armed: null }, fire: a.id, close: true };
  return { state: { armed: a.id }, fire: null, close: false };
}
