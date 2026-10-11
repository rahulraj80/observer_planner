import { useState } from "react";
import { Link } from "react-router-dom";
import { Plug, Route, TerminalSquare } from "lucide-react";
import { Obs } from "@/components/Obs";
import { useApi } from "@/lib/useApi";
import { fetchJSON } from "@/lib/api";
import { OnboardingCard as SharedOnboardingCard, type OnboardingStep } from "@shared/primitives/OnboardingCard";

// OnboardingCard: F1 + delight moment D-1 (usability arc P5.1), the
// empty-DB Overview's first-run checklist, on the shared card chrome
// (shared/primitives/OnboardingCard.tsx). Replaces the dead-end ("no data")
// first minute with the three steps that get a session on the board, each
// linking to the dashboard control that does it (no terminal required).
//
// Restraint rules §9.4: shows only while the DB has zero sessions,
// permanently dismissable (node-local flag), Obs idle cameo + ONE
// pixel-chip accent (the ▸ chip carries the arcade register at chip
// size; body copy stays calm), reduced-motion handled inside Obs.

const LINK = "text-accent hover:underline";

const STEPS: readonly OnboardingStep[] = [
  {
    key: "tools",
    icon: Plug,
    cta: (
      <Link to="/settings?section=tools" className={LINK}>
        Check your connected tools
      </Link>
    ),
    body: "See what's detected on this machine and run the per-tool setup wizard (every write previews first).",
  },
  {
    key: "proxy",
    icon: Route,
    cta: (
      <Link to="/compression" className={LINK}>
        Route Claude Code / Codex through the proxy
      </Link>
    ),
    body: "One click, durable, unlocks exact token accounting and compression. Optional but worth it.",
  },
  {
    key: "use",
    icon: TerminalSquare,
    cta: <span className="text-fg-1">Use your AI tool as usual</span>,
    body: "The first session shows up here the moment it lands.",
  },
];

export function OnboardingCard({ sessions }: { sessions: number | null }) {
  // Demo-mode offer (P6.7): only rendered when the server can seed.
  // While demo is active sessions > 0, so this card is gone anyway.
  const demo = useApi<{ available: boolean; active: boolean }>("/api/demo");
  const [seeding, setSeeding] = useState(false);
  if (sessions == null || sessions > 0) return null;
  const startDemo = async () => {
    setSeeding(true);
    try {
      await fetchJSON("/api/demo/start", undefined, { method: "POST" });
      window.location.reload();
    } catch {
      setSeeding(false);
    }
  };
  return (
    <SharedOnboardingCard
      storageKey="sb_onboarding_dismissed"
      visual={<Obs state="idle" size={44} />}
      title="Welcome. Let's get your first session on the board."
      intro="SuperBased captures what your AI coding tools actually do: once a tool is wired, sessions, costs, and cache behavior appear here on their own."
      steps={STEPS}
      actions={
        <>
          <Link
            to="/settings?section=tools"
            className="rounded-2 border border-accent/40 bg-accent-soft px-3 py-1.5 text-[11.5px] font-semibold text-accent hover:opacity-90"
          >
            <span className="font-mono tracking-[0.08em]">▸ PRESS START</span>
          </Link>
          <span className="text-[11px] text-fg-4">
            CLI twin: <code className="font-mono">observer init</code>
          </span>
        </>
      }
      footer={
        demo.data?.available && !demo.data.active ? (
          <>
            Just looking around?{" "}
            <button
              type="button"
              onClick={startDemo}
              disabled={seeding}
              className="font-medium text-accent hover:underline disabled:opacity-60"
            >
              {seeding ? "seeding…" : "Explore with demo data"}
            </button>{" "}
            - a seeded sample in a temporary database. Your real data stays untouched; one click clears it.
          </>
        ) : undefined
      }
    />
  );
}
