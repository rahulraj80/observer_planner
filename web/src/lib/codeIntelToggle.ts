// codeIntelToggle - the pure half of Settings > Intelligence > Code
// intelligence (D-307, 2026-10-05: code intelligence is OFF by default and
// runs only when the user turns it on).
//
// The card is a one-click switch for ONE config key, codeintel.enabled. It
// writes through the same PUT /api/config/keys path every schema-driven
// setting uses (dry run first, so the daemon validates the key and value
// before anything is written), and it never asks the user to run a command.

import type { ConfigKeyPatch } from "./configSchema.ts";

/** Dotted config key the card writes. */
export const CODEINTEL_ENABLED_KEY = "codeintel.enabled";

/** JSON field path of the same key in GET /api/config's `config` object. */
export const CODEINTEL_ENABLED_FIELD = "CodeIntel.Enabled";

export type CodeIntelState = "on" | "off" | "unknown";

/**
 * codeIntelState reads the module's state from GET /api/config's `config`
 * object. Anything but a real boolean is "unknown" (an older daemon, a
 * failed load), and an unknown state offers no switch, never a guess.
 */
export function codeIntelState(config: unknown): CodeIntelState {
  if (!config || typeof config !== "object") return "unknown";
  const block = (config as Record<string, unknown>).CodeIntel;
  if (!block || typeof block !== "object") return "unknown";
  const v = (block as Record<string, unknown>).Enabled;
  if (v === true) return "on";
  if (v === false) return "off";
  return "unknown";
}

/**
 * codeIntelTogglePatch is the single-key patch that flips the state, or
 * null when the state is unknown. `was` carries the current value so the
 * daemon's optimistic-concurrency check names the key if it moved.
 */
export function codeIntelTogglePatch(state: CodeIntelState): ConfigKeyPatch | null {
  if (state === "unknown") return null;
  const on = state === "on";
  return { key: CODEINTEL_ENABLED_KEY, value: !on, was: on };
}

type Copy = {
  label: string;
  tone: "success" | "neutral";
  summary: string;
  action: string;
  confirm: string;
  armedNote: string;
};

/** Every string the card shows, per state (no em dashes: copy rule). */
export const CODEINTEL_COPY: Readonly<Record<"on" | "off", Copy>> = {
  off: {
    label: "Off",
    tone: "neutral",
    summary:
      "Code intelligence indexes your projects' source code (symbols, imports, call graph) so the MCP symbol tools " +
      "(search_symbols, get_symbols, get_relations) and code-aware compression can use it. It is off by default " +
      "because indexing uses noticeable CPU and disk. Indexing is read-only on your repositories and never calls a model.",
    action: "Turn on",
    confirm: "Turn on?",
    armedNote:
      "Sets codeintel.enabled = true in config.toml (checked by the daemon before it is written). Indexing starts after the daemon restarts.",
  },
  on: {
    label: "On",
    tone: "success",
    summary:
      "Your projects are indexed when the daemon starts, and the MCP symbol tools and code-aware compression read " +
      "that index. Turning it off stops indexing; the index already built is kept until [codeintel].retention_days prunes it.",
    action: "Turn off",
    confirm: "Turn off?",
    armedNote:
      "Sets codeintel.enabled = false in config.toml (checked by the daemon before it is written). Takes effect after the daemon restarts.",
  },
};
