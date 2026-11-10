const DEPOSITS_PAUSED = "Deposits are paused right now. Withdrawals still work.";

export const DEPOSITS_PAUSED_MESSAGE = DEPOSITS_PAUSED;

const CONTRACT_MESSAGES = new Map<number, string>(Object.entries({
  1: "Enter an amount greater than zero.",
  2: "The price moved while you were confirming. Try again, or raise your slippage tolerance.",
  3: "That is more shares than you hold. Lower the amount and try again.",
  4: "There is not enough liquid USDC to pay that out right now. Try a smaller amount or try again later.",
  5: DEPOSITS_PAUSED,
  6: "A lending venue can't be read right now, so deposits are unavailable. Withdrawals still work. Try again shortly.",
}).map(([k, v]) => [Number(k), v]));

const NAMED_MESSAGES: [RegExp, number][] = [
  [/ZeroAmount/, 1],
  [/Slippage/, 2],
  [/InsufficientShares/, 3],
  [/InsufficientLiquidity/, 4],
  [/DepositsPaused/, 5],
  [/AdapterUnavailable/, 6],
];

/** Contract code of the first top-level HostError; later diagnostic-log entries may carry nested adapter errors. */
function contractCode(message: string): number | null {
  const m = /HostError: Error\(Contract, #(\d+)\)/.exec(message) ?? /Error\(Contract, #(\d+)\)/.exec(message);
  return m ? Number(m[1]) : null;
}

/** Maps wallet and contract failures to a message a saver can act on. */
export function describeSavingsError(err: unknown): string {
  if (err instanceof Error) {
    if (err.name === "UserRejectedError") {
      return "You cancelled the transaction in your wallet. No funds were moved.";
    }
    const code = contractCode(err.message);
    if (code !== null) {
      const mapped = CONTRACT_MESSAGES.get(code);
      if (mapped) return mapped;
    } else {
      const named = NAMED_MESSAGES.find(([re]) => re.test(err.message));
      if (named) return CONTRACT_MESSAGES.get(named[1]) ?? err.message;
    }
    if (err.name === "TransactionTimeoutError") {
      return "The transaction timed out. Check the network explorer for its status before retrying.";
    }
    return err.message;
  }
  return "Something went wrong. Please try again.";
}
