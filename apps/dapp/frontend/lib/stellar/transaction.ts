import {
  Contract,
  Operation,
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

/**
 * User-facing explanation shown when `simulateTransaction` returns a
 * `restorePreamble` and an archived ledger entry must be restored via a
 * `RestoreFootprintOp` transaction before the deposit/withdraw invocation (#1487).
 */
export const RESTORE_FOOTPRINT_NOTICE =
  "Your position needs to be reactivated first";

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

// ── Types ─────────────────────────────────────────────────────────────────────

export interface DepositParams {
  /** Stellar public key of the depositing user. */
  walletAddress: string;
  /** Vault contract ID on Soroban. */
  contractId: string;
  /** Amount in stroops (bigint, 7 decimals = 1 XLM, 6 decimals = 1 USDC). */
  amount: bigint | number;
  /** Whether to automatically build, sign, and submit a RestoreFootprint tx when needed. Defaults to true. */
  autoRestore?: boolean;
  /** Optional callback fired when a restorePreamble is detected and an extra signature is needed. */
  onRestoreRequired?: (message: string) => void;
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
  /** Whether to automatically build, sign, and submit a RestoreFootprint tx when needed. Defaults to true. */
  autoRestore?: boolean;
  /** Optional callback fired when a restorePreamble is detected and an extra signature is needed. */
  onRestoreRequired?: (message: string) => void;
}

export interface BuiltTransaction {
  /** Base64-encoded unsigned transaction XDR ready for signing. */
  xdr: string;
  /** The assembled transaction object (used for submission after signing). */
  transaction: Transaction;
  /** True if an archived footprint was restored prior to assembling this transaction. */
  restoredFootprint?: boolean;
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
  restoredFootprint?: boolean;
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
    const { transaction } = await buildDepositTransaction({ ...params, autoRestore: false });
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
    const { walletAddress, contractId, amount: rawAmount } = params;
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
    const { transaction } = await buildWithdrawTransaction({ ...params, autoRestore: false });
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

// ── Restore footprint helper (#1487) ─────────────────────────────────────────

/**
 * Build a `RestoreFootprintOp` transaction from `sim.restorePreamble` (#1487).
 *
 * When a Soroban simulation detects archived ledger entries in the footprint,
 * `SorobanRpc.Api.isSimulationRestore(sim)` returns `true` and provides
 * `restorePreamble.transactionData` and `restorePreamble.minResourceFee`.
 * This transaction must be signed and submitted before re-simulating the
 * contract invocation.
 */
export function buildRestoreFootprintTransaction(params: {
  account: Awaited<ReturnType<SorobanRpc.Server["getAccount"]>>;
  networkPassphrase: string;
  restorePreamble: SorobanRpc.Api.SimulateTransactionRestoreResponse["restorePreamble"];
}): BuiltTransaction {
  const { account, networkPassphrase, restorePreamble } = params;
  const minResourceFee = BigInt(restorePreamble.minResourceFee || "0");
  const fee = (BigInt(BASE_FEE) + minResourceFee).toString();
  const sorobanData =
    typeof restorePreamble.transactionData.build === "function"
      ? restorePreamble.transactionData.build()
      : restorePreamble.transactionData;

  const restoreTx = new TransactionBuilder(account, {
    fee,
    networkPassphrase,
  })
    .setSorobanData(sorobanData)
    .addOperation(Operation.restoreFootprint({}))
    .setTimeout(30)
    .build();

  return { xdr: restoreTx.toXDR(), transaction: restoreTx };
}

async function simulateWithRestoreIfNeeded(params: {
  server: SorobanRpc.Server;
  walletAddress: string;
  networkPassphrase: string;
  buildTx: (account: Awaited<ReturnType<SorobanRpc.Server["getAccount"]>>) => Transaction;
  autoRestore?: boolean;
  onRestoreRequired?: (message: string) => void;
}): Promise<{
  tx: Transaction;
  sim: SorobanRpc.Api.SimulateTransactionSuccessResponse;
  restoredFootprint: boolean;
}> {
  const {
    server,
    walletAddress,
    networkPassphrase,
    buildTx,
    autoRestore = true,
    onRestoreRequired,
  } = params;

  const initialAccount = await server.getAccount(walletAddress);
  const initialTx = buildTx(initialAccount);
  const firstSim = await server.simulateTransaction(initialTx);

  if (SorobanRpc.Api.isSimulationError(firstSim)) {
    throw new TransactionFailedError(
      (firstSim as SorobanRpc.Api.SimulateTransactionErrorResponse).error
    );
  }

  if (SorobanRpc.Api.isSimulationRestore(firstSim) && autoRestore) {
    onRestoreRequired?.(RESTORE_FOOTPRINT_NOTICE);
    const { xdr: restoreXdr } = buildRestoreFootprintTransaction({
      account: initialAccount,
      networkPassphrase,
      restorePreamble: firstSim.restorePreamble,
    });
    const signedRestoreXdr = await signTransaction(restoreXdr);
    await submitTransaction(signedRestoreXdr);

    const refreshedAccount = await server.getAccount(walletAddress);
    const rebuiltTx = buildTx(refreshedAccount);
    const secondSim = await server.simulateTransaction(rebuiltTx);

    if (SorobanRpc.Api.isSimulationError(secondSim)) {
      throw new TransactionFailedError(
        (secondSim as SorobanRpc.Api.SimulateTransactionErrorResponse).error
      );
    }

    return {
      tx: rebuiltTx,
      sim: secondSim as SorobanRpc.Api.SimulateTransactionSuccessResponse,
      restoredFootprint: true,
    };
  }

  return {
    tx: initialTx,
    sim: firstSim as SorobanRpc.Api.SimulateTransactionSuccessResponse,
    restoredFootprint: false,
  };
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
    autoRestore = true,
    onRestoreRequired,
  } = params;
  const network = getCurrentNetwork();
  const server = getServer(network.rpcUrl);

  // Convert amount to stroops if it's a number
  const amountStroops = typeof rawAmount === "bigint"
    ? rawAmount
    : BigInt(Math.round(rawAmount * 10_000_000));

  const contract = new Contract(assertValidContractId(contractId, "vault contract"));

  const { tx, sim, restoredFootprint } = await simulateWithRestoreIfNeeded({
    server,
    walletAddress,
    networkPassphrase: network.networkPassphrase,
    autoRestore,
    onRestoreRequired,
    buildTx: (account) =>
      new TransactionBuilder(account, {
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
        .setTimeout(30)
        .build(),
  });

  const assembled = SorobanRpc.assembleTransaction(tx, sim).build();

  return { xdr: assembled.toXDR(), transaction: assembled, restoredFootprint };
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
    autoRestore = true,
    onRestoreRequired,
  } = params;
  const network = getCurrentNetwork();
  const server = getServer(network.rpcUrl);

  // Convert amounts to stroops if they're numbers
  const sharesStroops = typeof rawShares === "bigint"
    ? rawShares
    : BigInt(Math.round(Number(rawShares) * 10_000_000));

  const minAssetsStroops = typeof rawMinAssets === "bigint"
    ? rawMinAssets
    : BigInt(Math.round(Number(rawMinAssets) * 10_000_000));

  const contract = new Contract(assertValidContractId(contractId, "vault contract"));

  const { tx, sim, restoredFootprint } = await simulateWithRestoreIfNeeded({
    server,
    walletAddress,
    networkPassphrase: network.networkPassphrase,
    autoRestore,
    onRestoreRequired,
    buildTx: (account) =>
      new TransactionBuilder(account, {
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
        .setTimeout(30)
        .build(),
  });

  const assembled = SorobanRpc.assembleTransaction(tx, sim).build();

  return { xdr: assembled.toXDR(), transaction: assembled, restoredFootprint };
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

function isRealContractId(id: string): boolean {
  return /^C[A-Z0-9]{55}$/.test(id);
}

export async function executeVaultDeposit(params: {
  walletAddress: string;
  vaultId: string;
  contractId: string;
  asset: "USDC" | "XLM";
  amount: number;
  onRestoreRequired?: (message: string) => void;
}): Promise<TransactionReceipt> {
  const { walletAddress, contractId, amount, onRestoreRequired } = params;

  if (!isRealContractId(contractId)) {
    throw new Error("Vault is not yet live. Deposits are currently disabled.");
  }

  const { xdr, restoredFootprint } = await buildDepositTransaction({
    walletAddress,
    contractId,
    amount,
    onRestoreRequired,
  });
  const signedXdr = await signTransaction(xdr);
  const receipt = await submitTransaction(signedXdr);
  return { ...receipt, restoredFootprint };
}

export async function executeVaultWithdraw(params: {
  walletAddress: string;
  vaultId: string;
  contractId: string;
  asset: "USDC" | "XLM";
  shares: number;
  minAssetsOut?: number;
  onRestoreRequired?: (message: string) => void;
}): Promise<TransactionReceipt> {
  const { walletAddress, contractId, shares, minAssetsOut, onRestoreRequired } = params;

  if (!isRealContractId(contractId)) {
    throw new Error("Vault is not yet live. Withdrawals are currently disabled.");
  }

  const { xdr, restoredFootprint } = await buildWithdrawTransaction({
    walletAddress,
    contractId,
    shares,
    minAssetsOut,
    onRestoreRequired,
  });
  const signedXdr = await signTransaction(xdr);
  const receipt = await submitTransaction(signedXdr);
  return { ...receipt, restoredFootprint };
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