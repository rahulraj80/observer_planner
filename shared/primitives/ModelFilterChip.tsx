import { useMemo } from "react";
import { Cpu } from "lucide-react";
import {
  modelFilterOptions,
  NO_DATA_NOTE,
  withSelectedModel,
} from "../lib/modelFilterOptions";
import { ComboChip, type ComboOption } from "./ComboChip";
import { Icon } from "./Icon";
import { ModelId } from "./ModelId";

// ModelFilterChip - the global Model filter control, shared by the node
// FilterBar and the org TopBar so both render and behave identically. Pure:
// the caller fetches the per-model rows for the current selection (window,
// tool, project; never the model filter itself) and owns the value.
//
// Single-select with an "All models" sentinel, like the Tool and Project
// chips. Each option is a ModelId (family mark + id) with its turn count.
// A selected model with no rows under the current selection is kept, listed
// first and marked "No data in this selection" (shared/lib/
// modelFilterOptions.ts withSelectedModel) instead of being silently cleared.
export function ModelFilterChip({
  value,
  onChange,
  rows,
  loaded,
  className,
  hideIconOnPhone,
  disabled = false,
}: {
  value: string;
  onChange: (next: string) => void;
  /** Per-model rows for the current selection ({key: model id, turn_count}). */
  rows: readonly { key: string; turn_count?: number }[];
  /** True once `rows` is the answer for the current selection. */
  loaded: boolean;
  className?: string;
  /** Drop the chip's glyph below `sm` (a compact phone filter row). */
  hideIconOnPhone?: boolean;
  /**
   * Render the chip but refuse to open it: the page in view does not read
   * the Model filter. The applied value stays visible (it still applies on
   * the pages that read it); the caller says WHY in visible copy beside the
   * chip (ComboChip's disabled contract).
   */
  disabled?: boolean;
}) {
  const { options, selectedMissing } = useMemo(() => {
    const { options: opts, selectedMissing: missing } = withSelectedModel(
      modelFilterOptions(rows),
      value,
      loaded,
    );
    const list: ComboOption[] = [
      { value: "all", label: "All models", searchable: "all models" },
      ...opts.map((m) => ({
        value: m.model,
        label: <ModelId model={m.model} markSize={12} className="max-w-[260px]" />,
        searchable: m.searchable,
        title: m.noData ? `${m.model}: ${NO_DATA_NOTE.toLowerCase()}` : m.model,
        rightMeta: m.noData ? "no data" : m.turns.toLocaleString(),
        detail: m.noData ? NO_DATA_NOTE : undefined,
      })),
    ];
    return { options: list, selectedMissing: missing };
  }, [rows, value, loaded]);

  return (
    <ComboChip
      className={className}
      disabled={disabled}
      label="Model"
      value={value}
      onChange={onChange}
      options={options}
      icon={
        <span className={hideIconOnPhone ? "hidden sm:inline-flex" : "inline-flex"}>
          <Icon icon={Cpu} size={13} className="text-fg-3" />
        </span>
      }
      popoverWidth={340}
      placeholder="Filter models…"
      // The full id (plus the no-data note), not the type-ahead haystack.
      triggerTooltip={
        value === "all"
          ? "Model: all models"
          : `Model: ${value}${selectedMissing ? ` (${NO_DATA_NOTE.toLowerCase()})` : ""}`
      }
      // The applied filter, even before the rows answer (a filter restored
      // from the URL is in force while the list loads).
      buttonValueRender={() => {
        if (value === "all") {
          return <b className="font-semibold text-fg-0">all</b>;
        }
        return (
          <span className="flex min-w-0 items-center gap-1.5">
            <ModelId
              model={value}
              short
              markSize={11}
              className="max-w-[150px] text-[11px] font-semibold sm:max-w-[200px]"
            />
            {selectedMissing && (
              <span className="shrink-0 rounded-1 border border-warn/40 px-1 text-[10px] text-warn">
                no data
              </span>
            )}
          </span>
        );
      }}
    />
  );
}
