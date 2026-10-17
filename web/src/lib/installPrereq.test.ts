import assert from "node:assert/strict";
import test from "node:test";

import { installPrereqRemedyFor } from "./installPrereq.ts";
import type { ToolPreflight } from "./types.ts";

const base = { tool: "kiro-cli", verdict: "not_found", can_install: false } as ToolPreflight;

test("no preflight or nothing missing renders nothing", () => {
  assert.equal(installPrereqRemedyFor(null), null);
  assert.equal(installPrereqRemedyFor(base), null);
  assert.equal(installPrereqRemedyFor({ ...base, install_missing_prereqs: [] }), null);
});

test("the server's sentence is shown verbatim", () => {
  const remedy =
    "The installer needs unzip, which is not installed on this machine. Install it with: sudo apt-get install -y unzip (Debian/Ubuntu), then press Re-check.";
  assert.equal(
    installPrereqRemedyFor({ ...base, install_missing_prereqs: ["unzip"], install_prereq_remedy: remedy }),
    remedy,
  );
});

test("a list without a sentence still names the programs", () => {
  assert.equal(
    installPrereqRemedyFor({ ...base, install_missing_prereqs: ["tar", "sha256sum|shasum"] }),
    "The installer needs tar and sha256sum or shasum, which are not installed on this machine. Install them with your system package manager, then press Re-check.",
  );
});
