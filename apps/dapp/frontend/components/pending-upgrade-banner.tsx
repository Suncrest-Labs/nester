"use client";

import React, { useEffect, useState } from "react";
import { ShieldAlert, ExternalLink } from "lucide-react";

export interface PendingProtocolUpgrade {
  contract_address: string;
  wasm_hash: string;
  eta: number;
  eta_iso: string;
  remaining_seconds?: number;
  proposer?: string;
  status: "pending" | "cancelled" | "executed";
  release_url: string;
}

export interface PendingUpgradeBannerProps {
  /** Optional preloaded upgrades (used in tests or SSR rehearsal). */
  initialUpgrades?: PendingProtocolUpgrade[];
  /** Optional custom endpoint override (defaults to /api/v1/protocol/upgrades). */
  endpoint?: string;
}

export function formatTimelockCountdown(remainingSeconds: number): string {
  if (remainingSeconds <= 0) {
    return "Ready for execution";
  }
  const hours = Math.floor(remainingSeconds / 3600);
  const minutes = Math.floor((remainingSeconds % 3600) / 60);
  if (hours >= 24) {
    const days = Math.floor(hours / 24);
    const remHours = hours % 24;
    return `${days}d ${remHours}h remaining`;
  }
  return `${hours}h ${minutes}m remaining`;
}

export function PendingUpgradeBanner({
  initialUpgrades,
  endpoint = "/api/v1/protocol/upgrades",
}: PendingUpgradeBannerProps) {
  const [upgrades, setUpgrades] = useState<PendingProtocolUpgrade[]>(
    initialUpgrades ?? [],
  );

  useEffect(() => {
    if (initialUpgrades !== undefined) {
      setUpgrades(initialUpgrades);
      return;
    }

    let cancelled = false;
    async function loadPendingUpgrades() {
      try {
        const res = await fetch(endpoint, {
          headers: { Accept: "application/json" },
        });
        if (!res.ok) return;
        const payload = await res.json();
        const list: PendingProtocolUpgrade[] =
          payload?.data?.upgrades ?? payload?.upgrades ?? [];
        if (!cancelled && Array.isArray(list)) {
          setUpgrades(list.filter((item) => item.status === "pending"));
        }
      } catch {
        // Non-blocking banner fetch
      }
    }

    void loadPendingUpgrades();
    return () => {
      cancelled = true;
    };
  }, [endpoint, initialUpgrades]);

  const activeUpgrades = upgrades.filter((u) => u.status === "pending");
  if (activeUpgrades.length === 0) {
    return null;
  }

  const primary = activeUpgrades[0];
  const nowSec = Math.floor(Date.now() / 1000);
  const remaining =
    typeof primary.remaining_seconds === "number"
      ? primary.remaining_seconds
      : Math.max(0, primary.eta - nowSec);
  const shortHash =
    primary.wasm_hash.length > 16
      ? `${primary.wasm_hash.slice(0, 8)}...${primary.wasm_hash.slice(-8)}`
      : primary.wasm_hash;
  const etaLabel =
    primary.eta_iso || new Date(primary.eta * 1000).toISOString();

  return (
    <div
      role="region"
      aria-label="Pending protocol upgrade notice"
      data-testid="pending-upgrade-banner"
      className="w-full border-b border-amber-500/40 bg-amber-950/90 px-4 py-2.5 text-xs sm:text-sm text-amber-100 flex flex-wrap items-center justify-center gap-3 z-[180]"
    >
      <div className="flex items-center gap-2 font-medium">
        <ShieldAlert className="h-4 w-4 shrink-0 text-amber-400" />
        <span>
          Timelocked Contract Upgrade Scheduled — ETA:{" "}
          <strong data-testid="upgrade-eta">{etaLabel}</strong> (
          <span data-testid="upgrade-countdown">
            {formatTimelockCountdown(remaining)}
          </span>
          )
        </span>
      </div>
      <div className="flex items-center gap-2 text-amber-200/90">
        <span>
          New WASM:{" "}
          <code
            data-testid="upgrade-wasm-hash"
            title={primary.wasm_hash}
            className="rounded bg-amber-900/60 px-1.5 py-0.5 font-mono text-xs text-amber-100"
          >
            {shortHash}
          </code>
        </span>
        <a
          href={primary.release_url}
          target="_blank"
          rel="noopener noreferrer"
          data-testid="upgrade-release-link"
          className="inline-flex items-center gap-1 underline decoration-amber-400/70 underline-offset-2 hover:text-white"
        >
          View diff / release
          <ExternalLink className="h-3.5 w-3.5" />
        </a>
        <span className="hidden md:inline text-amber-300/80">
          • Withdrawals remain open throughout the timelock window.
        </span>
      </div>
    </div>
  );
}
