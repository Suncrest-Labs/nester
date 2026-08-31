/**
 * First-run onboarding through to a funded testnet vault (nester#1127).
 *
 * The step machine lives here, apart from React, so the rules that decide
 * which step a user is on can be tested directly rather than through the DOM.
 */

export type StepId = "install" | "network" | "fund" | "deposit";

export const STEP_ORDER: StepId[] = ["install", "network", "fund", "deposit"];

/** Everything the stepper needs to decide where a user is. */
export interface OnboardingSignals {
  /** A wallet extension was detected in the browser. */
  walletAvailable: boolean;
  /** The wallet is connected and has given us an address. */
  walletConnected: boolean;
  /**
   * The wallet reports it is on testnet. `null` means the wallet does not
   * expose its network — not every wallet module implements getNetwork.
   */
  onTestnet: boolean | null;
  /** The account exists on-chain with a non-zero XLM balance. */
  funded: boolean;
  /** The user holds at least one vault position. */
  hasDeposit: boolean;
}

/**
 * completedSteps reports which steps are already satisfied.
 *
 * Each step is derived from observable state rather than from a "user clicked
 * next" counter, which is what lets the flow advance on its own and resume
 * correctly after a reload: a user who already has a funded wallet should
 * never be walked through installing one.
 */
export function completedSteps(signals: OnboardingSignals): Record<StepId, boolean> {
  const install = signals.walletAvailable && signals.walletConnected;

  // A wallet that does not report its network cannot be proven to be on
  // testnet. Treating unknown as "done" would advance a user on mainnet into
  // the funding step, where friendbot would fail with nothing explaining why;
  // being funded is the evidence that resolves it.
  const network = install && (signals.onTestnet === true || (signals.onTestnet === null && signals.funded));

  const fund = network && signals.funded;
  const deposit = fund && signals.hasDeposit;

  return { install, network, fund, deposit };
}

/**
 * currentStep is the first step that is not yet complete, or null when the
 * whole flow is done.
 *
 * Earlier steps are checked first so that losing a prerequisite — the user
 * disconnects, or switches the wallet to mainnet — moves the flow back rather
 * than stranding them on a later step they can no longer complete.
 */
export function currentStep(signals: OnboardingSignals): StepId | null {
  const done = completedSteps(signals);
  return STEP_ORDER.find((step) => !done[step]) ?? null;
}

export function isComplete(signals: OnboardingSignals): boolean {
  return currentStep(signals) === null;
}

/** Progress as a fraction, for the progress bar. */
export function progress(signals: OnboardingSignals): number {
  const done = completedSteps(signals);
  const count = STEP_ORDER.filter((step) => done[step]).length;
  return count / STEP_ORDER.length;
}

export const DISMISSED_KEY = "nester.onboarding.testnet.dismissed";

/**
 * Whether the user has dismissed the flow.
 *
 * Storage is read defensively: a private window, cleared site data, or a
 * browser configured to block storage all make this throw, and none of them
 * are a reason to fail to render the page.
 */
export function readDismissed(storage?: Pick<Storage, "getItem">): boolean {
  try {
    const store = storage ?? (typeof window !== "undefined" ? window.localStorage : undefined);
    return store?.getItem(DISMISSED_KEY) === "true";
  } catch {
    return false;
  }
}

export function writeDismissed(dismissed: boolean, storage?: Pick<Storage, "setItem" | "removeItem">): void {
  try {
    const store = storage ?? (typeof window !== "undefined" ? window.localStorage : undefined);
    if (!store) return;
    if (dismissed) {
      store.setItem(DISMISSED_KEY, "true");
    } else {
      store.removeItem(DISMISSED_KEY);
    }
  } catch {
    // Losing the dismissal is a smaller failure than breaking the page.
  }
}
