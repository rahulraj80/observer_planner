import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";
import {
  enumParamWrite,
  pickEnum,
  withParams,
  type UrlHistoryMode,
} from "@shared/lib/urlParams";

/** Options for useUrlTab. */
export type UseUrlTabOptions = {
  /** The query param that holds the choice. Default "tab" ("section" for a rail). */
  param?: string;
  /**
   * How a change lands in the browser history. Default "push": this hook is
   * for PAGE tabs and section rails, where Back must return to the previous
   * tab. A drawer's tabs (an in-place view, not navigation) pass "replace".
   * Note the org dashboard's useUrlEnum defaults to "replace" for its older
   * drawer callers; page lanes pass the mode explicitly in both apps.
   */
  history?: UrlHistoryMode;
};

/**
 * useUrlTab is the node dashboard's page-tab URL state, the twin of web2's
 * useUrlEnum over the same pure helpers (shared/lib/urlParams.ts):
 *  - read: `?<param>=<id>` through pickEnum, so a cold deep link to any
 *    allowed id renders that tab and an unknown or stale id falls back to
 *    `fallback` instead of rendering nothing;
 *  - write: the default is the param's ABSENCE (no `?tab=overview` left
 *    behind), other params are kept, and choosing the tab already shown
 *    writes nothing (no duplicate history entry on "push").
 */
export function useUrlTab<T extends string>(
  allowed: readonly T[],
  fallback: T,
  options: UseUrlTabOptions = {},
): [T, (v: T) => void] {
  const name = options.param ?? "tab";
  const history = options.history ?? "push";
  const [params, setParams] = useSearchParams();
  const value = pickEnum(params.get(name), allowed, fallback);
  const set = useCallback(
    (v: T) => {
      const write = enumParamWrite(params, name, v, fallback, history);
      if (write == null) return;
      setParams((prev) => withParams(prev, { [name]: write.params.get(name) }), {
        replace: write.replace,
      });
    },
    [params, name, fallback, history, setParams],
  );
  return [value, set];
}
