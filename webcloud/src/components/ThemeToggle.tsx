// The portal's theme control: the shared ThemeToggle (one owner,
// shared/primitives/ThemeToggle.tsx) over the shared theme state.
import { ThemeToggle as SharedThemeToggle } from "@shared/primitives/ThemeToggle";
import { useTheme } from "@shared/lib/useTheme";

export function ThemeToggle() {
  const { mode, setMode, modes } = useTheme();
  return <SharedThemeToggle value={mode} onChange={setMode} modes={modes} />;
}
