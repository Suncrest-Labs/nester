export type MoneyPathOperation = "deposit" | "withdrawal";

/** One row of GET /api/v1/money-path/status. */
export interface MoneyPathSwitch {
  operation: string;
  paused: boolean;
  reason: string;
  updated_at: string;
}

export interface PauseState {
  paused: boolean;
  reason: string;
}

/**
 * The pause state of one operation. A missing operation or list is "not
 * paused": the on-chain contract and the API still enforce a real pause, so a
 * gap in this read must not lock users out of a working vault.
 */
export function resolvePause(
  switches: readonly MoneyPathSwitch[] | undefined,
  operation: MoneyPathOperation
): PauseState {
  const match = switches?.find((s) => s.operation === operation);
  return match ? { paused: match.paused, reason: match.reason } : { paused: false, reason: "" };
}

/** Honest text for a paused operation, with the operator's reason if given. */
export function pauseMessage(operation: MoneyPathOperation, reason: string): string {
  const noun = operation === "deposit" ? "Deposits" : "Withdrawals";
  const trimmed = reason.trim();
  return trimmed
    ? `${noun} are temporarily paused: ${trimmed}`
    : `${noun} are temporarily paused for maintenance.`;
}
