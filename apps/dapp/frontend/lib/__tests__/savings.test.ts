import { describe, it, expect, vi, beforeEach } from "vitest";
import { TransactionBuilder, scValToNative } from "@stellar/stellar-sdk";

const apiRequest = vi.fn();
vi.mock("@/lib/api/client", () => ({ apiRequest: (...a: unknown[]) => apiRequest(...a) }));

const wallet = "GAAZI4TCR3TY5OJHCTJC2A4QSY6CJWJH5IAJTGKIN2ER7LBNVKOCCWN7";
const CORE_ID = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC";

const simulateTransaction = vi.fn();
vi.mock("@stellar/stellar-sdk", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@stellar/stellar-sdk")>();
  class FakeServer {
    getAccount = async () => new actual.Account(wallet, "1");
    simulateTransaction = (...a: unknown[]) => simulateTransaction(...a);
  }
  return {
    ...actual,
    rpc: {
      ...actual.rpc,
      Server: FakeServer,
      Api: { ...actual.rpc.Api, isSimulationError: () => false },
      assembleTransaction: (tx: { toXDR: () => string }) => ({ build: () => tx }),
    },
  };
});

const signTransaction = vi.fn(async (xdr: string) => xdr);
const submitTransaction = vi.fn(async (xdr: string) => ({ xdr, txHash: "h", explorerUrl: "u", ledger: 1 }));
const assertWalletOnDappNetwork = vi.fn(async () => undefined);
vi.mock("@/lib/stellar/transaction", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/stellar/transaction")>();
  return {
    ...actual,
    signTransaction: (x: string) => signTransaction(x),
    assertWalletOnDappNetwork: () => assertWalletOnDappNetwork(),
    submitTransaction: (x: string) => submitTransaction(x),
  };
});

import { getCurrentNetwork } from "@/lib/stellar/transaction";
import { fetchSavingsSummary, fetchSavingsHistory, fetchSavingsPosition } from "@/lib/api/savings";
import { nativeToScVal } from "@stellar/stellar-sdk";
import { buildSavingsDeposit, buildSavingsRedeem, executeSavingsDeposit, quoteMinShares, quoteMinAssets } from "@/lib/stellar/savings";

function decodeCall(xdr: string) {
  const tx = TransactionBuilder.fromXDR(xdr, getCurrentNetwork().networkPassphrase);
  const op = (tx as unknown as { operations: { func: { invokeContract: () => { functionName: () => Buffer; args: () => unknown[] } } }[] }).operations[0];
  const inv = op.func.invokeContract();
  return {
    fn: inv.functionName().toString(),
    args: inv.args().map((a) => scValToNative(a as never)),
  };
}

beforeEach(() => {
  apiRequest.mockReset();
  signTransaction.mockClear();
  submitTransaction.mockClear();
  simulateTransaction.mockReset();
  simulateTransaction.mockResolvedValue({});
  process.env.NEXT_PUBLIC_SAVINGS_CORE_CONTRACT_ID = CORE_ID;
});

describe("savings api client", () => {
  it("fetchSavingsSummary maps null rates", async () => {
    apiRequest.mockResolvedValue({
      rate7d: null,
      rate30d: null,
      adapters: null,
      pending: null,
      reserve: { shares: "0", value: "0" },
      sharePrice: "1.0000000",
      totalAssets: "0",
      idle: "0",
      feeBps: 1000,
      timelockSeconds: 259200,
    });
    const s = await fetchSavingsSummary();
    expect(apiRequest).toHaveBeenCalledWith("/savings");
    expect(s.rate7d).toBeNull();
    expect(s.rate30d).toBeNull();
    expect(s.adapters).toEqual([]);
    expect(s.pending).toEqual([]);
  });

  it("fetchSavingsSummary keeps string rates", async () => {
    apiRequest.mockResolvedValue({ rate7d: "5.35", rate30d: "4.10", adapters: [], pending: [] });
    const s = await fetchSavingsSummary();
    expect(s.rate7d).toBe("5.35");
    expect(s.rate30d).toBe("4.10");
  });

  it("fetchSavingsHistory passes the period and tolerates null", async () => {
    apiRequest.mockResolvedValue(null);
    await expect(fetchSavingsHistory("90d")).resolves.toEqual([]);
    expect(apiRequest).toHaveBeenCalledWith("/savings/history?period=90d");
  });

  it("fetchSavingsPosition returns deposited null", async () => {
    apiRequest.mockResolvedValue({ shares: "1", value: "1.0", deposited: null });
    const p = await fetchSavingsPosition();
    expect(apiRequest).toHaveBeenCalledWith("/savings/position");
    expect(p.deposited).toBeNull();
  });
});

describe("savings transactions", () => {
  it("buildSavingsDeposit calls core.deposit with min_shares", async () => {
    const xdr = await buildSavingsDeposit({ wallet, amount: "12.5", minShares: BigInt(124_000_000) });
    expect(assertWalletOnDappNetwork).toHaveBeenCalled();
    const call = decodeCall(xdr);
    expect(call.fn).toBe("deposit");
    expect(call.args).toEqual([wallet, BigInt(125_000_000), BigInt(124_000_000)]);
  });

  it("buildSavingsRedeem calls core.redeem with min_assets", async () => {
    const xdr = await buildSavingsRedeem({ wallet, shares: BigInt(50_000_000), minAssets: BigInt(49_000_000) });
    const call = decodeCall(xdr);
    expect(call.fn).toBe("redeem");
    expect(call.args).toEqual([wallet, BigInt(50_000_000), BigInt(49_000_000)]);
  });

  it("rejects zero amounts and a missing core id", async () => {
    await expect(buildSavingsDeposit({ wallet, amount: "0", minShares: BigInt(0) })).rejects.toThrow();
    delete process.env.NEXT_PUBLIC_SAVINGS_CORE_CONTRACT_ID;
    await expect(buildSavingsDeposit({ wallet, amount: "1", minShares: BigInt(0) })).rejects.toThrow(/NEXT_PUBLIC_SAVINGS_CORE_CONTRACT_ID/);
  });

  it("executeSavingsDeposit signs then submits", async () => {
    const receipt = await executeSavingsDeposit({ wallet, amount: "1", minShares: BigInt(1) });
    expect(signTransaction).toHaveBeenCalledTimes(1);
    expect(submitTransaction).toHaveBeenCalledTimes(1);
    expect(receipt.txHash).toBe("h");
  });
});

describe("savings quotes", () => {
  const ret = (v: bigint) => ({ result: { retval: nativeToScVal(v, { type: "i128" }) } });

  it("quoteMinShares floors the simulated quote by the slippage tolerance", async () => {
    simulateTransaction.mockResolvedValue(ret(BigInt(1_000_001)));
    await expect(quoteMinShares(wallet, "0.1", 100)).resolves.toBe(BigInt(990_000));
  });

  it("quoteMinAssets floors the simulated quote by the slippage tolerance", async () => {
    simulateTransaction.mockResolvedValue(ret(BigInt(2_000_000)));
    await expect(quoteMinAssets(wallet, BigInt(5), 50)).resolves.toBe(BigInt(1_990_000));
  });

  it("throws instead of returning a zero minimum when the quote is missing or zero", async () => {
    simulateTransaction.mockResolvedValue({});
    await expect(quoteMinShares(wallet, "1", 50)).rejects.toThrow();
    await expect(quoteMinAssets(wallet, BigInt(5), 50)).rejects.toThrow();
    simulateTransaction.mockResolvedValue(ret(BigInt(0)));
    await expect(quoteMinShares(wallet, "1", 50)).rejects.toThrow();
    await expect(quoteMinAssets(wallet, BigInt(5), 50)).rejects.toThrow();
  });

  it("rejects negative minimums in the builders", async () => {
    await expect(buildSavingsDeposit({ wallet, amount: "1", minShares: BigInt(-1) })).rejects.toThrow();
    await expect(buildSavingsRedeem({ wallet, shares: BigInt(1), minAssets: BigInt(-1) })).rejects.toThrow();
  });
});
