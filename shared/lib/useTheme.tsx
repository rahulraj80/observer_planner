import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import {
  THEME_APPS,
  applyTheme,
  queryThemeMode,
  readThemeMode,
  resolveEffective,
  writeThemeMode,
  type EffectiveTheme,
  type ThemeApp,
  type ThemeMode,
} from "./theme";

// The theme preference as UI state, one provider for every app; the per-app
// differences come from THEME_APPS. Owns a localStorage convenience and the
// <html> data-theme stamp; follows the OS live in "system" mode; a `storage`
// event keeps two tabs of the same app in step.

export type { EffectiveTheme, ThemeMode, ThemeApp } from "./theme";

export type ThemeCtx = {
  mode: ThemeMode;
  effective: EffectiveTheme;
  setMode: (m: ThemeMode) => void;
  /** The modes this app offers, in toggle order. */
  modes: readonly ThemeMode[];
};

const Ctx = createContext<ThemeCtx | null>(null);

function safeStorage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

function prefersDark(): boolean {
  if (typeof window === "undefined") return true;
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? true;
}

function docOrNull(): Document | null {
  return typeof document === "undefined" ? null : document;
}

/**
 * initialThemeMode: the stored mode, or a `?theme=` override where the app
 * honours one (persisted, so a previewed theme sticks).
 */
export function initialThemeMode(app: ThemeApp): ThemeMode {
  const row = THEME_APPS[app];
  const q = typeof window === "undefined" ? null : queryThemeMode(window.location.search, row);
  if (q) {
    writeThemeMode(safeStorage(), row, q);
    return q;
  }
  return readThemeMode(safeStorage(), row);
}

/**
 * initTheme applies the initial theme before the app renders (no flash of
 * the wrong palette for an app whose index.html has no pre-paint script).
 */
export function initTheme(app: ThemeApp): void {
  applyTheme(docOrNull(), resolveEffective(initialThemeMode(app), prefersDark()));
}

export function ThemeProvider({ app, children }: { app: ThemeApp; children: ReactNode }) {
  const row = THEME_APPS[app];
  const [mode, setModeState] = useState<ThemeMode>(() => initialThemeMode(app));
  const [effective, setEffective] = useState<EffectiveTheme>(() => resolveEffective(mode, prefersDark()));

  const setMode = useCallback(
    (m: ThemeMode) => {
      setModeState(m);
      writeThemeMode(safeStorage(), row, m);
    },
    [row],
  );

  // Apply the resolved theme + recompute when mode changes.
  useEffect(() => {
    const next = resolveEffective(mode, prefersDark());
    setEffective(next);
    applyTheme(docOrNull(), next);
  }, [mode]);

  // When the user picks System, follow OS-level changes live.
  useEffect(() => {
    if (mode !== "system" || typeof window === "undefined") return;
    const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
    if (!mq) return;
    const onChange = () => {
      const next: EffectiveTheme = mq.matches ? "dark" : "light";
      setEffective(next);
      applyTheme(docOrNull(), next);
    };
    mq.addEventListener?.("change", onChange);
    return () => mq.removeEventListener?.("change", onChange);
  }, [mode]);

  // Another tab of the same app changed the preference.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const onStorage = (e: StorageEvent) => {
      if (e.key === row.storageKey) setModeState(readThemeMode(safeStorage(), row));
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, [row]);

  const value = useMemo<ThemeCtx>(
    () => ({ mode, effective, setMode, modes: row.modes }),
    [mode, effective, setMode, row],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useTheme(): ThemeCtx {
  const v = useContext(Ctx);
  if (!v) throw new Error("useTheme must be used within ThemeProvider");
  return v;
}
