"use client";

import { useEffect, useState } from "react";
import { Building2, Loader2, Plus } from "lucide-react";
import {
  listBankAccounts,
  type SavedBankAccount,
} from "@/lib/api/bank-accounts";
import { cn } from "@/lib/utils";

export type PayoutMode = { type: "saved"; account: SavedBankAccount } | { type: "manual" };

interface SavedBankAccountPickerProps {
  currency: string;
  value: PayoutMode;
  onChange: (mode: PayoutMode) => void;
}

export function SavedBankAccountPicker({
  currency,
  value,
  onChange,
}: SavedBankAccountPickerProps) {
  const [accounts, setAccounts] = useState<SavedBankAccount[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    listBankAccounts()
      .then((data) => {
        if (!cancelled) {
          const filtered = data.filter((a) => a.currency === currency);
          setAccounts(filtered);
          if (filtered.length > 0 && value.type === "manual") {
            const preferred = filtered.find((a) => a.is_default) ?? filtered[0];
            onChange({ type: "saved", account: preferred });
          }
        }
      })
      .catch(() => {
        if (!cancelled) setAccounts([]);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only re-fetch when currency changes
  }, [currency]);

  if (loading) {
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        Loading saved accounts…
      </div>
    );
  }

  if (accounts.length === 0) {
    return null;
  }

  return (
    <div className="space-y-2 mb-4">
      <p className="text-xs font-medium text-muted-foreground">Saved accounts</p>
      <div className="flex flex-wrap gap-2">
        {accounts.map((acct) => {
          const selected =
            value.type === "saved" && value.account.id === acct.id;
          return (
            <button
              key={acct.id}
              type="button"
              onClick={() => onChange({ type: "saved", account: acct })}
              className={cn(
                "flex items-center gap-2 rounded-xl border px-3 py-2 text-left text-xs transition-colors min-h-[44px]",
                selected
                  ? "border-foreground bg-foreground/5"
                  : "border-border hover:border-foreground/20",
              )}
            >
              <Building2 className="h-4 w-4 shrink-0 text-muted-foreground" />
              <span>
                <span className="font-medium block">{acct.bank_name}</span>
                <span className="text-muted-foreground">
                  ••••{acct.account_last4}
                  {acct.is_default ? " · Default" : ""}
                </span>
              </span>
            </button>
          );
        })}
        <button
          type="button"
          onClick={() => onChange({ type: "manual" })}
          className={cn(
            "flex items-center gap-1.5 rounded-xl border px-3 py-2 text-xs font-medium min-h-[44px]",
            value.type === "manual"
              ? "border-foreground bg-foreground/5"
              : "border-dashed border-border text-muted-foreground hover:border-foreground/20",
          )}
        >
          <Plus className="h-3.5 w-3.5" />
          Use a different account
        </button>
      </div>
    </div>
  );
}
