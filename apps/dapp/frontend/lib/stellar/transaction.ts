import {
  Contract,
  rpc as SorobanRpc,
  Transaction,
  TransactionBuilder,
  BASE_FEE,
  nativeToScVal,
  scValToNative,
  Address,
  Operation,
} from "@stellar/stellar-sdk";

import { NETWORKS, DEFAULT_NETWORK } from "@/lib/networks";
import { readNetworkId } from "@/lib/storageKeys";
import { assertValidContractId, getContractId } from "@/lib/contracts";
import { depositAmountToStroops } from "@/lib/decimal";
import { applySlippage, resolveFreshQuote, type TimedQuote } from "@/lib/slippage";

// ── Config ────────────────────────────────────────────────────────────────────

/**
 * Resolve the active network. Exported so every module that builds or signs a
 * transaction agrees on it: a builder reading a different source than the
 * signer produces a transaction signed for the wrong passphrase.
 *
 * Routed through safeStorage (#1233) rather than raw localStorage: a
 * throwing storage accessor (private browsing, full quota) must fall back to
 * the safe explicit DEFAULT_NETWORK, never leave this undefined or crash the
 * caller — every deposit/withdraw transaction is built against this value.
 */
export const getCurrentNetwork = () => {
  const savedNetwork = readNetworkId();
  return savedNetwork ? NETWORKS[savedNetwork] : DEFAULT_NETWORK;
};

/**
 * Validity window for built transactions. Hardware wallets, WalletConnect and
 * mobile signers routinely need well over 30s, so keep this generous.
 */
export const TX_TIMEOUT_SECONDS = 180;

// Contract addresses come from lib/contracts.ts, which validates them and
// never substitutes "" for a missing value (#1094). These resolve to null when
// unconfigured; a caller that must have one calls requireContractId instead.
export const VAULT_CONTRACT_ID = getContractId("vault");

export const VAULT_XLM_CONTRACT_ID = getContractId("vaultXlm");

export const USDC_CONTRACT_ID = getContractId("usdc");

// ── Types ─────────────────────────────────────────────────────────────────────

export interface DepositParams {
  /** Stellar public key of the depositing user. */
  walletAddress: string;
  /** Vault contract ID on Soroban. */
  contractId: string;
  /**
   * Amount as bigint stroops, or a decimal string in whole units (e.g. "0.3",
   * at most 7 decimals). Numbers are not accepted: float * 1e7 misrounds.
   */
  amount: bigint | string;
  /**
   * Minimum vault shares the user accepts (slippage guard), in the contract's
   * share units. Derived from preview_deposit minus the user's tolerance.
   */
  minSharesOut: bigint;
}

export type DepositQuoteParams = Omit<DepositParams, "minSharesOut">;

export interface WithdrawParams {
  walletAddress: string;
  contractId: string;
  /** Number of nVault shares to burn (as bigint stroops or number). */
  shares: bigint | number;
  /**
   * Minimum underlying assets to receive in stroops (slippage guard). Defaults to 0.
   * Pass a non-zero value to reject withdrawals where the exchange rate slips too far.
   */
  minAssetsOut?: bigint | number;
}

export interface BuiltTransaction {
  /** Base64-encoded unsigned transaction XDR ready for signing. */
  xdr: string;
  /** The assembled transaction object (used for submission after signing). */
  transaction: Transaction;
  /** Present when archived ledger entries must be restored first */
  restoreTransaction?: Transaction;
  restoreXdr?: string;
  needsRestore?: boolean;
}

export interface NetworkFeeEstimate {
  /** Total fee in stroops (1 XLM = 10_000_000 stroops). */
  feeStroops: bigint;
  /** Human-readable XLM amount. */
  feeXlm: number;
  /** Whether estimation succeeded. */
  available: boolean;
  /** Error message when unavailable. */
  error?: string;
}

export interface TransactionReceipt {
  txHash: string;
  explorerUrl: string;
  ledger: number;
}

// ── Custom errors ─────────────────────────────────────────────────────────────

/**
 * Thrown when the user dismisses the Freighter signing popup.
 * Callers should show a friendly "You cancelled the transaction" message
 * rather than a generic error.
 */
export class UserRejectedError extends Error {
  constructor() {
    super("Transaction signing was cancelled by the user.");
    this.name = "UserRejectedError";
  }
}

/**
 * Thrown when the transaction is submitted but fails on-chain.
 * `reason` contains the Soroban result code string for display.
 */
export class TransactionFailedError extends Error {
  constructor(public readonly reason: string) {
    super(`Transaction failed on-chain: ${reason}`);
    this.name = "TransactionFailedError";
  }
}

/**
 * Thrown when the wallet's network does not match the app's configured network.
 */
export class NetworkMismatchError extends Error {
  constructor(walletNetwork: string, appNetwork: string) {
    super(`Wallet is on ${walletNetwork} but the app is on ${appNetwork}. Please switch your wallet network.`);
    this.name = "NetworkMismatchError";
  }
}

/**
 * Thrown when the wallet disconnects between building and signing a transaction.
 */
export class WalletDisconnectedError extends Error {
  constructor() {
    super("Wallet disconnected. Please reconnect your wallet and try again.");
    this.name = "WalletDisconnectedError";
  }
}

/**
 * Thrown when the submission times out waiting for ledger confirmation.
 */
export class TransactionTimeoutError extends Error {
  constructor() {
    super("Transaction timed out waiting for on-chain confirmation.");
    this.name = "TransactionTimeoutError";
  }
}

/**
 * Thrown when the wallet is on a different network than the dapp.
 */
export class WalletNetworkMismatchError extends Error {
  constructor(public readonly expectedNetworkName: string) {
    super(`Switch your wallet to ${expectedNetworkName}`);
    this.name = "WalletNetworkMismatchError";
  }
}

/**
 * Thrown when the transaction's time bounds elapsed before it could be
 * submitted (e.g. slow hardware/mobile signing, or a txTooLate rejection).
 */
export class TransactionExpiredError extends Error {
  constructor() {
    super("Signing took too long, try again");
    this.name = "TransactionExpiredError";
  }
}

// ── Soroban RPC client ────────────────────────────────────────────────────────

function getServer(rpcUrl: string): SorobanRpc.Server {
  const isProd = process.env.NODE_ENV === "production";

  if (isProd && !rpcUrl.startsWith("https://")) {
    throw new Error(
      `Production Soroban RPC URL must use HTTPS. Got: ${rpcUrl}. ` +
        `Set NEXT_PUBLIC_STELLAR_RPC_URL to a valid https:// endpoint.`
    );
  }

  return new SorobanRpc.Server(rpcUrl, { allowHttp: !isProd });
}

// ── Fee estimation ────────────────────────────────────────────────────────────

function stroopsToXlm(stroops: bigint): number {
  return Number(stroops) / 10_000_000;
}

/**
 * Estimate Stellar network fee for a deposit via simulateTransaction.
 */
export async function estimateDepositFee(
  params: DepositQuoteParams & { minSharesOut?: bigint }
): Promise<NetworkFeeEstimate> {
  try {
    const { transaction } = await buildDepositTransaction({
      minSharesOut: BigInt(0),
      ...params,
    });
    const total = BigInt(transaction.fee) || BigInt(BASE_FEE);
    return { feeStroops: total, feeXlm: stroopsToXlm(total), available: true };
  } catch (err) {
    return {
      feeStroops: BigInt(0),
      feeXlm: 0,
      available: false,
      error: err instanceof Error ? err.message : "Fee estimation unavailable",
    };
  }
}

/**
 * Preview expected vault shares for a deposit amount, via the vault
 * contract's read-only `preview_deposit` (Issue #1129) — simulated against
 * the live contract instead of assuming a 1:1 amount-to-shares ratio.
 */
export async function previewDeposit(params: DepositQuoteParams): Promise<{
  sharesExpected: bigint;
  available: boolean;
  error?: string;
}> {
  try {
    const { walletAddress, contractId, amount: rawAmount } = params;
    const network = getCurrentNetwork();
    const server = getServer(network.rpcUrl);
    const account = await server.getAccount(walletAddress);

    const amountStroops = depositAmountToStroops(rawAmount);

    const contract = new Contract(assertValidContractId(contractId, "vault contract"));
    const tx = new TransactionBuilder(account, {
      fee: BASE_FEE,
      networkPassphrase: network.networkPassphrase,
    })
      .addOperation(
        contract.call("preview_deposit", nativeToScVal(amountStroops, { type: "i128" }))
      )
      .setTimeout(30)
      .build();

    const sim = await server.simulateTransaction(tx);
    if (SorobanRpc.Api.isSimulationError(sim)) {
      throw new TransactionFailedError(
        (sim as SorobanRpc.Api.SimulateTransactionErrorResponse).error
      );
    }
    const result = (sim as SorobanRpc.Api.SimulateTransactionSuccessResponse).result;
    const sharesExpected: bigint = result ? BigInt(scValToNative(result.retval)) : BigInt(0);
    return { sharesExpected, available: true };
  } catch (err) {
    return {
      sharesExpected: BigInt(0),
      available: false,
      error: err instanceof Error ? err.message : "Deposit preview unavailable",
    };
  }
}

/**
 * Estimate Stellar network fee for a withdrawal via simulateTransaction.
 */
export async function estimateWithdrawFee(
  params: WithdrawParams
): Promise<NetworkFeeEstimate> {
  try {
    const { transaction } = await buildWithdrawTransaction(params);
    const total = BigInt(transaction.fee) || BigInt(BASE_FEE);
    return { feeStroops: total, feeXlm: stroopsToXlm(total), available: true };
  } catch (err) {
    return {
      feeStroops: BigInt(0),
      feeXlm: 0,
      available: false,
      error: err instanceof Error ? err.message : "Fee estimation unavailable",
    };
  }
}

/**
 * Build a transaction that restores archived footprint entries.
 *
 * Used when Soroban RPC returns `restorePreamble` from simulateTransaction,
 * indicating that one or more contract entries have been archived and must
 * be restored before the invocation can proceed.
 */
export function buildRestoreTransaction(
  account: SorobanRpc.Server.AccountResponse | { accountId(): string; sequenceNumber(): string },
  restorePreamble: {
    minResourceFee: string | number;
    transactionData: SorobanRpc.Api.SorobanTransactionData | string;
  },
  networkPassphrase: string
): BuiltTransaction {
  const fee = (BigInt(BASE_FEE) + BigInt(restorePreamble.minResourceFee)).toString();
  const tx = new TransactionBuilder(account, {
    fee,
    networkPassphrase,
  })
    .setSorobanData(restorePreamble.transactionData)
    .addOperation(Operation.restoreFootprint({}))
    .setTimeout(30)
    .build();

  return { xdr: tx.toXDR(), transaction: tx };
}

// ── Transaction builders ──────────────────────────────────────────────────────

/**
 * Build a Soroban `deposit` contract invocation transaction.
 *
 * The vault contract's `deposit(user, amount, min_shares_out)` function is
 * called. Amount is bigint stroops or a decimal string, parsed without
 * floating point.
 */
export async function buildDepositTransaction(
  params: DepositParams
): Promise<BuiltTransaction> {
  const { walletAddress, contractId, amount: rawAmount, minSharesOut } = params;
  const network = getCurrentNetwork();

  const server = getServer(network.rpcUrl);
  const account = await server.getAccount(walletAddress);

  const amountStroops = depositAmountToStroops(rawAmount);

  const contract = new Contract(assertValidContractId(contractId, "vault contract"));

  const tx = new TransactionBuilder(account, {
    fee: BASE_FEE,
    networkPassphrase: network.networkPassphrase,
  })
    .addOperation(
      contract.call(
        "deposit",
        new Address(walletAddress).toScVal(),
        nativeToScVal(amountStroops, { type: "i128" }),
        nativeToScVal(minSharesOut, { type: "i128" })
      )
    )
    .setTimeout(TX_TIMEOUT_SECONDS)
    .build();

  // Simulate to populate the transaction's footprint (Soroban requirement)
  const sim = await server.simulateTransaction(tx);

  if (SorobanRpc.Api.isSimulationRestore(sim)) {
    const restoreBuilt = buildRestoreTransaction(
      account,
      sim.restorePreamble,
      network.networkPassphrase
    );
    return {
      xdr: restoreBuilt.xdr,
      transaction: restoreBuilt.transaction,
      restoreTransaction: restoreBuilt.transaction,
      restoreXdr: restoreBuilt.xdr,
      needsRestore: true,
    };
  }

  if (SorobanRpc.Api.isSimulationError(sim)) {
    throw new TransactionFailedError(
      (sim as SorobanRpc.Api.SimulateTransactionErrorResponse).error
    );
  }

  const assembled = SorobanRpc.assembleTransaction(tx, sim).build();

  return { xdr: assembled.toXDR(), transaction: assembled };
}

/**
 * Build a Soroban `withdraw` contract invocation transaction.
 *
 * The vault contract's `withdraw(from, shares)` function is called.
 * Amounts can be provided as bigint stroops (recommended) or as human-readable numbers.
 */
export async function buildWithdrawTransaction(
  params: WithdrawParams
): Promise<BuiltTransaction> {
  const { walletAddress, contractId, shares: rawShares, minAssetsOut: rawMinAssets = 0 } = params;
  const network = getCurrentNetwork();

  const server = getServer(network.rpcUrl);
  const account = await server.getAccount(walletAddress);

  // Convert amounts to stroops if they're numbers
  const sharesStroops = typeof rawShares === "bigint"
    ? rawShares
    : BigInt(Math.round(Number(rawShares) * 10_000_000));

  const minAssetsStroops = typeof rawMinAssets === "bigint"
    ? rawMinAssets
    : BigInt(Math.round(Number(rawMinAssets) * 10_000_000));

  const contract = new Contract(assertValidContractId(contractId, "vault contract"));

  const tx = new TransactionBuilder(account, {
    fee: BASE_FEE,
    networkPassphrase: network.networkPassphrase,
  })
    .addOperation(
      contract.call(
        "withdraw",
        new Address(walletAddress).toScVal(),
        nativeToScVal(sharesStroops, { type: "i128" }),
        nativeToScVal(minAssetsStroops, { type: "i128" })
      )
    )
    .setTimeout(TX_TIMEOUT_SECONDS)
    .build();

  const sim = await server.simulateTransaction(tx);

  if (SorobanRpc.Api.isSimulationRestore(sim)) {
    const restoreBuilt = buildRestoreTransaction(
      account,
      sim.restorePreamble,
      network.networkPassphrase
    );
    return {
      xdr: restoreBuilt.xdr,
      transaction: restoreBuilt.transaction,
      restoreTransaction: restoreBuilt.transaction,
      restoreXdr: restoreBuilt.xdr,
      needsRestore: true,
    };
  }

  if (SorobanRpc.Api.isSimulationError(sim)) {
    throw new TransactionFailedError(
      (sim as SorobanRpc.Api.SimulateTransactionErrorResponse).error
    );
  }

  const assembled = SorobanRpc.assembleTransaction(tx, sim).build();

  return { xdr: assembled.toXDR(), transaction: assembled };
}

// ── Wallet signing ────────────────────────────────────────────────────────────

/**
 * Compare the connected wallet's network with the dapp's selected network.
 * Wallets that cannot report their network are not blocked.
 *
 * @throws {WalletNetworkMismatchError} on a definite passphrase mismatch.
 */
export async function assertWalletOnDappNetwork(): Promise<void> {
  const { StellarWalletsKit } = await import("@creit.tech/stellar-wallets-kit");

  const walletModule = StellarWalletsKit.selectedModule;
  if (!walletModule || typeof walletModule.getNetwork !== "function") return;

  let walletPassphrase: string | undefined;
  try {
    walletPassphrase = (await walletModule.getNetwork())?.networkPassphrase;
  } catch {
    return;
  }

  const network = getCurrentNetwork();
  if (walletPassphrase && walletPassphrase !== network.networkPassphrase) {
    throw new WalletNetworkMismatchError(network.name);
  }
}

/**
 * Sign a freshly built transaction. If the signer takes so long that the
 * transaction's time bounds have passed, rebuild (and re-simulate) once and ask
 * for a new signature.
 *
 * @throws {TransactionExpiredError} if the rebuilt transaction also expires.
 */
export async function signWithExpiryRebuild(
  build: () => Promise<{ xdr: string; transaction: { timeBounds?: { maxTime: string } } }>,
  sign: (xdr: string) => Promise<string>
): Promise<string> {
  for (let attempt = 0; attempt < 2; attempt++) {
    const built = await build();
    const signed = await sign(built.xdr);
    const maxTime = Number(built.transaction.timeBounds?.maxTime ?? 0);
    if (!maxTime || Math.floor(Date.now() / 1000) < maxTime) return signed;
  }
  throw new TransactionExpiredError();
}

function isTxTooLate(errorResult: unknown): boolean {
  try {
    const code = (
      errorResult as { result: () => { switch: () => { name: string } } }
    )
      .result()
      .switch().name;
    return code === "txTooLate";
  } catch {
    return false;
  }
}

/**
 * Request the user to sign a transaction via stellar-wallets-kit.
 * Returns the signed XDR string.
 *
 * @throws {UserRejectedError} if the user dismisses the wallet popup.
 */
export async function signTransaction(txXdr: string): Promise<string> {
  await assertWalletOnDappNetwork();

  const { StellarWalletsKit } = await import("@creit.tech/stellar-wallets-kit");

  const walletModule = StellarWalletsKit.selectedModule;
  if (!walletModule) {
    throw new WalletDisconnectedError();
  }

  const network = getCurrentNetwork();

  // Confirm the wallet is still connected (#1099). A locked, closed, or
  // switched-account wallet surfaces as getAddress() throwing, so a failure
  // here IS the disconnect case and must not be swallowed.
  try {
    const { address: currentAddress } = await walletModule.getAddress();
    if (!currentAddress) {
      throw new WalletDisconnectedError();
    }
  } catch (err) {
    if (err instanceof WalletDisconnectedError) throw err;
    throw new WalletDisconnectedError();
  }

  // Network mismatch check (#1095). Compare the wallet's actual network
  // against the one the transaction was built for. Signing a testnet
  // transaction while the wallet is on mainnet (or the reverse) means the
  // user approves something other than what they were shown.
  //
  // getNetwork() is not implemented by every wallet module; when it is
  // absent we cannot verify and deliberately proceed rather than blocking
  // signing on wallets that simply do not expose it.
  const getNetwork = (
    walletModule as unknown as {
      getNetwork?: () => Promise<{ networkPassphrase?: string; network?: string }>;
    }
  ).getNetwork;

  if (typeof getNetwork === "function") {
    let walletPassphrase: string | undefined;
    try {
      const net = await getNetwork.call(walletModule);
      walletPassphrase = net?.networkPassphrase;
    } catch {
      // Treat an unreadable network as unverifiable rather than mismatched.
      walletPassphrase = undefined;
    }

    if (walletPassphrase && walletPassphrase !== network.networkPassphrase) {
      throw new NetworkMismatchError(walletPassphrase, network.networkPassphrase);
    }
  }

  let result: { signedTxXdr: string };
  try {
    result = await walletModule.signTransaction(txXdr, {
      networkPassphrase: network.networkPassphrase,
    });
  } catch (err) {
    const msg = (err instanceof Error ? err.message : String(err)).toLowerCase();
    if (
      msg.includes("user declined") ||
      msg.includes("user rejected") ||
      msg.includes("cancelled") ||
      msg.includes("canceled")
    ) {
      throw new UserRejectedError();
    }
    throw err;
  }

  return result.signedTxXdr;
}

// ── Submission + polling ──────────────────────────────────────────────────────

const POLL_INTERVAL_MS = 2_000;
const MAX_POLL_ATTEMPTS = 15; // 30 seconds total

/**
 * Submit a signed transaction to the Soroban RPC and poll until it is
 * confirmed or fails.
 *
 * @throws {TransactionFailedError}  if the transaction fails on-chain.
 * @throws {TransactionTimeoutError} if confirmation is not received in time.
 */
export async function submitTransaction(
  signedXdr: string
): Promise<TransactionReceipt> {
  const network = getCurrentNetwork();
  const server = getServer(network.rpcUrl);

  // Re-parse from signed XDR so we have a Transaction object to submit
  const tx = new Transaction(signedXdr, network.networkPassphrase);
  const sendResult = await server.sendTransaction(tx);

  if (sendResult.status === "ERROR") {
    if (isTxTooLate(sendResult.errorResult)) {
      throw new TransactionExpiredError();
    }
    throw new TransactionFailedError(
      sendResult.errorResult?.toXDR("base64") ?? "unknown error"
    );
  }

  const txHash = sendResult.hash;

  // Poll for confirmation
  for (let attempt = 0; attempt < MAX_POLL_ATTEMPTS; attempt++) {
    await sleep(POLL_INTERVAL_MS);

    const getResult = await server.getTransaction(txHash);

    if (getResult.status === SorobanRpc.Api.GetTransactionStatus.SUCCESS) {
      return {
        txHash,
        explorerUrl: `${network.explorerUrl}/tx/${txHash}`,
        ledger: getResult.ledger,
      };
    }

    if (getResult.status === SorobanRpc.Api.GetTransactionStatus.FAILED) {
      throw new TransactionFailedError(
        getResult.resultMetaXdr?.toXDR("base64") ?? "on-chain execution failed"
      );
    }

    // NOT_FOUND means still pending — keep polling
  }

  throw new TransactionTimeoutError();
}

// ── High-level vault flows ───────────────────────────────────────────────────

/**
 * Minimum shares to accept for a deposit: preview_deposit minus the user's
 * slippage tolerance. Re-quotes when the cached preview is older than
 * QUOTE_MAX_AGE_MS so a stale quote is never signed. Throws when no live quote
 * can be obtained rather than falling back to an unbounded deposit.
 */
export async function resolveDepositMinSharesOut(
  params: DepositQuoteParams,
  slippageBps: number,
  cached?: TimedQuote<bigint> | null
): Promise<{ minSharesOut: bigint; quote: TimedQuote<bigint> }> {
  const quote = await resolveFreshQuote(cached, async () => {
    const preview = await previewDeposit(params);
    if (!preview.available) {
      throw new Error("Unable to fetch a current deposit quote. Please try again.");
    }
    return preview.sharesExpected;
  });
  return { minSharesOut: applySlippage(quote.value, slippageBps), quote };
}

function isRealContractId(id: string): boolean {
  return /^C[A-Z0-9]{55}$/.test(id);
}

export async function executeVaultDeposit(params: {
  walletAddress: string;
  vaultId: string;
  contractId: string;
  asset: "USDC" | "XLM";
  amount: bigint | string;
  minSharesOut: bigint;
  onStatusChange?: (status: "building" | "restoring" | "signing" | "submitting" | "pending") => void;
}): Promise<TransactionReceipt> {
  const { walletAddress, contractId, amount, minSharesOut, onStatusChange } = params;

  if (!isRealContractId(contractId)) {
    throw new Error("Vault is not yet live. Deposits are currently disabled.");
  }

  await assertWalletOnDappNetwork();
  onStatusChange?.("building");
  let built = await buildDepositTransaction({ walletAddress, contractId, amount, minSharesOut });

  if (built.needsRestore && built.restoreTransaction) {
    onStatusChange?.("restoring");
    onStatusChange?.("signing");
    const signedRestoreXdr = await signTransaction(built.xdr);
    onStatusChange?.("submitting");
    await submitTransaction(signedRestoreXdr);

    onStatusChange?.("building");
    built = await buildDepositTransaction({ walletAddress, contractId, amount, minSharesOut });
  }

  onStatusChange?.("signing");
  let firstAttempt = true;
  const signedXdr = await signWithExpiryRebuild(
    async () => {
      if (firstAttempt) {
        firstAttempt = false;
        return built;
      }
      onStatusChange?.("building");
      return buildDepositTransaction({ walletAddress, contractId, amount, minSharesOut });
    },
    signTransaction
  );
  onStatusChange?.("pending");
  return submitTransaction(signedXdr);
}

export async function executeVaultWithdraw(params: {
  walletAddress: string;
  vaultId: string;
  contractId: string;
  asset: "USDC" | "XLM";
  shares: number;
  minAssetsOut?: bigint | number;
  onStatusChange?: (status: "building" | "restoring" | "signing" | "submitting" | "pending") => void;
}): Promise<TransactionReceipt> {
  const { walletAddress, contractId, shares, minAssetsOut, onStatusChange } = params;

  if (!isRealContractId(contractId)) {
    throw new Error("Vault is not yet live. Withdrawals are currently disabled.");
  }

  await assertWalletOnDappNetwork();
  onStatusChange?.("building");
  let built = await buildWithdrawTransaction({ walletAddress, contractId, shares, minAssetsOut });

  if (built.needsRestore && built.restoreTransaction) {
    onStatusChange?.("restoring");
    onStatusChange?.("signing");
    const signedRestoreXdr = await signTransaction(built.xdr);
    onStatusChange?.("submitting");
    await submitTransaction(signedRestoreXdr);

    onStatusChange?.("building");
    built = await buildWithdrawTransaction({ walletAddress, contractId, shares, minAssetsOut });
  }

  onStatusChange?.("signing");
  let firstAttempt = true;
  const signedXdr = await signWithExpiryRebuild(
    async () => {
      if (firstAttempt) {
        firstAttempt = false;
        return built;
      }
      onStatusChange?.("building");
      return buildWithdrawTransaction({ walletAddress, contractId, shares, minAssetsOut });
    },
    signTransaction
  );
  onStatusChange?.("pending");
  return submitTransaction(signedXdr);
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Format a Stellar transaction hash for display (first 8 + last 8 chars).
 */
export function truncateTxHash(hash: string): string {
  if (hash.length <= 18) return hash;
  return `${hash.slice(0, 8)}…${hash.slice(-8)}`;
}