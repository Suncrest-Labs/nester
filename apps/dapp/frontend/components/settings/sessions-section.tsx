"use client";

import { useCallback, useEffect, useState } from "react";
import { Loader2, Monitor, ShieldAlert } from "lucide-react";
import { api, type SessionView } from "@/lib/api/client";
import { useAuth } from "@/components/auth-provider";
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

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const { sessions: list } = await api.auth.listSessions();
      setSessions(list);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load sessions");
      setSessions([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const handleTerminate = async (id: string) => {
    setTerminatingId(id);
    setError(null);
    try {
      await api.auth.revokeSession(id);
      await refresh();
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
    <section className="rounded-2xl border border-black/[0.06] dark:border-white/[0.06] bg-white dark:bg-[#100F0F] p-6">
      <div className="flex items-center justify-between gap-4 mb-6">
        <div>
          <h2 className="text-lg font-semibold text-black dark:text-white">Active sessions</h2>
          <p className="text-sm text-black/50 dark:text-white/50 mt-1">
            Devices currently signed in to your account.
          </p>
        </div>
        {sessions.length > 0 && (
          <button
            type="button"
            onClick={() => void handleSignOutAll()}
            disabled={signingOutAll}
            className="flex items-center gap-1.5 rounded-xl border border-red-600/20 px-4 py-2 text-sm font-medium text-red-600 hover:bg-red-600/5 disabled:opacity-40"
          >
            <ShieldAlert className="h-4 w-4" />
            {signingOutAll ? "Signing out…" : "Sign out of all devices"}
          </button>
        )}
      </div>

      {error && (
        <p className="mb-4 text-sm text-red-600" role="alert">
          {error}
        </p>
      )}

      {loading ? (
        <div className="flex items-center gap-2 text-black/40 text-sm py-8">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading sessions…
        </div>
      ) : sessions.length === 0 ? (
        <p className="text-sm text-black/40 py-4">No active sessions.</p>
      ) : (
        <ul className="space-y-3">
          {sessions.map((s) => (
            <li
              key={s.id}
              className="flex items-center justify-between gap-3 rounded-xl border border-black/[0.06] dark:border-white/[0.06] px-4 py-3"
            >
              <div className="flex items-start gap-3 min-w-0">
                <Monitor className="h-5 w-5 text-black/30 dark:text-white/30 shrink-0 mt-0.5" />
                <div className="min-w-0">
                  <p className="font-medium text-black dark:text-white truncate">
                    {s.user_agent ?? "Unknown device"}
                    {s.is_current && (
                      <span className="ml-2 inline-flex items-center rounded-md bg-emerald-50 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-700">
                        This device
                      </span>
                    )}
                  </p>
                  <p className="text-sm text-black/50 dark:text-white/50 truncate">
                    {s.ip_address ?? "Unknown location"} · last active {formatRelative(s.last_active_at)}
                  </p>
                </div>
              </div>
              {!s.is_current && (
                <button
                  type="button"
                  onClick={() => void handleTerminate(s.id)}
                  disabled={terminatingId === s.id}
                  className={cn(
                    "shrink-0 text-xs font-medium text-black/50 dark:text-white/50 hover:text-red-600",
                    terminatingId === s.id && "opacity-40"
                  )}
                >
                  {terminatingId === s.id ? "Signing out…" : "Sign out"}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
