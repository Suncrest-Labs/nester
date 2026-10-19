import { describe, expect, it } from "vitest";

import { describeDepositRejection } from "@/lib/stellar/contract-errors";

/**
 * A rejected simulation carries the raw host error, e.g.
 * "HostError: Error(Contract, #10)" followed by an event log. Users should see
 * why a deposit was refused, not that string. Codes are ContractError in
 * packages/contracts/libs/common/src/errors.rs.
 */

const raw = (code: number) =>
  `HostError: Error(Contract, #${code})\n\nEvent log (newest first):\n   0: [Diagnostic Event] contract:CAAA, topics:[error, Error(Contract, #${code})]`;

describe("describeDepositRejection", () => {
  it("explains a deposit that hits the vault limit", () => {
    expect(describeDepositRejection(raw(10))).toMatch(/deposit limit/i);
  });

  it("explains an account that is not on the launch allowlist", () => {
    expect(describeDepositRejection(raw(3))).toMatch(/approved|early.access/i);
  });

  it("explains the circuit breaker", () => {
    expect(describeDepositRejection(raw(11))).toMatch(/paused|circuit breaker/i);
  });

  it("explains a below-minimum deposit", () => {
    expect(describeDepositRejection(raw(21))).toMatch(/minimum/i);
  });

  it("returns null for codes it has no better text for", () => {
    expect(describeDepositRejection(raw(99))).toBeNull();
  });

  it("returns null for messages that are not contract errors", () => {
    expect(describeDepositRejection("network request failed")).toBeNull();
    expect(describeDepositRejection("")).toBeNull();
  });

  it("does not mistake a non-contract error code for a contract one", () => {
    expect(describeDepositRejection("HostError: Error(WasmVm, #10)")).toBeNull();
  });
});
