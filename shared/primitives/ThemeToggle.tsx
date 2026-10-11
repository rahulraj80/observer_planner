import clsx from "clsx";
import { Monitor, Moon, Sun, type LucideIcon } from "lucide-react";
import { Icon } from "./Icon";
import { Tooltip } from "./Tooltip";
import { THEME_MODE_LABEL, type ThemeMode } from "../lib/theme";

// ThemeToggle: the Light / Dark (/ System) segmented radio group in every
// app's top bar. Pure: the value comes in, the choice goes out through
// onChange; persistence and the data-theme stamp belong to the caller
// (lib/useTheme.tsx). `modes` is the app's offered set (THEME_APPS).

const MODE_ICON: Readonly<Record<ThemeMode, LucideIcon>> = {
  light: Sun,
  dark: Moon,
  system: Monitor,
};

export function ThemeToggle({
  value,
  onChange,
  modes,
  dataTour,
  className,
}: {
  value: ThemeMode;
  onChange: (mode: ThemeMode) => void;
  modes: readonly ThemeMode[];
  /** A guided-tour anchor (`data-tour`), when the app's tour points at it. */
  dataTour?: string;
  className?: string;
}) {
  return (
    <div
      role="radiogroup"
      aria-label="Theme"
      data-tour={dataTour}
      className={clsx("flex items-center gap-0.5 rounded-2 border border-line-2 bg-bg-2 p-0.5", className)}
    >
      {modes.map((m) => (
        <Tooltip key={m} content={THEME_MODE_LABEL[m]}>
          <button
            type="button"
            role="radio"
            aria-checked={value === m}
            aria-label={THEME_MODE_LABEL[m]}
            onClick={() => onChange(m)}
            className={clsx(
              "grid h-6 w-6 place-items-center rounded-1 transition-colors",
              value === m ? "bg-bg-4 text-fg-0" : "text-fg-3 hover:bg-bg-3 hover:text-fg-1",
            )}
          >
            <Icon icon={MODE_ICON[m]} size="xs" />
          </button>
        </Tooltip>
      ))}
    </div>
  );
}
