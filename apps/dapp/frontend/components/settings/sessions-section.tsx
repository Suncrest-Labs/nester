"use client";

import { useCallback, useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { api, type SessionView } from "@/lib/api/client";
import { useAuth } from "@/components/auth-provider";
import { BUTTON_SECONDARY, PANEL } from "@/components/ui/page-kit";
import { cn } from "@/lib/utils";

function formatRelative(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return iso;
  const diffMs = Date.now() - then;
  const minutes = Math.round(diffMs / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days}d ago`;
}

export function SessionsSection() {
  const { signOutAllDevices } = useAuth();
  const [sessions, setSessions] = useState<SessionView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [terminatingId, setTerminatingId] = useState<string | null>(null);
  const [signingOutAll, setSigningOutAll] = useState(false);

  const load = useCallback(async () => {
    try {
      const { sessions: list } = await api.auth.listSessions();
      setSessions(list);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load sessions");
      setSessions([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    api.auth
      .listSessions()
      .then(({ sessions: list }) => {
        if (!cancelled) setSessions(list);
      })
      .catch((e) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : "Failed to load sessions");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const handleTerminate = async (id: string) => {
    setTerminatingId(id);
    setError(null);
    try {
      await api.auth.revokeSession(id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not sign out that device");
    } finally {
      setTerminatingId(null);
    }
  };

  const handleSignOutAll = async () => {
    setSigningOutAll(true);
    try {
      await signOutAllDevices();
    } finally {
      setSigningOutAll(false);
    }
  };

  return (
    <div className={PANEL}>
      <div className="flex flex-wrap items-center justify-between gap-4">
        <p className="text-sm text-muted-foreground">Devices currently signed in to your account.</p>
        {sessions.length > 0 && (
          <button
            type="button"
            onClick={() => void handleSignOutAll()}
            disabled={signingOutAll}
            className={cn(BUTTON_SECONDARY, "text-red-600 dark:text-red-400")}
          >
            {signingOutAll ? "Signing out…" : "Sign out of all devices"}
          </button>
        )}
      </div>

      {error && (
        <p className="mt-4 text-sm text-red-600 dark:text-red-400" role="alert">
          {error}
        </p>
      )}

      {loading ? (
        <div className="mt-5 flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading sessions…
        </div>
      ) : sessions.length === 0 ? (
        <p className="mt-5 text-sm text-muted-foreground">
          No active sessions. Sign in with your wallet to start one.
        </p>
      ) : (
        <ul className="mt-5 divide-y border-t">
          {sessions.map((s) => (
            <li key={s.id} className="flex items-center justify-between gap-3 py-4">
              <div className="min-w-0">
                <p className="truncate text-sm text-foreground">
                  {s.user_agent ?? "Unknown device"}
                  {s.is_current && (
                    <span className="ml-2 inline-flex rounded-full border px-2 py-0.5 text-[11px] text-muted-foreground">
                      This device
                    </span>
                  )}
                </p>
                <p className="mt-0.5 truncate text-sm text-muted-foreground">
                  {s.ip_address ?? "Unknown location"} · last active {formatRelative(s.last_active_at)}
                </p>
              </div>
              {!s.is_current && (
                <button
                  type="button"
                  onClick={() => void handleTerminate(s.id)}
                  disabled={terminatingId === s.id}
                  className="shrink-0 text-[13px] font-medium text-muted-foreground transition-colors hover:text-red-600 disabled:opacity-50 dark:hover:text-red-400"
                >
                  {terminatingId === s.id ? "Signing out…" : "Sign out"}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
