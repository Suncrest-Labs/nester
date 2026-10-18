import React from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render } from "@testing-library/react";
import { buildNextRewrites, isAiEnabled } from "@/lib/ai-gate";
import { RebalanceSuggestionCard } from "@/components/dashboard/RebalanceSuggestionCard";
import { vaultsApi } from "@/lib/api/vaults";

describe("isAiEnabled and buildNextRewrites", () => {
  it("excludes /api/intelligence/:path* when NEXT_PUBLIC_ENABLE_AI is unset or false", () => {
    expect(isAiEnabled({})).toBe(false);
    expect(isAiEnabled({ NEXT_PUBLIC_ENABLE_AI: "false" })).toBe(false);

    const rulesUnset = buildNextRewrites({});
    expect(rulesUnset).toEqual([
      {
        source: "/api/v1/:path*",
        destination: "http://localhost:8080/api/v1/:path*",
      },
    ]);

    const rulesFalse = buildNextRewrites({
      NEXT_PUBLIC_ENABLE_AI: "false",
      INTELLIGENCE_SERVICE_URL: "https://ai.nester.fi",
    });
    expect(rulesFalse.some((r) => r.source.includes("/api/intelligence"))).toBe(false);
  });

  it("excludes /api/intelligence/:path* even when NEXT_PUBLIC_ENABLE_AI=true if INTELLIGENCE_SERVICE_URL is unset", () => {
    const rules = buildNextRewrites({
      NEXT_PUBLIC_ENABLE_AI: "true",
    });
    expect(rules.some((r) => r.source.includes("/api/intelligence"))).toBe(false);
  });

  it("includes /api/intelligence/:path* when NEXT_PUBLIC_ENABLE_AI=true and INTELLIGENCE_SERVICE_URL is set", () => {
    const rules = buildNextRewrites({
      NEXT_PUBLIC_ENABLE_AI: "true",
      INTELLIGENCE_SERVICE_URL: "https://ai.nester.fi/",
    });
    expect(rules).toContainEqual({
      source: "/api/intelligence/:path*",
      destination: "https://ai.nester.fi/:path*",
    });
  });
});

describe("AI UI gating", () => {
  const origEnv = process.env.NEXT_PUBLIC_ENABLE_AI;

  beforeEach(() => {
    delete process.env.NEXT_PUBLIC_ENABLE_AI;
  });

  afterEach(() => {
    if (origEnv === undefined) {
      delete process.env.NEXT_PUBLIC_ENABLE_AI;
    } else {
      process.env.NEXT_PUBLIC_ENABLE_AI = origEnv;
    }
    vi.restoreAllMocks();
  });

  it("does not render AI suggestion UI or make network calls when NEXT_PUBLIC_ENABLE_AI is unset", () => {
    const spy = vi.spyOn(vaultsApi, "getRebalanceSuggestion");
    const { container } = render(
      <RebalanceSuggestionCard vaultId="vault-1" vaultName="USDC Vault" />,
    );
    expect(container.firstChild).toBeNull();
    expect(spy).not.toHaveBeenCalled();
  });
});
