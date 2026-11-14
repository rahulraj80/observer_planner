import { useState } from "react";
import { useApi } from "@/lib/useApi";
import { Icon } from "@/components/primitives";
import { Activity, X } from "lucide-react";

// TelemetryNoticeBanner — the one-time notice that usage telemetry is on
// (telemetry plan Step 5.7: the content-free baseline tier is on by default
// for every install, with a notice). It reads GET /api/telemetry/notice
// (posture only) and shows while telemetry is active until dismissed.
// Dismissal persists per notice version in localStorage
// (`sb_telemetry_notice_v<N>`), so a changed notice shows once more.

type Notice = { active: boolean; tier: string; endpoint: string; version: number };

const keyFor = (v: number) => `sb_telemetry_notice_v${v}`;

function dismissedFor(v: number): boolean {
  try {
    return localStorage.getItem(keyFor(v)) === "1";
  } catch {
    return false;
  }
}

export function TelemetryNoticeBanner() {
  const notice = useApi<Notice>("/api/telemetry/notice");
  const [hidden, setHidden] = useState(false);
  const n = notice.data;
  if (hidden || n == null || !n.active || dismissedFor(n.version)) return null;

  const dismiss = () => {
    try {
      localStorage.setItem(keyFor(n.version), "1");
    } catch {
      // ignore: worst case it shows again next load
    }
    setHidden(true);
  };

  return (
    <section className="flex items-start gap-3 rounded-3 border border-line-2 bg-bg-2 px-4 py-2.5">
      <Icon icon={Activity} size="sm" className="mt-0.5 shrink-0 text-fg-3" />
      <div className="min-w-0 flex-1 text-[12px] text-fg-2">
        <p>
          Usage telemetry is on ({n.tier}). It sends content-free counters only (activity, token totals,
          tool counts, error classes, and per prompt the model family, token sizes, cache share, time and a
          prompt/nudge/tool label classified on this machine) to{" "}
          <span className="font-mono">{n.endpoint}</span>. No prompt or output text, paths, project names or
          commands.
        </p>
        <p className="mt-1 text-fg-3">
          See it with <span className="font-mono">observer telemetry data</span>, delete it with{" "}
          <span className="font-mono">observer telemetry delete --yes</span>, turn it off with{" "}
          <span className="font-mono">observer telemetry consent off</span> or in Settings, Observability.
        </p>
      </div>
      <button
        type="button"
        onClick={dismiss}
        className="shrink-0 text-[11px] text-fg-4 hover:text-fg-2"
        aria-label="Dismiss"
      >
        <Icon icon={X} size="xs" />
      </button>
    </section>
  );
}
