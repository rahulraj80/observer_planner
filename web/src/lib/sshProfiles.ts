// sshProfiles.ts: the pure half of the Remote systems editor (T-488), the
// dashboard control that replaced "add a [[terminal.ssh.profiles]] block to
// config.toml and restart the daemon". The server
// (PUT /api/terminal/ssh/profiles) is the authority: it validates every
// profile with the config loader's own rules and refuses the whole write on
// the first bad entry. These helpers only give the form an early, honest hint
// and summarize what a save will change.

/** One editable remote system, the wire shape of the editor route. key_path
 *  is a PATH only: nothing ever reads or shows the key file's contents. */
export type SSHProfileDraft = {
  name: string;
  label: string;
  host: string;
  user: string;
  port: number;
  key_path: string;
  jump: string;
  dashboard_port: number;
  reverse_proxy: boolean;
  reverse_proxy_port: number;
};

/** GET /api/terminal/ssh/profiles. */
export type SSHProfilesEditorResponse = {
  confirm_token: string;
  config_writable: boolean;
  terminal_enabled: boolean;
  enabled: boolean;
  profiles: SSHProfileDraft[];
  restart_required_on_save: boolean;
  test_available: boolean;
};

export function emptyProfile(): SSHProfileDraft {
  return {
    name: "",
    label: "",
    host: "",
    user: "",
    port: 0,
    key_path: "",
    jump: "",
    dashboard_port: 0,
    reverse_proxy: false,
    reverse_proxy_port: 0,
  };
}

// The name rule is internal/sshprofile's validateName: it travels in request
// bodies and is matched exactly, so it is deliberately narrow.
const NAME_RE = /^[a-z0-9][a-z0-9._-]*$/;

/**
 * draftProblem returns the first problem the form can see before the save,
 * or null. It mirrors the server's most common refusals only; the server
 * re-validates everything (including that key_path is an existing file).
 */
export function draftProblem(d: SSHProfileDraft, others: SSHProfileDraft[]): string | null {
  const name = d.name.trim();
  if (!name) return "Give the system a name (lowercase letters, digits, . _ -).";
  if (!NAME_RE.test(name)) return "The name may use lowercase letters, digits, dot, underscore and dash, and must start with a letter or digit.";
  if (others.some((o) => o.name.trim() === name)) return `Another system is already named ${name}.`;
  const host = d.host.trim();
  if (!host) return "Enter the host: a hostname, an IP address or a ~/.ssh/config Host alias.";
  if (host.startsWith("-")) return "The host may not start with a dash.";
  if (/\s/.test(host)) return "The host may not contain spaces.";
  if (d.port !== 0 && (!Number.isInteger(d.port) || d.port < 1 || d.port > 65535)) return "The SSH port must be between 1 and 65535 (leave it 0 for the default 22).";
  const key = d.key_path.trim();
  if (key && !key.startsWith("/") && !/^[A-Za-z]:[\\/]/.test(key)) return "The key path must be an absolute path to the private key file.";
  if (d.jump.includes(",")) return "Only one jump host is supported.";
  return null;
}

/** normalize trims every text field, the same way the server does. */
export function normalize(d: SSHProfileDraft): SSHProfileDraft {
  return {
    ...d,
    name: d.name.trim(),
    label: d.label.trim(),
    host: d.host.trim(),
    user: d.user.trim(),
    key_path: d.key_path.trim(),
    jump: d.jump.trim(),
  };
}

/** target renders the user@host:port line the picker shows. */
export function target(d: SSHProfileDraft): string {
  const host = d.port && d.port !== 22 ? `${d.host}:${d.port}` : d.host;
  return d.user ? `${d.user}@${host}` : host;
}

function same(a: SSHProfileDraft, b: SSHProfileDraft): boolean {
  return JSON.stringify(normalize(a)) === JSON.stringify(normalize(b));
}

export type ProfileChanges = { added: string[]; changed: string[]; removed: string[] };

/** changesBetween summarizes what saving `next` over `saved` does, by name. */
export function changesBetween(saved: SSHProfileDraft[], next: SSHProfileDraft[]): ProfileChanges {
  const byName = new Map(saved.map((p) => [p.name, p]));
  const nextNames = new Set(next.map((p) => p.name.trim()));
  const added: string[] = [];
  const changed: string[] = [];
  for (const p of next) {
    const prev = byName.get(p.name.trim());
    if (!prev) added.push(p.name.trim());
    else if (!same(prev, p)) changed.push(p.name.trim());
  }
  const removed = saved.map((p) => p.name).filter((n) => !nextNames.has(n));
  return { added, changed, removed };
}

/** changesSentence is the one-line review shown beside Save, or "" when
 *  nothing changed. enabledTo is the new switch value when it changed. */
export function changesSentence(c: ProfileChanges, enabledTo?: boolean): string {
  const parts: string[] = [];
  if (c.added.length) parts.push(`add ${c.added.join(", ")}`);
  if (c.changed.length) parts.push(`change ${c.changed.join(", ")}`);
  if (c.removed.length) parts.push(`remove ${c.removed.join(", ")}`);
  if (enabledTo !== undefined) parts.push(`turn SSH terminals ${enabledTo ? "on" : "off"}`);
  if (parts.length === 0) return "";
  const s = parts.join("; ");
  return `Saving will ${s}.`;
}
