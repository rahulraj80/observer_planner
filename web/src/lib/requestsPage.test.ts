// Source pins for the node Requests page's request types (T-437): every type
// the daemon lists is offered, and an "answer" type says the organization
// answers with a note.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const src = readFileSync(new URL("../pages/Requests.tsx", import.meta.url), "utf8");

test("the type picker offers every request type, including something else", () => {
  assert.match(src, /options=\{types\.map\(\(t\) => \(\{ value: t\.id, label: t\.label \}\)\)\}/);
  assert.doesNotMatch(src, /t\.id !== "other"/);
});

test("an answer type tells the developer it is answered with a note", () => {
  assert.match(src, /typeMeta\?\.landing === "answer"/);
  assert.match(src, /answers with a note you will read here/);
});
