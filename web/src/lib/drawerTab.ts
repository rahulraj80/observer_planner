// drawerTab.ts - the URL rules of the session drawer's tab (SessionDetailPanel).
//
// The platform tabs plan (docs/audits/platform-ia-tabs-audit-2026-10-02.md
// §1.3) gives PAGE tabs `?tab=` and DRAWER tabs `?dtab=`. The session drawer
// used `?tab=` until 2026-10-02, which collided with the page tabs that
// node pages grew (a closed drawer deleted `?tab=`, i.e. the page's own tab).
// It now owns `?dtab=` (the project drawer's param too) and never touches
// `?tab=`, so a page with page tabs and an open drawer keeps both.
//
// Old links still work: `/sessions?session=<id>&tab=messages` (the shape the
// drawer's deep links had, still in bookmarks and in the docs) is READ once
// and rewritten to `dtab`. The legacy read is gated on the `session` param,
// because that is the only shape such a link ever had; on any other URL a
// `tab` value belongs to the page.
//
// Pure: no React, no router. Importable under `node --test`.

/** The drawer's own param. */
export const DRAWER_TAB_PARAM = "dtab";

/** The param the drawer used before 2026-10-02 (read for old links only). */
export const LEGACY_DRAWER_TAB_PARAM = "tab";

/** legacyDrawerTab: the drawer tab an old `?session=..&tab=..` link names, or
 *  null (no session param, a dtab already present, or not a drawer tab). */
export function legacyDrawerTab<T extends string>(params: URLSearchParams, ids: readonly T[]): T | null {
  if (!params.has("session") || params.has(DRAWER_TAB_PARAM)) return null;
  const v = params.get(LEGACY_DRAWER_TAB_PARAM);
  return v != null && (ids as readonly string[]).includes(v) ? (v as T) : null;
}

/**
 * drawerTabFromParams is the tab the URL names for the drawer: `dtab` when it
 * is a drawer tab, else an old link's `tab` (legacyDrawerTab), else null (the
 * caller's default).
 */
export function drawerTabFromParams<T extends string>(params: URLSearchParams, ids: readonly T[]): T | null {
  const v = params.get(DRAWER_TAB_PARAM);
  if (v != null && (ids as readonly string[]).includes(v)) return v as T;
  return legacyDrawerTab(params, ids);
}

/**
 * migrateLegacyDrawerTab rewrites an old link's `tab` into `dtab` (dropping
 * `tab`; the default tab is written as dtab's absence), or returns null when
 * there is nothing to migrate. Run once while the drawer is open, as a
 * replace navigation.
 */
export function migrateLegacyDrawerTab<T extends string>(
  params: URLSearchParams,
  ids: readonly T[],
  defaultId: T,
): URLSearchParams | null {
  const legacy = legacyDrawerTab(params, ids);
  if (legacy == null) return null;
  const next = new URLSearchParams(params);
  next.delete(LEGACY_DRAWER_TAB_PARAM);
  if (legacy !== defaultId) next.set(DRAWER_TAB_PARAM, legacy);
  return next;
}

/** writeDrawerTab: the params with the drawer on `id` (the default is dtab's
 *  absence); a page's own `tab` is never touched. */
export function writeDrawerTab<T extends string>(params: URLSearchParams, id: T, defaultId: T): URLSearchParams {
  const next = new URLSearchParams(params);
  if (id === defaultId) next.delete(DRAWER_TAB_PARAM);
  else next.set(DRAWER_TAB_PARAM, id);
  return next;
}

/** clearDrawerTab: the params without `dtab` when the drawer closes, or null
 *  when there is none (no navigation needed). The page's `tab` stays. */
export function clearDrawerTab(params: URLSearchParams): URLSearchParams | null {
  if (!params.has(DRAWER_TAB_PARAM)) return null;
  const next = new URLSearchParams(params);
  next.delete(DRAWER_TAB_PARAM);
  return next;
}
