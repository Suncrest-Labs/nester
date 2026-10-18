import { describe, expect, it } from "vitest";
import {
  isPlaceholderContractId,
  validateProductionNetworkEnv,
} from "@/lib/networks";

describe("validateProductionNetworkEnv", () => {
  it("does not throw outside production builds", () => {
    expect(() =>
      validateProductionNetworkEnv({
        NODE_ENV: "development",
      })
    ).not.toThrow();

    expect(() =>
      validateProductionNetworkEnv({
        NODE_ENV: "test",
      })
    ).not.toThrow();
  });

  it("throws in production when NEXT_PUBLIC_STELLAR_NETWORK is missing", () => {
    expect(() =>
      validateProductionNetworkEnv({
        NODE_ENV: "production",
      })
    ).toThrow(/Missing required environment variable NEXT_PUBLIC_STELLAR_NETWORK/i);
  });

  it("throws in production when NEXT_PUBLIC_STELLAR_NETWORK is invalid", () => {
    expect(() =>
      validateProductionNetworkEnv({
        NODE_ENV: "production",
        NEXT_PUBLIC_STELLAR_NETWORK: "futurenet",
      })
    ).toThrow(/Invalid NEXT_PUBLIC_STELLAR_NETWORK/i);
  });

  it("throws in production when selected network has missing or placeholder contract IDs", () => {
    expect(() =>
      validateProductionNetworkEnv({
        NODE_ENV: "production",
        NEXT_PUBLIC_STELLAR_NETWORK: "mainnet",
      })
    ).toThrow(
      /Production build failed for network "mainnet": missing or placeholder contract IDs configured for:.*NEXT_PUBLIC_MAINNET_VAULT_CONTRACT/i
    );
  });

  it("succeeds in production when all contract IDs for the selected network are set", () => {
    expect(() =>
      validateProductionNetworkEnv({
        NODE_ENV: "production",
        NEXT_PUBLIC_STELLAR_NETWORK: "mainnet",
        NEXT_PUBLIC_MAINNET_VAULT_CONTRACT:
          "CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75",
        NEXT_PUBLIC_MAINNET_VAULT_TOKEN_CONTRACT:
          "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA",
        NEXT_PUBLIC_MAINNET_YIELD_REGISTRY_CONTRACT:
          "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
        NEXT_PUBLIC_MAINNET_ALLOCATION_STRATEGY_CONTRACT:
          "CAS3J7GYLGXMF6TDJBBYYSE3HQ6BBSMLNUQ34T6TZMYMW2EVH34XOWMA",
        NEXT_PUBLIC_MAINNET_TREASURY_CONTRACT:
          "CBQHNAXSI55GX2GN6D67GK7BHVPSLJUGZQEU7WJ5LKR5PNUCGLIMAO4K",
        NEXT_PUBLIC_MAINNET_ACCESS_CONTROL_CONTRACT:
          "CA7QYNF7SOWQ3GLR2BGMZEHXAVIRZA4KVWLTJJFC7MGXUA74P7UJVSGZ",
      })
    ).not.toThrow();
  });

  it("detects placeholder contract IDs accurately", () => {
    expect(isPlaceholderContractId("")).toBe(true);
    expect(isPlaceholderContractId("C_MAINNET_VAULT_PLACEHOLDER")).toBe(true);
    expect(isPlaceholderContractId("C_TESTNET_TREASURY_PLACEHOLDER")).toBe(true);
    expect(
      isPlaceholderContractId(
        "CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75"
      )
    ).toBe(false);
  });
});
