/**
 * Turns the host error a rejected deposit simulation carries into text a user
 * can act on. Codes are ContractError in
 * packages/contracts/libs/common/src/errors.rs; keep the numbers in step.
 *
 * Only codes with a clearly better message are mapped. Anything else returns
 * null so the caller keeps the raw reason rather than a wrong guess.
 */
const DEPOSIT_REJECTIONS: Readonly<Record<number, string>> = {
  // Unauthorized: with the deposit allowlist on, this is the only way an
  // un-listed wallet is refused (the caller's own signature is always present).
  3: "Deposits are limited to approved early-access accounts right now. This wallet is not on the list.",
  10: "This vault has reached its deposit limit. Try a smaller amount or check back later.",
  11: "Deposits are paused by the safety circuit breaker. Try again later.",
  21: "That amount is below the vault's minimum deposit.",
};

const CONTRACT_ERROR_CODE = /Error\(Contract, #(\d+)\)/;

export function describeDepositRejection(reason: string): string | null {
  const match = CONTRACT_ERROR_CODE.exec(reason);
  if (!match) return null;
  return DEPOSIT_REJECTIONS[Number(match[1])] ?? null;
}
