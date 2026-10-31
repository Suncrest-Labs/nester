import {
  Address,
  BASE_FEE,
  Contract,
  rpc as SorobanRpc,
  TransactionBuilder,
  nativeToScVal,
  scValToNative,
} from "@stellar/stellar-sdk";

import { requireContractId } from "@/lib/contracts";
import { depositAmountToStroops } from "@/lib/decimal";
import { applySlippage } from "@/lib/slippage";
import {
  TX_TIMEOUT_SECONDS,
  TransactionFailedError,
  assertWalletOnDappNetwork,
  getCurrentNetwork,
  getServer,
  signTransaction,
  signWithExpiryRebuild,
  submitTransaction,
  type BuiltTransaction,
  type TransactionReceipt,
} from "@/lib/stellar/transaction";

export interface SavingsDepositParams {
  wallet: string;
  /** USDC as a decimal string (at most 7 decimals) or bigint stroops. */
  amount: string | bigint;
  /** Minimum shares accepted, in stroops (slippage guard). */
  minShares: bigint;
}

export interface SavingsRedeemParams {
  wallet: string;
  /** Shares to burn, in stroops. */
  shares: bigint;
  /** Minimum USDC accepted, in stroops (slippage guard). */
  minAssets: bigint;
}

type ScVal = ReturnType<typeof nativeToScVal>;

function i128(value: bigint): ScVal {
  return nativeToScVal(value, { type: "i128" });
}

async function buildCoreCall(
  wallet: string,
  method: string,
  args: ScVal[]
): Promise<BuiltTransaction> {
  const network = getCurrentNetwork();
  const contract = new Contract(requireContractId("savingsCore"));
  const server = getServer(network.rpcUrl);
  const account = await server.getAccount(wallet);

  const tx = new TransactionBuilder(account, {
    fee: BASE_FEE,
    networkPassphrase: network.networkPassphrase,
  })
    .addOperation(contract.call(method, new Address(wallet).toScVal(), ...args))
    .setTimeout(TX_TIMEOUT_SECONDS)
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

function depositTx({ wallet, amount, minShares }: SavingsDepositParams) {
  if (minShares < BigInt(0)) throw new Error("minShares must not be negative");
  const stroops = depositAmountToStroops(amount);
  return buildCoreCall(wallet, "deposit", [i128(stroops), i128(minShares)]);
}

function redeemTx({ wallet, shares, minAssets }: SavingsRedeemParams) {
  if (shares <= BigInt(0)) throw new Error("Shares must be greater than zero");
  if (minAssets < BigInt(0)) throw new Error("minAssets must not be negative");
  return buildCoreCall(wallet, "redeem", [i128(shares), i128(minAssets)]);
}

/** Builds, simulates and wallet-signs `deposit(from, assets, min_shares)`; returns the signed XDR. */
export async function buildSavingsDeposit(params: SavingsDepositParams): Promise<string> {
  await assertWalletOnDappNetwork();
  return signWithExpiryRebuild(() => depositTx(params), signTransaction);
}

/** Builds, simulates and wallet-signs `redeem(owner, shares, min_assets)`; returns the signed XDR. */
export async function buildSavingsRedeem(params: SavingsRedeemParams): Promise<string> {
  await assertWalletOnDappNetwork();
  return signWithExpiryRebuild(() => redeemTx(params), signTransaction);
}

export async function executeSavingsDeposit(
  params: SavingsDepositParams
): Promise<TransactionReceipt> {
  return submitTransaction(await buildSavingsDeposit(params));
}

export async function executeSavingsRedeem(
  params: SavingsRedeemParams
): Promise<TransactionReceipt> {
  return submitTransaction(await buildSavingsRedeem(params));
}

async function simulateReadRaw(
  wallet: string,
  method: string,
  args: ScVal[]
): Promise<unknown> {
  const network = getCurrentNetwork();
  const contract = new Contract(requireContractId("savingsCore"));
  const server = getServer(network.rpcUrl);
  const account = await server.getAccount(wallet);
  const tx = new TransactionBuilder(account, {
    fee: BASE_FEE,
    networkPassphrase: network.networkPassphrase,
  })
    .addOperation(contract.call(method, ...args))
    .setTimeout(TX_TIMEOUT_SECONDS)
    .build();
  const sim = await server.simulateTransaction(tx);
  if (SorobanRpc.Api.isSimulationError(sim)) {
    throw new TransactionFailedError(
      (sim as SorobanRpc.Api.SimulateTransactionErrorResponse).error
    );
  }
  const result = (sim as SorobanRpc.Api.SimulateTransactionSuccessResponse).result;
  if (!result) {
    throw new Error(`Savings quote unavailable: ${method} simulation returned no result`);
  }
  return scValToNative(result.retval);
}

async function simulateRead(wallet: string, method: string, arg: bigint): Promise<bigint> {
  return BigInt((await simulateReadRaw(wallet, method, [i128(arg)])) as bigint);
}

/** Whether the guardian has paused new deposits (withdrawals are never paused). */
export async function fetchDepositsPaused(wallet: string): Promise<boolean> {
  return (await simulateReadRaw(wallet, "deposits_paused", [])) === true;
}

/** minShares for a deposit: convert_to_shares(assets) less the slippage tolerance. */
export async function quoteMinShares(
  wallet: string,
  amount: string | bigint,
  slippageBps: number
): Promise<bigint> {
  const expected = await simulateRead(wallet, "convert_to_shares", depositAmountToStroops(amount));
  if (expected <= BigInt(0)) throw new Error("Savings quote unavailable: zero shares");
  return applySlippage(expected, slippageBps);
}

/** minAssets for a redeem: convert_to_assets(shares) less the slippage tolerance. */
export async function quoteMinAssets(
  wallet: string,
  shares: bigint,
  slippageBps: number
): Promise<bigint> {
  const expected = await simulateRead(wallet, "convert_to_assets", shares);
  if (expected <= BigInt(0)) throw new Error("Savings quote unavailable: zero assets");
  return applySlippage(expected, slippageBps);
}
