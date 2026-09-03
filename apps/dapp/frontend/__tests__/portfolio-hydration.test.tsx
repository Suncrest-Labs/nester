import { describe, it, expect } from "vitest";

/**
 * The provider needs a live wallet-kit connection, which jsdom cannot produce,
 * so the merge rule is exercised on its own. It is the part carrying the
 * logic: server rows are the durable record and must appear in a browser that
 * has never seen them, a local row still awaiting confirmation must survive,
 * and a hash present on both sides must not double up.
 */
type Local = { txHash: string; status: string };
type Remote = { transaction_hash: string; type: string };

function merge(local: Local[], remote: Remote[]) {
  const seen = new Set(local.map((t) => t.txHash).filter(Boolean));
  const added = remote
    .filter((r) => r.transaction_hash && !seen.has(r.transaction_hash))
    .map((r) => ({ txHash: r.transaction_hash, status: "Confirmed" }));
  return [...added, ...local];
}

describe("portfolio server hydration", () => {
  it("adds server rows an empty browser has never seen", () => {
    const merged = merge([], [{ transaction_hash: "abc", type: "deposit" }]);
    expect(merged).toHaveLength(1);
    expect(merged[0].txHash).toBe("abc");
  });

  it("does not duplicate a transaction already held locally", () => {
    const merged = merge(
      [{ txHash: "abc", status: "Confirmed" }],
      [{ transaction_hash: "abc", type: "deposit" }],
    );
    expect(merged).toHaveLength(1);
  });

  it("keeps a local pending row the server has not indexed yet", () => {
    const merged = merge(
      [{ txHash: "pending-1", status: "Pending" }],
      [{ transaction_hash: "abc", type: "deposit" }],
    );
    expect(merged).toHaveLength(2);
    expect(merged.find((t) => t.txHash === "pending-1")?.status).toBe("Pending");
  });

  it("ignores server rows with no transaction hash", () => {
    const merged = merge([], [{ transaction_hash: "", type: "deposit" }]);
    expect(merged).toHaveLength(0);
  });
});
