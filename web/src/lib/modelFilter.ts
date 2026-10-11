// modelFilter - re-export shim. The Model chip's option logic lives in
// shared/lib/modelFilterOptions.ts so the node FilterBar and the org TopBar
// offer the same choices the same way; this path keeps existing importers.
export * from "../../../shared/lib/modelFilterOptions.ts";
