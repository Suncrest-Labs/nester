import config from "@/lib/config";

export const USDC_CODE = "USDC";
export const USDC_ISSUER = process.env.NEXT_PUBLIC_USDC_ISSUER ?? "";

/**
 * The connected wallet's holding of the savings asset, read from Horizon.
 * `unfunded` means the account does not exist on the network yet (404).
 * `balance` is Horizon's decimal string (7 decimals).
 */
export type WalletUsdcBalance =
  | { status: "unfunded" }
  | { status: "no_trustline" }
  | { status: "funded"; balance: string };

interface HorizonBalance {
  asset_type: string;
  asset_code?: string;
  asset_issuer?: string;
  balance: string;
}

export function parseWalletUsdcBalance(
  httpStatus: number,
  body: unknown,
  issuer: string = USDC_ISSUER
): WalletUsdcBalance {
  if (httpStatus === 404) return { status: "unfunded" };
  if (httpStatus < 200 || httpStatus >= 300) {
    throw new Error(`Horizon account lookup failed (${httpStatus})`);
  }
  const balances = (body as { balances?: HorizonBalance[] } | null)?.balances ?? [];
  const usdc = balances.find(
    (b) => b.asset_type !== "native" && b.asset_code === USDC_CODE && b.asset_issuer === issuer
  );
  return usdc ? { status: "funded", balance: usdc.balance } : { status: "no_trustline" };
}

export async function fetchWalletUsdcBalance(
  address: string,
  horizonUrl: string = config.stellarHorizonUrl,
  issuer: string = USDC_ISSUER
): Promise<WalletUsdcBalance> {
  const base = horizonUrl.replace(/\/$/, "");
  const res = await fetch(`${base}/accounts/${encodeURIComponent(address)}`);
  const body = res.ok ? await res.json() : null;
  return parseWalletUsdcBalance(res.status, body, issuer);
}
