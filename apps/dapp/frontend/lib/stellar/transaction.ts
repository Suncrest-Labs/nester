import {
  Contract,
  rpc as SorobanRpc,
  Transaction,
  TransactionBuilder,
  BASE_FEE,
  nativeToScVal,
  scValToNative,
  Address,
} from "@stellar/stellar-sdk";

import { NETWORKS, DEFAULT_NETWORK } from "@/lib/networks";
import { readNetworkId } from "@/lib/storageKeys";
import { assertValidContractId, getContractId } from "@/lib/contracts";

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

// Contract addresses come from lib/contracts.ts, which validates them and
// never substitutes "" for a missing value (#1094). These resolve to null when
// unconfigured; a caller that must have one calls requireContractId instead.
export const VAULT_CONTRACT_ID = getContractId("vault");

export const VAULT_XLM_CONTRACT_ID = getContractId("vaultXlm");

export const USDC_CONTRACT_ID = getContractId("usdc");

/**
 * Default transaction validity window in seconds (#1510).
 * Hardware wallets (Ledger), WalletConnect, and mobile signing often take
 * longer than 30 seconds, so 180 seconds prevents premature expiry.
 */
export const TRANSACTION_TIMEOUT_SECONDS = 180;

// ── Types ─────────────────────────────────────────────────────────────────────

export interface DepositParams {
  /** Stellar public key of the depositing user. */
  walletAddress: string;
  /** Vault contract ID on Soroban. */
  contractId: string;
  /** Amount in stroops (bigint, 7 decimals = 1 XLM, 6 decimals = 1 USDC). */
  amount: bigint | number;
  /** Optional custom validity timeout in seconds (defaults to 180s). */
  timeoutSeconds?: number;
}

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
  /** Optional custom validity timeout in seconds (defaults to 180s). */
  timeoutSeconds?: number;
}

export interface BuiltTransaction {
  /** Base64-encoded unsigned transaction XDR ready for signing. */
  xdr: string;
  /** The assembled transaction object (used for submission after signing). */
  transaction: Transaction;
  /** Epoch timestamp (ms) when the transaction timebounds expire. */
  expiresAtMs?: number;
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
  constructor(public readonly reason: string, message?: string) {
    super(message ?? `Transaction failed on-chain: ${reason}`);
    this.name = "TransactionFailedError";
  }
}

/**
 * Thrown when signing took longer than the transaction's validity window
 * or submission fails with `txTOO_LATE` (#1510).
 */
export class TransactionExpiredError extends TransactionFailedError {
  constructor() {
    super("txTOO_LATE", "Signing took too long, try again");
    this.name = "TransactionExpiredError";
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
  params: DepositParams
): Promise<NetworkFeeEstimate> {
  try {
    const { transaction } = await buildDepositTransaction(params);
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
export async function previewDeposit(params: DepositParams): Promise<{
  sharesExpected: bigint;
  available: boolean;
  error?: string;
}> {
  try {
    const { walletAddress, contractId, amount: rawAmount, timeoutSeconds = TRANSACTION_TIMEOUT_SECONDS } = params;
    const network = getCurrentNetwork();
    const server = getServer(network.rpcUrl);
    const account = await server.getAccount(walletAddress);

    const amountStroops = typeof rawAmount === "bigint"
      ? rawAmount
      : BigInt(Math.round(rawAmount * 10_000_000));

    const contract = new Contract(assertValidContractId(contractId, "vault contract"));
    const tx = new TransactionBuilder(account, {
      fee: BASE_FEE,
      networkPassphrase: network.networkPassphrase,
    })
      .addOperation(
        contract.call("preview_deposit", nativeToScVal(amountStroops, { type: "i128" }))
      )
      .setTimeout(timeoutSeconds)
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

// ── Transaction builders ──────────────────────────────────────────────────────

/**
 * Build a Soroban `deposit` contract invocation transaction.
 *
 * The vault contract's `deposit(user, amount)` function is called.
 * Amount can be provided as bigint stroops (recommended) or as a human-readable number
 * (for backwards compatibility; will be converted to stroops using Math.round).
 */
export async function buildDepositTransaction(
  params: DepositParams
): Promise<BuiltTransaction> {
  const {
    walletAddress,
    contractId,
    amount: rawAmount,
    timeoutSeconds = TRANSACTION_TIMEOUT_SECONDS,
  } = params;
  const network = getCurrentNetwork();

  const server = getServer(network.rpcUrl);
  const account = await server.getAccount(walletAddress);

  // Convert amount to stroops if it's a number
  const amountStroops = typeof rawAmount === "bigint"
    ? rawAmount
    : BigInt(Math.round(rawAmount * 10_000_000));

  const contract = new Contract(assertValidContractId(contractId, "vault contract"));
  const builtAtMs = Date.now();

  const tx = new TransactionBuilder(account, {
    fee: BASE_FEE,
    networkPassphrase: network.networkPassphrase,
  })
    .addOperation(
      contract.call(
        "deposit",
        new Address(walletAddress).toScVal(),
        nativeToScVal(amountStroops, { type: "i128" })
      )
    )
    .setTimeout(timeoutSeconds)
    .build();

  // Simulate to populate the transaction's footprint (Soroban requirement)
  const sim = await server.simulateTransaction(tx);

  if (SorobanRpc.Api.isSimulationError(sim)) {
    throw new TransactionFailedError(
      (sim as SorobanRpc.Api.SimulateTransactionErrorResponse).error
    );
  }

  const assembled = SorobanRpc.assembleTransaction(tx, sim).build();
  const maxTimeSec = Number(assembled.timeBounds?.maxTime ?? 0);
  const expiresAtMs = maxTimeSec > 0 ? maxTimeSec * 1000 : builtAtMs + timeoutSeconds * 1000;

  return { xdr: assembled.toXDR(), transaction: assembled, expiresAtMs };
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
  const {
    walletAddress,
    contractId,
    shares: rawShares,
    minAssetsOut: rawMinAssets = 0,
    timeoutSeconds = TRANSACTION_TIMEOUT_SECONDS,
  } = params;
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
  const builtAtMs = Date.now();

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
    .setTimeout(timeoutSeconds)
    .build();

  const sim = await server.simulateTransaction(tx);

  if (SorobanRpc.Api.isSimulationError(sim)) {
    throw new TransactionFailedError(
      (sim as SorobanRpc.Api.SimulateTransactionErrorResponse).error
    );
  }

  const assembled = SorobanRpc.assembleTransaction(tx, sim).build();
  const maxTimeSec = Number(assembled.timeBounds?.maxTime ?? 0);
  const expiresAtMs = maxTimeSec > 0 ? maxTimeSec * 1000 : builtAtMs + timeoutSeconds * 1000;

  return { xdr: assembled.toXDR(), transaction: assembled, expiresAtMs };
}

/**
 * Check whether a built transaction's timebounds have expired (#1510).
 */
export function isTransactionExpired(
  built: BuiltTransaction,
  nowMs: number = Date.now()
): boolean {
  if (typeof built.expiresAtMs === "number" && built.expiresAtMs > 0) {
    return nowMs >= built.expiresAtMs;
  }
  const maxTimeSec = Number(built.transaction?.timeBounds?.maxTime ?? 0);
  if (maxTimeSec > 0) {
    return Math.floor(nowMs / 1000) >= maxTimeSec;
  }
  return false;
}

/**
 * Build and sign a transaction, automatically rebuilding and re-simulating
 * once if the user finishes signing after the first transaction's timebounds
 * have expired (#1510).
 */
export async function signTransactionWithRebuild(
  buildTx: () => Promise<BuiltTransaction>,
  signer: (xdr: string) => Promise<string> = signTransaction,
  now: () => number = Date.now
): Promise<{ signedXdr: string; rebuilt: boolean }> {
  let built = await buildTx();
  let signedXdr = await signer(built.xdr);

  if (isTransactionExpired(built, now())) {
    built = await buildTx();
    signedXdr = await signer(built.xdr);
    if (isTransactionExpired(built, now())) {
      throw new TransactionExpiredError();
    }
    return { signedXdr, rebuilt: true };
  }

  return { signedXdr, rebuilt: false };
}

// ── Wallet signing ────────────────────────────────────────────────────────────

/**
 * Request the user to sign a transaction via stellar-wallets-kit.
 * Returns the signed XDR string.
 *
 * @throws {UserRejectedError} if the user dismisses the wallet popup.
 */
export async function signTransaction(txXdr: string): Promise<string> {
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

function isTxTooLateReason(reason: string): boolean {
  const normalized = reason.toLowerCase();
  return (
    normalized.includes("txtoo_late") ||
    normalized.includes("tx_too_late") ||
    normalized.includes("txtoolate")
  );
}

/**
 * Submit a signed transaction to the Soroban RPC and poll until it is
 * confirmed or fails.
 *
 * @throws {TransactionExpiredError} if the transaction failed with `txTOO_LATE`.
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
    const errorCode =
      (sendResult.errorResult as { result?: () => { switch?: () => { name?: string } } } | undefined)
        ?.result?.()
        ?.switch?.()?.name ?? "";
    const reason = sendResult.errorResult?.toXDR("base64") ?? "unknown error";
    if (isTxTooLateReason(errorCode) || isTxTooLateReason(reason)) {
      throw new TransactionExpiredError();
    }
    throw new TransactionFailedError(reason);
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
      const resultCode =
        (getResult as { resultXdr?: { result?: () => { switch?: () => { name?: string } } } })
          .resultXdr?.result?.()?.switch?.()?.name ?? "";
      const reason = getResult.resultMetaXdr?.toXDR("base64") ?? "on-chain execution failed";
      if (isTxTooLateReason(resultCode) || isTxTooLateReason(reason)) {
        throw new TransactionExpiredError();
      }
      throw new TransactionFailedError(reason);
    }

    // NOT_FOUND means still pending — keep polling
  }

  throw new TransactionTimeoutError();
}

// ── High-level vault flows ───────────────────────────────────────────────────

function isRealContractId(id: string): boolean {
  return /^C[A-Z0-9]{55}$/.test(id);
}

export async function executeVaultDeposit(params: {
  walletAddress: string;
  vaultId: string;
  contractId: string;
  asset: "USDC" | "XLM";
  amount: number;
}): Promise<TransactionReceipt> {
  const { walletAddress, contractId, amount } = params;

  if (!isRealContractId(contractId)) {
    throw new Error("Vault is not yet live. Deposits are currently disabled.");
  }

  const { signedXdr } = await signTransactionWithRebuild(() =>
    buildDepositTransaction({ walletAddress, contractId, amount })
  );
  return submitTransaction(signedXdr);
}

export async function executeVaultWithdraw(params: {
  walletAddress: string;
  vaultId: string;
  contractId: string;
  asset: "USDC" | "XLM";
  shares: number;
  minAssetsOut?: number;
}): Promise<TransactionReceipt> {
  const { walletAddress, contractId, shares, minAssetsOut } = params;

  if (!isRealContractId(contractId)) {
    throw new Error("Vault is not yet live. Withdrawals are currently disabled.");
  }

  const { signedXdr } = await signTransactionWithRebuild(() =>
    buildWithdrawTransaction({ walletAddress, contractId, shares, minAssetsOut })
  );
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