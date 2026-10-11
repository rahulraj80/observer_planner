// Pure helpers for page state that lives in the query string (a page's tab,
// a settings section, the open drawer, a drawer's tab), so reload,
// back/forward and a copy-pasted link all land on the same view. No React, no
// router: each app binds these to react-router's search params in its own
// hook (web2 `useUrlEnum` in src/lib/useUrlState.ts, web `useUrlTab` in
// src/lib/useUrlTab.ts). Promoted from web2/src/lib/urlParams.ts (which is
// now a one-line re-export shim) so the node and org hooks cannot drift.

/**
 * withParams returns a copy of `prev` with each entry applied: a string sets
 * the param, null deletes it. The input is never mutated.
 */
export function withParams(
  prev: URLSearchParams,
  changes: Record<string, string | null>,
): URLSearchParams {
  const next = new URLSearchParams(prev);
  for (const [name, value] of Object.entries(changes)) {
    if (value == null) next.delete(name);
    else next.set(name, value);
  }
  return next;
}

/**
 * pickEnum reads `raw` as one of `allowed`, else `fallback`. An unknown or
 * stale value in a shared link falls back instead of rendering nothing.
 */
export function pickEnum<T extends string>(
  raw: string | null,
  allowed: readonly T[],
  fallback: T,
): T {
  return raw != null && (allowed as readonly string[]).includes(raw) ? (raw as T) : fallback;
}

/**
 * enumParamValue is what to WRITE for a choice: the default is written as
 * "no param" so "no preference" and "chose the default" stay the same URL and
 * a param is not left behind on every open (the node dashboard's ?tab= rule).
 */
export function enumParamValue<T extends string>(value: T, fallback: T): string | null {
  return value === fallback ? null : value;
}

/**
 * UrlHistoryMode says how a URL-state write lands in the browser history.
 * "push" adds an entry, so Back returns to the previous choice: use it for
 * PAGE tabs and section rails (a tab that used to be its own page must still
 * behave like one). "replace" rewrites the current entry: use it for drawer
 * tabs and other in-place toggles that are not navigation.
 */
export type UrlHistoryMode = "push" | "replace";

/** EnumParamWrite is one decided write: the next params and the history mode. */
export type EnumParamWrite = {
  params: URLSearchParams;
  replace: boolean;
};

/**
 * enumParamWrite decides how to write `value` into the param `name`. It
 * returns null when the URL would not change (choosing the tab you are on),
 * so a "push" never stacks a duplicate history entry. The default value is
 * written as an absent param (enumParamValue). Every other param is kept.
 */
export function enumParamWrite<T extends string>(
  prev: URLSearchParams,
  name: string,
  value: T,
  fallback: T,
  history: UrlHistoryMode,
): EnumParamWrite | null {
  const next = withParams(prev, { [name]: enumParamValue(value, fallback) });
  if (next.toString() === prev.toString()) return null;
  return { params: next, replace: history === "replace" };
}
