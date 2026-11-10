"use client";

import { useEffect, useState } from "react";
import { Card } from "@/components/ui/card";
import type { SavingsPending } from "@/lib/api/savings";

const TICK_MS = 30_000;

function humanizeKind(kind: string): string {
  const spaced = kind.replace(/([a-z])([A-Z])/g, "$1 $2").toLowerCase();
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

function countdown(secondsLeft: number): string {
  if (secondsLeft <= 0) return "Ready to execute";
  const d = Math.floor(secondsLeft / 86_400);
  const h = Math.floor((secondsLeft % 86_400) / 3_600);
  const m = Math.floor((secondsLeft % 3_600) / 60);
  if (d > 0) return `Executable in ${d}d ${h}h`;
  if (h > 0) return `Executable in ${h}h ${m}m`;
  return `Executable in ${Math.max(m, 1)}m`;
}

interface PendingChangesCardProps {
  pending: SavingsPending[];
  /** Unix seconds; defaults to the wall clock. Exposed for tests. */
  now?: number;
}

export function PendingChangesCard({ pending, now }: PendingChangesCardProps) {
  const [clock, setClock] = useState(() => Math.floor(Date.now() / 1000));

  useEffect(() => {
    if (now !== undefined) return;
    const id = setInterval(() => setClock(Math.floor(Date.now() / 1000)), TICK_MS);
    return () => clearInterval(id);
  }, [now]);

  if (pending.length === 0) return null;
  const current = now ?? clock;

  return (
    <Card className="bg-card">
      <h3 className="text-sm font-medium text-muted-foreground">Pending changes</h3>
      <ul className="mt-3 divide-y divide-border">
        {pending.map((change) => (
          <li key={change.id} className="flex items-center justify-between gap-4 py-2 text-sm">
            <span className="text-foreground">{humanizeKind(change.kind)}</span>
            <span className="tabular-nums text-muted-foreground">
              {countdown(change.eta - current)}
            </span>
          </li>
        ))}
      </ul>
    </Card>
  );
}
