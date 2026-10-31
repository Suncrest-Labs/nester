const DEPOSITS_PAUSED = "Deposits are paused right now. Withdrawals still work.";

export const DEPOSITS_PAUSED_MESSAGE = DEPOSITS_PAUSED;

/** Maps wallet and contract failures to a message a saver can act on. */
export function describeSavingsError(err: unknown): string {
  if (err instanceof Error) {
    if (err.name === "UserRejectedError") {
      return "You cancelled the transaction in your wallet. No funds were moved.";
    }
    if (/Error\(Contract, #5\)|DepositsPaused/.test(err.message)) {
      return DEPOSITS_PAUSED;
    }
    if (/Error\(Contract, #6\)|AdapterUnavailable/.test(err.message)) {
      return "A lending venue can't be read right now, so deposits are unavailable. Withdrawals still work. Try again shortly.";
    }
    if (err.name === "TransactionTimeoutError") {
      return "The transaction timed out. Check the network explorer for its status before retrying.";
    }
    return err.message;
  }
  return "Something went wrong. Please try again.";
}
