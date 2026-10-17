// The theme preference is owned by the shared design system
// (shared/lib/theme.ts, THEME_APPS row "webcloud": key "sb_theme", light /
// dark, dark default, ?theme= override). initTheme stamps the theme before
// the first render; the ThemeProvider in main.tsx keeps it afterwards.
import { initTheme as sharedInitTheme } from "@shared/lib/useTheme";

export function initTheme(): void {
  sharedInitTheme("webcloud");
}
