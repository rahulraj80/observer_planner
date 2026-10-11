// AI tool setting edit requests (T-436): a developer on an org-managed
// machine asks to edit an AI tool setting their organization's policy locks,
// or grants no editing of. An approved grant opens it until it ends; then the
// policy applies again (agentconfigorg.WithEditGrants on the daemon).
//
// Pure: no React, no router. Importable under `node --test`.

/** The part of the editor's access block these helpers read. */
export type EditAccess = {
  read_only: boolean;
  grants_only?: boolean;
  grants?: string[];
  locked?: Record<string, string>;
};

/** The part of a setting row these helpers read. */
export type EditSetting = { id: string; owner: string; locked?: string };

function granted(access: EditAccess, id: string): boolean {
  if (!access.grants_only) return true;
  const tool = id.split("/", 1)[0];
  return (access.grants ?? []).some((g) => g === id || g === `${tool}/*`);
}

/**
 * canAskToEdit reports whether an edit request could open this setting: it
 * is one the developer edits (owner "user"; Observer-, tool- or
 * vendor-owned rows stay out of reach whatever the org grants) and the
 * organization is what stops it (an enforced lock, a read-only machine, or
 * a grants-only policy that does not name it).
 */
export function canAskToEdit(s: EditSetting, access: EditAccess | null | undefined): boolean {
  if (!access || s.owner !== "user") return false;
  return Boolean(s.locked) || access.read_only || !granted(access, s.id);
}

/** One target an edit request may name. */
export type EditChoice = { id: string; label: string };

/**
 * configEditChoices lists what an edit request may name: the asked setting
 * first (when there is one), then every setting of each tool ("<tool>/*").
 */
export function configEditChoices(asked: string | undefined, tools: readonly string[]): EditChoice[] {
  const out: EditChoice[] = [];
  const seen = new Set<string>();
  const add = (id: string, label: string) => {
    if (!seen.has(id)) {
      seen.add(id);
      out.push({ id, label });
    }
  };
  if (asked && !asked.endsWith("/*")) {
    add(asked, `This setting: ${asked}`);
    add(`${asked.split("/", 1)[0]}/*`, `Every ${asked.split("/", 1)[0]} setting`);
  } else if (asked) {
    add(asked, `Every ${asked.slice(0, -2)} setting`);
  }
  for (const t of tools) add(`${t}/*`, `Every ${t} setting`);
  return out;
}
