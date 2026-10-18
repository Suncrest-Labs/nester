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

const contractCallSpy = vi.fn();

vi.mock("@stellar/stellar-sdk", () => {
  const AddressMock = vi.fn(function (this: { address: string }, address: string) {
    this.address = address;
  });
  AddressMock.prototype.toScVal = vi.fn(function (this: { address: string }) {
    return { type: "address", value: this.address };
  });

  const ContractMock = vi.fn(function (this: { contractId: string }, contractId: string) {
    this.contractId = contractId;
  });
  ContractMock.prototype.call = vi.fn(function (method: string, ...args: unknown[]) {
    contractCallSpy(method, args);
    return {
      toXDR: vi.fn().mockReturnValue("op_xdr"),
    };
  });

  const TransactionBuilderMock = vi.fn(function () {});
  TransactionBuilderMock.prototype.addOperation = vi.fn(function (this: unknown) {
    return this;
  });
  TransactionBuilderMock.prototype.setTimeout = vi.fn(function (this: unknown) {
    return this;
  });
  TransactionBuilderMock.prototype.build = vi.fn().mockReturnValue({
    toXDR: vi.fn().mockReturnValue("tx_xdr"),
  });

  return {
    Contract: ContractMock,
    TransactionBuilder: TransactionBuilderMock,
    Transaction: vi.fn(),
    Address: AddressMock,
    BASE_FEE: "100",
    nativeToScVal: vi.fn((val: unknown, opts?: { type?: string }) => ({
      type: opts?.type ?? "scval",
      value: val,
    })),
    scValToNative: vi.fn(),
    rpc: {
      Server: vi.fn(),
      assembleTransaction: vi.fn().mockReturnValue({
        build: vi.fn().mockReturnValue({
          toXDR: vi.fn().mockReturnValue("assembled_xdr"),
        }),
      }),
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
  buildDepositTransaction,
  computeMinSharesOut,
} from "@/lib/stellar/transaction";

const VALID_CONTRACT_ID = "CCJAJVEFZPGIGMYIBQO4U7VL2PGEQR2XGVQ4YFQUHK37IJDJWEXHKGFA";
const VALID_WALLET = "GBXYZ1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890ABCDE";

describe("Vault deposit 3-argument invocation with min_shares_out (#1502)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("derives min_shares_out from preview_deposit minus slippage tolerance", () => {
    const sharesExpected = BigInt(100_000_000); // 10 shares
    // Default 0.5% (50 bps) -> 99.5% = 99_500_000
    expect(computeMinSharesOut(sharesExpected)).toBe(BigInt(99_500_000));
    // Custom 1% (100 bps) -> 99% = 99_000_000
    expect(computeMinSharesOut(sharesExpected, 100)).toBe(BigInt(99_000_000));
  });

  it("invokes deposit with 3 arguments (user, amount, min_shares_out)", async () => {
    const mockServer = {
      getAccount: vi.fn().mockResolvedValue({ sequenceNumber: "1", id: VALID_WALLET }),
      simulateTransaction: vi.fn().mockResolvedValue({ result: {} }),
    };
    vi.mocked(StellarSdk.rpc.Server).mockImplementation(
      () => mockServer as unknown as StellarSdk.rpc.Server
    );

    const amountStroops = BigInt(100_000_000);
    const minSharesOut = computeMinSharesOut(BigInt(98_000_000), 50); // 97_510_000

    await buildDepositTransaction({
      walletAddress: VALID_WALLET,
      contractId: VALID_CONTRACT_ID,
      amount: amountStroops,
      minSharesOut,
    });

    expect(contractCallSpy).toHaveBeenCalledTimes(1);
    const [method, invokeArgs] = contractCallSpy.mock.calls[0];
    expect(method).toBe("deposit");
    expect(invokeArgs).toHaveLength(3);
    expect(invokeArgs[0]).toEqual({ type: "address", value: VALID_WALLET });
    expect(invokeArgs[1]).toEqual({ type: "i128", value: amountStroops });
    expect(invokeArgs[2]).toEqual({ type: "i128", value: BigInt(97_510_000) });
  });
});
