// The pure core of saveConfigSection (configSectionSave.ts, T-432): preview
// first, write only on an explicit yes, at the etag the preview was computed
// against. No imports, so node:test pins it directly; the transport and the
// dialog are injected.

/** The server's dry run of a section save (PUT ...?dry_run=1). */
export type SectionDryRun = {
  dry_run: true;
  would_write: boolean;
  changed_keys: string[];
  restart_required: boolean;
  restart_required_keys: string[];
  applied_live_keys: string[];
  next_spawn_keys: string[];
  comments_preserved: boolean;
  write_mode: "surgical" | "reserialize";
  write_mode_reason?: string;
  config_path: string;
  config_etag: string;
  diff: string;
  diff_stats: { added: number; removed: number };
  needs_confirm: boolean;
  secret_warnings: { key: string; message: string }[];
  section?: string;
};

/** The answer of a section save (a superset of the pre-T-432 shape). */
export type SectionSaveResult = {
  saved: boolean;
  section: string;
  restart_required: boolean;
  changed_keys: string[];
  restart_required_keys: string[];
  applied_live_keys: string[];
  next_spawn_keys: string[];
  write_mode: "surgical" | "reserialize" | "noop";
  write_mode_reason?: string;
  secret_warnings: { key: string; message: string }[];
  config_path?: string;
  backup_path?: string;
  config_etag?: string;
};

/** What the preview dialog is asked to show. */
export type SectionPreviewRequest = {
  section: string;
  title?: string;
  dryRun: SectionDryRun & { section: string };
};

/** Thrown when the user cancels the preview: nothing was written. */
export class ConfigSaveCancelled extends Error {
  constructor() {
    super("Save cancelled; nothing was written.");
    this.name = "ConfigSaveCancelled";
  }
}

/** The message when no preview dialog is installed (fail closed). */
export const NO_PREVIEW_MESSAGE = "The save preview is not available on this page; nothing was written.";

export type SectionSaveDeps = {
  /** PUT /api/config/section/<section> with the query params and JSON body. */
  put: (section: string, params: Record<string, string>, body: string) => Promise<unknown>;
  /** The preview dialog, or null when none is installed. */
  prompt: ((req: SectionPreviewRequest) => Promise<boolean>) | null;
};

/**
 * runSectionSave previews then writes one config section:
 *  1. dry run; a dry run that would write nothing resolves as a no-op
 *     without opening the dialog and without writing;
 *  2. the dialog shows the diff and warnings; no dialog fails closed;
 *  3. cancel rejects with ConfigSaveCancelled; Save writes at the
 *     previewed etag, so a file changed in between answers 409.
 */
export async function runSectionSave(
  section: string,
  body: unknown,
  deps: SectionSaveDeps,
  opts: { title?: string } = {},
): Promise<SectionSaveResult> {
  const payload = JSON.stringify(body);
  const dryRun = (await deps.put(section, { dry_run: "1" }, payload)) as SectionDryRun;
  if (!dryRun.would_write) {
    return {
      saved: true,
      section,
      restart_required: false,
      changed_keys: [],
      restart_required_keys: [],
      applied_live_keys: [],
      next_spawn_keys: [],
      write_mode: "noop",
      secret_warnings: dryRun.secret_warnings ?? [],
      config_etag: dryRun.config_etag,
    };
  }
  if (!deps.prompt) throw new Error(NO_PREVIEW_MESSAGE);
  const ok = await deps.prompt({ section, title: opts.title, dryRun: { ...dryRun, section } });
  if (!ok) throw new ConfigSaveCancelled();
  return (await deps.put(section, { base_etag: dryRun.config_etag }, payload)) as SectionSaveResult;
}
