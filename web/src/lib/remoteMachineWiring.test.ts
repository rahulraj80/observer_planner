import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// T-425 wiring pins (source scans; the logic itself is pinned in
// remoteIdentity.test.ts). D-325: a remote machine's dashboard opened through
// the instance switcher looked exactly like the local one, so a stale remote's
// correct refusal was read as a fault on the operator's own node.

const read = (rel: string) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("the top bar carries the one remote-machine slot", () => {
  const src = read("../components/TopBar.tsx");
  assert.match(src, /import \{ RemoteMachineChip \} from "@\/components\/RemoteMachineChip";/);
  assert.equal(src.match(/<RemoteMachineChip \/>/g)?.length, 1);
});

test("the switcher opens the remote tab with its identity and shows the remote build", () => {
  const src = read("../components/InstanceSwitcher.tsx");
  assert.match(src, /window\.open\(remoteDashboardURL\(/, "Connect must open the identity-carrying URL");
  assert.match(src, /href=\{remoteDashboardURL\(/, "Open must link the identity-carrying URL");
  assert.doesNotMatch(src, /window\.open\(info\.url/, "a bare forward URL loses which machine it is");
  assert.match(src, /remoteBuildLine\(/);
});

test("every API error on a remote view names the machine", () => {
  const src = read("./api.ts");
  assert.match(src, /\$\{errorMachineSuffix\(\)\}/);
});
