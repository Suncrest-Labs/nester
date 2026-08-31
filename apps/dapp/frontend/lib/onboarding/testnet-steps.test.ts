import { describe, expect, it } from "vitest";

import {
  DISMISSED_KEY,
  type OnboardingSignals,
  completedSteps,
  currentStep,
  isComplete,
  progress,
  readDismissed,
  writeDismissed,
} from "./testnet-steps";

const nothingDone: OnboardingSignals = {
  walletAvailable: false,
  walletConnected: false,
  onTestnet: null,
  funded: false,
  hasDeposit: false,
};

function signals(overrides: Partial<OnboardingSignals>): OnboardingSignals {
  return { ...nothingDone, ...overrides };
}

describe("onboarding step machine", () => {
  it("starts a brand-new visitor on the install step", () => {
    expect(currentStep(nothingDone)).toBe("install");
    expect(progress(nothingDone)).toBe(0);
  });

  // The flow has to advance on its own as each condition becomes true, rather
  // than on a "next" click — that is what the acceptance criteria call
  // "detects completion and advances automatically".
  it("advances as each condition becomes true", () => {
    expect(currentStep(signals({ walletAvailable: true }))).toBe("install");

    expect(currentStep(signals({ walletAvailable: true, walletConnected: true }))).toBe("network");

    expect(
      currentStep(signals({ walletAvailable: true, walletConnected: true, onTestnet: true })),
    ).toBe("fund");

    expect(
      currentStep(
        signals({ walletAvailable: true, walletConnected: true, onTestnet: true, funded: true }),
      ),
    ).toBe("deposit");
  });

  it("reports completion once a deposit exists", () => {
    const done = signals({
      walletAvailable: true,
      walletConnected: true,
      onTestnet: true,
      funded: true,
      hasDeposit: true,
    });

    expect(currentStep(done)).toBeNull();
    expect(isComplete(done)).toBe(true);
    expect(progress(done)).toBe(1);
  });

  // Resuming after a reload is the same computation: the state is derived
  // from the wallet and chain, not from a stored step counter, so a reload
  // lands the user exactly where they were.
  it("resumes mid-flow from observable state alone", () => {
    const midFlow = signals({ walletAvailable: true, walletConnected: true, onTestnet: true });

    expect(currentStep(midFlow)).toBe("fund");
    // Recomputing from the same signals — as a fresh page load would — gives
    // the same answer, with no stored progress involved.
    expect(currentStep({ ...midFlow })).toBe("fund");
  });

  // Losing a prerequisite has to move the flow back. Otherwise a user who
  // switches to mainnet sits on "make your first deposit" while every
  // transaction fails.
  it("moves back when a prerequisite is lost", () => {
    const funded = signals({
      walletAvailable: true,
      walletConnected: true,
      onTestnet: true,
      funded: true,
    });
    expect(currentStep(funded)).toBe("deposit");

    expect(currentStep({ ...funded, onTestnet: false })).toBe("network");
    expect(currentStep({ ...funded, walletConnected: false })).toBe("install");
  });

  // Not every wallet module implements getNetwork. An unknown network must not
  // count as "on testnet" on its own, or a mainnet user is walked into
  // friendbot funding that cannot work.
  it("does not treat an unknown network as testnet", () => {
    const unknownNetwork = signals({ walletAvailable: true, walletConnected: true, onTestnet: null });
    expect(currentStep(unknownNetwork)).toBe("network");
  });

  // Being funded is the evidence that settles it: a funded account proves the
  // wallet is pointed somewhere friendbot could reach.
  it("accepts an unknown network once the account is funded", () => {
    const unknownButFunded = signals({
      walletAvailable: true,
      walletConnected: true,
      onTestnet: null,
      funded: true,
    });
    expect(currentStep(unknownButFunded)).toBe("deposit");
  });

  it("never marks a later step done while an earlier one is outstanding", () => {
    // A user with a deposit but a disconnected wallet: the deposit flag must
    // not carry the flow past the steps in front of it.
    const done = completedSteps(signals({ hasDeposit: true, funded: true, onTestnet: true }));
    expect(done.install).toBe(false);
    expect(done.network).toBe(false);
    expect(done.fund).toBe(false);
    expect(done.deposit).toBe(false);
  });
});

describe("dismissal persistence", () => {
  function memoryStorage() {
    const map = new Map<string, string>();
    return {
      getItem: (k: string) => map.get(k) ?? null,
      setItem: (k: string, v: string) => void map.set(k, v),
      removeItem: (k: string) => void map.delete(k),
      raw: map,
    };
  }

  it("round-trips the dismissal", () => {
    const store = memoryStorage();
    expect(readDismissed(store)).toBe(false);

    writeDismissed(true, store);
    expect(store.raw.get(DISMISSED_KEY)).toBe("true");
    expect(readDismissed(store)).toBe(true);

    writeDismissed(false, store);
    expect(readDismissed(store)).toBe(false);
  });

  // Private windows and storage-blocking browsers make these throw. The page
  // must still render, so both helpers swallow the failure.
  it("survives storage that throws", () => {
    const hostile = {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
      removeItem: () => {
        throw new Error("blocked");
      },
    };

    expect(readDismissed(hostile)).toBe(false);
    expect(() => writeDismissed(true, hostile)).not.toThrow();
  });
});
