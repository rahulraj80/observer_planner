import { test } from "node:test";
import assert from "node:assert/strict";
import { ORG_NAME_FALLBACK, orgDisplayLabel, orgDisplayLabelSentence } from "./orgName.ts";

// The name is always a NAME: the first real candidate (the governance notice's
// display name, the enrolment's org name, the daemon's org_display), never an
// org id, else the one fallback. One row per rule.
const ORG_LABEL_ROWS: [string, (string | null | undefined)[], string][] = [
  ["notice display name wins", ["Acme Corp", "acme.example"], "Acme Corp"],
  ["enrolment name when no notice name", [undefined, "acme.example"], "acme.example"],
  ["daemon org_display (server host) is a name", ["org.acme.test:8443"], "org.acme.test:8443"],
  ["org id is never shown", [undefined, "07d3f6e5dcd736effa3c58dc818adb8a"], "your organization"],
  ["id-shaped display name skipped for the real name", ["07D3F6E5DCD736EFFA3C58DC818ADB8A", "Acme"], "Acme"],
  ["names are trimmed", ["  Acme  "], "Acme"],
  ["blank falls back", ["  ", ""], "your organization"],
  ["null falls back", [null], "your organization"],
  ["nothing at all falls back", [], "your organization"],
];
for (const [name, cands, want] of ORG_LABEL_ROWS) {
  test(`orgDisplayLabel: ${name}`, () => {
    assert.equal(orgDisplayLabel(...cands), want);
  });
}

test("the fallback is the one US-spelled phrase", () => {
  assert.equal(ORG_NAME_FALLBACK, "your organization");
});

test("orgDisplayLabelSentence capitalises only the fallback", () => {
  assert.equal(orgDisplayLabelSentence(undefined, "07d3f6e5dcd736effa3c58dc818adb8a"), "Your organization");
  assert.equal(orgDisplayLabelSentence("acme.example"), "acme.example");
});
