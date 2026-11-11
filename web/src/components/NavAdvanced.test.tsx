// Render tests of the sidebar's Advanced heading (lib/navLayout.ts): folded
// it says how many pages it holds; unfolded it offers the choice of which
// pages stay in the main list; on the icon rail its words give way to the
// chevron and a tooltip.

import { test } from "node:test";
import assert from "node:assert/strict";
import { renderToStaticMarkup } from "react-dom/server";
import { NavAdvancedToggle } from "@/components/NavAdvanced";
import { NAV_GROUPS } from "@/lib/nav";
import { layoutNav } from "@/lib/navLayout";

const noop = () => {};

test("folded, the heading counts the pages it holds", () => {
  const l = layoutNav(NAV_GROUPS);
  const html = renderToStaticMarkup(
    <NavAdvancedToggle open={false} onToggle={noop} folded={l.folded} choices={l.choices} onPin={noop} />,
  );
  assert.match(html, /aria-expanded="false"/);
  assert.match(html, />Advanced</);
  assert.ok(html.includes(`aria-label="${l.folded} more pages"`), html);
  assert.doesNotMatch(html, /Choose which pages stay in the main list/);
});

test("one page is one page", () => {
  const html = renderToStaticMarkup(
    <NavAdvancedToggle open={false} onToggle={noop} folded={1} choices={[]} onPin={noop} />,
  );
  assert.match(html, /aria-label="1 more page"/);
});

test("unfolded, the heading offers the choice of main-list pages", () => {
  const l = layoutNav(NAV_GROUPS, { open: true });
  const html = renderToStaticMarkup(
    <NavAdvancedToggle open onToggle={noop} folded={l.folded} choices={l.choices} onPin={noop} />,
  );
  assert.match(html, /aria-expanded="true"/);
  assert.match(html, /aria-label="Choose which pages stay in the main list"/);
  // The count belongs to the folded heading only.
  assert.doesNotMatch(html, /more pages/);
  // The list of choices stays closed until asked for.
  assert.doesNotMatch(html, /type="checkbox"/);
});

test("on the icon rail the words are hidden at desktop width", () => {
  const html = renderToStaticMarkup(
    <NavAdvancedToggle open={false} onToggle={noop} folded={3} choices={[]} onPin={noop} collapsed />,
  );
  assert.match(html, /lg:justify-center/);
  assert.match(html, /lg:hidden[^"]*"[^>]*>Advanced</);
});
