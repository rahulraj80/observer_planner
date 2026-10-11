import { useEffect, useMemo, useSyncExternalStore } from "react";
import { useApi, type ApiState } from "./useApi";
import { NAV_COMPONENTS } from "./components.gen";
import {
  inactiveNavIds,
  inactiveRoutesSnapshot,
  setInactiveRoutes,
  subscribeInactiveRoutes,
  tileVisible,
  type TileOwner,
  type CatalogComponent,
  type CatalogReport,
  type ComponentsStateResponse,
} from "./componentState";

// useComponents: the React side of lib/componentState (modular packaging plan
// section 7, lane MP-DASH). One state read and one catalog read, both shared
// through the query cache, so the sidebar, the route guard and the Settings >
// Components page ask the daemon once.

/** How often the shell re-reads the component state while visible. A change
 *  made on the Components page reloads it at once; this only catches a hand
 *  edit of config.toml or an `observer components` run in a terminal. */
const COMPONENT_STATE_REFRESH_MS = 30_000;

/** useComponentState reads GET /api/components/state and keeps the fetch
 *  skip list (lib/componentState) in step with it. */
export function useComponentState(): ApiState<ComponentsStateResponse> {
  const st = useApi<ComponentsStateResponse>("/api/components/state", undefined, [], {
    refreshMs: COMPONENT_STATE_REFRESH_MS,
  });
  const routes = st.data ? st.data.inactive_routes.join("\n") : null;
  useEffect(() => {
    if (routes != null) setInactiveRoutes(routes ? routes.split("\n") : []);
  }, [routes]);
  return st;
}

/** useComponentCatalog reads GET /api/components (static per binary). */
export function useComponentCatalog(): ApiState<CatalogReport> {
  return useApi<CatalogReport>("/api/components");
}

/** useCatalogRow returns one catalog row by id, or undefined. */
export function useCatalogRow(id: string | undefined): CatalogComponent | undefined {
  const cat = useComponentCatalog();
  return useMemo(() => cat.data?.components.find((c) => c.id === id), [cat.data, id]);
}

/** useInactiveNavIds lists the nav ids whose component is not active. Empty
 *  until the state is known (fail open: a slow read never hides a page). */
export function useInactiveNavIds(): string[] {
  const st = useComponentState();
  const view = st.data?.view;
  return useMemo(() => inactiveNavIds(view, NAV_COMPONENTS), [view]);
}

/** useTileVisible returns the tile gate (lib/componentState tileVisible) bound
 *  to this node's component state; it re-renders when the state or the
 *  inactive-route list changes, and shows every tile until the state is known. */
export function useTileVisible(): (owner: TileOwner) => boolean {
  const st = useComponentState();
  // Subscribing re-renders the reader when the inactive-route list changes;
  // tileVisible reads that list from the module store.
  useSyncExternalStore(subscribeInactiveRoutes, inactiveRoutesSnapshot, inactiveRoutesSnapshot);
  const view = st.data?.view;
  return (owner: TileOwner) => tileVisible(view, owner);
}
