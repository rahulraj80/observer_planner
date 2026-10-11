// remediation: the Health page's reading of a doctor check's structured fixes
// (GET /api/health/doctor `remediations`, T-421 item 3). The server renders
// each fix from internal/remedy's one table (the command the CLI text quotes,
// the control's label and in-app link); this module only decides which fixes
// become buttons. Pure (no React), so the node test runner covers it.

export type DoctorRemediation = {
  action: string;
  tool?: string;
  command?: string;
  label: string;
  href: string;
};

/** The deep-link parameters a remediation link carries into Settings. */
export type SettingsDeepLink = {
  section: string;
  tool?: string;
  open?: string;
  target?: string;
};

/**
 * remediationButtons keeps the fixes a button can honestly perform: an
 * in-app link (never an external URL), one button per distinct link + label.
 */
export function remediationButtons(rs: DoctorRemediation[] | undefined): DoctorRemediation[] {
  if (!rs) return [];
  const seen = new Set<string>();
  const out: DoctorRemediation[] = [];
  for (const r of rs) {
    if (!r.label || !r.href.startsWith("/") || r.href.startsWith("//")) continue;
    const key = `${r.href}\u0000${r.label}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(r);
  }
  return out;
}

/** parseSettingsDeepLink reads the Settings query a remediation link sets. */
export function parseSettingsDeepLink(search: string): SettingsDeepLink | null {
  const q = new URLSearchParams(search.startsWith("?") ? search.slice(1) : search);
  const section = q.get("section");
  if (!section) return null;
  const out: SettingsDeepLink = { section };
  for (const k of ["tool", "open", "target"] as const) {
    const v = q.get(k);
    if (v) out[k] = v;
  }
  return out;
}
