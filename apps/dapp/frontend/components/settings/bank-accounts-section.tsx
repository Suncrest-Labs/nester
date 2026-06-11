"use client";

import { useCallback, useEffect, useState } from "react";
import { Building2, Loader2, Star, Trash2 } from "lucide-react";
import { BankCombobox } from "@/components/offramp/BankCombobox";
import { AccountNameField } from "@/components/offramp/AccountNameField";
import { useBankResolver } from "@/hooks/useBankResolver";
import {
  addBankAccount,
  listBankAccounts,
  removeBankAccount,
  setDefaultBankAccount,
  type SavedBankAccount,
} from "@/lib/api/bank-accounts";
import { cn } from "@/lib/utils";

const CURRENCY_FLAGS: Record<string, string> = {
  NGN: "🇳🇬",
  GHS: "🇬🇭",
  KES: "🇰🇪",
};

export function BankAccountsSection() {
  const [accounts, setAccounts] = useState<SavedBankAccount[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [accountNumber, setAccountNumber] = useState("");
  const [bankCode, setBankCode] = useState("");
  const [currency, setCurrency] = useState("NGN");
  const [country, setCountry] = useState("NG");
  const [saving, setSaving] = useState(false);

  const { resolveState, accountInfo } = useBankResolver(accountNumber, bankCode, country);
  const resolvedName = resolveState === "success" ? accountInfo?.account_name ?? null : null;

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await listBankAccounts();
      setAccounts(data);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load accounts");
      setAccounts([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const handleAdd = async () => {
    if (!resolvedName || accountNumber.length !== 10 || !bankCode) return;
    setSaving(true);
    setError(null);
    try {
      await addBankAccount({
        bank_name: accountInfo?.bank_name || "Bank",
        bank_code: bankCode,
        account_number: accountNumber,
        account_name: resolvedName,
        currency,
        country,
        is_default: accounts.length === 0,
      });
      setShowAdd(false);
      setAccountNumber("");
      setBankCode("");
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not save account");
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="rounded-2xl border border-black/[0.06] bg-white p-6">
      <div className="flex items-center justify-between gap-4 mb-6">
        <div>
          <h2 className="text-lg font-semibold text-black">Bank accounts</h2>
          <p className="text-sm text-black/50 mt-1">
            Save accounts for faster monthly offramps.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowAdd((v) => !v)}
          className="rounded-xl bg-black px-4 py-2 text-sm font-medium text-white hover:bg-black/90"
        >
          {showAdd ? "Cancel" : "Add account"}
        </button>
      </div>

      {error && (
        <p className="mb-4 text-sm text-red-600" role="alert">
          {error}
        </p>
      )}

      {loading ? (
        <div className="flex items-center gap-2 text-black/40 text-sm py-8">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading saved accounts…
        </div>
      ) : accounts.length === 0 ? (
        <p className="text-sm text-black/40 py-4">No saved accounts yet.</p>
      ) : (
        <ul className="space-y-3">
          {accounts.map((acct) => (
            <li
              key={acct.id}
              className="flex items-center justify-between gap-3 rounded-xl border border-black/[0.06] px-4 py-3"
            >
              <div className="flex items-start gap-3 min-w-0">
                <Building2 className="h-5 w-5 text-black/30 shrink-0 mt-0.5" />
                <div className="min-w-0">
                  <p className="font-medium text-black truncate">
                    {CURRENCY_FLAGS[acct.currency] ?? ""} {acct.bank_name}
                    {acct.is_default && (
                      <span className="ml-2 inline-flex items-center gap-0.5 rounded-md bg-emerald-50 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-700">
                        <Star className="h-3 w-3" /> Default
                      </span>
                    )}
                  </p>
                  <p className="text-sm text-black/50 truncate">
                    {acct.account_name} · ••••{acct.account_last4}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                {!acct.is_default && (
                  <button
                    type="button"
                    className="text-xs font-medium text-black/50 hover:text-black"
                    onClick={() => void setDefaultBankAccount(acct.id).then(refresh)}
                  >
                    Set default
                  </button>
                )}
                <button
                  type="button"
                  aria-label="Remove account"
                  className="p-2 text-black/30 hover:text-red-600"
                  onClick={() => void removeBankAccount(acct.id).then(refresh)}
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {showAdd && (
        <div className="mt-6 pt-6 border-t border-black/[0.06] space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <label className="text-xs font-medium text-black/50">
              Currency
              <select
                value={currency}
                onChange={(e) => {
                  setCurrency(e.target.value);
                  setCountry(e.target.value === "NGN" ? "NG" : e.target.value === "GHS" ? "GH" : "KE");
                }}
                className="mt-1 w-full rounded-xl border border-black/10 px-3 py-2 text-sm"
              >
                <option value="NGN">NGN</option>
                <option value="GHS">GHS</option>
                <option value="KES">KES</option>
              </select>
            </label>
          </div>
          <div>
            <label className="text-xs font-medium text-black/50">Account number</label>
            <input
              value={accountNumber}
              onChange={(e) => setAccountNumber(e.target.value.replace(/\D/g, "").slice(0, 10))}
              className="mt-1 w-full rounded-xl border border-black/10 px-3 py-2 text-sm"
              placeholder="10-digit account number"
            />
          </div>
          {accountNumber.length === 10 && (
            <BankCombobox
              value={bankCode}
              onChange={setBankCode}
              country={country}
            />
          )}
          <AccountNameField resolveState={resolveState} accountName={resolvedName} />
          <button
            type="button"
            disabled={saving || resolveState !== "success" || !bankCode}
            onClick={() => void handleAdd()}
            className={cn(
              "w-full rounded-xl py-3 text-sm font-medium text-white",
              saving || resolveState !== "success" ? "bg-black/30" : "bg-black hover:bg-black/90",
            )}
          >
            {saving ? "Saving…" : "Save account"}
          </button>
        </div>
      )}
    </section>
  );
}
