import { afterEach, describe, expect, it, vi } from "vitest";

import { fetchWalletUsdcBalance, parseWalletUsdcBalance } from "@/lib/stellar/wallet-balance";

const ISSUER = "GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56";
const native = { asset_type: "native", balance: "9999.9999900" };

describe("parseWalletUsdcBalance", () => {
  it("treats a 404 as an unfunded account", () => {
    expect(parseWalletUsdcBalance(404, null, ISSUER)).toEqual({ status: "unfunded" });
  });

  it("reports no trustline when the account holds no USDC from the issuer", () => {
    expect(parseWalletUsdcBalance(200, { balances: [native] }, ISSUER)).toEqual({
      status: "no_trustline",
    });
  });

  it("ignores USDC from a different issuer", () => {
    const body = {
      balances: [
        native,
        {
          asset_type: "credit_alphanum4",
          asset_code: "USDC",
          asset_issuer: "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5",
          balance: "50.0000000",
        },
      ],
    };
    expect(parseWalletUsdcBalance(200, body, ISSUER)).toEqual({ status: "no_trustline" });
  });

  it("returns the matching USDC balance string", () => {
    const body = {
      balances: [
        native,
        {
          asset_type: "credit_alphanum4",
          asset_code: "USDC",
          asset_issuer: ISSUER,
          balance: "1000.1234567",
        },
      ],
    };
    expect(parseWalletUsdcBalance(200, body, ISSUER)).toEqual({
      status: "funded",
      balance: "1000.1234567",
    });
  });

  it("throws on other Horizon errors", () => {
    expect(() => parseWalletUsdcBalance(500, null, ISSUER)).toThrow(/500/);
  });
});

describe("fetchWalletUsdcBalance", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("reads /accounts/{address} from Horizon", async () => {
    const fetchMock = vi.fn(async () =>
      new Response(
        JSON.stringify({
          balances: [
            { asset_type: "credit_alphanum4", asset_code: "USDC", asset_issuer: ISSUER, balance: "5.0000000" },
          ],
        }),
        { status: 200 }
      )
    );
    vi.stubGlobal("fetch", fetchMock);
    await expect(fetchWalletUsdcBalance("GABC", "https://horizon.test/", ISSUER)).resolves.toEqual({
      status: "funded",
      balance: "5.0000000",
    });
    expect(fetchMock).toHaveBeenCalledWith("https://horizon.test/accounts/GABC");
  });

  it("maps a 404 to unfunded without reading the body", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("not found", { status: 404 })));
    await expect(fetchWalletUsdcBalance("GABC", "https://horizon.test", ISSUER)).resolves.toEqual({
      status: "unfunded",
    });
  });
});
