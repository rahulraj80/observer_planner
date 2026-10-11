import assert from "node:assert/strict";
import test from "node:test";

import { INSTALL_STATUS_COPY, installRecordView, uninstallErrorMessage } from "./toolUninstall.ts";
import type { ToolInstallRecord } from "./types.ts";

const base: ToolInstallRecord = {
  id: 1,
  tool: "codex",
  channel: "npm",
  package: "@openai/codex",
  command: "npm install --global --prefix /home/u/.local @openai/codex",
  status: "installed",
  installed_at: "2026-10-02T09:00:00Z",
  can_uninstall: true,
  uninstall_command: "npm uninstall --global --prefix /home/u/.local @openai/codex",
  can_forget: true,
};

test("installed npm record offers Uninstall with a note naming package + command", () => {
  const v = installRecordView(base);
  assert.match(v.headline, /^Installed from this dashboard on /);
  assert.equal(v.showUninstall, true);
  assert.equal(
    v.confirmNote,
    "Removes @openai/codex by running: npm uninstall --global --prefix /home/u/.local @openai/codex",
  );
  assert.equal(v.guidance, null);
  assert.equal(v.showForget, true);
  assert.equal(v.tone, "ok");
});

test("server says no -> no Uninstall, even with a command", () => {
  const v = installRecordView({ ...base, can_uninstall: false });
  assert.equal(v.showUninstall, false);
  assert.equal(v.confirmNote, null);
});

test("vendor script: guidance, no Uninstall, Forget offered", () => {
  const v = installRecordView({
    ...base,
    channel: "script",
    package: undefined,
    command: "curl -fsSL https://claude.ai/install.sh | bash",
    can_uninstall: false,
    uninstall_command: undefined,
    uninstall_guidance: "Remove it with the vendor's uninstaller.",
  });
  assert.equal(v.showUninstall, false);
  assert.equal(v.guidance, "Remove it with the vendor's uninstaller.");
  assert.equal(v.showForget, true);
  assert.match(v.detail ?? "", /vendor's install script/);
});

test("uninstalled with a still-resolving binary says so", () => {
  const v = installRecordView({ ...base, status: "uninstalled", can_uninstall: false, resolved_bin: "/usr/bin/codex" });
  assert.match(v.headline, /since uninstalled/);
  assert.match(v.note ?? "", /\/usr\/bin\/codex/);
  assert.equal(v.guidance, null);
});

test("failed install carries a readable remedy with the exit code, never a dead end", () => {
  const v = installRecordView({
    ...base,
    tool: "goose",
    channel: "script",
    package: undefined,
    command: "curl -fsSL https://github.com/aaif-goose/goose/releases/download/stable/download_cli.sh | bash",
    status: "install_failed",
    exit_code: 1,
    can_uninstall: false,
    uninstall_command: undefined,
  });
  assert.equal(v.tone, "warn");
  assert.match(v.headline, /did not finish successfully/);
  assert.match(v.note ?? "", /^The installer exited with code 1\./);
  assert.match(v.note ?? "", /install terminal's output/);
  assert.match(v.note ?? "", /start the install again/);
  assert.equal(v.showUninstall, false);
  assert.doesNotMatch(v.note ?? "", /—/, "no em-dashes in product copy");
});

test("failed install without an exit code still says what to do", () => {
  const v = installRecordView({ ...base, status: "install_failed", exit_code: undefined, can_uninstall: false });
  assert.match(v.note ?? "", /^The installer failed\./);
});

test("every lifecycle status has copy, and an unknown status still renders", () => {
  for (const s of [
    "installing",
    "installed",
    "installed_not_found",
    "install_failed",
    "install_interrupted",
    "uninstalling",
    "uninstalled",
    "uninstall_failed",
    "uninstall_interrupted",
  ]) {
    assert.ok(INSTALL_STATUS_COPY[s], s);
  }
  const v = installRecordView({ ...base, status: "mystery" });
  assert.match(v.headline, /status: mystery/);
});

test("uninstallErrorMessage maps statuses and keeps the server's 409 sentence", () => {
  assert.match(uninstallErrorMessage(403, "forbidden: missing or mismatched confirm token"), /stale/);
  // T-489: the remedy names the dashboard control, not a config.toml key.
  assert.match(uninstallErrorMessage(403, "disabled"), /Allow guided install and uninstall" in Terminals > Settings/);
  assert.match(uninstallErrorMessage(404, ""), /no record/);
  assert.equal(uninstallErrorMessage(409, "the install record changed"), "the install record changed");
});

test("exit 0 with no binary is its own warn outcome with a remedy, uninstall kept (T-034j)", () => {
  const v = installRecordView({ ...base, status: "installed_not_found" });
  assert.equal(v.tone, "warn");
  assert.match(v.headline, /not found afterwards/);
  assert.match(v.note ?? "", /not on PATH/);
  assert.equal(v.showUninstall, true);
  assert.equal(v.waitingForInput, false);
  assert.doesNotMatch(v.headline + (v.note ?? ""), /[\u2013\u2014]/, "no em or en dashes in product copy");
});

test("a long-running install says it may be waiting for input (T-034j)", () => {
  const v = installRecordView({
    ...base,
    status: "installing",
    can_uninstall: false,
    can_forget: false,
    maybe_waiting_for_input: true,
    terminal_handle: "h-1",
  });
  assert.equal(v.waitingForInput, true);
  assert.match(v.note ?? "", /waiting for input: open its terminal/);
  const young = installRecordView({ ...base, status: "installing", can_uninstall: false, can_forget: false });
  assert.equal(young.waitingForInput, false);
  assert.equal(young.note, null);
});
