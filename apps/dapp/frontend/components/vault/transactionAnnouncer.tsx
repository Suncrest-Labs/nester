"use client";

import { useEffect, useState } from "react";

/**
 * The transaction lifecycle shared by the deposit and withdraw modals.
 * Mirrors each modal's own ActionState.
 */
export type TransactionPhase =
  | "input"
  | "building"
  | "signing"
  | "submitting"
  | "pending"
  | "success"
  | "error";

interface TransactionAnnouncerProps {
  /** Current phase of the transaction. */
  phase: TransactionPhase;
  /** Error text to announce when phase is "error". */
  errorMessage?: string;
  /** Human-readable amount, e.g. "$250.00 USDC". */
  amountLabel?: string;
  /** "Deposit" or "Withdrawal", used to build the announcement. */
  action: string;
}

// What each phase says out loud. Deliberately plain: a screen reader user
// mid-transaction needs to know what is happening to their money, not to parse
// interface vocabulary.
function phraseFor(
  phase: TransactionPhase,
  action: string,
  amountLabel?: string,
): string {
  const subject = amountLabel ? `${action} of ${amountLabel}` : action;

  switch (phase) {
    case "building":
      return `Preparing ${subject}.`;
    case "signing":
      return `Waiting for you to sign the ${action.toLowerCase()} in your wallet.`;
    case "submitting":
      return `Submitting ${subject} to the network.`;
    case "pending":
      return `${subject} submitted. Waiting for network confirmation.`;
    case "success":
      return `${subject} confirmed.`;
    default:
      return "";
  }
}

/**
 * Announces transaction progress, amount and errors to screen readers
 * (nester#1128).
 *
 * Money-path state changes are conveyed visually by a stepper and a spinner,
 * neither of which a screen reader reports. Without a live region the user
 * signs in their wallet and then hears nothing at all until they navigate the
 * dialog manually to find out whether their money moved.
 *
 * Two regions rather than one, because they have different urgencies:
 * progress is polite so it does not interrupt, while an error is assertive
 * because it means the transaction did not happen.
 */
export function TransactionAnnouncer({
  phase,
  errorMessage,
  amountLabel,
  action,
}: TransactionAnnouncerProps) {
  const [message, setMessage] = useState("");

  useEffect(() => {
    const phrase = phraseFor(phase, action, amountLabel);
    if (!phrase) {
      setMessage("");
      return;
    }

    // Clearing first forces the region to re-announce when two phases
    // produce the same string; assistive tech ignores an unchanged value.
    setMessage("");
    const id = window.setTimeout(() => setMessage(phrase), 50);
    return () => window.clearTimeout(id);
  }, [phase, action, amountLabel]);

  return (
    <>
      <div aria-live="polite" aria-atomic="true" className="sr-only">
        {message}
      </div>
      <div
        aria-live="assertive"
        aria-atomic="true"
        role="alert"
        className="sr-only"
      >
        {phase === "error" && errorMessage
          ? `${action} failed. ${errorMessage}`
          : ""}
      </div>
    </>
  );
}
