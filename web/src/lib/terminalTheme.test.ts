import assert from "node:assert/strict";
import test from "node:test";

import {
  TERMINAL_MIN_CONTRAST,
  TERMINAL_THEMES,
  parseTerminalThemePref,
  resolveTerminalTheme,
} from "../../../shared/lib/terminalTheme.ts";

// The light terminal theme (backlog item 1: "terminals only have a dark
// background") must keep every ANSI color legible: TUIs paint yellow, cyan
// and even "white" text on the default background.

function lum(hex: string): number {
  const h = hex.replace("#", "");
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16) / 255);
  const f = (c: number) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}
function contrast(a: string, b: string): number {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

const ANSI = [
  "foreground",
  "black",
  "red",
  "green",
  "yellow",
  "blue",
  "magenta",
  "cyan",
  "white",
  "brightBlack",
  "brightRed",
  "brightGreen",
  "brightYellow",
  "brightBlue",
  "brightMagenta",
  "brightCyan",
  "brightWhite",
] as const;

test("every light-theme ANSI color clears WCAG AA on the canvas", () => {
  const t = TERMINAL_THEMES.light;
  for (const k of ANSI) {
    const c = contrast(t[k], t.background);
    assert.ok(c >= 4.5, `${k} ${t[k]} is ${c.toFixed(2)}:1`);
  }
});

test("dark-theme foreground colors (not black) clear AA on the canvas", () => {
  const t = TERMINAL_THEMES.dark;
  for (const k of ANSI) {
    if (k === "black" || k === "brightBlack") continue;
    const c = contrast(t[k], t.background);
    assert.ok(c >= 4.5, `${k} ${t[k]} is ${c.toFixed(2)}:1`);
  }
});

// Hue preservation: the operator's complaint was program colors coming out
// monochrome. Each chromatic ANSI slot must stay a saturated color in its own
// hue family in BOTH themes (red stays red, green stays green, ...), and no two
// chromatic families may collapse onto each other.
function hsl(hex: string): { h: number; s: number } {
  const x = hex.replace("#", "");
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(x.slice(i, i + 2), 16) / 255);
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const d = max - min;
  const l = (max + min) / 2;
  const s = d === 0 ? 0 : d / (1 - Math.abs(2 * l - 1));
  let h = 0;
  if (d !== 0) {
    if (max === r) h = 60 * (((g - b) / d) % 6);
    else if (max === g) h = 60 * ((b - r) / d + 2);
    else h = 60 * ((r - g) / d + 4);
  }
  return { h: (h + 360) % 360, s };
}
function inBand(h: number, [lo, hi]: [number, number]): boolean {
  return lo <= hi ? h >= lo && h <= hi : h >= lo || h <= hi;
}

const HUE_BANDS: Record<string, [number, number]> = {
  red: [345, 15],
  yellow: [30, 65],
  green: [90, 170],
  cyan: [175, 205],
  blue: [210, 250],
  magenta: [255, 320],
};

for (const name of ["dark", "light"] as const) {
  test(`${name}: every chromatic ANSI slot keeps its hue family and saturation`, () => {
    const t = TERMINAL_THEMES[name];
    for (const [family, band] of Object.entries(HUE_BANDS)) {
      for (const slot of [family, "bright" + family[0].toUpperCase() + family.slice(1)]) {
        const hex = t[slot as keyof typeof t] as string;
        const { h, s } = hsl(hex);
        assert.ok(s >= 0.4, `${name}.${slot} ${hex} is near-grey (s=${s.toFixed(2)})`);
        assert.ok(inBand(h, band), `${name}.${slot} ${hex} hue ${h.toFixed(0)} outside ${family} ${band}`);
      }
    }
  });
}

test("contrast floor: dark passes colors through, light only nudges lightness", () => {
  // Dark: the canvas every CLI assumes, so no remapping at all.
  assert.equal(TERMINAL_MIN_CONTRAST.dark, 1);
  // Light: AA floor so dark-assuming truecolor output (white / pale text,
  // default-fg on dark diff rows) stays readable. Bounded so the safeguard
  // never forces everything toward pure black.
  assert.ok(TERMINAL_MIN_CONTRAST.light >= 4.5 && TERMINAL_MIN_CONTRAST.light <= 7);
});

test("preference resolution", () => {
  assert.equal(resolveTerminalTheme("app", "light"), "light");
  assert.equal(resolveTerminalTheme("app", "dark"), "dark");
  assert.equal(resolveTerminalTheme("dark", "light"), "dark");
  assert.equal(resolveTerminalTheme("light", "dark"), "light");
  assert.equal(parseTerminalThemePref("bogus"), "app");
  assert.equal(parseTerminalThemePref(null), "app");
  assert.equal(parseTerminalThemePref("light"), "light");
});
