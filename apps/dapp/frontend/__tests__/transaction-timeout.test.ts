import { describe, it, expect, vi, beforeEach } from "vitest";
import * as StellarSdk from "@stellar/stellar-sdk";

vi.mock("@/lib/networks", () => ({
  NETWORKS: {
    testnet: {
      id: "testnet",
      name: "Testnet",
      rpcUrl: "https://soroban-testnet.stellar.org",
      horizonUrl: "https://horizon-testnet.stellar.org",
      networkPassphrase: "Test SDF Network ; September 2015",
      explorerUrl: "https://testnet.stellarchain.io",
      friendbotUrl: "https://friendbot.stellar.org",
      contracts: {},
    },
  },
  DEFAULT_NETWORK: {
    id: "testnet",
    name: "Testnet",
    rpcUrl: "https://soroban-testnet.stellar.org",
    horizonUrl: "https://horizon-testnet.stellar.org",
    networkPassphrase: "Test SDF Network ; September 2015",
    explorerUrl: "https://testnet.stellarchain.io",
    friendbotUrl: "https://friendbot.stellar.org",
    contracts: {},
  },
}));

const setTimeoutSpy = vi.fn();

vi.mock("@stellar/stellar-sdk", () => {
  const AddressMock = vi.fn(function (this: { address: string }, address: string) {
    this.address = address;
  });
  AddressMock.prototype.toScVal = vi.fn().mockReturnValue({ type: "address" });

  const ContractMock = vi.fn(function (this: { contractId: string }, contractId: string) {
    this.contractId = contractId;
  });
  ContractMock.prototype.call = vi.fn().mockReturnValue({
    toXDR: vi.fn().mockReturnValue("op_xdr"),
  });

  const TransactionBuilderMock = vi.fn(function (
    this: { account: unknown; options: unknown; timeout?: number },
    account: unknown,
    options: unknown
  ) {
    this.account = account;
    this.options = options;
  });
  TransactionBuilderMock.prototype.addOperation = vi.fn(function (this: unknown) {
    return this;
  });
  TransactionBuilderMock.prototype.setTimeout = vi.fn(function (
    this: { timeout?: number },
    timeout: number
  ) {
    setTimeoutSpy(timeout);
    this.timeout = timeout;
    return this;
  });
  TransactionBuilderMock.prototype.build = vi.fn(function (this: { timeout?: number }) {
    return {
      toXDR: vi.fn().mockReturnValue("tx_xdr"),
      timeBounds: {
        minTime: "0",
        maxTime: String(Math.floor(Date.now() / 1000) + (this.timeout ?? 180)),
      },
    };
  });

  return {
    Contract: ContractMock,
    TransactionBuilder: TransactionBuilderMock,
    Transaction: vi.fn(),
    Address: AddressMock,
    BASE_FEE: "100",
    nativeToScVal: vi.fn(),
    scValToNative: vi.fn(),
    rpc: {
      Server: vi.fn(),
      assembleTransaction: vi.fn(),
      Api: {
        isSimulationError: vi.fn().mockReturnValue(false),
        GetTransactionStatus: {
          SUCCESS: "SUCCESS",
          FAILED: "FAILED",
          NOT_FOUND: "NOT_FOUND",
        },
      },
    },
  };
});

import {
  TRANSACTION_TIMEOUT_SECONDS,
  TransactionExpiredError,
  buildDepositTransaction,
  signTransactionWithRebuild,
  submitTransaction,
} from "@/lib/stellar/transaction";

const VALID_CONTRACT_ID = "CCJAJVEFZPGIGMYIBQO4U7VL2PGEQR2XGVQ4YFQUHK37IJDJWEXHKGFA";
const VALID_WALLET = "GBXYZ1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890ABCDE";

describe("Transaction timeout, delayed signer rebuild, and txTOO_LATE (#1510)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("uses a 180-second default timeout when building deposit transactions", async () => {
    const mockServer = {
      getAccount: vi.fn().mockResolvedValue({ sequenceNumber: "1", id: VALID_WALLET }),
      simulateTransaction: vi.fn().mockResolvedValue({ result: {} }),
    };
    vi.mocked(StellarSdk.rpc.Server).mockImplementation(() => mockServer as unknown as StellarSdk.rpc.Server);
    vi.mocked(StellarSdk.rpc.assembleTransaction).mockReturnValue({
      build: () => ({
        toXDR: () => "assembled_xdr_1",
        timeBounds: { minTime: "0", maxTime: String(Math.floor(Date.now() / 1000) + 180) },
      }),
    } as unknown as ReturnType<typeof StellarSdk.rpc.assembleTransaction>);

    expect(TRANSACTION_TIMEOUT_SECONDS).toBe(180);
    const built = await buildDepositTransaction({
      walletAddress: VALID_WALLET,
      contractId: VALID_CONTRACT_ID,
      amount: BigInt(10_000_000),
    });

    expect(setTimeoutSpy).toHaveBeenCalledWith(180);
    expect(built.xdr).toBe("assembled_xdr_1");
    expect(built.expiresAtMs).toBeGreaterThan(Date.now());
  });

  it("detects expiry with a mocked delayed signer and rebuilds + re-simulates", async () => {
    let currentClockMs = 1_700_000_000_000;
    let buildCount = 0;

    const buildTx = vi.fn(async () => {
      buildCount += 1;
      const expiresAtMs = currentClockMs + TRANSACTION_TIMEOUT_SECONDS * 1000;
      return {
        xdr: `unsigned_xdr_attempt_${buildCount}`,
        transaction: {
          timeBounds: {
            minTime: "0",
            maxTime: String(Math.floor(expiresAtMs / 1000)),
          },
        } as unknown as StellarSdk.Transaction,
        expiresAtMs,
      };
    });

    // Mocked delayed signer: first attempt takes 195s (past 180s expiry),
    // second attempt after rebuild signs within 15s.
    const delayedSigner = vi.fn(async (xdr: string) => {
      if (xdr === "unsigned_xdr_attempt_1") {
        currentClockMs += 195_000;
        return "signed_expired_xdr_1";
      }
      currentClockMs += 15_000;
      return "signed_fresh_xdr_2";
    });

    const result = await signTransactionWithRebuild(buildTx, delayedSigner, () => currentClockMs);

    expect(buildTx).toHaveBeenCalledTimes(2);
    expect(delayedSigner).toHaveBeenCalledTimes(2);
    expect(delayedSigner).toHaveBeenNthCalledWith(1, "unsigned_xdr_attempt_1");
    expect(delayedSigner).toHaveBeenNthCalledWith(2, "unsigned_xdr_attempt_2");
    expect(result).toEqual({
      signedXdr: "signed_fresh_xdr_2",
      rebuilt: true,
    });
  });

  it("throws TransactionExpiredError with 'Signing took too long, try again' if rebuild also expires", async () => {
    let currentClockMs = 1_700_000_000_000;

    const buildTx = vi.fn(async () => ({
      xdr: "unsigned_xdr",
      transaction: {} as unknown as StellarSdk.Transaction,
      expiresAtMs: currentClockMs + TRANSACTION_TIMEOUT_SECONDS * 1000,
    }));

    const alwaysSlowSigner = vi.fn(async () => {
      currentClockMs += 200_000;
      return "signed_late_xdr";
    });

    await expect(
      signTransactionWithRebuild(buildTx, alwaysSlowSigner, () => currentClockMs)
    ).rejects.toThrow("Signing took too long, try again");
  });

  it("surfaces 'Signing took too long, try again' when submission fails with txTOO_LATE", async () => {
    const mockServer = {
      sendTransaction: vi.fn().mockResolvedValue({
        status: "ERROR",
        errorResult: {
          toXDR: () => "txTOO_LATE",
        },
      }),
    };
    vi.mocked(StellarSdk.rpc.Server).mockImplementation(() => mockServer as unknown as StellarSdk.rpc.Server);

    await expect(submitTransaction("signed_xdr")).rejects.toThrow(TransactionExpiredError);
    await expect(submitTransaction("signed_xdr")).rejects.toThrow("Signing took too long, try again");
  });
});
