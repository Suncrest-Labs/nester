"use client";

import { useState } from "react";
import { addBankAccount } from "@/lib/api/bank-accounts";

interface SaveAccountPromptProps {
  bankName: string;
  bankCode: string;
  accountNumber: string;
  accountName: string;
  currency: string;
  country: string;
  onDismiss: () => void;
  onSaved: () => void;
}

export function SaveAccountPrompt({
  bankName,
  bankCode,
  accountNumber,
  accountName,
  currency,
  country,
  onDismiss,
  onSaved,
}: SaveAccountPromptProps) {
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      await addBankAccount({
        bank_name: bankName,
        bank_code: bankCode,
        account_number: accountNumber,
        account_name: accountName,
        currency,
        country,
        is_default: true,
      });
      onSaved();
      onDismiss();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not save account");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="rounded-xl border border-emerald-200 bg-emerald-50/80 p-4 space-y-3">
      <p className="text-sm font-medium text-emerald-900">
        Save this account for next time?
      </p>
      <p className="text-xs text-emerald-800/80">
        Skip re-entering details on your next monthly withdrawal.
      </p>
      {error && <p className="text-xs text-red-600">{error}</p>}
      <div className="flex gap-2">
        <button
          type="button"
          onClick={() => void handleSave()}
          disabled={saving}
          className="rounded-lg bg-emerald-700 px-3 py-2 text-xs font-medium text-white hover:bg-emerald-800 disabled:opacity-50"
        >
          {saving ? "Saving…" : "Save account"}
        </button>
        <button
          type="button"
          onClick={onDismiss}
          className="rounded-lg px-3 py-2 text-xs font-medium text-emerald-800 hover:bg-emerald-100"
        >
          Not now
        </button>
      </div>
    </div>
  );
}
