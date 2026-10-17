import assert from "node:assert/strict";
import test from "node:test";

import {
  buildVerdict,
  errorMachineSuffix,
  machineName,
  parseRemoteFragment,
  pinHost,
  remoteBuildLine,
  remoteDashboardURL,
  remoteFragment,
} from "./remoteIdentity.ts";

test("remoteFragment round-trips through parseRemoteFragment (unicode safe)", () => {
  const id = { label: "Dev box é", target: "azureuser@20.102.78.154", localVersion: "1.35.0-rc.13", localSchema: 158 };
  const frag = remoteFragment(id);
  assert.match(frag, /^#sb-remote=[A-Za-z0-9_-]+$/);
  assert.deepEqual(parseRemoteFragment(frag), id);
});

test("parseRemoteFragment ignores every hash that is not the switcher's", () => {
  for (const h of ["", "#", "#launch-policy", "#sb-remote=", "#sb-remote=!!!", "#sb-remote=e30"]) {
    assert.equal(parseRemoteFragment(h), null, h);
  }
});

test("pinHost pins the first hostname and drops an identity pinned to another machine", () => {
  const id = { label: "sb-devbox", target: "u@h" };
  const pinned = pinHost(id, "devbox-vm");
  assert.equal(pinned?.hostName, "devbox-vm");
  assert.equal(pinHost(pinned!, "devbox-vm"), pinned);
  assert.equal(pinHost(pinned!, "someone-else"), null, "a reused forward port must not inherit the label");
  assert.equal(pinHost(id, undefined), id);
});

test("machineName adds the hostname only when it says something new", () => {
  assert.equal(machineName({ label: "sb-devbox", target: "", hostName: "devbox-vm" }), "sb-devbox (devbox-vm)");
  assert.equal(machineName({ label: "node1", target: "", hostName: "node1" }), "node1");
  assert.equal(machineName({ label: "node1", target: "" }), "node1");
});

test("buildVerdict flags an older remote release or schema, never a guess", () => {
  const cases: [string, string | undefined, number | undefined, string | undefined, number | undefined, boolean][] = [
    // D-325: sb-devbox on 1.33 / schema 121 viewed from a 1.35 / 158 node.
    ["older release and schema", "1.33.0+org-v65.4d83358d3", 121, "1.35.0-rc.13+demo.946c485e0", 158, true],
    ["older schema only", "1.35.0", 150, "1.35.0", 158, true],
    ["same build", "1.35.0", 158, "1.35.0", 158, false],
    ["newer remote", "1.36.0", 160, "1.35.0", 158, false],
    ["pre-release of the same core is not older", "1.35.0-rc.13", 158, "1.35.0", 158, false],
    ["unknown remote", undefined, undefined, "1.35.0", 158, false],
    ["dev build", "dev", 158, "1.35.0", 158, false],
  ];
  for (const [label, rv, rs, lv, ls, want] of cases) {
    const v = buildVerdict(rv, rs, lv, ls);
    assert.equal(v.outdated, want, label);
    if (want) assert.ok(v.reason.length > 0, label);
  }
});

test("remoteDashboardURL carries the identity to the remote tab, and nothing without a forward", () => {
  const row = { label: "sb-devbox", target: "azureuser@devbox:22", url: "http://127.0.0.1:41234/" };
  const url = remoteDashboardURL(row, { version: "1.35.0", schema: 158 });
  assert.ok(url.startsWith("http://127.0.0.1:41234/#sb-remote="));
  assert.deepEqual(parseRemoteFragment(url.slice(url.indexOf("#"))), {
    label: "sb-devbox",
    target: "azureuser@devbox:22",
    localVersion: "1.35.0",
    localSchema: 158,
  });
  assert.equal(remoteDashboardURL({ label: "x", target: "" }, {}), "");
});

test("remoteBuildLine names the remote build and flags an older one (D-325)", () => {
  const local = { version: "1.35.0-rc.13+demo.946c485e0", schema: 158 };
  const stale = remoteBuildLine(
    { label: "sb-devbox", target: "", remote_version: "1.33.0", remote_schema_version: 121, remote_host_name: "devbox-vm" },
    local,
  );
  assert.equal(stale?.text, "Remote build v1.33.0, schema 121, host devbox-vm");
  assert.equal(stale?.outdated, true);
  const same = remoteBuildLine({ label: "n", target: "", remote_version: "1.35.0", remote_schema_version: 158 }, local);
  assert.equal(same?.outdated, false);
  assert.equal(remoteBuildLine({ label: "down", target: "" }, local), null);
});

test("errorMachineSuffix is empty where no identity is stored (a local dashboard, or node)", () => {
  assert.equal(errorMachineSuffix(), "");
});
