"use client";

import { useState } from "react";
import { usePathname } from "next/navigation";
import { useWallet } from "@/components/wallet-provider";

const APP_VERSION = process.env.NEXT_PUBLIC_APP_VERSION || "0.1.0";
const SUPPORT_EMAIL = process.env.NEXT_PUBLIC_SUPPORT_EMAIL || "support@example.com";

interface ReportProblemButtonProps {
  /** Most recent transaction hash on this screen, if any (Issue #1143). */
  lastTransactionHash?: string | null;
  /** Most recent error message shown on this screen, if any. */
  lastError?: string | null;
}

/**
 * A "report a problem" control for money-path screens (Issue #1143).
 * Attaches screen, wallet address, app version, last transaction hash, and
 * last error automatically — never a secret key or session token — and
 * shows the user exactly what will be sent before it goes.
 *
 * Submission opens the user's mail client with the report pre-filled
 * rather than requiring a new backend endpoint; wiring this to a ticketing
 * system instead is real follow-up work, not done here.
 */
export function ReportProblemButton({ lastTransactionHash, lastError }: ReportProblemButtonProps) {
  const [open, setOpen] = useState(false);
  const [description, setDescription] = useState("");
  const pathname = usePathname();
  const { address } = useWallet();

  const report = {
    screen: pathname,
    wallet_address: address || "not connected",
    app_version: APP_VERSION,
    last_transaction_hash: lastTransactionHash || "none",
    last_error: lastError || "none",
    description,
  };

  const handleSend = () => {
    const subject = encodeURIComponent(`Nester problem report: ${report.screen}`);
    const body = encodeURIComponent(
      [
        `Screen: ${report.screen}`,
        `Wallet address: ${report.wallet_address}`,
        `App version: ${report.app_version}`,
        `Last transaction hash: ${report.last_transaction_hash}`,
        `Last error: ${report.last_error}`,
        "",
        "Description:",
        report.description || "(none provided)",
      ].join("\n")
    );
    window.location.href = `mailto:${SUPPORT_EMAIL}?subject=${subject}&body=${body}`;
    setOpen(false);
  };

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground"
      >
        Report a problem
      </button>
    );
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="w-full max-w-md rounded-lg bg-background p-4 shadow-lg">
        <h2 className="mb-2 text-sm font-semibold">Report a problem</h2>
        <p className="mb-3 text-xs text-muted-foreground">
          This is exactly what will be sent — nothing else.
        </p>
        <pre className="mb-3 max-h-40 overflow-auto rounded bg-muted p-2 text-xs">
          {JSON.stringify(
            {
              screen: report.screen,
              wallet_address: report.wallet_address,
              app_version: report.app_version,
              last_transaction_hash: report.last_transaction_hash,
              last_error: report.last_error,
            },
            null,
            2
          )}
        </pre>
        <textarea
          className="mb-3 w-full rounded border p-2 text-xs"
          rows={3}
          placeholder="What happened? (optional)"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={() => setOpen(false)}
            className="rounded px-3 py-1 text-xs"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={handleSend}
            className="rounded bg-primary px-3 py-1 text-xs text-primary-foreground"
          >
            Send report
          </button>
        </div>
      </div>
    </div>
  );
}
