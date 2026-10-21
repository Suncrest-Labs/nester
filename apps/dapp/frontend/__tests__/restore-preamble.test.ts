import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  Account,
  BASE_FEE,
  Operation,
  Transaction,
  TransactionBuilder,
  rpc as SorobanRpc,
  nativeToScVal,
  SorobanDataBuilder,
} from "@stellar/stellar-sdk";

import {
  executeVaultWithdraw,
  executeVaultDeposit,
  buildRestoreTransaction,
} from "@/lib/stellar/transaction";

const mockAddress = "GCVVVNSKJAOJ6LCWBK4TG7IR3YEJJPGSBPVBYYBFI7M53V3QL5ABZ66D";
const mockContractId = "CAAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQCAIBAEAQC526";

const signCalls: string[] = [];

vi.mock("@creit.tech/stellar-wallets-kit", () => ({
  StellarWalletsKit: {
    selectedModule: {
      getAddress: vi.fn().mockResolvedValue({ address: "GCVVVNSKJAOJ6LCWBK4TG7IR3YEJJPGSBPVBYYBFI7M53V3QL5ABZ66D" }),
      signTransaction: vi.fn().mockImplementation(async (xdr: string) => {
        signCalls.push(xdr);
        return { signedTxXdr: xdr };
      }),
    },
  },
}));

describe("Soroban restorePreamble handling (#1487)", () => {
  let simulateCallCount = 0;

  beforeEach(() => {
    signCalls.length = 0;
    simulateCallCount = 0;
    vi.restoreAllMocks();
  });

  it("builds a valid restore transaction with RestoreFootprintOp", () => {
    const account = new Account(mockAddress, "100");
    const restoreData = new SorobanDataBuilder().build();
    const built = buildRestoreTransaction(
      account,
      {
        minResourceFee: "5000",
        transactionData: restoreData,
      },
      "Test SDF Network ; September 2015"
    );

    expect(built.transaction).toBeDefined();
    expect(built.transaction.operations.length).toBe(1);
    expect(built.transaction.operations[0].type).toBe("restoreFootprint");
    expect(built.transaction.fee).toBe((BigInt(BASE_FEE) + BigInt(5000)).toString());
  });

  it("produces two signed transactions in the right order when restore is needed", async () => {
    const networkPassphrase = "Test SDF Network ; September 2015";
    const dummySorobanData = new SorobanDataBuilder().build();

    const mockSimRestoreResponse = {
      _parsed: true,
      transactionData: new SorobanDataBuilder(),
      restorePreamble: {
        minResourceFee: "4500",
        transactionData: dummySorobanData.toXDR("base64"),
      },
      minResourceFee: "4500",
      latestLedger: 12345,
    };

    const mockSimSuccessResponse = {
      _parsed: true,
      transactionData: new SorobanDataBuilder(),
      minResourceFee: "1000",
      result: {
        retval: nativeToScVal(100, { type: "i128" }),
        auth: [],
      },
      latestLedger: 12346,
    };

    vi.spyOn(SorobanRpc.Server.prototype, "getAccount").mockImplementation(async () => {
      return new Account(mockAddress, String(100 + simulateCallCount)) as any;
    });

    vi.spyOn(SorobanRpc.Server.prototype, "simulateTransaction").mockImplementation(async () => {
      simulateCallCount++;
      if (simulateCallCount === 1) {
        return mockSimRestoreResponse as any;
      }
      return mockSimSuccessResponse as any;
    });

    vi.spyOn(SorobanRpc.Server.prototype, "sendTransaction").mockImplementation(async () => {
      return {
        status: "PENDING",
        hash: "abc123hash",
      } as any;
    });

    vi.spyOn(SorobanRpc.Server.prototype, "getTransaction").mockImplementation(async () => {
      return {
        status: SorobanRpc.Api.GetTransactionStatus.SUCCESS,
        ledger: 12346,
      } as any;
    });

    const statusUpdates: string[] = [];

    const receipt = await executeVaultWithdraw({
      walletAddress: mockAddress,
      vaultId: "usdc-vault",
      contractId: mockContractId,
      asset: "USDC",
      shares: 10,
      minAssetsOut: 10,
      onStatusChange: (status) => statusUpdates.push(status),
    });

    // Verify receipt was returned
    expect(receipt.txHash).toBe("abc123hash");

    // Must have produced exactly two signed transactions
    expect(signCalls.length).toBe(2);

    // Transaction 1 must be the restore footprint operation
    const firstTx = new Transaction(signCalls[0], networkPassphrase);
    expect(firstTx.operations.length).toBe(1);
    expect(firstTx.operations[0].type).toBe("restoreFootprint");

    // Transaction 2 must be the invokeHostFunction (vault withdraw)
    const secondTx = new Transaction(signCalls[1], networkPassphrase);
    expect(secondTx.operations.length).toBe(1);
    expect(secondTx.operations[0].type).toBe("invokeHostFunction");

    // Status transitions should have included "restoring"
    expect(statusUpdates).toContain("restoring");
    expect(statusUpdates.indexOf("restoring")).toBeLessThan(statusUpdates.lastIndexOf("signing"));
  });
});
