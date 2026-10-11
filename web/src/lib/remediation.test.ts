// Pins the Health page's remediation buttons (remediation.ts, T-421 item 3).
import { test } from "node:test";
import assert from "node:assert/strict";
import { parseSettingsDeepLink, remediationButtons } from "./remediation.ts";

test("only in-app links become buttons, one per link and label", () => {
  const out = remediationButtons([
    { action: "init", command: "observer init", label: "Open Connected tools", href: "/settings?section=tools" },
    { action: "init", command: "observer init", label: "Open Connected tools", href: "/settings?section=tools" },
    { action: "bad", label: "Elsewhere", href: "https://example.test/x" },
    { action: "bad2", label: "Protocol-relative", href: "//example.test/x" },
    { action: "codex_trust", label: "See the Codex trust steps", href: "/settings?section=tools&tool=codex&open=setup" },
  ]);
  assert.deepEqual(
    out.map((r) => r.action),
    ["init", "codex_trust"],
  );
  assert.deepEqual(remediationButtons(undefined), []);
});

test("a remediation link's Settings parameters round-trip", () => {
  assert.deepEqual(parseSettingsDeepLink("?open=remove&section=tools&tool=claude-code"), {
    section: "tools",
    open: "remove",
    tool: "claude-code",
  });
  assert.deepEqual(parseSettingsDeepLink("section=tools&open=windows-home&target=codex-windows"), {
    section: "tools",
    open: "windows-home",
    target: "codex-windows",
  });
  assert.equal(parseSettingsDeepLink("?tool=x"), null);
});
