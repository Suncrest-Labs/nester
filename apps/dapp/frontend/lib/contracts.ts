/**
 * contracts.ts
 *
 * The single source of truth for deployed Soroban contract addresses.
 *
 * Canonical naming is `NEXT_PUBLIC_<CONTRACT>_CONTRACT_ID` (#1094). That is the
 * shape `packages/contracts/scripts/deploy-testnet.sh` writes into
 * `deployed-testnet.env`, and the deploy script is the only thing that ever
 * produces these values — so the deploy script, not the frontend, defines the
 * names. `NEXT_PUBLIC_VAULT_CONTRACT_ADDRESS` was a second name for the same
 * value that no deploy path ever wrote: every reader of it got `undefined`, and
 * the `|| ""` default in lib/config.ts turned that into an address of "", i.e.
 * a transaction built against no contract at all. Nothing here ever falls back
 * to "": a caller gets a validated address, `null`, or an error naming the
 * exact variable to set.
 */

export type ContractKey = "vault" | "vaultXlm" | "vaultToken" | "usdc";

/**
 * The environment variable carrying each contract address. Exported so tests
 * and error messages name the same string an operator has to set.
 */
export const CONTRACT_ENV_VARS: Readonly<Record<ContractKey, string>> = {
  vault: "NEXT_PUBLIC_VAULT_CONTRACT_ID",
  vaultXlm: "NEXT_PUBLIC_VAULT_XLM_CONTRACT_ID",
  vaultToken: "NEXT_PUBLIC_VAULT_TOKEN_CONTRACT_ID",
  usdc: "NEXT_PUBLIC_USDC_CONTRACT_ID",
};

/**
 * Contracts the deposit/withdraw money path cannot run without.
 *
 * `vaultToken` and `usdc` are deliberately absent: nothing on the money path
 * addresses them today, so requiring them would fail deployments for a value
 * that is never used. Move a key in here the moment something calls it.
 */
export const REQUIRED_CONTRACTS: readonly ContractKey[] = ["vault", "vaultXlm"];

/**
 * A Soroban contract id: `C` followed by 55 base32 characters.
 *
 * Matches the check `app/savings/page.tsx` and `components/vault/depositModal.tsx`
 * already applied inline, so this module accepts exactly what the app accepted
 * before — tightening the character class is a separate change.
 */
export const CONTRACT_ID_PATTERN = /^C[A-Z0-9]{55}$/;

export type ContractConfigReason = "missing" | "invalid";

export interface ContractConfigProblem {
  key: ContractKey;
  envVar: string;
  reason: ContractConfigReason;
}

/**
 * Thrown when a required contract address is absent or malformed. Carries the
 * variable name so the message tells an operator what to set rather than
 * surfacing as an opaque SDK failure several calls later.
 */
export class ContractConfigError extends Error {
  readonly problems: readonly ContractConfigProblem[];

  constructor(problems: readonly ContractConfigProblem[]) {
    super(describeProblems(problems));
    this.name = "ContractConfigError";
    this.problems = problems;
  }
}

function describeProblems(problems: readonly ContractConfigProblem[]): string {
  const lines = problems.map((problem) =>
    problem.reason === "missing"
      ? `${problem.envVar} is not set`
      : `${problem.envVar} is not a valid contract id (expected C followed by 55 characters)`
  );
  return (
    `Contract configuration is incomplete: ${lines.join("; ")}. ` +
    `Copy the values from packages/contracts/scripts/deployed-testnet.env into .env.local.`
  );
}

/**
 * Reads the raw environment values.
 *
 * Every lookup is written out literally on purpose: Next.js inlines
 * `process.env.NEXT_PUBLIC_*` at build time only when the whole expression
 * appears in the source, so a computed `process.env[name]` would read
 * `undefined` in the browser bundle — reintroducing the empty-address bug in
 * production while every test still passed. Do not collapse these into a loop.
 *
 * Read inside a function rather than at module scope so a caller always sees
 * the current environment (and so tests can vary it without module resets).
 */
function readRawContractEnv(): Record<ContractKey, string | undefined> {
  return {
    vault: process.env.NEXT_PUBLIC_VAULT_CONTRACT_ID,
    vaultXlm: process.env.NEXT_PUBLIC_VAULT_XLM_CONTRACT_ID,
    vaultToken: process.env.NEXT_PUBLIC_VAULT_TOKEN_CONTRACT_ID,
    usdc: process.env.NEXT_PUBLIC_USDC_CONTRACT_ID,
  };
}

/**
 * Returns the configured address for a contract, or `null` when it is unset or
 * malformed. Use this where a missing contract is a renderable state (a
 * disabled button, a hidden panel); use requireContractId where it is not.
 */
export function getContractId(key: ContractKey): string | null {
  const raw = readRawContractEnv()[key];
  const trimmed = raw?.trim() ?? "";
  if (trimmed === "" || !CONTRACT_ID_PATTERN.test(trimmed)) {
    return null;
  }
  return trimmed;
}

/**
 * Returns the configured address for a contract, or throws ContractConfigError.
 * This is the fail-loud path: any code about to build a transaction should go
 * through it rather than accepting a possibly-empty string.
 */
export function requireContractId(key: ContractKey): string {
  const resolved = getContractId(key);
  if (resolved === null) {
    throw new ContractConfigError([contractProblem(key)]);
  }
  return resolved;
}

function contractProblem(key: ContractKey): ContractConfigProblem {
  const raw = readRawContractEnv()[key]?.trim() ?? "";
  return {
    key,
    envVar: CONTRACT_ENV_VARS[key],
    reason: raw === "" ? "missing" : "invalid",
  };
}

/**
 * Non-throwing inspection of contract configuration, for surfaces that want to
 * report every problem at once instead of failing on the first.
 */
export function describeContractConfig(
  keys: readonly ContractKey[] = REQUIRED_CONTRACTS
): { configured: ContractKey[]; problems: ContractConfigProblem[] } {
  const configured: ContractKey[] = [];
  const problems: ContractConfigProblem[] = [];

  for (const key of keys) {
    if (getContractId(key) === null) {
      problems.push(contractProblem(key));
    } else {
      configured.push(key);
    }
  }

  return { configured, problems };
}

/**
 * Throws unless every named contract resolves. Reports all problems in one
 * error so a misconfigured deployment is fixed in one pass rather than one
 * variable per restart.
 */
export function assertContractsConfigured(
  keys: readonly ContractKey[] = REQUIRED_CONTRACTS
): void {
  const { problems } = describeContractConfig(keys);
  if (problems.length > 0) {
    throw new ContractConfigError(problems);
  }
}

/**
 * Validates an address that reached a transaction builder from somewhere other
 * than this module (a vault registry entry, an API response, a URL parameter).
 *
 * This is the last line of defence on the money path: without it an empty or
 * malformed id reaches `new Contract(id)` and surfaces as an SDK parse error
 * after a network round trip, with nothing pointing at the real cause.
 */
export function assertValidContractId(contractId: string | null | undefined, context: string): string {
  const trimmed = contractId?.trim() ?? "";
  if (trimmed === "") {
    throw new ContractConfigError([
      { key: "vault", envVar: CONTRACT_ENV_VARS.vault, reason: "missing" },
    ]);
  }
  if (!CONTRACT_ID_PATTERN.test(trimmed)) {
    throw new Error(
      `${context}: "${trimmed}" is not a valid Soroban contract id (expected C followed by 55 characters).`
    );
  }
  return trimmed;
}
