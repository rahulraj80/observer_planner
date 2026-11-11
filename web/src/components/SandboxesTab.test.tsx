// Render tests of the Terminals > Sandboxes tab's views (react-dom/server
// under node's test runner, scripts/test-components.mjs). The views take
// their data as props.

import { test } from "node:test";
import assert from "node:assert/strict";
import { renderToStaticMarkup } from "react-dom/server";
import { AdvisorView, PlacesView, RunsView, type SandholdPlace } from "@/components/SandboxesTab";
import type { SandboxAvailability } from "@/lib/types";

const consoleError = console.error;
console.error = (...args: unknown[]) => {
  if (typeof args[0] === "string" && args[0].includes("useLayoutEffect does nothing on the server")) return;
  consoleError(...args);
};

const probe = {
  available: true,
  default_on: false,
  backends: [
    { id: "bwrap", label: "bubblewrap", class: "process", location: "local", maturity: "shipped", available: true, enabled: true, beta: false, component: "terminals.sandbox" },
    { id: "oci-runc", label: "Docker or Podman", class: "container", location: "local", maturity: "preview", available: false, verdict: "binary_missing", reason: "docker is not installed", enabled: true, beta: true, component: "terminals.sandbox.oci" },
    { id: "sandhold", label: "SandHold", class: "container", location: "remote", maturity: "preview", available: false, enabled: false, beta: true, component: "terminals.sandbox.sandhold" },
    { id: "future", label: "Planned row", class: "hw_vm", location: "local", maturity: "planned", available: false, enabled: false, beta: true, component: "x" },
  ],
} as SandboxAvailability;

const connected: SandholdPlace = {
  connected: true,
  node: "https://sandhold.example.com",
  user: "alice",
  org: "acme",
  expires_at: "2026-12-11T00:00:00Z",
  key_store: "this machine's keychain (service sbo-sandbox-sandhold-v1)",
  offers: [
    { tier: "shared", title: "Shared", networks: ["none", "limited"] },
    { tier: "vault", title: "Vault", means: "its own kernel in a virtual machine", networks: ["none", "limited", "open"] },
  ],
  component_active: true,
  connect_command: "observer sandbox connect <node-url>",
  confirm_token: "t",
};

test("places: this machine's rows in SandHold's words, the node, and how to connect another", () => {
  const html = renderToStaticMarkup(<PlacesView probe={probe} place={connected} />);
  assert.match(html, /bubblewrap/);
  assert.match(html, />Shared</);
  assert.match(html, /proven/);
  assert.match(html, /Docker or Podman/);
  assert.match(html, /beta/);
  assert.match(html, /docker is not installed/);
  assert.doesNotMatch(html, /data-backend="sandhold"/, "the remote row is the node card's");
  assert.doesNotMatch(html, /Planned row/);
  assert.match(html, /https:\/\/sandhold\.example\.com/);
  assert.match(html, /signed in as <span[^>]*>alice<\/span>/);
  assert.match(html, /network None, Limited, Open/);
  assert.match(html, /observer sandbox connect &lt;node-url&gt;/);
  assert.match(html, /observer sandbox disconnect/);
  assert.doesNotMatch(html, /Runs cannot go there yet/);
});

test("places: no node, a node whose component is off, and a build without SandHold", () => {
  const none = renderToStaticMarkup(<PlacesView probe={probe} place={{ ...connected, connected: false, offers: [] }} />);
  assert.match(none, /No node is connected/);
  const off = renderToStaticMarkup(<PlacesView probe={probe} place={{ ...connected, component_active: false }} />);
  assert.match(off, /Runs cannot go there yet/);
  const missing = renderToStaticMarkup(<PlacesView probe={probe} place={null} placeMissing />);
  assert.match(missing, /not part of this build/);
});

const noop = () => {};
const advisor = (over: Partial<Parameters<typeof AdvisorView>[0]>) =>
  renderToStaticMarkup(
    <AdvisorView
      place={connected}
      questions={null}
      answers={{ runs: "", trust: "", reach: [] }}
      extraHosts=""
      advice={null}
      busy=""
      err={null}
      onAsk={noop}
      onAnswer={noop}
      onExtraHosts={noop}
      onAdvise={noop}
      {...over}
    />,
  );

test("advisor: says how to connect when no node is connected", () => {
  const html = advisor({ place: { ...connected, connected: false } });
  assert.match(html, /observer sandbox connect &lt;node-url&gt;/);
  assert.doesNotMatch(html, /Ask the node its questions/);
});

test("advisor: the questions, the advice and the node's refusal", () => {
  assert.match(advisor({}), /Ask the node its questions/);
  const questions = [
    { id: "runs", ask: "What will run in it?", choices: [{ id: "agent", label: "An AI agent at work" }] },
    { id: "trust", ask: "How much do you trust the code?", choices: [{ id: "generated", label: "Code an AI wrote" }] },
    { id: "reach", ask: "What must it reach on the network?", choices: [{ id: "npm", label: "JavaScript packages (npm)", hosts: ["registry.npmjs.org"] }] },
  ];
  const asked = advisor({ questions, answers: { runs: "agent", trust: "", reach: ["npm"] } });
  assert.match(asked, /1\. What will run in it\?/);
  assert.match(asked, /aria-pressed="true"[^>]*>An AI agent at work/);
  assert.match(asked, /title="registry\.npmjs\.org"/);
  assert.match(asked, /Answer the first two questions/);
  const advised = advisor({
    questions,
    answers: { runs: "agent", trust: "generated", reach: [] },
    advice: {
      isolation: "shielded",
      isolationMeans: "a hardened container on the host's kernel",
      network: "limited",
      command: "sandhold create --isolation shielded --allow registry.npmjs.org",
      why: [{ about: "isolation", choice: "Shielded: a hardened container", because: "Nobody has checked this code." }],
      limits: ["Shares the host's kernel"],
    },
  });
  assert.match(advised, /Advised: Shielded, network Limited/);
  assert.match(advised, /Nobody has checked this code\./);
  assert.match(advised, /What it gives up: <\/span><span[^>]*>Shares the host&#x27;s kernel/);
  assert.match(advised, /sandhold create --isolation shielded/);
  const refused = advisor({ err: { error: "trust \"maybe\" is not one of the answers", fix: "say own, generated or unknown" } });
  assert.match(refused, /role="alert"/);
  assert.match(refused, /To fix: say own, generated or unknown/);
});

test("recent runs: backend, isolation, network, status, duration, findings", () => {
  const html = renderToStaticMarkup(
    <RunsView
      runs={[
        { run_id: "run-node-1", tool: "claude-code", kind: "fresh", launched_at: "2026-11-11T10:00:00Z", state: "ended", exit_code: 0, duration_seconds: 840, backend: "sandhold", isolation_class: "container", egress_tier: "proxy_only", refused: 2 },
        { run_id: "run-local-1", tool: "claude-code", kind: "fresh", launched_at: "2026-11-11T09:00:00Z", state: "running", backend: "bwrap", isolation_class: "process", egress_tier: "none" },
      ]}
    />,
  );
  assert.match(html, /SandHold node/);
  assert.match(html, /This machine \(bwrap\)/);
  assert.match(html, />Shielded</);
  assert.match(html, />Limited</);
  assert.match(html, /ended, exit 0/);
  assert.match(html, /14 min/);
  assert.match(html, /2 connections refused/);
  const local = html.slice(html.indexOf('data-run="run-local-1"'));
  assert.match(local, /<td class="px-2 py-1\.5 "><\/td>/, "a run with nothing on record shows nothing");
  assert.match(renderToStaticMarkup(<RunsView runs={[]} />), /No sandboxed runs yet/);
});
