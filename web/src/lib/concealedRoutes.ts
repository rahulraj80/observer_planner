// concealedRoutes: the SPA's ONE shared fetch guard for routes the visibility
// posture hides on this machine (T-401).
//
// GET /api/governance lists `concealed_routes`: the API routes whose READ the
// daemon's governance guard refuses as hidden right now, computed server-side
// from the same function the guard refuses with
// (internal/intelligence/dashboard/governance.go concealment). useApi consults
// this module before every fetch, so a component on a page that is still
// shown (the TopBar's cloud badge, the Settings rail's backup control, the
// session drawer's enrichment row) never polls a hidden section's API and
// logs a 404. The guard stays the enforcement; this only avoids asking.
//
// Until the first governance read lands, the list remembered from the
// previous page load (sessionStorage, a per-tab convenience) applies, so a
// reload does not race the TopBar's first fetch. An ungoverned node's list is
// empty, which makes every check here a no-op.
//
// The matcher and the store are plain functions (no React), so
// concealedRoutes.test.ts runs under `node --test`.

const STORAGE_KEY = "sbo_concealed_routes";

/** The governance read itself is never held. */
const NEVER_HELD = "/api/governance";

/**
 * routeMatches reports whether a request path matches one mux pattern: an
 * exact path, a subtree prefix ending in "/", or segments with {wildcards}
 * ("{name}" one segment, "{name...}" the rest).
 */
export function routeMatches(path: string, pattern: string): boolean {
  const pathname = path.split(/[?#]/, 1)[0] || "/";
  if (!pattern.includes("{")) {
    return pattern.endsWith("/") ? pathname.startsWith(pattern) : pathname === pattern;
  }
  const want = pattern.split("/");
  const got = pathname.split("/");
  for (let i = 0; i < want.length; i++) {
    const seg = want[i];
    if (seg.startsWith("{") && seg.endsWith("...}")) return got.length > i;
    if (i >= got.length) return false;
    if (seg.startsWith("{") && seg.endsWith("}")) {
      if (got[i] === "") return false;
      continue;
    }
    if (seg !== got[i]) return false;
  }
  return got.length === want.length;
}

/** routeConcealed reports whether path matches any concealed pattern. */
export function routeConcealed(path: string, concealed: readonly string[]): boolean {
  if (concealed.length === 0) return false;
  const pathname = path.split(/[?#]/, 1)[0] || "/";
  if (pathname === NEVER_HELD) return false;
  return concealed.some((p) => routeMatches(pathname, p));
}

function readStored(): readonly string[] {
  try {
    const raw = globalThis.sessionStorage?.getItem(STORAGE_KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter((p): p is string => typeof p === "string") : [];
  } catch {
    return [];
  }
}

function writeStored(list: readonly string[]): void {
  try {
    if (list.length === 0) globalThis.sessionStorage?.removeItem(STORAGE_KEY);
    else globalThis.sessionStorage?.setItem(STORAGE_KEY, JSON.stringify(list));
  } catch {
    // Storage unavailable (private window, blocked site data): the list
    // still applies for this page load.
  }
}

let current: readonly string[] = readStored();
let snapshot = current.join("\n");
const listeners = new Set<() => void>();

/**
 * setConcealedRoutes records the list from a governance read. Absent (an
 * older daemon) is an empty list. Subscribers re-render only on a change.
 */
export function setConcealedRoutes(list: readonly string[] | undefined): void {
  const next = [...(list ?? [])].sort();
  const key = next.join("\n");
  if (key === snapshot) return;
  current = next;
  snapshot = key;
  writeStored(next);
  for (const fn of listeners) fn();
}

/** isRouteConcealed reports whether a fetch of path must be skipped now. */
export function isRouteConcealed(path: string): boolean {
  return routeConcealed(path, current);
}

/** subscribeConcealedRoutes is the useSyncExternalStore subscribe. */
export function subscribeConcealedRoutes(fn: () => void): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

/** concealedRoutesSnapshot is the useSyncExternalStore snapshot (a string). */
export function concealedRoutesSnapshot(): string {
  return snapshot;
}
