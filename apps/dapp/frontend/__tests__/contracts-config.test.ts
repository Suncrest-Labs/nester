import { afterEach, describe, expect, it, vi } from "vitest";

import {
  CONTRACT_ENV_VARS,
  CONTRACT_ID_PATTERN,
  ContractConfigError,
  REQUIRED_CONTRACTS,
  assertContractsConfigured,
  assertValidContractId,
  describeContractConfig,
  getContractId,
  requireContractId,
  type ContractKey,
} from "@/lib/contracts";

/**
 * #1094: the dapp read the vault address from NEXT_PUBLIC_VAULT_CONTRACT_ADDRESS
 * while the deploy script only ever wrote NEXT_PUBLIC_VAULT_CONTRACT_ID. Because
 * the reader defaulted to "", a correctly-deployed environment produced an empty
 * contract address and the app built transactions against nothing — silently.
 *
 * These tests pin the two halves of the fix: one canonical name, and no
 * empty-string default anywhere on the path from environment to transaction.
 */

// Synthetic but well-formed contract ids: C followed by 55 base32 characters.
const VALID_IDS: Record<ContractKey, string> = {
  vault: `C${"A".repeat(55)}`,
  vaultXlm: `C${"B".repeat(55)}`,
  vaultToken: `C${"D".repeat(55)}`,
  usdc: `C${"E".repeat(55)}`,
};

/** The name #1094 is about: read by the app, never written by any deploy path. */
const LEGACY_VAULT_ENV_VAR = "NEXT_PUBLIC_VAULT_CONTRACT_ADDRESS";

function setAllContracts() {
  for (const key of Object.keys(CONTRACT_ENV_VARS) as ContractKey[]) {
    vi.stubEnv(CONTRACT_ENV_VARS[key], VALID_IDS[key]);
  }
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("contract environment variable naming", () => {
  it("uses the *_CONTRACT_ID spelling the deploy script writes", () => {
    // deploy-testnet.sh emits NEXT_PUBLIC_<NAME>_CONTRACT_ID for every
    // contract. A regression to _CONTRACT_ADDRESS reintroduces #1094, so the
    // naming is asserted rather than left to review.
    for (const [key, envVar] of Object.entries(CONTRACT_ENV_VARS)) {
      expect(envVar, `${key} should be read from a *_CONTRACT_ID variable`).toMatch(
        /^NEXT_PUBLIC_[A-Z0-9_]+_CONTRACT_ID$/
      );
      expect(envVar).not.toContain("_CONTRACT_ADDRESS");
    }
  });

  it("declares an environment variable for every contract key", () => {
    for (const key of REQUIRED_CONTRACTS) {
      expect(CONTRACT_ENV_VARS[key], `no env var declared for ${key}`).toBeTruthy();
    }
  });
});

describe("resolution for every contract the application requires", () => {
  it("resolves each required contract when its canonical variable is set", () => {
    setAllContracts();

    for (const key of REQUIRED_CONTRACTS) {
      const resolved = getContractId(key);
      expect(resolved, `${CONTRACT_ENV_VARS[key]} should resolve`).toBe(VALID_IDS[key]);
      expect(resolved).toMatch(CONTRACT_ID_PATTERN);
      expect(requireContractId(key)).toBe(VALID_IDS[key]);
    }

    expect(() => assertContractsConfigured()).not.toThrow();
    expect(describeContractConfig().problems).toEqual([]);
  });

  it("resolves every declared contract, not only the required ones", () => {
    setAllContracts();

    for (const key of Object.keys(CONTRACT_ENV_VARS) as ContractKey[]) {
      expect(getContractId(key), `${key} should resolve`).toBe(VALID_IDS[key]);
    }
  });

  it("trims surrounding whitespace from a pasted value", () => {
    vi.stubEnv(CONTRACT_ENV_VARS.vault, `  ${VALID_IDS.vault}\n`);
    expect(getContractId("vault")).toBe(VALID_IDS.vault);
  });
});

describe("missing configuration fails loudly", () => {
  it("returns null rather than an empty string when a variable is unset", () => {
    vi.stubEnv(CONTRACT_ENV_VARS.vault, "");

    const resolved = getContractId("vault");
    expect(resolved).toBeNull();
    // The whole point of #1094: "" must never be handed out as an address.
    expect(resolved).not.toBe("");
  });

  it("throws naming the exact variable to set", () => {
    vi.stubEnv(CONTRACT_ENV_VARS.vault, "");

    expect(() => requireContractId("vault")).toThrow(ContractConfigError);
    expect(() => requireContractId("vault")).toThrow(/NEXT_PUBLIC_VAULT_CONTRACT_ID/);
  });

  it("rejects a malformed contract id instead of passing it to the SDK", () => {
    vi.stubEnv(CONTRACT_ENV_VARS.vault, "not-a-contract-id");

    expect(getContractId("vault")).toBeNull();
    expect(() => requireContractId("vault")).toThrow(/not a valid contract id/);

    const { problems } = describeContractConfig(["vault"]);
    expect(problems).toHaveLength(1);
    expect(problems[0].reason).toBe("invalid");
  });

  it("reports every missing contract at once", () => {
    // Nothing stubbed: both required contracts are absent.
    const { configured, problems } = describeContractConfig();

    expect(configured).toEqual([]);
    expect(problems.map((p) => p.envVar).sort()).toEqual(
      REQUIRED_CONTRACTS.map((key) => CONTRACT_ENV_VARS[key]).sort()
    );

    // One error listing all of them, so a misconfigured deploy is fixed in one
    // pass rather than one variable per restart.
    let message = "";
    try {
      assertContractsConfigured();
    } catch (err) {
      message = (err as Error).message;
    }
    for (const key of REQUIRED_CONTRACTS) {
      expect(message).toContain(CONTRACT_ENV_VARS[key]);
    }
  });

  it("fails when only some required contracts are configured", () => {
    vi.stubEnv(CONTRACT_ENV_VARS.vault, VALID_IDS.vault);

    const { configured, problems } = describeContractConfig();
    expect(configured).toContain("vault");
    expect(problems.map((p) => p.key)).toContain("vaultXlm");
    expect(() => assertContractsConfigured()).toThrow(ContractConfigError);
  });
});

describe("the #1094 bug class", () => {
  it("does not resolve the vault contract from the legacy *_ADDRESS variable", () => {
    // This is the exact broken deployment: the operator followed the old
    // .env.example, set the _ADDRESS name, and every reader silently got "".
    vi.stubEnv(LEGACY_VAULT_ENV_VAR, VALID_IDS.vault);

    expect(getContractId("vault")).toBeNull();
    expect(() => requireContractId("vault")).toThrow(/NEXT_PUBLIC_VAULT_CONTRACT_ID/);
  });

  it("exposes null, never an empty address, through lib/config", async () => {
    vi.stubEnv(LEGACY_VAULT_ENV_VAR, VALID_IDS.vault);
    vi.resetModules();

    const { config } = await import("@/lib/config");

    expect(config.contracts.vault).toBeNull();
    // The removed fields defaulted to ""; nothing may reintroduce that shape.
    expect(Object.values(config.contracts)).not.toContain("");
  });

  it("resolves through lib/config once the canonical variable is set", async () => {
    setAllContracts();
    vi.resetModules();

    const { config } = await import("@/lib/config");

    expect(config.contracts.vault).toBe(VALID_IDS.vault);
    expect(config.contracts.vaultXlm).toBe(VALID_IDS.vaultXlm);
  });

  it("leaves vault registry addresses null rather than empty when unconfigured", async () => {
    vi.resetModules();

    const { vaultContracts } = await import("@/lib/vault-contracts");

    expect(vaultContracts.length).toBeGreaterThan(0);
    for (const vault of vaultContracts) {
      expect(vault.contractAddress, `${vault.id} should not carry an empty address`).toBeNull();
    }
  });

  it("populates vault registry addresses from the canonical variables", async () => {
    setAllContracts();
    vi.resetModules();

    const { vaultContracts } = await import("@/lib/vault-contracts");

    for (const vault of vaultContracts) {
      expect(vault.contractAddress).toBe(VALID_IDS.vault);
    }
  });
});

describe("assertValidContractId guards the transaction builders", () => {
  it("accepts a well-formed id", () => {
    expect(assertValidContractId(VALID_IDS.vault, "deposit")).toBe(VALID_IDS.vault);
  });

  it("rejects an empty address before a transaction is built", () => {
    // Without this guard "" reaches new Contract("") and surfaces as an opaque
    // SDK error after a network round trip.
    expect(() => assertValidContractId("", "deposit")).toThrow(ContractConfigError);
    expect(() => assertValidContractId(null, "deposit")).toThrow(ContractConfigError);
    expect(() => assertValidContractId(undefined, "deposit")).toThrow(ContractConfigError);
  });

  it("rejects a malformed address and names the calling context", () => {
    expect(() => assertValidContractId("CNOPE", "withdraw")).toThrow(/withdraw/);
  });
});
