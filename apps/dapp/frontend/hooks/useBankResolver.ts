// hooks/useBankResolver.ts
// Manages the account name auto-resolve lifecycle:
//   idle → loading → success | not_found | provider_error
//
// Fires automatically 400ms after both accountNumber (10 digits) and
// bankCode are non-empty, matching the debounce spec in issue #216.

import { useEffect, useRef, useState } from "react";
import { resolveAccountName, type AccountInfo } from "@/lib/api/bank";

export type ResolveState =
  | "idle"        // fields not filled yet
  | "loading"     // waiting for API response
  | "success"     // account name resolved
  | "not_found"   // account doesn't exist
  | "provider_error"; // both providers down — allow manual entry

export interface UseBankResolverResult {
  resolveState: ResolveState;
  accountInfo: AccountInfo | null;
}

const DEBOUNCE_MS = 500;

export function useBankResolver(
  accountNumber: string,
  bankCode: string,
  country = "NG"
): UseBankResolverResult {
  const [resolveState, setResolveState] = useState<ResolveState>("idle");
  const [accountInfo, setAccountInfo] = useState<AccountInfo | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Track the latest request so stale responses are discarded.
  const requestIdRef = useRef(0);

  const [prevInputs, setPrevInputs] = useState({ accountNumber, bankCode, country });
  const ready =
    accountNumber.length === 10 &&
    /^\d{10}$/.test(accountNumber) &&
    bankCode.length > 0;

  // Reset synchronously if inputs change and become invalid
  if (
    (prevInputs.accountNumber !== accountNumber ||
      prevInputs.bankCode !== bankCode ||
      prevInputs.country !== country)
  ) {
    setPrevInputs({ accountNumber, bankCode, country });
    if (!ready) {
      setResolveState("idle");
      setAccountInfo(null);
    }
  }

  useEffect(() => {
    // Clear any pending debounce timer.
    if (timerRef.current) clearTimeout(timerRef.current);

    if (!ready) {
      return;
    }

    timerRef.current = setTimeout(async () => {
      // Bump request ID so any previous in-flight response is ignored.
      const currentId = ++requestIdRef.current;

      setResolveState("loading");
      setAccountInfo(null);

      const result = await resolveAccountName(accountNumber, bankCode, country);

      // Discard stale responses.
      if (currentId !== requestIdRef.current) return;

      if (result.status === "success") {
        setAccountInfo(result.data);
        setResolveState("success");
      } else if (result.status === "not_found") {
        setAccountInfo(null);
        setResolveState("not_found");
      } else {
        setAccountInfo(null);
        setResolveState("provider_error");
      }
    }, DEBOUNCE_MS);

    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [accountNumber, bankCode, country, ready]);

  return { resolveState, accountInfo };
}