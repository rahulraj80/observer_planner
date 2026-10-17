import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { X } from "lucide-react";
import { Button } from "@/components/primitives";
import { useApi } from "@/lib/useApi";
import { orgIntelBanner, orgIntelBannerDismissKey, type OrgIntelStatusResponse } from "@/lib/orgIntel";

// OrgIntelConsentBanner is the blocking-but-dismissable banner of design
// docs/plans/cloud-intelligence-parity-design-2026-10-03.md 3.7 step 3: while
// an individual-tenancy machine has a compulsory Cloud Intelligence
// requirement it has not accepted, every page says so with the two choices,
// Accept (the consent screen in Settings) or Leave (the enrolment page's
// Leave flow). Dismissal is per browser session and per terms version; a newer
// version shows it again. Renders nothing on managed or unenrolled machines,
// on a daemon without the route (501) and on a remote view.
export function OrgIntelConsentBanner() {
  const st = useApi<OrgIntelStatusResponse>("/api/intel/status", undefined, [], { refreshMs: 300000 });
  const navigate = useNavigate();
  const status = st.data?.status ?? null;
  const banner = orgIntelBanner(status);
  const key = status ? orgIntelBannerDismissKey(status) : "";
  const [dismissed, setDismissed] = useState<string | null>(() => {
    try {
      return sessionStorage.getItem("sbo-intel-banner-dismissed");
    } catch {
      return null;
    }
  });
  if (!banner || dismissed === key) return null;
  function dismiss() {
    setDismissed(key);
    try {
      sessionStorage.setItem("sbo-intel-banner-dismissed", key);
    } catch {
      // storage unavailable: dismissed for this render only
    }
  }
  return (
    <div
      role="alert"
      className="flex flex-wrap items-center gap-x-3 gap-y-1.5 border-b border-warn/30 bg-warn-soft px-4 py-2 text-[11.5px] text-fg-1"
    >
      <span className="shrink-0 font-semibold">{banner.title}</span>
      <span className="min-w-0 flex-1 basis-60">{banner.text}</span>
      <div className="flex shrink-0 items-center gap-2">
        <Button size="sm" variant="primary" onClick={() => navigate("/settings?section=intel")}>
          Review and accept
        </Button>
        {status?.pending_reason !== "terms_changed" && (
          <Button size="sm" variant="secondary" onClick={() => navigate("/settings?section=enrolment")}>
            Leave
          </Button>
        )}
        <button
          type="button"
          aria-label="Dismiss for now"
          onClick={dismiss}
          className="rounded-1 p-1 text-fg-3 hover:bg-bg-3 hover:text-fg-1"
        >
          <X className="h-3.5 w-3.5" aria-hidden />
        </button>
      </div>
    </div>
  );
}
