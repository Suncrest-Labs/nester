import { beforeEach, describe, expect, it, vi } from "vitest";
import { NETWORKS } from "@/lib/networks";
import {
  assertWalletMatchesAppNetwork,
  buildDepositTransaction,
  buildWithdrawTransaction,
  NetworkMismatchError,
} from "@/lib/stellar/transaction";
import {
  readWalletNetworkPassphrase,
  resolveNetworkDisplayName,
} from "@/lib/stellar/wallet-network";

const VALID_CONTRACT_ID =
  "CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75";
const VALID_WALLET =
  "GCEZWKCA5VLDNRLN3RPRJMRZOX3Z6G5CHCGSNFHEYVXM3XOJMDS674JZ";

describe("wallet network mismatch guard (#1511)", () => {
  beforeEach(() => {
    delete (window as unknown as { __NESTER_MOCK_WALLET__?: unknown })
      .__NESTER_MOCK_WALLET__;
    delete (window as unknown as { freighterApi?: unknown }).freighterApi;
    window.localStorage.clear();
  });

  it("maps Stellar network passphrases to human-readable network labels", () => {
    expect(resolveNetworkDisplayName(NETWORKS.mainnet.networkPassphrase)).toBe(
      "Mainnet"
    );
    expect(resolveNetworkDisplayName(NETWORKS.testnet.networkPassphrase)).toBe(
      "Testnet"
    );
  });

  it("reads wallet passphrase from getNetworkDetails on the wallet API", async () => {
    (window as unknown as Record<string, unknown>).freighterApi = {
      getNetworkDetails: vi.fn().mockResolvedValue({
        networkPassphrase: NETWORKS.testnet.networkPassphrase,
        network: "TESTNET",
      }),
    };

    await expect(readWalletNetworkPassphrase()).resolves.toBe(
      NETWORKS.testnet.networkPassphrase
    );
  });

  it("blocks buildDepositTransaction before RPC calls when wallet is on Testnet and dApp is on Mainnet", async () => {
    window.localStorage.setItem("nester_network_id", "mainnet");
    (window as unknown as Record<string, unknown>).__NESTER_MOCK_WALLET__ = {
      getNetworkDetails: vi.fn().mockResolvedValue({
        networkPassphrase: NETWORKS.testnet.networkPassphrase,
      }),
    };

    await expect(
      buildDepositTransaction({
        walletAddress: VALID_WALLET,
        contractId: VALID_CONTRACT_ID,
        amount: 100n,
      })
    ).rejects.toThrowError(NetworkMismatchError);

    await expect(
      buildDepositTransaction({
        walletAddress: VALID_WALLET,
        contractId: VALID_CONTRACT_ID,
        amount: 100n,
      })
    ).rejects.toThrow(/Switch your wallet to Mainnet/i);
  });

  it("blocks buildWithdrawTransaction when wallet is on Mainnet and dApp is on Testnet", async () => {
    window.localStorage.setItem("nester_network_id", "testnet");
    (window as unknown as Record<string, unknown>).__NESTER_MOCK_WALLET__ = {
      getNetworkDetails: vi.fn().mockResolvedValue({
        networkPassphrase: NETWORKS.mainnet.networkPassphrase,
      }),
    };

    await expect(
      buildWithdrawTransaction({
        walletAddress: VALID_WALLET,
        contractId: VALID_CONTRACT_ID,
        shares: 100n,
      })
    ).rejects.toThrow(/Switch your wallet to Testnet/i);
  });

  it("allows transaction building when wallet network matches dApp network", async () => {
    (window as unknown as Record<string, unknown>).__NESTER_MOCK_WALLET__ = {
      getNetworkDetails: vi.fn().mockResolvedValue({
        networkPassphrase: NETWORKS.mainnet.networkPassphrase,
      }),
    };

    await expect(
      assertWalletMatchesAppNetwork(NETWORKS.mainnet)
    ).resolves.toBeUndefined();
  });
});
