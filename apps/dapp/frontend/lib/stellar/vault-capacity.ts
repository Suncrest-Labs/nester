import {
  Contract,
  rpc as SorobanRpc,
  TransactionBuilder,
  BASE_FEE,
  scValToNative,
} from "@stellar/stellar-sdk";
import { assertValidContractId } from "@/lib/contracts";
import { getCurrentNetwork } from "@/lib/stellar/transaction";

export type VaultCapacityStatus = "open" | "nearly_full" | "full";

export interface VaultCapacityInput {
  /** Total assets currently held in the vault (on-chain `total_assets` or TVL). */
  totalAssets?: number | null;
  /** Maximum total assets permitted under the active deposit cap. */
  depositCap?: number | null;
  /** Connected user's available wallet balance for the selected asset. */
  userBalance: number;
  /** Asset symbol (e.g. "USDC" or "XLM"). */
  asset?: string;
}

export interface VaultCapacityState {
  hasCap: boolean;
  filled: number;
  cap: number | null;
  remainingRoom: number;
  fillRatio: number;
  status: VaultCapacityStatus;
  /** Clamped maximum deposit amount: min(userBalance, remainingRoom). */
  maxDepositable: number;
  /** Formatted "X of Y USDC filled" string when a cap is active. */
  filledLabel: string | null;
}

/** Threshold at or above which a capped vault is considered "nearly full". */
export const NEARLY_FULL_THRESHOLD_RATIO = 0.85;

function formatAmount(amount: number): string {
  return amount.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

/**
 * Computes the vault capacity state, clamping the user's max deposit to the
 * remaining room under the deposit cap (issue #1558).
 */
export function computeVaultCapacityState({
  totalAssets,
  depositCap,
  userBalance,
  asset = "USDC",
}: VaultCapacityInput): VaultCapacityState {
  const safeBalance = Number.isFinite(userBalance) ? Math.max(0, userBalance) : 0;
  const filled =
    typeof totalAssets === "number" && Number.isFinite(totalAssets)
      ? Math.max(0, totalAssets)
      : 0;

  const hasCap =
    typeof depositCap === "number" &&
    Number.isFinite(depositCap) &&
    depositCap > 0;

  if (!hasCap || depositCap == null) {
    return {
      hasCap: false,
      filled,
      cap: null,
      remainingRoom: Number.POSITIVE_INFINITY,
      fillRatio: 0,
      status: "open",
      maxDepositable: safeBalance,
      filledLabel: null,
    };
  }

  const remainingRoom = Math.max(0, depositCap - filled);
  const fillRatio = Math.min(1, filled / depositCap);

  let status: VaultCapacityStatus = "open";
  if (remainingRoom <= 0) {
    status = "full";
  } else if (fillRatio >= NEARLY_FULL_THRESHOLD_RATIO) {
    status = "nearly_full";
  }

  const maxDepositable = Math.max(0, Math.min(safeBalance, remainingRoom));
  const filledLabel = `${formatAmount(filled)} of ${formatAmount(depositCap)} ${asset} filled`;

  return {
    hasCap: true,
    filled,
    cap: depositCap,
    remainingRoom,
    fillRatio,
    status,
    maxDepositable,
    filledLabel,
  };
}

/**
 * Reads `total_assets()` and `get_max_deposit()` from the Soroban vault
 * contract via `simulateTransaction`. Returns null values if the contract is
 * not deployed or simulation is unavailable.
 */
export async function fetchOnChainVaultCapacity(params: {
  contractId: string;
  walletAddress: string;
  decimals?: number;
}): Promise<{ totalAssets: number | null; depositCap: number | null }> {
  const { contractId, walletAddress, decimals = 7 } = params;
  try {
    const validContract = assertValidContractId(contractId, "vault contract");
    const network = getCurrentNetwork();
    const isProd = process.env.NODE_ENV === "production";
    const server = new SorobanRpc.Server(network.rpcUrl, { allowHttp: !isProd });
    const account = await server.getAccount(walletAddress);
    const contract = new Contract(validContract);
    const scale = Math.pow(10, decimals);

    const simulateRead = async (method: string): Promise<bigint | null> => {
      const tx = new TransactionBuilder(account, {
        fee: BASE_FEE,
        networkPassphrase: network.networkPassphrase,
      })
        .addOperation(contract.call(method))
        .setTimeout(30)
        .build();

      const sim = await server.simulateTransaction(tx);
      if (SorobanRpc.Api.isSimulationError(sim)) {
        return null;
      }
      const result = (sim as SorobanRpc.Api.SimulateTransactionSuccessResponse).result;
      if (!result) return null;
      return BigInt(scValToNative(result.retval));
    };

    const [rawTotalAssets, rawMaxDeposit] = await Promise.all([
      simulateRead("total_assets"),
      simulateRead("get_max_deposit"),
    ]);

    const totalAssets =
      rawTotalAssets !== null && rawTotalAssets >= BigInt(0)
        ? Number(rawTotalAssets) / scale
        : null;

    // i128::MAX indicates no cap configured on the contract.
    const I128_UNBOUNDED_THRESHOLD = BigInt("1000000000000000000000000");
    const depositCap =
      rawMaxDeposit !== null &&
      rawMaxDeposit > BigInt(0) &&
      rawMaxDeposit < I128_UNBOUNDED_THRESHOLD
        ? Number(rawMaxDeposit) / scale
        : null;

    return { totalAssets, depositCap };
  } catch {
    return { totalAssets: null, depositCap: null };
  }
}
