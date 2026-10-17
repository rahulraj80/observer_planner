import {
  autoUpdate,
  flip,
  FloatingFocusManager,
  FloatingPortal,
  offset,
  shift,
  useClick,
  useDismiss,
  useFloating,
  useInteractions,
  useListNavigation,
  useRole,
} from "@floating-ui/react";
import clsx from "clsx";
import { Fragment, useRef, useState, type ReactNode, type SyntheticEvent } from "react";
import { Ellipsis, type LucideIcon } from "lucide-react";
import { Button } from "./Button";
import { Icon } from "./Icon";
import { confirmStep, needsConfirm, splitRowActions, type ConfirmState } from "./rowActionsModel";

// RowActions: a table row's actions (docs/app-design-system.md "Row
// actions"). Destructive actions (Delete, Revoke, Deactivate) stop repeating
// as red buttons on every row: they live in one overflow menu ("..."), after
// a separator, and confirm IN the menu before they fire (the item's label
// turns into its confirm label, a second choice fires it). An action that
// opens its own confirm dialog passes `confirm: false`.
//
// Non-destructive actions may stay visible on the row (`inline`, rendered as
// quiet ghost buttons); everything else goes in the menu. The menu is a real
// WAI-ARIA menu: the trigger is a labelled button with aria-haspopup, arrow
// keys move between items, Escape or a click outside closes it and returns
// focus to the trigger. Clicks and keys inside never reach the row's own
// handlers (a clickable row does not open when a menu item is chosen).
//
// Pure: no fetch, no app state, no router. The partition and the confirm
// step are the tested rowActionsModel.
export type RowAction = {
  id: string;
  label: ReactNode;
  onSelect: () => void;
  icon?: LucideIcon;
  destructive?: boolean;
  /** Keep this non-destructive action visible on the row. */
  inline?: boolean;
  /** The in-menu confirm step for a destructive item; false when the action opens its own dialog. */
  confirm?: false | { label?: ReactNode; note?: ReactNode };
  disabled?: boolean;
  /** Shown under a disabled item: why it cannot be used. */
  disabledReason?: ReactNode;
  /** Accessible name when `label` is not plain text. */
  ariaLabel?: string;
};

export type RowActionsProps = {
  actions: RowAction[];
  /** The trigger's accessible name, e.g. "Actions for Team platform". */
  menuLabel?: string;
  /** Disables every action (a request in flight). */
  disabled?: boolean;
  className?: string;
};

const stop = (e: SyntheticEvent) => e.stopPropagation();

export function RowActions({ actions, menuLabel = "More actions", disabled, className }: RowActionsProps) {
  const { inline, menu, firstDestructive } = splitRowActions(actions);
  const [open, setOpen] = useState(false);
  const [confirm, setConfirm] = useState<ConfirmState>({ armed: null });
  const [activeIndex, setActiveIndex] = useState<number | null>(null);
  const listRef = useRef<Array<HTMLElement | null>>([]);

  const { refs, floatingStyles, context } = useFloating({
    open,
    onOpenChange: (next) => {
      setOpen(next);
      if (!next) setConfirm({ armed: null });
    },
    placement: "bottom-end",
    middleware: [offset(4), flip(), shift({ padding: 8 })],
    whileElementsMounted: autoUpdate,
  });
  const click = useClick(context, { enabled: !disabled });
  const dismiss = useDismiss(context);
  const role = useRole(context, { role: "menu" });
  const nav = useListNavigation(context, {
    listRef,
    activeIndex,
    onNavigate: setActiveIndex,
    loop: true,
  });
  const { getReferenceProps, getFloatingProps, getItemProps } = useInteractions([click, dismiss, role, nav]);

  const choose = (a: RowAction) => {
    const r = confirmStep(confirm, { type: "choose", action: { ...a, disabled: a.disabled || disabled } });
    setConfirm(r.state);
    if (r.close) setOpen(false);
    if (r.fire) a.onSelect();
  };

  return (
    <span className={clsx("inline-flex items-center justify-end gap-1", className)} onClick={stop} onKeyDown={stop}>
      {inline.map((a) => (
        <Button
          key={a.id}
          variant="ghost"
          size="sm"
          iconLeft={a.icon}
          disabled={disabled || a.disabled}
          aria-label={a.ariaLabel}
          onClick={a.onSelect}
        >
          {a.label}
        </Button>
      ))}
      {menu.length > 0 && (
        <Button
          ref={refs.setReference}
          variant="ghost"
          size="sm"
          aria-label={menuLabel}
          disabled={disabled}
          className="!px-1.5"
          {...getReferenceProps()}
        >
          <Icon icon={Ellipsis} size="sm" />
        </Button>
      )}
      {open && (
        <FloatingPortal>
          <FloatingFocusManager context={context} modal={false} initialFocus={0}>
            <div
              ref={refs.setFloating}
              style={floatingStyles}
              aria-label={menuLabel}
              className="z-[85] min-w-[11rem] max-w-[18rem] rounded-2 border border-line-3 bg-bg-2 py-1 shadow-[var(--shadow-2)]"
              {...getFloatingProps({ onClick: stop, onKeyDown: stop })}
            >
              {menu.map((a, i) => {
                const armed = confirm.armed === a.id;
                const off = disabled || a.disabled;
                const note = armed && a.confirm ? a.confirm.note : undefined;
                return (
                  <Fragment key={a.id}>
                    {i === firstDestructive && i > 0 && <div role="separator" className="my-1 border-t border-line-2" />}
                    <button
                      type="button"
                      role="menuitem"
                      aria-disabled={off || undefined}
                      aria-label={a.ariaLabel}
                      ref={(node) => {
                        listRef.current[i] = node;
                      }}
                      tabIndex={activeIndex === i ? 0 : -1}
                      className={clsx(
                        "flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[12px] focus:outline-none",
                        off
                          ? "cursor-not-allowed text-fg-4"
                          : a.destructive
                            ? clsx("text-danger hover:bg-danger-soft focus:bg-danger-soft", armed && "bg-danger-soft font-semibold")
                            : "text-fg-1 hover:bg-bg-3 focus:bg-bg-3",
                      )}
                      {...getItemProps({ onClick: () => choose(a) })}
                    >
                      {a.icon && <Icon icon={a.icon} size="xs" className="shrink-0" />}
                      <span className="min-w-0 flex-1">
                        {armed && needsConfirm(a) ? ((a.confirm && a.confirm.label) ?? "Confirm?") : a.label}
                        {off && a.disabledReason && (
                          <span className="mt-0.5 block text-[11px] font-normal text-fg-4">{a.disabledReason}</span>
                        )}
                      </span>
                    </button>
                    {note && <div className="px-2.5 pb-1.5 text-[11px] leading-snug text-fg-3">{note}</div>}
                  </Fragment>
                );
              })}
            </div>
          </FloatingFocusManager>
        </FloatingPortal>
      )}
    </span>
  );
}
