import {
  Contract,
  Networks,
  rpc as SorobanRpc,
  Transaction,
  TransactionBuilder,
  BASE_FEE,
  nativeToScVal,
  Address,
  xdr,
} from "@stellar/stellar-sdk";

import { NETWORKS, DEFAULT_NETWORK } from "@/lib/networks";

// ── Config ────────────────────────────────────────────────────────────────────

const getCurrentNetwork = () => {
  if (typeof window !== "undefined") {
    const savedNetwork = localStorage.getItem("nester_network_id");
    if (savedNetwork && (savedNetwork === "testnet" || savedNetwork === "mainnet")) {
      return NETWORKS[savedNetwork];
    }
  }
  return DEFAULT_NETWORK;
};

// These are set via environment variables so the contracts can be swapped
// without code changes when moving from testnet to mainnet.
export const VAULT_CONTRACT_ID =
  process.env.NEXT_PUBLIC_VAULT_CONTRACT_ID ?? "";

export const USDC_CONTRACT_ID =
  process.env.NEXT_PUBLIC_USDC_CONTRACT_ID ?? "";

// ── Types ─────────────────────────────────────────────────────────────────────

export interface DepositParams {
  /** Stellar public key of the depositing user. */
  walletAddress: string;
  /** Vault contract ID on Soroban. */
  contractId: string;
  /** USDC token contract ID. */
  tokenAddress: string;
  /** Amount in USDC (human-readable, e.g. 100.50). Converted to stroops internally. */
  amount: number;
}

export interface WithdrawParams {
  walletAddress: string;
  contractId: string;
  tokenAddress: string;
  /** Number of nVault shares to burn. */
  shares: number;
}

export interface BuiltTransaction {
  /** Base64-encoded unsigned transaction XDR ready for signing. */
  xdr: string;
  /** The assembled transaction object (used for submission after signing). */
  transaction: Transaction;
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
  return new SorobanRpc.Server(rpcUrl, { allowHttp: true });
}

// ── Transaction builders ──────────────────────────────────────────────────────

/**
 * Build a Soroban `deposit` contract invocation transaction.
 *
 * The vault contract's `deposit(from, token, amount)` function is called.
 * Amount is converted from human-readable USDC to stroops (7 decimal places).
 */
export async function buildDepositTransaction(
  params: DepositParams
): Promise<BuiltTransaction> {
  const { walletAddress, contractId, tokenAddress, amount } = params;
  const network = getCurrentNetwork();

  const server = getServer(network.rpcUrl);
  const account = await server.getAccount(walletAddress);

  const amountStroops = BigInt(Math.round(amount * 10_000_000));

  const contract = new Contract(contractId);

  const tx = new TransactionBuilder(account, {
    fee: BASE_FEE,
    networkPassphrase: network.networkPassphrase,
  })
    .addOperation(
      contract.call(
        "deposit",
        new Address(walletAddress).toScVal(),
        new Address(tokenAddress).toScVal(),
        nativeToScVal(amountStroops, { type: "i128" })
      )
    )
    .setTimeout(30)
    .build();

  // Simulate to populate the transaction's footprint (Soroban requirement)
  const sim = await server.simulateTransaction(tx);

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
 * The vault contract's `withdraw(from, token, shares)` function is called.
 */
export async function buildWithdrawTransaction(
  params: WithdrawParams
): Promise<BuiltTransaction> {
  const { walletAddress, contractId, tokenAddress, shares } = params;
  const network = getCurrentNetwork();

  const server = getServer(network.rpcUrl);
  const account = await server.getAccount(walletAddress);

  const sharesStroops = BigInt(Math.round(shares * 10_000_000));

  const contract = new Contract(contractId);

  const tx = new TransactionBuilder(account, {
    fee: BASE_FEE,
    networkPassphrase: network.networkPassphrase,
  })
    .addOperation(
      contract.call(
        "withdraw",
        new Address(walletAddress).toScVal(),
        new Address(tokenAddress).toScVal(),
        nativeToScVal(sharesStroops, { type: "i128" })
      )
    )
    .setTimeout(30)
    .build();

  const sim = await server.simulateTransaction(tx);

  if (SorobanRpc.Api.isSimulationError(sim)) {
    throw new TransactionFailedError(
      (sim as SorobanRpc.Api.SimulateTransactionErrorResponse).error
    );
  }

  const assembled = SorobanRpc.assembleTransaction(tx, sim).build();

  return { xdr: assembled.toXDR(), transaction: assembled };
}

// ── Freighter signing ─────────────────────────────────────────────────────────

/**
 * Request the user to sign a transaction via Freighter (or any injected
 * Stellar wallet). Returns the signed XDR string.
 *
 * @throws {UserRejectedError} if the user dismisses the Freighter popup.
 */
export async function signTransaction(txXdr: string): Promise<string> {
  // Access Freighter via the global injected by the browser extension.
  // @creit.tech/stellar-wallets-kit exposes the same interface.
  const freighter = (window as typeof window & { freighter?: {
    signTransaction: (xdr: string, opts: { networkPassphrase: string }) => Promise<{ signedTxXdr: string; error?: string }>
  } }).freighter;

  if (!freighter) {
    throw new Error(
      "No Stellar wallet detected. Please install Freighter (freighter.app) and try again."
    );
  }

  const network = getCurrentNetwork();

  const result = await freighter.signTransaction(txXdr, {
    networkPassphrase: network.networkPassphrase,
  });

  // Freighter signals user rejection via a specific error message string
  if (result.error) {
    const msg = result.error.toLowerCase();
    if (
      msg.includes("user declined") ||
      msg.includes("user rejected") ||
      msg.includes("cancelled") ||
      msg.includes("canceled")
    ) {
      throw new UserRejectedError();
    }
    throw new Error(result.error);
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