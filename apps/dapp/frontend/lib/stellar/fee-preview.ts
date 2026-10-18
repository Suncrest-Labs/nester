import {
  Address,
  BASE_FEE,
  Contract,
  nativeToScVal,
  rpc as SorobanRpc,
  scValToNative,
  TransactionBuilder,
} from "@stellar/stellar-sdk";
import { assertValidContractId } from "@/lib/contracts";
import { getCurrentNetwork } from "@/lib/stellar/transaction";

const STROOPS_PER_UNIT = 10_000_000;

export interface RawContractWithdrawalFeePreview {
  gross_asset_value?: bigint | number | string;
  management_fee_deducted?: bigint | number | string;
  performance_fee_deducted?: bigint | number | string;
  early_withdrawal_fee_deducted?: bigint | number | string;
  net_amount_received?: bigint | number | string;
}

export interface RawContractFeeSchedulePreview {
  tenure_secs?: bigint | number | string;
  current_exit_fee_bps?: bigint | number | string;
  current_performance_fee_bps?: bigint | number | string;
  next_boundary_secs?: bigint | number | string | null;
  next_boundary_exit_fee_bps?: bigint | number | string;
  next_boundary_perf_fee_bps?: bigint | number | string;
}

export interface WithdrawalFeePreviewResult {
  grossAssetValue: number;
  feeAmount: number;
  earlyWithdrawalFee: number;
  performanceFee: number;
  managementFee: number;
  netReceived: number;
  currentTierBps: number;
  currentTierLabel: string;
  nextLowerTierDate: string | null;
  nextLowerTierBps: number | null;
  source: "on-chain" | "fallback";
}

export interface DepositFeeScheduleSummary {
  depositFeeLabel: string;
  managementFeeLabel: string;
  performanceFeeLabel: string;
  earlyExitTiersLabel: string;
  scheduleHref: string;
}

function toUnits(rawStroops: bigint | number | string | undefined): number {
  if (rawStroops === undefined || rawStroops === null) return 0;
  const num = typeof rawStroops === "bigint" ? Number(rawStroops) : Number(rawStroops);
  if (!Number.isFinite(num)) return 0;
  return num / STROOPS_PER_UNIT;
}

export function formatBpsAsPercent(bps: number): string {
  return `${(bps / 100).toFixed(2)}%`;
}

export function formatTierDate(timestampMs: number): string {
  return new Date(timestampMs).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  });
}

/**
 * Compute a normalized `WithdrawalFeePreviewResult` from raw contract simulation
 * values (`withdrawal_fee_preview` + optional `fee_schedule_preview`), or fallback
 * quote data when off-chain.
 */
export function buildWithdrawalFeePreview(params: {
  rawPreview: RawContractWithdrawalFeePreview;
  rawSchedule?: RawContractFeeSchedulePreview | null;
  daysRemaining?: number;
  fallbackExitFeeBps?: number;
  nowMs?: number;
  source?: "on-chain" | "fallback";
}): WithdrawalFeePreviewResult {
  const {
    rawPreview,
    rawSchedule,
    daysRemaining = 0,
    fallbackExitFeeBps = 50,
    nowMs = Date.now(),
    source = "on-chain",
  } = params;

  const grossAssetValue = toUnits(rawPreview.gross_asset_value);
  const earlyWithdrawalFee = toUnits(rawPreview.early_withdrawal_fee_deducted);
  const performanceFee = toUnits(rawPreview.performance_fee_deducted);
  const managementFee = toUnits(rawPreview.management_fee_deducted);
  const netReceived = toUnits(rawPreview.net_amount_received);
  const feeAmount = Math.max(0, earlyWithdrawalFee + performanceFee + managementFee);

  const inferredBps =
    grossAssetValue > 0
      ? Math.round((earlyWithdrawalFee / grossAssetValue) * 10_000)
      : fallbackExitFeeBps;

  const currentTierBps =
    rawSchedule?.current_exit_fee_bps !== undefined
      ? Number(rawSchedule.current_exit_fee_bps)
      : inferredBps;

  const currentTierLabel =
    currentTierBps === 0
      ? "Matured Tier (0.00% exit fee)"
      : `Early Exit Tier (${formatBpsAsPercent(currentTierBps)})`;

  let nextLowerTierDate: string | null = null;
  let nextLowerTierBps: number | null = null;

  if (rawSchedule && rawSchedule.next_boundary_secs !== undefined && rawSchedule.next_boundary_secs !== null) {
    const tenureSecs = Number(rawSchedule.tenure_secs ?? 0);
    const nextBoundarySecs = Number(rawSchedule.next_boundary_secs);
    if (Number.isFinite(nextBoundarySecs) && nextBoundarySecs > tenureSecs) {
      const deltaMs = (nextBoundarySecs - tenureSecs) * 1000;
      nextLowerTierDate = formatTierDate(nowMs + deltaMs);
      nextLowerTierBps =
        rawSchedule.next_boundary_exit_fee_bps !== undefined
          ? Number(rawSchedule.next_boundary_exit_fee_bps)
          : 0;
    }
  } else if (currentTierBps > 0 && daysRemaining > 0) {
    nextLowerTierDate = formatTierDate(nowMs + daysRemaining * 24 * 60 * 60 * 1000);
    nextLowerTierBps = 0;
  }

  return {
    grossAssetValue,
    feeAmount,
    earlyWithdrawalFee,
    performanceFee,
    managementFee,
    netReceived,
    currentTierBps,
    currentTierLabel,
    nextLowerTierDate,
    nextLowerTierBps,
    source,
  };
}

/**
 * Simulate the vault contract's `withdrawal_fee_preview(user, shares)` (and
 * `fee_schedule_preview(user)` when supported) so the user sees the exact
 * fee amount, current tier, next lower tier date, and net amount before signing (#1556).
 */
export async function fetchOnChainWithdrawalFeePreview(params: {
  walletAddress: string;
  contractId: string;
  shares: number;
  daysRemaining?: number;
  fallbackGross?: number;
  fallbackPenaltyPct?: number;
  nowMs?: number;
  rpcServer?: Pick<SorobanRpc.Server, "getAccount" | "simulateTransaction">;
}): Promise<WithdrawalFeePreviewResult> {
  const {
    walletAddress,
    contractId,
    shares,
    daysRemaining = 0,
    fallbackGross = shares,
    fallbackPenaltyPct = 0.5,
    nowMs = Date.now(),
  } = params;

  const sharesStroops = BigInt(Math.max(0, Math.round(shares * STROOPS_PER_UNIT)));

  try {
    const validContractId = assertValidContractId(contractId, "vault contract");
    const network = getCurrentNetwork();
    const server =
      params.rpcServer ??
      new SorobanRpc.Server(network.rpcUrl, {
        allowHttp: process.env.NODE_ENV !== "production",
      });

    const account = await server.getAccount(walletAddress);
    const contract = new Contract(validContractId);
    const userScVal = new Address(walletAddress).toScVal();

    const previewTx = new TransactionBuilder(account, {
      fee: BASE_FEE,
      networkPassphrase: network.networkPassphrase,
    })
      .addOperation(
        contract.call(
          "withdrawal_fee_preview",
          userScVal,
          nativeToScVal(sharesStroops, { type: "i128" }),
        ),
      )
      .setTimeout(30)
      .build();

    const previewSim = await server.simulateTransaction(previewTx);
    if (SorobanRpc.Api.isSimulationError(previewSim)) {
      throw new Error(previewSim.error);
    }

    const previewRetval = (previewSim as SorobanRpc.Api.SimulateTransactionSuccessResponse).result?.retval;
    const rawPreview = (previewRetval ? scValToNative(previewRetval) : {}) as RawContractWithdrawalFeePreview;

    let rawSchedule: RawContractFeeSchedulePreview | null = null;
    try {
      const scheduleTx = new TransactionBuilder(account, {
        fee: BASE_FEE,
        networkPassphrase: network.networkPassphrase,
      })
        .addOperation(contract.call("fee_schedule_preview", userScVal))
        .setTimeout(30)
        .build();
      const scheduleSim = await server.simulateTransaction(scheduleTx);
      if (!SorobanRpc.Api.isSimulationError(scheduleSim)) {
        const schedRet = (scheduleSim as SorobanRpc.Api.SimulateTransactionSuccessResponse).result?.retval;
        if (schedRet) {
          rawSchedule = scValToNative(schedRet) as RawContractFeeSchedulePreview;
        }
      }
    } catch {
      // Optional enrichment if contract deployment predates fee_schedule_preview
    }

    return buildWithdrawalFeePreview({
      rawPreview,
      rawSchedule,
      daysRemaining,
      fallbackExitFeeBps: Math.round(fallbackPenaltyPct * 100),
      nowMs,
      source: "on-chain",
    });
  } catch {
    const grossStroops = BigInt(Math.max(0, Math.round(fallbackGross * STROOPS_PER_UNIT)));
    const activePenaltyPct = daysRemaining > 0 ? fallbackPenaltyPct : 0;
    const feeStroops = BigInt(Math.round(Number(grossStroops) * (activePenaltyPct / 100)));
    const netStroops = grossStroops > feeStroops ? grossStroops - feeStroops : BigInt(0);

    return buildWithdrawalFeePreview({
      rawPreview: {
        gross_asset_value: grossStroops,
        early_withdrawal_fee_deducted: feeStroops,
        performance_fee_deducted: BigInt(0),
        management_fee_deducted: BigInt(0),
        net_amount_received: netStroops,
      },
      daysRemaining,
      fallbackExitFeeBps: Math.round(activePenaltyPct * 100),
      nowMs,
      source: "fallback",
    });
  }
}

/**
 * Return the pre-deposit fee schedule summary and documentation link (#1556).
 */
export function getDepositFeeScheduleSummary(options?: {
  managementFeePct?: number;
  performanceFeePct?: number;
  earlyExitPenaltyPct?: number;
  lockDays?: number;
}): DepositFeeScheduleSummary {
  const mgmt = options?.managementFeePct ?? 0.5;
  const perf = options?.performanceFeePct ?? 10;
  const exitPct = options?.earlyExitPenaltyPct ?? 0.5;
  const lockDays = options?.lockDays ?? 30;

  return {
    depositFeeLabel: "0.00% (No deposit fee)",
    managementFeeLabel: `${mgmt.toFixed(2)}% annualized`,
    performanceFeeLabel: `${perf}% on net yield only`,
    earlyExitTiersLabel:
      lockDays > 0
        ? `${exitPct.toFixed(2)}% before ${lockDays}d maturity → 0.00% after maturity`
        : "0.50% (< 7d) → 0.25% (7–30d) → 0.00% (≥ 30d)",
    scheduleHref: "/docs/fees#fee-schedule",
  };
}
