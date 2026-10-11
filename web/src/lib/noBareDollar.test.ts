import assert from "node:assert/strict";
import test from "node:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

// -----------------------------------------------------------------------------
// The node dashboard's no-bare-dollar guard (T-204), the twin of web2's
// noBareUsd.test.ts.
//
// Under the token-usage developer cost display (D-013) every node surface
// renders cost through the one formatter seam (shared/lib/format.ts: fmtCost,
// fmtUSD, dollarsShown, maskDollarAmounts) and exports through
// lib/costExport.ts. A literal "$" glued to an interpolation is a site that
// renders a dollar figure while ignoring the display, so this test fails when
// one appears in web/src or shared/ unless it is allow-listed here with a
// reason AND carries a `no-bare-usd:allow` marker at the site. An entry that
// stops matching fails too, so the list cannot rot.
// -----------------------------------------------------------------------------

const HERE = fileURLToPath(new URL(".", import.meta.url));
const WEB_SRC = join(HERE, "..");
const SHARED = join(HERE, "..", "..", "..", "shared");

// The formatter seam itself is the one place allowed to build a dollar string.
const FORMATTERS = new Set(["shared/lib/format.ts"]);

const ALLOW_LIST: Record<string, string> = {
  "web/lib/costSurfaces.ts":
    "savedMilestoneCopy builds the milestone chip only after dollarsShown() returned true; in the token-usage view the milestone does not exist (null).",
  "web/components/sessiondetail/CostTab.tsx":
    "the model-switch forecast picker's input-rate hint ($/M); the whole ForecastWidget renders only when dollarsShown() is true.",
  "web/pages/Settings.tsx":
    "not money: the summary-key note names an environment variable ($ANTHROPIC_API_KEY style), not a dollar figure.",
};

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules") continue;
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (/\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name)) out.push(p);
  }
  return out;
}

function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/[^\n]*/g, "$1");
}

// stripTemplateLiterals drops every template literal (nested ones included,
// e.g. `a${x ? ` b ${y}` : ""}`) so a "${" interpolation is never read as a
// JSX text "$" followed by an expression. A scanner, because a regex cannot
// pair nested backticks.
function stripTemplateLiterals(src: string): string {
  let out = "";
  // Each frame is a template ("t") or an interpolation with its brace depth.
  const stack: { kind: "t" | "e"; depth: number }[] = [];
  for (let i = 0; i < src.length; i++) {
    const c = src[i];
    const top = stack[stack.length - 1];
    if (!top) {
      if (c === "`") {
        stack.push({ kind: "t", depth: 0 });
        out += "``";
      } else out += c;
      continue;
    }
    if (top.kind === "t") {
      if (c === "\\") i++;
      else if (c === "`") stack.pop();
      else if (c === "$" && src[i + 1] === "{") {
        stack.push({ kind: "e", depth: 1 });
        i++;
      }
      continue;
    }
    if (c === "`") stack.push({ kind: "t", depth: 0 });
    else if (c === "{") top.depth++;
    else if (c === "}" && --top.depth === 0) stack.pop();
  }
  return out;
}

// A template literal with a literal "$" before an interpolation renders a
// dollar sign whatever the interpolation is; a JSX text "$" before a
// spend-shaped expression does the same; so does "$" + amount.
const TEMPLATE_DOLLAR = /\$\$\{[^}]*\}/g;
const JSX_DOLLAR = /\$\s*\{[^}]*\}/g;
const CONCAT_DOLLAR = /["']\$["']\s*\+/g;
const SPEND_SHAPED = /cost|usd|spend|price|rate|saved/i;

export function dollarHits(src: string): string[] {
  const out: string[] = [];
  const clean = stripComments(src);
  for (const m of clean.match(TEMPLATE_DOLLAR) ?? []) out.push(m);
  for (const m of clean.match(CONCAT_DOLLAR) ?? []) out.push(m);
  for (const m of stripTemplateLiterals(clean).match(JSX_DOLLAR) ?? []) {
    if (SPEND_SHAPED.test(m)) out.push(m);
  }
  return out;
}

function files(): { rel: string; path: string }[] {
  return [
    ...walk(WEB_SRC).map((p) => ({ rel: "web/" + relative(WEB_SRC, p).split("\\").join("/"), path: p })),
    ...walk(SHARED).map((p) => ({ rel: "shared/" + relative(SHARED, p).split("\\").join("/"), path: p })),
  ];
}

test("no-bare-dollar: no node surface renders a literal $ outside the formatter seam", () => {
  const offenders: string[] = [];
  for (const f of files()) {
    if (FORMATTERS.has(f.rel)) continue;
    const hits = dollarHits(readFileSync(f.path, "utf8"));
    if (hits.length === 0 || f.rel in ALLOW_LIST) continue;
    offenders.push(`${f.rel} (${hits[0]})`);
  }
  assert.deepEqual(
    offenders,
    [],
    `these files render a literal "$" in front of a value, which ignores the developer cost display ` +
      `(T-204). Use fmtCost / fmtUSD / dollarsShown from the formatter seam (or lib/costExport for a ` +
      `download), or add the file to ALLOW_LIST with a reason and a no-bare-usd:allow marker: ${offenders.join(", ")}`,
  );
});

test("no-bare-dollar: every allow-listed file still needs its entry and says why", () => {
  const byRel = new Map(files().map((f) => [f.rel, f.path]));
  for (const [rel, reason] of Object.entries(ALLOW_LIST)) {
    const path = byRel.get(rel);
    assert.ok(path, `${rel} is allow-listed but does not exist`);
    const src = readFileSync(path, "utf8");
    assert.ok(dollarHits(src).length > 0, `${rel} is allow-listed but no longer renders a literal $ - remove the entry`);
    assert.ok(src.includes("no-bare-usd:allow"), `${rel} carries no no-bare-usd:allow marker at the site`);
    assert.ok(reason.length > 40, `${rel}'s entry needs a real reason`);
  }
});

test("no-bare-dollar: the patterns match what they are meant to catch, and only that", () => {
  for (const s of ["`$${v.toFixed(2)}`", "<span>${r.cost_usd}</span>", `"$" + amount`]) {
    assert.ok(dollarHits(s).length > 0, `misses ${s}`);
  }
  for (const s of [
    "`${team.name}`",
    "`budget${pace ? ` on pace for ${money(sc.forecast_usd)}` : \"\"}.`",
    "${sessionId}", "fmtUSD(row.cost_usd)", "`Spend: ${fmtCost(v, t)}`", "/^x$/"]) {
    assert.equal(dollarHits(s).length, 0, `over-matches ${s}`);
  }
});
