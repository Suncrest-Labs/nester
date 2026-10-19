"use client";

import { useEffect, useState } from "react";

import { apiRequest } from "@/lib/api/client";
import { resolvePause, type MoneyPathSwitch, type PauseState } from "@/lib/money-path";

const POLL_INTERVAL_MS = 30_000;

interface MoneyPathStatus {
  deposit: PauseState;
  withdrawal: PauseState;
}

const NOT_PAUSED: MoneyPathStatus = {
  deposit: { paused: false, reason: "" },
  withdrawal: { paused: false, reason: "" },
};

/**
 * Whether deposits and withdrawals are globally paused, from the public
 * status endpoint, so the UI can say so instead of showing a broken flow.
 *
 * An unreachable endpoint reads as "not paused": this only explains a pause,
 * it is not what enforces one.
 */
export function useMoneyPathStatus(): MoneyPathStatus {
  const [status, setStatus] = useState<MoneyPathStatus>(NOT_PAUSED);

  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      try {
        const switches = await apiRequest<MoneyPathSwitch[]>("/money-path/status");
        if (cancelled) return;
        setStatus({
          deposit: resolvePause(switches, "deposit"),
          withdrawal: resolvePause(switches, "withdrawal"),
        });
      } catch {
        if (!cancelled) setStatus(NOT_PAUSED);
      }
    };

    void load();
    const timer = setInterval(load, POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, []);

  return status;
}
