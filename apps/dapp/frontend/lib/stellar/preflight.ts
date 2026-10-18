import {
  Account,
  Asset,
  BASE_FEE,
  Operation,
  TransactionBuilder,
} from "@stellar/stellar-sdk";
import { NETWORKS, DEFAULT_NETWORK } from "@/lib/networks";
import { readNetworkId } from "@/lib/storageKeys";

function resolveActiveNetwork() {
  const saved = readNetworkId();
  return saved && NETWORKS[saved] ? NETWORKS[saved] : DEFAULT_NETWORK;
}

export const BASE_RESERVE_XLM = 0.5;
export const DEFAULT_SOROBAN_FEE_HEADROOM_XLM = 0.1;

export const DEFAULT_USDC_ISSUERS: Record<"testnet" | "mainnet", string> = {
  testnet:
    process.env.NEXT_PUBLIC_TESTNET_USDC_ISSUER ||
    "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5",
  mainnet:
    process.env.NEXT_PUBLIC_MAINNET_USDC_ISSUER ||
    "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
};

export interface HorizonBalanceLine {
  asset_type: string;
  asset_code?: string;
  asset_issuer?: string;
  balance: string;
}

export interface HorizonAccountPayload {
  id?: string;
  sequence: string;
  subentry_count: number;
  num_sponsoring?: number;
  num_sponsored?: number;
  balances: HorizonBalanceLine[];
}

export type DepositPreflightStatus =
  | "ok"
  | "unfunded_account"
  | "missing_usdc_trustline"
  | "insufficient_usdc"
  | "insufficient_xlm_reserve";

export interface DepositPreflightResult {
  ok: boolean;
  status: DepositPreflightStatus;
  message?: string;
  canAddTrustline?: boolean;
  usdcIssuer?: string;
  requiredXlmReserve: number;
  availableXlm: number;
  availableUsdc: number;
  accountSequence?: string;
}

export interface EvaluatePreflightOptions {
  accountNotFound?: boolean;
  asset: "USDC" | "XLM";
  amount: number;
  usdcIssuer?: string;
  feeHeadroomXlm?: number;
}

export function calculateMinimumXlmReserve(
  subentryCount: number,
  numSponsoring = 0,
  numSponsored = 0,
  feeHeadroomXlm = DEFAULT_SOROBAN_FEE_HEADROOM_XLM,
): number {
  const effectiveEntries = Math.max(
    2,
    2 + subentryCount + numSponsoring - numSponsored,
  );
  return Number((effectiveEntries * BASE_RESERVE_XLM + feeHeadroomXlm).toFixed(7));
}

export function evaluateHorizonAccountPreflight(
  account: HorizonAccountPayload | null,
  options: EvaluatePreflightOptions,
): DepositPreflightResult {
  const feeHeadroom = options.feeHeadroomXlm ?? DEFAULT_SOROBAN_FEE_HEADROOM_XLM;

  if (options.accountNotFound || !account) {
    const minNeeded = calculateMinimumXlmReserve(0, 0, 0, feeHeadroom);
    return {
      ok: false,
      status: "unfunded_account",
      message: `Your Stellar account is not funded on-chain yet. Fund your wallet with at least ${minNeeded.toFixed(1)} XLM to activate it before depositing.`,
      requiredXlmReserve: minNeeded,
      availableXlm: 0,
      availableUsdc: 0,
    };
  }

  const nativeLine = account.balances.find((b) => b.asset_type === "native");
  const availableXlm = nativeLine ? Number(nativeLine.balance) || 0 : 0;

  const usdcLine = account.balances.find(
    (b) =>
      b.asset_type !== "native" &&
      b.asset_code === "USDC" &&
      (!options.usdcIssuer || b.asset_issuer === options.usdcIssuer),
  );
  const availableUsdc = usdcLine ? Number(usdcLine.balance) || 0 : 0;

  const requiredXlmReserve = calculateMinimumXlmReserve(
    account.subentry_count ?? 0,
    account.num_sponsoring ?? 0,
    account.num_sponsored ?? 0,
    feeHeadroom,
  );

  if (options.asset === "USDC" && !usdcLine) {
    return {
      ok: false,
      status: "missing_usdc_trustline",
      canAddTrustline: true,
      usdcIssuer: options.usdcIssuer,
      message:
        "Your wallet does not have a USDC trustline yet. Add a USDC trustline before depositing USDC.",
      requiredXlmReserve,
      availableXlm,
      availableUsdc: 0,
      accountSequence: account.sequence,
    };
  }

  if (options.asset === "USDC" && options.amount > 0 && availableUsdc < options.amount) {
    return {
      ok: false,
      status: "insufficient_usdc",
      message: `Insufficient on-chain USDC balance. You have ${availableUsdc.toFixed(2)} USDC available, but entered ${options.amount.toFixed(2)} USDC.`,
      requiredXlmReserve,
      availableXlm,
      availableUsdc,
      accountSequence: account.sequence,
    };
  }

  const totalRequiredXlm =
    options.asset === "XLM" && options.amount > 0
      ? Number((requiredXlmReserve + options.amount).toFixed(7))
      : requiredXlmReserve;

  if (availableXlm < totalRequiredXlm) {
    return {
      ok: false,
      status: "insufficient_xlm_reserve",
      message: `Insufficient XLM for account reserve and Soroban fees. You need at least ${totalRequiredXlm.toFixed(2)} XLM (${requiredXlmReserve.toFixed(2)} XLM reserve + fee buffer), but have ${availableXlm.toFixed(2)} XLM.`,
      requiredXlmReserve: totalRequiredXlm,
      availableXlm,
      availableUsdc,
      accountSequence: account.sequence,
    };
  }

  return {
    ok: true,
    status: "ok",
    requiredXlmReserve,
    availableXlm,
    availableUsdc,
    accountSequence: account.sequence,
  };
}

export async function fetchDepositPreflight(params: {
  walletAddress: string;
  asset: "USDC" | "XLM";
  amount: number;
  horizonUrl?: string;
  usdcIssuer?: string;
  fetchFn?: typeof fetch;
}): Promise<DepositPreflightResult | null> {
  const fetcher = params.fetchFn ?? (typeof fetch === "function" ? fetch : undefined);
  if (!fetcher || !params.walletAddress) return null;

  const network = resolveActiveNetwork();
  const horizonBase = (params.horizonUrl ?? network.horizonUrl).replace(/\/+$/, "");
  const usdcIssuer = params.usdcIssuer ?? DEFAULT_USDC_ISSUERS[network.id];

  try {
    const res = await fetcher(
      `${horizonBase}/accounts/${encodeURIComponent(params.walletAddress)}`,
      { headers: { Accept: "application/json" } },
    );
    if (res.status === 404) {
      return evaluateHorizonAccountPreflight(null, {
        accountNotFound: true,
        asset: params.asset,
        amount: params.amount,
        usdcIssuer,
      });
    }
    if (!res.ok) {
      return null;
    }
    const payload = (await res.json()) as HorizonAccountPayload;
    if (!payload || !Array.isArray(payload.balances)) {
      return null;
    }
    return evaluateHorizonAccountPreflight(payload, {
      asset: params.asset,
      amount: params.amount,
      usdcIssuer,
    });
  } catch {
    return null;
  }
}

export function buildAddUsdcTrustlineTransaction(params: {
  walletAddress: string;
  accountSequence: string;
  usdcIssuer?: string;
  networkPassphrase?: string;
}): string {
  const network = resolveActiveNetwork();
  const issuer = params.usdcIssuer ?? DEFAULT_USDC_ISSUERS[network.id];
  const passphrase = params.networkPassphrase ?? network.networkPassphrase;

  const account = new Account(params.walletAddress, params.accountSequence);
  const usdcAsset = new Asset("USDC", issuer);

  const tx = new TransactionBuilder(account, {
    fee: BASE_FEE,
    networkPassphrase: passphrase,
  })
    .addOperation(
      Operation.changeTrust({
        asset: usdcAsset,
      }),
    )
    .setTimeout(180)
    .build();

  return tx.toXDR();
}
