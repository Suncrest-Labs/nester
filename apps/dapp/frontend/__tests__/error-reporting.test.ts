import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import {
  buildErrorEvent,
  categorizeError,
  reportError,
  resetErrorReportDedupe,
} from "@/lib/observability/report-error";
import {
  sanitizeText,
  sanitizeRoute,
  sanitizeContext,
  REDACTED,
} from "@/lib/observability/sanitize";

const WALLET = "GABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRSTUVW";
const CONTRACT = "CABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRSTUVW";
const SECRET = "SABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRSTUVW";
const TX_HASH = "a".repeat(64);
/**
 * Assembled at runtime rather than written as a literal: a JWT-shaped string in
 * source trips secret scanners even when, as here, it is synthetic test data.
 */
const JWT = [
  Buffer.from(JSON.stringify({ alg: "HS256" })).toString("base64url"),
  Buffer.from(JSON.stringify({ sub: "1234567890" })).toString("base64url"),
  "dBjftJeZ4CVPmB92K27uhbUJU1p1r".concat("_wW1gFWFOEjXk"),
].join(".");

describe("sanitizeText", () => {
  it("redacts Stellar public keys", () => {
    const out = sanitizeText(`Deposit failed for ${WALLET}`);
    expect(out).not.toContain(WALLET);
    expect(out).toContain(REDACTED);
  });

  it("redacts contract ids, secrets and muxed accounts", () => {
    expect(sanitizeText(CONTRACT)).not.toContain(CONTRACT);
    expect(sanitizeText(SECRET)).not.toContain(SECRET);
    expect(sanitizeText(`M${"A".repeat(68)}`)).toContain(REDACTED);
  });

  it("redacts transaction hashes", () => {
    expect(sanitizeText(`tx ${TX_HASH} reverted`)).not.toContain(TX_HASH);
  });

  it("redacts bearer tokens and bare JWTs", () => {
    expect(sanitizeText(`Bearer ${JWT}`)).not.toContain(JWT);
    expect(sanitizeText(`token=${JWT}`)).not.toContain(JWT);
  });

  it("redacts currency amounts and balances", () => {
    expect(sanitizeText("Balance $12,450.33 is too low")).not.toContain("12,450.33");
    expect(sanitizeText("only 1043.50 XLM available")).not.toContain("1043.50");
  });

  it("leaves ordinary diagnostic text intact", () => {
    expect(sanitizeText("Failed to fetch vault list")).toBe(
      "Failed to fetch vault list"
    );
  });
});

describe("sanitizeRoute", () => {
  it("strips query strings", () => {
    expect(sanitizeRoute("/portfolio?tab=activity")).toBe("/portfolio");
  });

  it("replaces wallet addresses in the path with a placeholder", () => {
    expect(sanitizeRoute(`/savings/${WALLET}`)).toBe("/savings/[id]");
  });

  it("replaces numeric and uuid-ish ids", () => {
    expect(sanitizeRoute("/vaults/12345")).toBe("/vaults/[id]");
    expect(sanitizeRoute("/vaults/9f8b7c6d5e4f3a2b")).toBe("/vaults/[id]");
  });

  it("keeps static segments", () => {
    expect(sanitizeRoute("/dashboard/analytics")).toBe("/dashboard/analytics");
  });
});

describe("sanitizeContext", () => {
  it("drops sensitive keys wholesale", () => {
    const out = sanitizeContext({
      address: WALLET,
      balance: 1200.55,
      totalValueUsd: 90210,
      authorization: `Bearer ${JWT}`,
      section: "vaults",
    });
    expect(out.address).toBe(REDACTED);
    expect(out.balance).toBe(REDACTED);
    expect(out.authorization).toBe(REDACTED);
    expect(out.section).toBe("vaults");
  });

  it("scrubs sensitive values hiding in allowed keys", () => {
    const out = sanitizeContext({ detail: `failed for ${WALLET}` });
    expect(out.detail).not.toContain(WALLET);
  });

  it("stops recursing past a bounded depth", () => {
    const deep = { a: { b: { c: { d: { e: "leak" } } } } };
    expect(JSON.stringify(sanitizeContext(deep))).not.toContain("leak");
  });
});

describe("categorizeError", () => {
  it("classifies network failures", () => {
    expect(categorizeError(new Error("Failed to fetch"))).toBe("network");
  });

  it("classifies auth failures", () => {
    expect(categorizeError(new Error("Request failed with 401"))).toBe("auth");
  });

  it("classifies server failures", () => {
    expect(categorizeError(new Error("API error 500"))).toBe("server");
  });

  it("falls back to render for plain exceptions", () => {
    expect(categorizeError(new TypeError("x is not a function"))).toBe("render");
  });
});

describe("buildErrorEvent", () => {
  it("includes route and boundary context", () => {
    const event = buildErrorEvent({
      error: new Error("boom"),
      route: "/vaults/12345",
      boundary: "vault-detail",
    });
    expect(event.route).toBe("/vaults/[id]");
    expect(event.boundary).toBe("vault-detail");
    expect(event.category).toBe("render");
    expect(event.timestamp).toMatch(/^\d{4}-\d{2}-\d{2}T/);
  });

  it("never carries a stack trace", () => {
    const err = new Error("boom");
    const event = buildErrorEvent({ error: err, route: "/portfolio" });
    expect(JSON.stringify(event)).not.toContain("at ");
    expect(event).not.toHaveProperty("stack");
  });

  it("scrubs wallet addresses, balances and tokens out of the payload", () => {
    const event = buildErrorEvent({
      error: new Error(
        `Withdraw of $4,200.00 from ${WALLET} failed (Bearer ${JWT})`
      ),
      route: `/portfolio/${WALLET}`,
      boundary: "portfolio",
      context: { balance: 4200, address: WALLET, section: "positions" },
    });

    const serialized = JSON.stringify(event);
    expect(serialized).not.toContain(WALLET);
    expect(serialized).not.toContain(JWT);
    expect(serialized).not.toContain("4,200.00");
    expect(serialized).not.toContain("4200");
    expect(event.context.section).toBe("positions");
  });
});

describe("reportError", () => {
  beforeEach(() => {
    resetErrorReportDedupe();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("writes one structured entry to the logging sink", () => {
    reportError({
      error: new Error("vault list unavailable"),
      route: "/vaults",
      boundary: "vaults",
    });

    expect(console.error).toHaveBeenCalledTimes(1);
    const [tag, payload] = (console.error as unknown as ReturnType<typeof vi.fn>)
      .mock.calls[0];
    expect(tag).toBe("[nester:client-error]");
    const parsed = JSON.parse(payload as string);
    expect(parsed.route).toBe("/vaults");
    expect(parsed.boundary).toBe("vaults");
    expect(parsed.category).toBe("render");
  });

  it("does not log the same failure twice from a re-rendering boundary", () => {
    const error = new Error("repeat failure");
    reportError({ error, route: "/vaults", boundary: "vaults" });
    reportError({ error, route: "/vaults", boundary: "vaults" });
    expect(console.error).toHaveBeenCalledTimes(1);
  });

  it("emits no sensitive field even when the error message is hostile", () => {
    reportError({
      error: new Error(`balance 9,900.12 for ${WALLET} token ${JWT}`),
      route: `/savings/${WALLET}`,
      boundary: "savings",
      context: { walletAddress: WALLET, portfolioValue: 9900.12 },
    });

    const payload = (console.error as unknown as ReturnType<typeof vi.fn>).mock
      .calls[0][1] as string;
    expect(payload).not.toContain(WALLET);
    expect(payload).not.toContain(JWT);
    expect(payload).not.toContain("9,900.12");
    expect(payload).not.toContain("9900.12");
  });
});
