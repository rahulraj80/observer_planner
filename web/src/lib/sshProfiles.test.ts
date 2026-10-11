import assert from "node:assert/strict";
import test from "node:test";

import {
  changesBetween,
  changesSentence,
  draftProblem,
  emptyProfile,
  normalize,
  target,
  type SSHProfileDraft,
} from "./sshProfiles.ts";

function p(over: Partial<SSHProfileDraft>): SSHProfileDraft {
  return { ...emptyProfile(), ...over };
}

test("draftProblem: one row per early refusal, null for a good draft", () => {
  const cases: [string, SSHProfileDraft, SSHProfileDraft[], RegExp | null][] = [
    ["good", p({ name: "devbox", host: "devbox.example.test" }), [], null],
    ["missing name", p({ host: "h" }), [], /Give the system a name/],
    ["uppercase name", p({ name: "DevBox", host: "h" }), [], /lowercase/],
    ["duplicate name", p({ name: "a", host: "h" }), [p({ name: "a", host: "x" })], /already named a/],
    ["missing host", p({ name: "a" }), [], /Enter the host/],
    ["dash host (option injection)", p({ name: "a", host: "-oProxyCommand=x" }), [], /dash/],
    ["host with a space", p({ name: "a", host: "a b" }), [], /spaces/],
    ["bad port", p({ name: "a", host: "h", port: 70000 }), [], /between 1 and 65535/],
    ["relative key path", p({ name: "a", host: "h", key_path: "id_rsa" }), [], /absolute path/],
    ["absolute key path", p({ name: "a", host: "h", key_path: "/home/u/.ssh/id_ed25519" }), [], null],
    ["windows key path", p({ name: "a", host: "h", key_path: "C:\\Users\\u\\.ssh\\id" }), [], null],
    ["multi-hop jump", p({ name: "a", host: "h", jump: "j1,j2" }), [], /one jump host/],
  ];
  for (const [label, d, others, want] of cases) {
    const got = draftProblem(d, others);
    if (want === null) assert.equal(got, null, label);
    else assert.match(got ?? "", want, label);
  }
});

test("normalize trims text fields and target renders user@host:port", () => {
  const d = normalize(p({ name: " a ", host: " h.example.test ", user: " dev ", port: 2222 }));
  assert.equal(d.name, "a");
  assert.equal(target(d), "dev@h.example.test:2222");
  assert.equal(target(p({ host: "h", port: 22 })), "h");
});

test("changesBetween and changesSentence summarize a save by name", () => {
  const saved = [p({ name: "a", host: "h1" }), p({ name: "b", host: "h2" })];
  const next = [p({ name: "a", host: "h1-new" }), p({ name: "c", host: "h3" })];
  const c = changesBetween(saved, next);
  assert.deepEqual(c, { added: ["c"], changed: ["a"], removed: ["b"] });
  assert.equal(changesSentence(c), "Saving will add c; change a; remove b.");
  assert.equal(changesSentence({ added: [], changed: [], removed: [] }), "");
  assert.equal(changesSentence({ added: [], changed: [], removed: [] }, true), "Saving will turn SSH terminals on.");
  // Whitespace-only edits are not changes (the server trims).
  assert.deepEqual(changesBetween(saved, [p({ name: "a", host: " h1 " }), saved[1]]), { added: [], changed: [], removed: [] });
});
