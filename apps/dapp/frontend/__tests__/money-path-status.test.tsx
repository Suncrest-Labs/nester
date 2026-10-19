import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { pauseMessage, resolvePause, type MoneyPathSwitch } from "@/lib/money-path";

const apiRequest = vi.fn();
vi.mock("@/lib/api/client", () => ({ apiRequest: (...args: unknown[]) => apiRequest(...args) }));

import { useMoneyPathStatus } from "@/hooks/useMoneyPathStatus";

const sw = (operation: string, paused: boolean, reason = ""): MoneyPathSwitch => ({
  operation,
  paused,
  reason,
  updated_at: "2026-10-12T00:00:00Z",
});

describe("resolvePause", () => {
  it("reports a paused operation with the operator's reason", () => {
    expect(resolvePause([sw("deposit", true, "maintenance")], "deposit")).toEqual({
      paused: true,
      reason: "maintenance",
    });
  });

  it("pauses deposits and withdrawals independently", () => {
    const list = [sw("deposit", true), sw("withdrawal", false)];
    expect(resolvePause(list, "deposit").paused).toBe(true);
    expect(resolvePause(list, "withdrawal").paused).toBe(false);
  });

  it("treats a missing operation or missing list as not paused", () => {
    expect(resolvePause([], "deposit")).toEqual({ paused: false, reason: "" });
    expect(resolvePause(undefined, "withdrawal")).toEqual({ paused: false, reason: "" });
    expect(resolvePause([sw("something-else", true)], "deposit").paused).toBe(false);
  });
});

describe("pauseMessage", () => {
  it("says honestly what is paused and why", () => {
    expect(pauseMessage("deposit", "")).toBe("Deposits are temporarily paused for maintenance.");
    expect(pauseMessage("withdrawal", "")).toBe("Withdrawals are temporarily paused for maintenance.");
    expect(pauseMessage("deposit", "Investigating an RPC issue")).toBe(
      "Deposits are temporarily paused: Investigating an RPC issue"
    );
  });
});

describe("useMoneyPathStatus", () => {
  beforeEach(() => {
    apiRequest.mockReset();
  });

  it("exposes the pause state from the public status endpoint", async () => {
    apiRequest.mockResolvedValue([sw("deposit", true, "maintenance"), sw("withdrawal", false)]);

    const { result } = renderHook(() => useMoneyPathStatus());

    await waitFor(() => expect(result.current.deposit.paused).toBe(true));
    expect(result.current.deposit.reason).toBe("maintenance");
    expect(result.current.withdrawal.paused).toBe(false);
    expect(apiRequest).toHaveBeenCalledWith("/money-path/status");
  });

  it("does not block users when the status endpoint is unreachable", async () => {
    apiRequest.mockImplementation(async () => {
      throw new Error("network down");
    });

    const { result } = renderHook(() => useMoneyPathStatus());

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20));
    });
    expect(apiRequest.mock.calls).toHaveLength(1);
    expect(result.current.deposit.paused).toBe(false);
    expect(result.current.withdrawal.paused).toBe(false);
  });
});
