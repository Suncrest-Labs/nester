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

const nativeToScValSpy = vi.fn((val: unknown) => ({ value: val }));

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
    nativeToScVal: (val: unknown, opts?: unknown) => nativeToScValSpy(val, opts),
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
  parseDepositAmountToStroops,
  buildDepositTransaction,
} from "@/lib/stellar/transaction";

const VALID_CONTRACT_ID = "CCJAJVEFZPGIGMYIBQO4U7VL2PGEQR2XGVQ4YFQUHK37IJDJWEXHKGFA";
const VALID_WALLET = "GBXYZ1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890ABCDE";

describe("Deposit amount string parser (#1508)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  const validTable: Array<[string | bigint, bigint, string]> = [
    ["0.3", BigInt("3000000"), "0.3 decimal string without float drift"],
    ["1234567.1234567", BigInt("12345671234567"), "exact 7 decimal places"],
    ["9007199254.7409931", BigInt("90071992547409931"), "very large value above 2^53 stroops"],
    ["999999999999999.9999999", BigInt("9999999999999999999999"), "massive institutional amount"],
    [BigInt("12345671234567"), BigInt("12345671234567"), "direct bigint stroops"],
  ];

  it.each(validTable)("converts %s to %s stroops (%s)", (input, expected) => {
    expect(parseDepositAmountToStroops(input)).toBe(expected);
  });

  const rejectedTable: Array<[unknown, string]> = [
    ["0.00000001", "more than 7 decimal places (8 decimals)"],
    ["1.12345678", "more than 7 decimal places"],
    ["0", "zero string"],
    ["0.0000000", "zero decimal string"],
    [BigInt(0), "zero bigint"],
    [BigInt(-10), "negative bigint"],
    ["-1.5", "negative string"],
    ["1e7", "scientific notation"],
    ["", "empty string"],
    [0.3, "numeric float (removed overload)"],
  ];

  it.each(rejectedTable)("rejects invalid deposit amount %s (%s)", (input) => {
    expect(() => parseDepositAmountToStroops(input as string)).toThrow();
  });

  it("buildDepositTransaction encodes exact stroops from decimal string without float multiplication", async () => {
    const mockServer = {
      getAccount: vi.fn().mockResolvedValue({ sequenceNumber: "1", id: VALID_WALLET }),
      simulateTransaction: vi.fn().mockResolvedValue({ result: {} }),
    };
    vi.mocked(StellarSdk.rpc.Server).mockImplementation(
      () => mockServer as unknown as StellarSdk.rpc.Server
    );

    await buildDepositTransaction({
      walletAddress: VALID_WALLET,
      contractId: VALID_CONTRACT_ID,
      amount: "1234567.1234567",
    });

    expect(nativeToScValSpy).toHaveBeenCalledWith(BigInt("12345671234567"), {
      type: "i128",
    });
  });
});
