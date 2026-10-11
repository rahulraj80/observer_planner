import type { ReactNode } from "react";
import { ThemeProvider as SharedThemeProvider } from "@shared/lib/useTheme";

// The theme preference is owned by the shared design system
// (shared/lib/theme.ts + shared/lib/useTheme.tsx); this file binds the
// "web" row of THEME_APPS so importers are unchanged.
export { useTheme, type ThemeMode, type EffectiveTheme } from "@shared/lib/useTheme";

export function ThemeProvider({ children }: { children: ReactNode }) {
  return <SharedThemeProvider app="web">{children}</SharedThemeProvider>;
}
