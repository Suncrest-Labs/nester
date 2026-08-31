import { describe, it, expect } from "vitest";
import { assertApyHistoryResponseShape } from "@/lib/api/contracts";

describe("apyHistoryResponseSchema (Issue #1130)", () => {
  it("accepts a well-shaped response", () => {
    expect(() =>
      assertApyHistoryResponseShape({
        vault_id: "vault-1",
        period: "30d",
        points: [{ timestamp: "2026-01-01T00:00:00Z", apy: 0.0823 }],
      })
    ).not.toThrow();
  });

  it("rejects a response missing a required field", () => {
    expect(() =>
      assertApyHistoryResponseShape({
        period: "30d",
        points: [],
      })
    ).toThrow(/vault_id/);
  });

  it("rejects a response with a wrong-typed field", () => {
    expect(() =>
      assertApyHistoryResponseShape({
        vault_id: "vault-1",
        period: "30d",
        points: [{ timestamp: "2026-01-01T00:00:00Z", apy: "not-a-number" }],
      })
    ).toThrow();
  });

  // Follow-up (not done here): a live-contract-test variant of this file
  // would fetch GET /vaults/:id/apy-history from a real running backend in
  // CI and run the response through assertApyHistoryResponseShape, so a
  // backend shape change fails CI immediately instead of only surfacing
  // when a real user hits it.
});
