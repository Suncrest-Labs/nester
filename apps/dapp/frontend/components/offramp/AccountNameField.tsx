"use client";

// components/offramp/AccountNameField.tsx
// Read-only field that displays the resolved account name.
// Handles all five states from the issue spec:
//   idle | loading | success | not_found | provider_error

import { AlertTriangle, CheckCircle2, Loader2, XCircle } from "lucide-react";
import { cn } from "@/lib/utils";
import type { ResolveState } from "@/hooks/useBankResolver";

interface AccountNameFieldProps {
  resolveState: ResolveState;
  accountName: string | null;
  /** Called when user clicks "proceed anyway" in provider_error state */
  onManualName?: (name: string) => void;
  manualName?: string;
}

export function AccountNameField({
  resolveState,
  accountName,
  onManualName,
  manualName = "",
}: AccountNameFieldProps) {
  return (
    <div className="space-y-1">
      <label className="text-xs text-muted-foreground font-medium block">
        Account name
      </label>

      {/* ── idle ── */}
      {resolveState === "idle" && (
        <div className="w-full px-4 py-3 rounded-xl border border-border bg-secondary/30 min-h-[52px] flex items-center">
          <span className="text-sm text-muted-foreground/60 italic">
            Will auto-fill
          </span>
        </div>
      )}

      {/* ── loading ── */}
      {resolveState === "loading" && (
        <div className="w-full px-4 py-3 rounded-xl border border-border bg-white min-h-[52px] flex items-center gap-2">
          <Loader2 className="h-4 w-4 text-muted-foreground animate-spin shrink-0" />
          <span className="text-sm text-muted-foreground">Looking up account…</span>
        </div>
      )}

      {/* ── success ── */}
      {resolveState === "success" && accountName && (
        <div className="w-full px-4 py-3 rounded-xl border border-emerald-200 bg-emerald-50 min-h-[52px] flex items-center gap-2">
          <CheckCircle2 className="h-4 w-4 text-emerald-600 shrink-0" />
          <span className="text-sm font-medium text-emerald-800 flex-1">
            {accountName}
          </span>
          <span className="text-[10px] text-emerald-600 font-medium shrink-0">
            Verified ✓
          </span>
        </div>
      )}

      {/* ── not_found ── */}
      {resolveState === "not_found" && (
        <div className="space-y-1">
          <div className="w-full px-4 py-3 rounded-xl border border-red-200 bg-red-50 min-h-[52px] flex items-center gap-2">
            <XCircle className="h-4 w-4 text-red-500 shrink-0" />
            <span className="text-sm text-red-700">
              Account not found — check the number and bank
            </span>
          </div>
        </div>
      )}

      {/* ── provider_error ── */}
      {resolveState === "provider_error" && (
        <div className="space-y-2">
          <div className="w-full px-4 py-3 rounded-xl border border-amber-200 bg-amber-50 min-h-[52px] flex items-start gap-2">
            <AlertTriangle className="h-4 w-4 text-amber-600 shrink-0 mt-0.5" />
            <div className="flex-1">
              <p className="text-sm text-amber-800 font-medium">
                Could not verify — you may proceed manually
              </p>
              <p className="text-xs text-amber-700 mt-0.5">
                Bank verification is temporarily unavailable.
              </p>
            </div>
          </div>
          {/* Allow manual name entry on provider outage */}
          {onManualName && (
            <input
              type="text"
              value={manualName}
              onChange={(e) => onManualName(e.target.value)}
              placeholder="Enter account name manually"
              className={cn(
                "w-full px-4 py-3 rounded-xl border border-border bg-white text-sm",
                "text-foreground placeholder:text-muted-foreground/60 outline-none",
                "focus:border-foreground/20 transition-colors min-h-[52px]"
              )}
            />
          )}
        </div>
      )}
    </div>
  );
}