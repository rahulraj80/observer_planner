// Component tests of the sandbox setup panel (sandbox installation
// lifecycle plan section 3.7 and 10), rendered with react-dom/server under
// node's test runner (scripts/test-components.mjs). Server rendering runs no
// effect, so the panel is fed fixture data through its test seeds; the copy
// itself is pinned in src/lib/sandboxSetup.test.ts.

import { test } from "node:test";
import assert from "node:assert/strict";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { SandboxSetupPanel } from "@/components/SandboxSetupPanel";
import { SandboxBackendsList, SandboxSetupRowsList } from "@/components/SandboxSettingsCard";
import type { SandboxBackendAvail } from "@/lib/types";
import type { SandboxSetupPlan, SandboxSetupSummary } from "@/lib/sandboxSetupTypes";
import { setupLine } from "@/lib/sandboxSetup";

const setupSummaries: SandboxSetupSummary[] = [
  { backend: "bwrap", label: "Bubblewrap", ready: true },
  { backend: "cloud-hypervisor", label: "Cloud Hypervisor", ready: false, next: "Cloud Hypervisor" },
  { backend: "openshell", label: "OpenShell", ready: false, blocked: "OpenShell does not run in WSL2" },
];

const setupPlan: SandboxSetupPlan = {
  backend: "cloud-hypervisor",
  label: "Cloud Hypervisor",
  next: 1,
  ready: false,
  checked: false,
  fingerprint: "fp-1",
  steps: [
    {
      prereq: "component",
      kind: "component",
      label: "Sandboxes and VMs module: VM",
      state: "done",
      class: "auto",
      channel: "component_select",
      can_run: false,
      can_undo: true,
    },
    {
      prereq: "cloud-hypervisor",
      kind: "program",
      label: "Cloud Hypervisor",
      state: "missing",
      class: "auto",
      channel: "pinned_download",
      can_run: true,
      can_undo: false,
      consent: {
        pin: "cloud-hypervisor-linux-amd64",
        version: "54.0",
        sha256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
        bytes: 4_800_000,
        source: "vendor_release",
      },
      docs: "sandboxed-terminals.md#microvm-sandboxes-cloud-hypervisor-beta",
    },
    {
      prereq: "virtiofsd",
      kind: "program",
      label: "virtiofsd",
      state: "missing",
      class: "guide",
      channel: "pm",
      display: "sudo apt-get install -y virtiofsd",
      can_run: false,
      can_undo: false,
    },
    {
      prereq: "kvm-device",
      kind: "kernel_feature",
      label: "/dev/kvm",
      state: "blocked",
      class: "never",
      channel: "none",
      display: "WSL2 needs nested virtualization turned on in your Windows .wslconfig.",
      after: "reboot",
      can_run: false,
      can_undo: false,
      docs: "sandboxed-terminals.md#the-probe",
    },
    {
      prereq: "e2fsprogs",
      kind: "program",
      label: "e2fsprogs",
      state: "in_flight",
      class: "elevated",
      channel: "pm",
      disclosure: "Installs a system package with your password.",
      can_run: false,
      can_undo: false,
      record: {
        id: 9,
        tool: "e2fsprogs",
        channel: "apt",
        command: "sudo apt-get install -y e2fsprogs",
        status: "installing",
        installed_at: "2026-11-06T09:00:00Z",
        can_uninstall: false,
        can_forget: false,
        terminal_handle: "h-9",
        maybe_waiting_for_input: true,
        subject_kind: "sandbox",
        backend: "cloud-hypervisor",
      },
    },
    {
      prereq: "enable",
      kind: "enable",
      label: "Turn on Cloud Hypervisor",
      state: "missing",
      class: "auto",
      channel: "sandbox_enable",
      can_run: true,
      can_undo: false,
    },
  ],
};

function render(compact: boolean, plan: SandboxSetupPlan = setupPlan): string {
  return renderToStaticMarkup(
    <MemoryRouter>
      <SandboxSetupPanel compact={compact} backend="cloud-hypervisor" initialSummaries={setupSummaries} initialPlan={plan} onTerminal={() => {}} />
    </MemoryRouter>,
  );
}

// stepHTML returns the markup of one step's list item.
function stepHTML(html: string, prereq: string): string {
  const m = html.match(new RegExp(`<li data-step="${prereq}"[\\s\\S]*?</li>`));
  assert.ok(m, `no step ${prereq}`);
  return m[0];
}

test("the row chooser is a picker of the offered rows, on the preselected row", () => {
  const html = render(true);
  assert.match(html, /<select id="sandbox-setup-backend"/);
  assert.match(html, /<option value="bwrap">Bubblewrap \(set up\)<\/option>/);
  assert.match(html, /<option value="cloud-hypervisor" selected="">Cloud Hypervisor \(next: Cloud Hypervisor\)<\/option>/);
  assert.match(html, /<option value="openshell">OpenShell \(cannot run here\)<\/option>/);
  assert.match(html, /Cloud Hypervisor is not set up yet\. Next: Cloud Hypervisor\./);
  assert.match(html, /Check this machine/);
  assert.match(html, /Re-check/);
});

test("each step shows its chip and the button its class offers", () => {
  const html = render(true);
  assert.match(stepHTML(html, "component"), /Done/);
  assert.doesNotMatch(stepHTML(html, "component"), /Turn on/);
  assert.match(stepHTML(html, "cloud-hypervisor"), /Needed/);
  assert.match(stepHTML(html, "cloud-hypervisor"), /Download and install \(5 MB, v54\.0\)/);
  assert.match(stepHTML(html, "virtiofsd"), /Copy command/);
  assert.match(stepHTML(html, "enable"), /Turn on/);
  const never = stepHTML(html, "kvm-device");
  assert.match(never, /Blocked on this machine/);
  assert.doesNotMatch(never, /<button/);
  assert.match(never, /restart the computer/);
  assert.match(never, /href="https:\/\/github\.com\/superbasedapp\/observer\/blob\/main\/docs\/sandboxed-terminals\.md#the-probe"/);
});

test("a download's consent line and a disclosure come before the button", () => {
  const html = render(true);
  const dl = stepHTML(html, "cloud-hypervisor");
  const consentAt = dl.indexOf("checked against sha256 0123456789ab");
  const buttonAt = dl.indexOf("Download and install");
  assert.ok(consentAt > 0 && consentAt < buttonAt, "consent before the button");
  const el = stepHTML(html, "e2fsprogs");
  assert.ok(el.indexOf("Before you run it: Installs a system package") < el.indexOf("Open the install terminal"));
});

test("a step in flight links its terminal and says it may be waiting for input", () => {
  const el = stepHTML(render(true), "e2fsprogs");
  assert.match(el, /In progress/);
  assert.match(el, /Open the install terminal/);
  assert.match(el, /may be waiting for input/);
  assert.match(el, /Installing from this dashboard/);
});

test("an elevated step degraded to a guide says how it becomes a button", () => {
  assert.match(stepHTML(render(true), "virtiofsd"), /allow_elevated = true/);
});

test("Manage mode offers Undo where the server allows it; the compact form does not", () => {
  assert.match(stepHTML(render(false), "component"), /Undo/);
  assert.doesNotMatch(stepHTML(render(true), "component"), /Undo/);
});

test("Manage mode offers Repair for a step this dashboard set up that no longer verifies", () => {
  const plan: SandboxSetupPlan = {
    ...setupPlan,
    steps: [
      {
        ...setupPlan.steps[1],
        state: "missing",
        reason: "the placed file no longer matches its checksum",
        record: { ...setupPlan.steps[4].record!, status: "installed", tool: "cloud-hypervisor", terminal_handle: undefined, maybe_waiting_for_input: false },
      },
    ],
  };
  const el = stepHTML(render(false, plan), "cloud-hypervisor");
  assert.match(el, /Repair/);
  // The step is not done now, so its record reads as history (D-R4-5).
  assert.match(el, /Last run from this dashboard on /);
});

test("a ready plan says so", () => {
  const ready: SandboxSetupPlan = { ...setupPlan, ready: true, next: -1, steps: [setupPlan.steps[0]] };
  assert.match(render(true, ready), /Cloud Hypervisor is set up and runs here\./);
});

test("the compact form links to the Backends setting; Settings' own form does not", () => {
  assert.match(render(true), /href="\/terminals#sandbox-backends"/);
  assert.doesNotMatch(render(false), /href="\/terminals#sandbox-backends"/);
});

test("no copy in the panel carries an em or en dash", () => {
  assert.doesNotMatch(render(false), /[\u2013\u2014]/);
  assert.doesNotMatch(render(true), /[\u2013\u2014]/);
});

// Settings > Terminals > Sandbox isolation: each backend gets "Set up" (not
// ready) or "Manage" (ready), opening this panel under the row; the
// Enabled checkboxes are unchanged.
const row = (over: Partial<SandboxBackendAvail>): SandboxBackendAvail => ({
  id: "bwrap",
  label: "Bubblewrap",
  class: "process",
  location: "local",
  maturity: "beta",
  available: true,
  beta: false,
  capture_by_tier: {},
  component: "terminals.sandbox",
  ...over,
});

const listRows = [
  row({}),
  row({ id: "cloud-hypervisor", label: "Cloud Hypervisor", class: "hw_vm", available: false, verdict: "backend_missing", reason: "cloud-hypervisor not found" }),
];

function settingsList(withSetup: boolean, open: string | null = null): string {
  return renderToStaticMarkup(
    <MemoryRouter>
      <SandboxBackendsList
        backends={listRows}
        order={["bwrap"]}
        egress="internet"
        writable
        onOrder={() => {}}
        {...(withSetup
          ? {
              onSetup: () => {},
              setupRows: {
                bwrap: { backend: "bwrap", label: "Bubblewrap", ready: true },
                "cloud-hypervisor": { backend: "cloud-hypervisor", label: "Cloud Hypervisor", ready: false, next: "x" },
              },
              setupOpen: open,
              setupPanel: (
                <SandboxSetupPanel backend="cloud-hypervisor" initialSummaries={setupSummaries} initialPlan={setupPlan} onTerminal={() => {}} />
              ),
            }
          : {})}
      />
    </MemoryRouter>,
  );
}

test("Settings: Set up for a row that is not ready, Manage for one that is", () => {
  const html = settingsList(true);
  assert.match(html, /aria-label="Manage Bubblewrap"[^>]*>Manage<\/button>/);
  assert.match(html, /aria-label="Set up Cloud Hypervisor"[^>]*>Set up<\/button>/);
  assert.match(html, /aria-label="Enable Bubblewrap"/);
  assert.doesNotMatch(html, /data-testid="sandbox-setup-panel"/);
});

test("Settings: the open row shows the full setup panel under it", () => {
  const html = settingsList(true, "cloud-hypervisor");
  assert.match(html, /data-testid="sandbox-setup-panel"/);
  // Full size (not compact): Manage actions show, the dialog's link does not.
  assert.match(html, /Undo/);
  assert.doesNotMatch(html, /Manage sandbox backends in Terminals settings/);
});

test("Settings: a row the setup summaries do not list (it does not run here) offers no Set up (D-4)", () => {
  const html = renderToStaticMarkup(
    <MemoryRouter>
      <SandboxBackendsList
        backends={listRows}
        order={["bwrap"]}
        egress="internet"
        writable
        onOrder={() => {}}
        onSetup={() => {}}
        setupRows={{ bwrap: { backend: "bwrap", label: "Bubblewrap", ready: false, unconfirmed: true } }}
      />
    </MemoryRouter>,
  );
  assert.match(html, /aria-label="Manage Bubblewrap"/);
  assert.doesNotMatch(html, /Set up Cloud Hypervisor/);
});

test("Settings: with the sandbox off the summaries still list Set up and Manage (D-3)", () => {
  const html = renderToStaticMarkup(
    <MemoryRouter>
      <SandboxSetupRowsList
        rows={[
          { backend: "bwrap", label: "Bubblewrap", ready: false, next: "Bubblewrap (bwrap) 0.4.0 or newer" },
          { backend: "process_lite", label: "Landlock only", ready: true },
        ]}
        onSetup={() => {}}
      />
    </MemoryRouter>,
  );
  assert.match(html, /data-testid="sandbox-setup-rows"/);
  assert.match(html, /aria-label="Set up Bubblewrap"[^>]*>Set up<\/button>/);
  assert.match(html, /aria-label="Manage Landlock only"[^>]*>Manage<\/button>/);
  assert.doesNotMatch(html, new RegExp(`[${String.fromCharCode(0x2013, 0x2014)}]`));
});

test("Settings: without the setup routes no Set up or Manage button shows", () => {
  const html = settingsList(false);
  assert.doesNotMatch(html, />Set up</);
  assert.doesNotMatch(html, />Manage</);
  assert.match(html, /aria-label="Enable Bubblewrap"/);
});

test("a beta row: the panel shows a Beta badge and the sentence, Settings reads (beta), the dialog line names it", () => {
  const sentence = "Setup on Windows 11 Pro is beta: it has not been checked on a live host yet.";
  const html = renderToStaticMarkup(
    <MemoryRouter>
      <SandboxSetupPanel
        backend="cloud-hypervisor"
        initialSummaries={setupSummaries.map((s) => (s.backend === "cloud-hypervisor" ? { ...s, beta: sentence } : s))}
        initialPlan={{ ...setupPlan, beta: sentence }}
        onTerminal={() => {}}
      />
    </MemoryRouter>,
  );
  assert.match(html, /data-testid="sandbox-setup-headline"[^>]*>[\s\S]*?>Beta<\/span>/);
  assert.match(html, new RegExp(`data-testid="sandbox-setup-beta"[^>]*>${sentence}</p>`));
  assert.match(html, /<option value="cloud-hypervisor" selected="">Cloud Hypervisor \(beta\) \(next: Cloud Hypervisor\)<\/option>/);
  assert.match(html, /<option value="bwrap">Bubblewrap \(set up\)<\/option>/);
  assert.doesNotMatch(render(true), /sandbox-setup-beta|>Beta</);

  const list = renderToStaticMarkup(
    <MemoryRouter>
      <SandboxBackendsList
        backends={listRows}
        order={["bwrap"]}
        egress="internet"
        writable
        onOrder={() => {}}
        onSetup={() => {}}
        setupRows={{
          bwrap: { backend: "bwrap", label: "Bubblewrap", ready: true },
          "cloud-hypervisor": { backend: "cloud-hypervisor", label: "Cloud Hypervisor", ready: false, next: "x", beta: sentence },
        }}
      />
    </MemoryRouter>,
  );
  assert.match(list, /aria-label="Set up Cloud Hypervisor"[^>]*>Set up \(beta\)<\/button>/);
  assert.match(list, /aria-label="Manage Bubblewrap"[^>]*>Manage<\/button>/);

  assert.equal(
    setupLine(2, ["Hyper-V isolated containers (HCS)"]),
    "Set up a sandbox backend (2 available to set up): Hyper-V isolated containers (HCS) (beta)",
  );
  assert.equal(setupLine(2), "Set up a sandbox backend (2 available to set up)");
});

// Fix round 4: a long unbroken token wraps (D-R4-4), the chooser follows a
// checked plan (D-R4-3), and a step not done reads its record as history
// (D-R4-5).
test("fix4: the chooser follows the checked plan, a needed step's record is history, long tokens wrap", () => {
  const digest = "ghcr.io/example/agent@sha256:" + "a".repeat(64);
  const summaries: SandboxSetupSummary[] = [{ backend: "apple-container", label: "Apple container", ready: false, unconfirmed: true }];
  const plan: SandboxSetupPlan = {
    backend: "apple-container",
    label: "Apple container",
    next: 0,
    ready: false,
    checked: true,
    fingerprint: "fp",
    steps: [
      {
        prereq: "apple-container-image",
        kind: "content",
        label: "The sandbox image",
        state: "missing",
        class: "auto",
        channel: "terminal_cmd",
        can_run: true,
        can_undo: false,
        reason: "The image " + digest + " is not present.",
        record: {
          id: 16,
          tool: "apple-container-image",
          channel: "terminal_cmd",
          command: "container image pull",
          status: "installed",
          installed_at: "2026-11-07T05:00:00Z",
          can_uninstall: false,
          can_forget: true,
        },
      },
    ],
  };
  const html = renderToStaticMarkup(
    <MemoryRouter>
      <SandboxSetupPanel backend="apple-container" compact onTerminal={() => {}} initialSummaries={summaries} initialPlan={plan} />
    </MemoryRouter>,
  );
  assert.match(html, /Apple container \(next: The sandbox image\)/);
  assert.doesNotMatch(html, /set up, not checked/);
  assert.match(html, /Last run from this dashboard on /);
  assert.doesNotMatch(html, /Installed from this dashboard/);
  assert.match(html, /data-testid="sandbox-setup-panel" class="[^"]*\[overflow-wrap:anywhere\]/);
});
