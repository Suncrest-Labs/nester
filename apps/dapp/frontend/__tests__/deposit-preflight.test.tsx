import { describe, it, expect, vi } from "vitest";
import {
  buildAddUsdcTrustlineTransaction,
  calculateMinimumXlmReserve,
  evaluateHorizonAccountPreflight,
  fetchDepositPreflight,
  type HorizonAccountPayload,
} from "@/lib/stellar/preflight";

const WALLET = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN";
const USDC_ISSUER = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5";

const healthyAccount: HorizonAccountPayload = {
  id: WALLET,
  sequence: "100",
  subentry_count: 1,
  balances: [
    {
      asset_type: "credit_alphanum4",
      asset_code: "USDC",
      asset_issuer: USDC_ISSUER,
      balance: "500.0000000",
    },
    {
      asset_type: "native",
      balance: "10.0000000",
    },
  ],
};

describe("evaluateHorizonAccountPreflight", () => {
  it("reports unfunded_account when account is missing (404)", () => {
    const res = evaluateHorizonAccountPreflight(null, {
      accountNotFound: true,
      asset: "USDC",
      amount: 50,
      usdcIssuer: USDC_ISSUER,
    });
    expect(res.ok).toBe(false);
    expect(res.status).toBe("unfunded_account");
    expect(res.message).toContain("not funded on-chain yet");
  });

  it("reports missing_usdc_trustline and offers canAddTrustline when USDC trustline is absent", () => {
    const noTrustlineAccount: HorizonAccountPayload = {
      ...healthyAccount,
      subentry_count: 0,
      balances: [{ asset_type: "native", balance: "10.0000000" }],
    };
    const res = evaluateHorizonAccountPreflight(noTrustlineAccount, {
      asset: "USDC",
      amount: 50,
      usdcIssuer: USDC_ISSUER,
    });
    expect(res.ok).toBe(false);
    expect(res.status).toBe("missing_usdc_trustline");
    expect(res.canAddTrustline).toBe(true);
    expect(res.message).toContain("does not have a USDC trustline");
  });

  it("reports insufficient_usdc when USDC balance is below the deposit amount", () => {
    const res = evaluateHorizonAccountPreflight(healthyAccount, {
      asset: "USDC",
      amount: 750,
      usdcIssuer: USDC_ISSUER,
    });
    expect(res.ok).toBe(false);
    expect(res.status).toBe("insufficient_usdc");
    expect(res.message).toContain("Insufficient on-chain USDC balance");
  });

  it("reports insufficient_xlm_reserve when XLM does not cover (2 + subentry_count)*0.5 + 0.1 XLM fee buffer", () => {
    // subentry_count = 1 -> (2 + 1) * 0.5 + 0.1 = 1.6 XLM required
    expect(calculateMinimumXlmReserve(1)).toBe(1.6);

    const lowXlmAccount: HorizonAccountPayload = {
      ...healthyAccount,
      balances: [
        healthyAccount.balances[0],
        { asset_type: "native", balance: "1.5000000" },
      ],
    };
    const res = evaluateHorizonAccountPreflight(lowXlmAccount, {
      asset: "USDC",
      amount: 50,
      usdcIssuer: USDC_ISSUER,
    });
    expect(res.ok).toBe(false);
    expect(res.status).toBe("insufficient_xlm_reserve");
    expect(res.requiredXlmReserve).toBe(1.6);
    expect(res.message).toContain("Insufficient XLM for account reserve and Soroban fees");
  });

  it("passes when account exists, USDC trustline + balance are sufficient, and XLM covers reserve + fee", () => {
    const res = evaluateHorizonAccountPreflight(healthyAccount, {
      asset: "USDC",
      amount: 100,
      usdcIssuer: USDC_ISSUER,
    });
    expect(res.ok).toBe(true);
    expect(res.status).toBe("ok");
  });
});

describe("fetchDepositPreflight and buildAddUsdcTrustlineTransaction", () => {
  it("handles Horizon 404 as unfunded_account", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(new Response("Not Found", { status: 404 }));
    const res = await fetchDepositPreflight({
      walletAddress: WALLET,
      asset: "USDC",
      amount: 25,
      horizonUrl: "https://horizon-testnet.stellar.org",
      usdcIssuer: USDC_ISSUER,
      fetchFn: fetchMock as unknown as typeof fetch,
    });
    expect(res?.status).toBe("unfunded_account");
  });

  it("builds a valid changeTrust XDR for adding a USDC trustline", () => {
    const xdr = buildAddUsdcTrustlineTransaction({
      walletAddress: WALLET,
      accountSequence: "100",
      usdcIssuer: USDC_ISSUER,
      networkPassphrase: "Test SDF Network ; September 2015",
    });
    expect(typeof xdr).toBe("string");
    expect(xdr.length).toBeGreaterThan(20);
  });
});
