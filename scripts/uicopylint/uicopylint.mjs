#!/usr/bin/env node
// uicopylint: the UI copy ratchet (docs/app-design-system.md "Copy rules",
// design review 2026-10-13, lane DS2). It counts internal vocabulary that
// leaks into user-facing text in an app's TSX/TS source:
//
//   snake    snake_case identifiers (fail_closed, api_turns, org_disallow)
//   section  spec section references (the section sign)
//   config   config paths and keys: ~/.observer/..., [policy] style TOML
//            section names, *.toml / *.json / *.yaml file names, dotted
//            lowercase keys (node.governance, share.routing_summary)
//
// "User-facing text" is read from the TypeScript AST, so comments, class
// names, ids, routes and API calls never count:
//
//   - JSX text between tags;
//   - a string or template literal that flows straight into a JSX child
//     ({"..."}, {ok ? "a" : "b"}) or into a JSX attribute whose name reads
//     as copy (title, label, placeholder, aria-label, help, description,
//     note, reason, summary, ...);
//   - a string or template literal assigned to an object property whose key
//     reads as copy (label:, title:, help:, description:, sub:, detail:, ...),
//     which is how vocab tables and column definitions carry their text.
//
// It is a RATCHET, like web/src/lib/dashRatchet.test.ts: the per-root,
// per-category counts in baseline.json may only go down. A change that adds
// a finding fails; a change that removes some lowers the ceiling with
// `--update` in the same commit (which never raises a count).
//
//   node scripts/uicopylint/uicopylint.mjs web2/src          check one root
//   node scripts/uicopylint/uicopylint.mjs --list web2/src   print every finding
//   node scripts/uicopylint/uicopylint.mjs --update          lower the baseline
//
// With no root it checks every baseline root present on disk (web2/ is not
// in the public tree; a missing root is skipped, never a failure). The
// TypeScript compiler is resolved from the app's own node_modules (each
// app is an npm workspace member), so run it after `npm ci`.

import { createRequire } from "node:module";
import { existsSync, readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "..", "..");
const baselinePath = path.join(here, "baseline.json");

const CATEGORIES = ["snake", "section", "config"];

// A copy-bearing name: a JSX attribute or an object key whose value is text
// a person reads. Matched case-insensitively against the whole name.
const COPY_NAME =
  /^(aria-?label|aria-?description|alt|title|subtitle|sub|label|labels|placeholder|help|hint|tooltip|description|desc|detail|details|note|armednote|confirmlabel|tolabel|menulabel|disabledreason|reason|summary|caption|heading|headline|message|body|text|consequence|learnmore|emptytext|emptylabel|emptytitle|emptybody|snippetlabel|freeidlabel|why|blurb|lede|intro|explain|explanation|meaning|tip)$/i;

const SNAKE = /\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b/g;
const SECTION = /§/g;
const CONFIG = [
  /~\/\.?[A-Za-z0-9_.-]+(?:\/[A-Za-z0-9_.-]+)*/g,
  /\[[a-z][a-z0-9_]{2,}(?:\.[a-z0-9_]+)*\](?:\.[a-z][a-z0-9_]*)?/g,
  /\b[\w-]+\.(?:toml|ya?ml|json)\b/g,
  /\b[a-z][a-z0-9]{2,}(?:\.[a-z][a-z0-9_]{2,})+\b/g,
];
// Dotted words that are not config keys: domains and source file names.
// Reverse-DNS ids and example hosts (com.example.tool) are not config keys either.
const DOT_HEAD_OK = new Set(["com", "org", "net", "io", "dev", "www"]);
const DOT_TAIL_OK = new Set(["app", "com", "io", "dev", "org", "net", "ai", "js", "ts", "tsx", "jsx", "md", "html", "css", "png", "svg", "exe", "sh", "zip"]);

function countIn(text) {
  const out = { snake: [], section: [], config: [] };
  for (const m of text.matchAll(SNAKE)) out.snake.push(m[0]);
  for (const m of text.matchAll(SECTION)) out.section.push(m[0]);
  const seen = new Set();
  for (const re of CONFIG) {
    for (const m of text.matchAll(re)) {
      const tok = m[0];
      if (re === CONFIG[3]) {
        const segs = tok.split(".");
        if (DOT_TAIL_OK.has(segs[segs.length - 1]) || DOT_HEAD_OK.has(segs[0]) || segs.includes("example")) continue;
      }
      // One finding per span, even when two patterns overlap.
      const key = `${m.index}`;
      if (seen.has(key)) continue;
      seen.add(key);
      out.config.push(tok);
    }
  }
  return out;
}

function loadTs(root) {
  const app = root.split("/")[0];
  for (const base of [path.join(repo, app, "package.json"), path.join(repo, "package.json")]) {
    try {
      return createRequire(base)("typescript");
    } catch {
      // try the next location
    }
  }
  throw new Error(`uicopylint: typescript is not installed for ${root}; run npm ci in ${app}/ first`);
}

// The help drawer's registry is where technical depth is MEANT to live (a
// key, a path, a table name), so it is not UI copy for this lint.
const HELP_FILE = /^help(Content|Entries|Registry)?\.tsx?$/;

function walk(dir, out) {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name === "dist") continue;
    const p = path.join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (/\.(ts|tsx)$/.test(name) && !/\.(test|gen)\.tsx?$/.test(name) && !name.endsWith(".d.ts") && !HELP_FILE.test(name)) out.push(p);
  }
}

// copyContext walks up from a literal through value-preserving wrappers and
// reports whether the value lands in a user-facing slot.
function copyContext(ts, node) {
  let n = node;
  for (let p = n.parent; p; n = p, p = p.parent) {
    switch (p.kind) {
      case ts.SyntaxKind.ParenthesizedExpression:
      case ts.SyntaxKind.AsExpression:
      case ts.SyntaxKind.SatisfiesExpression:
      case ts.SyntaxKind.TemplateSpan:
      case ts.SyntaxKind.TemplateExpression:
        continue;
      case ts.SyntaxKind.ConditionalExpression:
        if (n === p.condition) return false;
        continue;
      case ts.SyntaxKind.BinaryExpression: {
        const op = p.operatorToken.kind;
        if (op === ts.SyntaxKind.AmpersandAmpersandToken || op === ts.SyntaxKind.BarBarToken || op === ts.SyntaxKind.QuestionQuestionToken || op === ts.SyntaxKind.PlusToken) {
          if (op === ts.SyntaxKind.AmpersandAmpersandToken && n === p.left) return false;
          continue;
        }
        return false;
      }
      case ts.SyntaxKind.JsxExpression: {
        const host = p.parent;
        if (!host) return false;
        if (host.kind === ts.SyntaxKind.JsxAttribute) return COPY_NAME.test(host.name.getText());
        return host.kind === ts.SyntaxKind.JsxElement || host.kind === ts.SyntaxKind.JsxFragment;
      }
      case ts.SyntaxKind.JsxAttribute:
        return COPY_NAME.test(p.name.getText());
      case ts.SyntaxKind.PropertyAssignment: {
        if (n !== p.initializer) return false;
        const nm = p.name;
        const key = nm.kind === ts.SyntaxKind.Identifier || nm.kind === ts.SyntaxKind.StringLiteral ? nm.text : "";
        return COPY_NAME.test(key);
      }
      default:
        return false;
    }
  }
  return false;
}

function literalText(ts, node) {
  if (node.kind === ts.SyntaxKind.StringLiteral || node.kind === ts.SyntaxKind.NoSubstitutionTemplateLiteral) return node.text;
  if (node.kind === ts.SyntaxKind.TemplateExpression) {
    return [node.head.text, ...node.templateSpans.map((s) => s.literal.text)].join(" ");
  }
  return null;
}

function scanFile(ts, file) {
  const src = readFileSync(file, "utf8");
  const sf = ts.createSourceFile(file, src, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const findings = [];
  const add = (node, text) => {
    const c = countIn(text);
    for (const cat of CATEGORIES) {
      for (const tok of c[cat]) {
        const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf));
        findings.push({ cat, tok, line: line + 1 });
      }
    }
  };
  const visit = (node) => {
    if (node.kind === ts.SyntaxKind.JsxText) {
      const t = node.getText(sf);
      if (t.trim()) add(node, t);
    } else if (node.kind === ts.SyntaxKind.StringLiteral || node.kind === ts.SyntaxKind.NoSubstitutionTemplateLiteral || node.kind === ts.SyntaxKind.TemplateExpression) {
      // A string that is itself a JSX attribute value parents to the attribute.
      if (copyContext(ts, node)) {
        const t = literalText(ts, node);
        if (t) add(node, t);
      }
      if (node.kind === ts.SyntaxKind.TemplateExpression) {
        // Visit only the substitutions, not the literal parts again.
        for (const s of node.templateSpans) visit(s.expression);
        return;
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return findings;
}

function scanRoot(root) {
  const ts = loadTs(root);
  const files = [];
  walk(path.join(repo, root), files);
  const counts = { snake: 0, section: 0, config: 0 };
  const per = new Map();
  const all = [];
  for (const f of files.sort()) {
    const rel = path.relative(repo, f).split(path.sep).join("/");
    for (const x of scanFile(ts, f)) {
      counts[x.cat]++;
      const k = `${x.cat}\t${rel}`;
      per.set(k, (per.get(k) ?? 0) + 1);
      all.push({ ...x, file: rel });
    }
  }
  return { counts, per, all };
}

function main(argv) {
  const update = argv.includes("--update");
  const list = argv.includes("--list");
  const roots = argv.filter((a) => !a.startsWith("--"));
  const baseline = JSON.parse(readFileSync(baselinePath, "utf8"));
  const targets = (roots.length ? roots : Object.keys(baseline.roots)).map((r) => r.replace(/\/+$/, ""));
  let failed = false;
  for (const root of targets) {
    if (!existsSync(path.join(repo, root))) {
      console.log(`uicopylint: ${root}: not in this tree, skipped`);
      continue;
    }
    const { counts, per, all } = scanRoot(root);
    const ceil = baseline.roots[root];
    if (list) for (const x of all) console.log(`${x.file}:${x.line}: ${x.cat}: ${x.tok}`);
    if (!ceil) {
      console.log(`uicopylint: ${root}: no baseline; current ${JSON.stringify(counts)}`);
      if (update) baseline.roots[root] = counts;
      continue;
    }
    for (const cat of CATEGORIES) {
      const have = counts[cat];
      const max = ceil[cat] ?? 0;
      if (have > max) {
        failed = true;
        const top = [...per.entries()]
          .filter(([k]) => k.startsWith(`${cat}\t`))
          .sort((a, b) => b[1] - a[1])
          .slice(0, 5)
          .map(([k, c]) => `${k.split("\t")[1]} ${c}`)
          .join(", ");
        console.log(
          `uicopylint: ${root}: ${cat} ${have} > ceiling ${max}. Keep internal names out of UI text (docs/app-design-system.md "Copy rules"): use the vocab label, or move the detail into a HelpTooltip "Technical details". Top files: ${top}. Run with --list to see each finding.`,
        );
      } else if (update && have < max) {
        ceil[cat] = have;
      }
    }
    console.log(`uicopylint: ${root}: snake ${counts.snake}/${ceil.snake}, section ${counts.section}/${ceil.section}, config ${counts.config}/${ceil.config}`);
  }
  if (update) writeFileSync(baselinePath, JSON.stringify(baseline, null, 2) + "\n");
  if (failed) process.exit(1);
}

main(process.argv.slice(2));
