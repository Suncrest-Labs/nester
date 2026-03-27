import { StatusCodes } from "http-status-codes";

import type { AssetBalance } from "@/api/stellar/stellarModel";
import { buildBalancesMap, resolveAssetKey, validateBalances } from "@/api/stellar/stellarService";

const mockBalances: AssetBalance[] = [
  { asset_type: "native", balance: "100.0000000" },
  {
    asset_type: "credit_alphanum4",
    asset_code: "USDC",
    asset_issuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
    balance: "500.0000000",
  },
  {
    asset_type: "credit_alphanum4",
    asset_code: "USDT",
    asset_issuer: "GCQTGZQQ5G4PTM2GL7CDIFKUBBER43VNFL4O7W7Y2CQWQ5LCT7L5QA",
    balance: "250.0000000",
  },
];

describe("stellarService", () => {
  describe("buildBalancesMap", () => {
    it("maps native asset as XLM", () => {
      const map = buildBalancesMap(mockBalances);
      expect(map.XLM).toBe(100);
    });

    it("maps non-native assets as code:issuer", () => {
      const map = buildBalancesMap(mockBalances);
      expect(map["USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"]).toBe(500);
      expect(map["USDT:GCQTGZQQ5G4PTM2GL7CDIFKUBBER43VNFL4O7W7Y2CQWQ5LCT7L5QA"]).toBe(250);
    });

    it("returns empty map for empty balances", () => {
      const map = buildBalancesMap([]);
      expect(Object.keys(map)).toHaveLength(0);
    });
  });

  describe("resolveAssetKey", () => {
    it("returns XLM for native asset", () => {
      expect(resolveAssetKey("XLM")).toBe("XLM");
    });

    it("returns code:issuer for non-native asset", () => {
      expect(resolveAssetKey("USDC", "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN")).toBe(
        "USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
      );
    });
  });

  describe("validateBalances", () => {
    beforeEach(() => {
      vi.spyOn(globalThis, "fetch").mockResolvedValue({
        ok: true,
        json: async () => ({ balances: mockBalances }),
      } as Response);
    });

    it("returns sufficient for valid XLM payment", async () => {
      const result = await validateBalances("GABC", [{ asset_code: "XLM", amount: "50" }]);
      expect(result.success).toBe(true);
      expect(result.responseObject?.all_sufficient).toBe(true);
      expect(result.responseObject?.results[0].sufficient).toBe(true);
    });

    it("returns sufficient for valid non-native payment", async () => {
      const result = await validateBalances("GABC", [
        { asset_code: "USDC", asset_issuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN", amount: "200" },
      ]);
      expect(result.success).toBe(true);
      expect(result.responseObject?.all_sufficient).toBe(true);
    });

    it("returns insufficient when amount exceeds balance", async () => {
      const result = await validateBalances("GABC", [
        { asset_code: "USDC", asset_issuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN", amount: "999" },
      ]);
      expect(result.success).toBe(true);
      expect(result.responseObject?.all_sufficient).toBe(false);
      expect(result.responseObject?.results[0].sufficient).toBe(false);
    });

    it("treats missing trustline as zero balance", async () => {
      const result = await validateBalances("GABC", [{ asset_code: "BTC", asset_issuer: "GSOMETHING", amount: "1" }]);
      expect(result.success).toBe(true);
      expect(result.responseObject?.all_sufficient).toBe(false);
      expect(result.responseObject?.results[0].available).toBe(0);
    });

    it("aggregates multiple payments of the same asset", async () => {
      const result = await validateBalances("GABC", [
        { asset_code: "XLM", amount: "60" },
        { asset_code: "XLM", amount: "60" },
      ]);
      expect(result.success).toBe(true);
      expect(result.responseObject?.all_sufficient).toBe(false);
      expect(result.responseObject?.results[0].required).toBe(120);
    });

    it("validates mixed assets independently", async () => {
      const result = await validateBalances("GABC", [
        { asset_code: "XLM", amount: "50" },
        { asset_code: "USDC", asset_issuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN", amount: "999" },
      ]);
      expect(result.success).toBe(true);
      expect(result.responseObject?.all_sufficient).toBe(false);
      const xlmResult = result.responseObject?.results.find((r) => r.asset_key === "XLM");
      const usdcResult = result.responseObject?.results.find((r) => r.asset_key.startsWith("USDC"));
      expect(xlmResult?.sufficient).toBe(true);
      expect(usdcResult?.sufficient).toBe(false);
    });

    it("returns failure when account not found", async () => {
      vi.spyOn(globalThis, "fetch").mockResolvedValue({
        ok: false,
        status: 404,
      } as Response);

      const result = await validateBalances("GNOTFOUND", [{ asset_code: "XLM", amount: "1" }]);
      expect(result.success).toBe(false);
      expect(result.statusCode).toBe(StatusCodes.BAD_GATEWAY);
    });
  });
});
