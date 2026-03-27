import { StatusCodes } from "http-status-codes";

import { ServiceResponse } from "@/common/models/serviceResponse";
import { env } from "@/common/utils/envConfig";
import { logger } from "@/server";
import type { AssetBalance, BalanceValidationResponse, BalancesMap, PaymentValidationResult } from "./stellarModel";

const HORIZON_URL = env.HORIZON_URL ?? "https://horizon-testnet.stellar.org";

export function buildBalancesMap(balances: AssetBalance[]): BalancesMap {
  const map: BalancesMap = {};
  for (const entry of balances) {
    const key = entry.asset_type === "native" ? "XLM" : `${entry.asset_code}:${entry.asset_issuer}`;
    map[key] = Number(entry.balance);
  }
  return map;
}

export function resolveAssetKey(assetCode: string, assetIssuer?: string): string {
  if (assetCode === "XLM" && !assetIssuer) {
    return "XLM";
  }
  return `${assetCode}:${assetIssuer}`;
}

export async function fetchAccountBalances(accountId: string): Promise<AssetBalance[]> {
  const response = await fetch(`${HORIZON_URL}/accounts/${accountId}`);
  if (!response.ok) {
    if (response.status === 404) {
      throw new Error(`Account ${accountId} not found on the network`);
    }
    throw new Error(`Horizon API error: ${response.status}`);
  }
  const data = (await response.json()) as { balances: AssetBalance[] };
  return data.balances;
}

export async function validateBalances(
  accountId: string,
  payments: { asset_code: string; asset_issuer?: string; amount: string }[],
): Promise<ServiceResponse<BalanceValidationResponse | null>> {
  try {
    const balances = await fetchAccountBalances(accountId);
    const balancesMap = buildBalancesMap(balances);

    // Aggregate required amounts per asset (handles multiple payments of the same asset)
    const requiredByAsset: Record<string, number> = {};
    for (const payment of payments) {
      const key = resolveAssetKey(payment.asset_code, payment.asset_issuer);
      requiredByAsset[key] = (requiredByAsset[key] ?? 0) + Number(payment.amount);
    }

    const results: PaymentValidationResult[] = [];
    let allSufficient = true;

    for (const [assetKey, required] of Object.entries(requiredByAsset)) {
      const available = balancesMap[assetKey] ?? 0;
      const sufficient = available >= required;
      if (!sufficient) allSufficient = false;

      results.push({ asset_key: assetKey, required, available, sufficient });
    }

    return ServiceResponse.success<BalanceValidationResponse>(
      allSufficient ? "All balances sufficient" : "Insufficient balance for one or more assets",
      { account_id: accountId, all_sufficient: allSufficient, results },
    );
  } catch (ex) {
    const message = ex instanceof Error ? ex.message : "Failed to validate balances";
    logger.error(`Balance validation error: ${message}`);
    return ServiceResponse.failure(message, null, StatusCodes.BAD_GATEWAY);
  }
}
