import Decimal from "decimal.js";
import { scValToNative, xdr } from "@stellar/stellar-sdk";
import config from "@/lib/config";

export const ACTIVITY_PAGE_SIZE = 50;

export type SavingsActivityKind = "deposit" | "withdraw";

export interface SavingsActivityItem {
  id: string;
  kind: SavingsActivityKind;
  /** USDC moved, as a decimal string. */
  amount: string;
  hash: string;
  createdAt: string;
}

export interface SavingsActivityPage {
  items: SavingsActivityItem[];
  /** Number of operations read from Horizon for this page. */
  scanned: number;
  /** Cursor for the next page, or null when Horizon has no more operations. */
  nextCursor: string | null;
}

interface HorizonParameter {
  type: string;
  value: string;
}

interface HorizonBalanceChange {
  type: string;
  from?: string;
  to?: string;
  amount: string;
}

export interface HorizonOperation {
  id: string;
  paging_token: string;
  type: string;
  transaction_successful?: boolean;
  transaction_hash: string;
  created_at: string;
  parameters?: HorizonParameter[] | null;
  asset_balance_changes?: HorizonBalanceChange[] | null;
}

const KIND_BY_FUNCTION = new Map<unknown, SavingsActivityKind>([
  ["deposit", "deposit"],
  ["redeem", "withdraw"],
  ["withdraw", "withdraw"],
]);

function decodeParameter(param: HorizonParameter | undefined): unknown {
  if (!param) return undefined;
  try {
    return scValToNative(xdr.ScVal.fromXDR(param.value, "base64"));
  } catch {
    return undefined;
  }
}

function parseOperation(
  op: HorizonOperation,
  wallet: string,
  coreId: string
): SavingsActivityItem | null {
  if (op.type !== "invoke_host_function" || op.transaction_successful === false) return null;
  const params = op.parameters ?? [];
  if (decodeParameter(params[0]) !== coreId) return null;
  const kind = KIND_BY_FUNCTION.get(decodeParameter(params[1]));
  if (!kind) return null;

  const total = (op.asset_balance_changes ?? [])
    .filter((c) => c.type === "transfer" && (kind === "deposit" ? c.from === wallet : c.to === wallet))
    .reduce((sum, c) => sum.plus(c.amount), new Decimal(0));
  if (total.isZero()) return null;

  return {
    id: op.id,
    kind,
    amount: total.toFixed(),
    hash: op.transaction_hash,
    createdAt: op.created_at,
  };
}

/** Keeps the wallet's successful deposit and withdraw calls into the savings core. */
export function parseSavingsOperations(
  records: HorizonOperation[],
  wallet: string,
  coreId: string
): SavingsActivityItem[] {
  return records.flatMap((op) => {
    const item = parseOperation(op, wallet, coreId);
    return item ? [item] : [];
  });
}

export async function fetchSavingsActivity(
  wallet: string,
  coreId: string,
  cursor?: string
): Promise<SavingsActivityPage> {
  const params = new URLSearchParams({
    order: "desc",
    limit: String(ACTIVITY_PAGE_SIZE),
    join: "transactions",
  });
  if (cursor) params.set("cursor", cursor);
  const base = config.stellarHorizonUrl.replace(/\/$/, "");
  const res = await fetch(`${base}/accounts/${wallet}/operations?${params}`);
  if (res.status === 404) return { items: [], scanned: 0, nextCursor: null };
  if (!res.ok) throw new Error(`Horizon returned ${res.status}`);

  const body = (await res.json()) as { _embedded?: { records?: HorizonOperation[] } };
  const records = body._embedded?.records ?? [];
  return {
    items: parseSavingsOperations(records, wallet, coreId),
    scanned: records.length,
    nextCursor:
      records.length === ACTIVITY_PAGE_SIZE ? records[records.length - 1].paging_token : null,
  };
}
