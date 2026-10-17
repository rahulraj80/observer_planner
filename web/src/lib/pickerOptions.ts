// pickerOptions: the pure option builders behind the node dashboard's
// pickers (D-571: a known set is picked, never typed; lane PICKERS-2). Each
// builder turns one server response (or a shared catalog) into the option
// rows the shared EntityMultiPicker / SinglePicker render, so every page
// derives the same labels from the same source and a test pins them.
//
// No imports besides `import type`, so it runs under the node test runner.

/** One picker row (structurally the shared EntityMultiPickerOption). */
export type PickOption = {
  id: string;
  label: string;
  sublabel?: string;
  group?: string;
  disabledReason?: string;
};

/** uniqueSorted drops empties and duplicates and sorts ids. */
export function uniqueSorted(ids: Iterable<string | null | undefined>): string[] {
  const out = new Set<string>();
  for (const id of ids) {
    const t = (id ?? "").trim();
    if (t) out.add(t);
  }
  return [...out].sort((a, b) => a.localeCompare(b));
}

/** splitList parses a comma- or newline-separated string into items. */
export function splitList(s: string): string[] {
  return s
    .split(/[,\n]/)
    .map((x) => x.trim())
    .filter(Boolean);
}

/** joinList renders items back to the comma form a profile key stores. */
export function joinList(items: readonly string[]): string {
  return items.join(", ");
}

/** keepSelected appends any selected id the option list does not carry, so a
 *  chip never shows a value the list cannot explain (a value set by hand in
 *  config.toml, or an option an older daemon no longer lists). */
export function keepSelected(options: PickOption[], selected: readonly string[], group = "Set in config"): PickOption[] {
  const have = new Set(options.map((o) => o.id));
  const extra = selected.filter((id) => id && !have.has(id)).map((id) => ({ id, label: id, group }));
  return extra.length ? [...options, ...extra] : options;
}

/** pricedModelIds: every model the cost engine prices on this node (the
 *  built-in defaults of GET /api/config/pricing/defaults plus the operator's
 *  own [intelligence.pricing.models] overrides), sorted. */
export function pricedModelIds(
  defaults?: Record<string, unknown> | null,
  overrides?: Record<string, unknown> | null,
): string[] {
  return uniqueSorted([...Object.keys(defaults ?? {}), ...Object.keys(overrides ?? {})]);
}

/** modelOptions: the models this node has used (GET /api/models rows, most
 *  turns first) and then every other model the cost engine prices. Placeholder
 *  ids ("<unknown>", "(unknown)", "<synthetic>") are dropped. */
export function modelOptions(
  used: readonly { key: string; turn_count?: number }[],
  priced: readonly string[],
): PickOption[] {
  const placeholder = (k: string) => !k || /^[<(].*[>)]$/.test(k);
  const usedRows = used
    .filter((r) => !placeholder(r.key))
    .slice()
    .sort((a, b) => (b.turn_count ?? 0) - (a.turn_count ?? 0) || a.key.localeCompare(b.key));
  const seen = new Set<string>();
  const out: PickOption[] = [];
  for (const r of usedRows) {
    if (seen.has(r.key)) continue;
    seen.add(r.key);
    out.push({
      id: r.key,
      label: r.key,
      sublabel: r.turn_count ? `${r.turn_count} turn${r.turn_count === 1 ? "" : "s"}` : undefined,
      group: "Used on this node",
    });
  }
  for (const id of uniqueSorted(priced)) {
    if (seen.has(id) || placeholder(id)) continue;
    seen.add(id);
    out.push({ id, label: id, group: "Priced" });
  }
  return out;
}

/** projectRootOptions: the projects this node has seen (GET /api/projects),
 *  most sessions first; the label is the folder name, the id the full root. */
export function projectRootOptions(rows: readonly { root_path: string; session_count?: number }[]): PickOption[] {
  const seen = new Set<string>();
  return rows
    .filter((r) => r.root_path && !seen.has(r.root_path) && seen.add(r.root_path))
    .slice()
    .sort((a, b) => (b.session_count ?? 0) - (a.session_count ?? 0) || a.root_path.localeCompare(b.root_path))
    .map((r) => ({
      id: r.root_path,
      label: baseName(r.root_path),
      sublabel: r.session_count ? `${r.session_count} session${r.session_count === 1 ? "" : "s"}` : undefined,
    }));
}

/** sessionOptions: recent sessions (GET /api/sessions rows), newest first;
 *  label = project folder and tool, sublabel = when it started. */
export function sessionOptions(
  rows: readonly { id: string; tool?: string; project?: string; started_at?: string }[],
): PickOption[] {
  const seen = new Set<string>();
  return rows
    .filter((r) => r.id && !seen.has(r.id) && seen.add(r.id))
    .slice()
    .sort((a, b) => (b.started_at ?? "").localeCompare(a.started_at ?? ""))
    .map((r) => ({
      id: r.id,
      label: [r.project ? baseName(r.project) : "", r.tool ?? ""].filter(Boolean).join(" · ") || r.id,
      sublabel: r.started_at ? r.started_at.replace("T", " ").slice(0, 16) : undefined,
    }));
}

/** toolOptions: the AI tools this binary knows (GET /api/tools/status), the
 *  detected ones first; a tool not detected on this machine is still listed. */
export function toolOptions(rows: readonly { tool: string; detected?: boolean; enabled?: boolean }[]): PickOption[] {
  return rows
    .filter((r) => r.tool)
    .slice()
    .sort((a, b) => Number(Boolean(b.detected)) - Number(Boolean(a.detected)) || a.tool.localeCompare(b.tool))
    .map((r) => ({
      id: r.tool,
      label: r.tool,
      group: r.detected ? "Detected on this machine" : "Not detected",
    }));
}

/** mcpServerOptions: the MCP servers configured in this machine's AI tools
 *  (GET /api/guard/mcp), one row per server name with the clients that
 *  carry it. */
export function mcpServerOptions(
  servers: readonly { name: string; client?: string; present?: boolean }[],
): PickOption[] {
  const clients = new Map<string, Set<string>>();
  for (const s of servers) {
    if (!s.name) continue;
    const set = clients.get(s.name) ?? new Set<string>();
    if (s.client) set.add(s.client);
    clients.set(s.name, set);
  }
  return [...clients.keys()]
    .sort((a, b) => a.localeCompare(b))
    .map((name) => ({ id: name, label: name, sublabel: [...(clients.get(name) ?? [])].sort().join(", ") || undefined }));
}

/** endUserOptions: the end-user ids a policy can name: the users seen on
 *  admission verdicts (GET /api/obs/admission/verdicts) plus any the draft
 *  already names (a cohort map key, a rule's user matcher). */
export function endUserOptions(
  events: readonly { user?: string }[],
  named: readonly string[] = [],
): PickOption[] {
  const seen = new Set(uniqueSorted(events.map((e) => e.user)));
  return [
    ...[...seen].map((u) => ({ id: u, label: u, group: "Seen on verdicts" })),
    ...uniqueSorted(named)
      .filter((u) => !seen.has(u))
      .map((u) => ({ id: u, label: u, group: "Named in this policy" })),
  ];
}

/** NODE_FEATURES mirrors internal/policyfam/nodefeatures.AllFeatures: the
 *  node features an organization can turn off (and a feature grant names). */
export const NODE_FEATURES = ["terminals", "remote", "routing_apply", "patterns_write"];

/** featureOptions: what a feature-access request can name (accessgrant
 *  TypeFeatureEnable): a node feature, or a setting the organization pins on
 *  this machine (GET /api/governance read_only_settings). */
export function featureOptions(pinnedSettings: readonly string[]): PickOption[] {
  return [
    ...NODE_FEATURES.map((f) => ({ id: f, label: f, group: "Node features" })),
    ...uniqueSorted(pinnedSettings)
      .filter((k) => !NODE_FEATURES.includes(k))
      .map((k) => ({ id: k, label: k, group: "Settings your organization pins" })),
  ];
}

/** componentOptions: the governable node components of this build
 *  (GET /api/components), grouped by class; core components always run, so
 *  they are not offered. */
export function componentOptions(
  components: readonly { id: string; class: string; title: string; compiled?: boolean }[],
): PickOption[] {
  return components
    .filter((c) => c.id && c.class !== "core")
    .slice()
    .sort((a, b) => a.class.localeCompare(b.class) || a.title.localeCompare(b.title))
    .map((c) => ({
      id: c.id,
      label: c.title || c.id,
      group: c.class,
      disabledReason: c.compiled === false ? "Not in this build" : undefined,
    }));
}

/** npmPackagesFromCommands: the npm packages the configured MCP servers run
 *  (GET /api/guard/mcp `command`, e.g. "npx -y @scope/server-x /path"): the
 *  first non-flag argument after npx / bunx / pnpm dlx / yarn dlx, with any
 *  version suffix dropped. A command that runs no package yields nothing. */
export function npmPackagesFromCommands(commands: readonly (string | undefined)[]): string[] {
  const out: string[] = [];
  for (const cmd of commands) {
    const toks = (cmd ?? "").trim().split(/\s+/).filter(Boolean);
    let i = 0;
    const bin = (toks[0] ?? "").replace(/^.*[\\/]/, "").replace(/\.(cmd|exe)$/i, "");
    if (bin === "npx" || bin === "bunx") i = 1;
    else if ((bin === "pnpm" || bin === "yarn") && toks[1] === "dlx") i = 2;
    else continue;
    while (i < toks.length && toks[i].startsWith("-")) {
      // "--package <name>" names the package outright.
      if ((toks[i] === "--package" || toks[i] === "-p") && toks[i + 1]) {
        i += 1;
        break;
      }
      i += 1;
    }
    const name = (toks[i] ?? "").replace(/^(@[^/@]+\/[^@]+|[^@]+)@.*$/, "$1");
    if (/^(@[a-z0-9][\w.-]*\/)?[a-z0-9][\w.-]*$/.test(name)) out.push(name);
  }
  return uniqueSorted(out);
}

/** projectFileOptions: files the sessions in one project read or edited
 *  (GET /api/actions rows), as project-relative paths, most touched first. */
export function projectFileOptions(
  rows: readonly { action_type: string; target: string }[],
  root: string,
): PickOption[] {
  const base = root.replace(/[\\/]+$/, "").replace(/\\/g, "/");
  const counts = new Map<string, number>();
  for (const r of rows) {
    if (!/^(read_file|edit_file|write_file)$/.test(r.action_type) || !r.target) continue;
    const t = r.target.replace(/\\/g, "/");
    if (!base || !t.startsWith(base + "/")) continue;
    const rel = t.slice(base.length + 1);
    if (rel) counts.set(rel, (counts.get(rel) ?? 0) + 1);
  }
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([rel, n]) => ({ id: rel, label: rel, sublabel: `touched ${n} time${n === 1 ? "" : "s"}` }));
}

/** isModelSetting: a setting path whose last segment names a model
 *  ("model", "small_model", "model.default", "modelName"). */
export function isModelSetting(path: string): boolean {
  const last = path.split(/[.\/]/).filter(Boolean).pop() ?? "";
  return /(^|_|-)model(_?name|_?id)?$/i.test(last) || /^model$/i.test(last) || /Model$/.test(last);
}

/** settingValueOptions: the values a string setting already takes somewhere
 *  (its default, every layer that sets it, every tool that sets the common
 *  key), offered as picks; models are appended when the setting names one. */
export function settingValueOptions(values: readonly unknown[], models: readonly PickOption[] = []): PickOption[] {
  const seen = new Set<string>();
  const out: PickOption[] = [];
  for (const v of values) {
    if (typeof v !== "string" || !v.trim() || seen.has(v)) continue;
    seen.add(v);
    out.push({ id: v, label: v, group: "Already used" });
  }
  for (const m of models) {
    if (seen.has(m.id)) continue;
    seen.add(m.id);
    out.push(m);
  }
  return out;
}

/** guardRuleOptions: the guard rule catalog grouped by category. */
export function guardRuleOptions(
  catalog: readonly { id: string; category: string; severity: string; doc: string }[],
): PickOption[] {
  return catalog.map((r) => ({
    id: r.id,
    label: `${r.id} ${r.doc}`,
    sublabel: r.severity,
    group: r.category,
  }));
}

/** contentClassOptions: the values an egress `content_class` matcher can
 *  match (internal/obs/egress contentClassMatches): a criterion id, a
 *  severity, or "<criterion>/<severity>". */
export function contentClassOptions(criteria: readonly string[], severities: readonly string[]): PickOption[] {
  const sev = severities.filter(Boolean);
  const crit = uniqueSorted(criteria);
  return [
    ...crit.map((c) => ({ id: c, label: c, group: "Criterion" })),
    ...sev.map((s) => ({ id: s, label: s, group: "Severity" })),
    ...crit.flatMap((c) => sev.map((s) => ({ id: `${c}/${s}`, label: `${c}/${s}`, group: "Criterion and severity" }))),
  ];
}

function baseName(p: string): string {
  const parts = p.replace(/[\\/]+$/, "").split(/[\\/]/);
  return parts[parts.length - 1] || p;
}
