import { describe, it, expect } from "vitest";
import {
  scrubSentryBreadcrumb,
  scrubSentryEvent,
} from "@/lib/observability/sentry-scrub";

/**
 * scrubSentryEvent is intentionally generic over `object` (see sentry-scrub.ts
 * for why) rather than constrained to an exported shape, so tests build their
 * fixtures against this local mirror of the fields it actually reads/mutates.
 */
interface TestSentryEvent {
  message?: string;
  transaction?: string;
  exception?: { values?: Array<{ value?: string }> };
  request?: {
    url?: string;
    headers?: Record<string, string>;
    cookies?: unknown;
    data?: unknown;
  };
  breadcrumbs?: Array<{ message?: string; data?: Record<string, unknown> }>;
  extra?: Record<string, unknown>;
  tags?: Record<string, string>;
  user?: Record<string, unknown>;
}

const WALLET = "GABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRSTUVW";
const CONTRACT = "CABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRSTUVW";
const SAMPLE_XDR =
  "AAAAAgAAAABi1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ012345==";
const JWT = [
  Buffer.from(JSON.stringify({ alg: "HS256" })).toString("base64url"),
  Buffer.from(JSON.stringify({ sub: "1234567890" })).toString("base64url"),
  "dBjftJeZ4CVPmB92K27uhbUJU1p1r".concat("_wW1gFWFOEjXk"),
].join(".");

describe("scrubSentryEvent", () => {
  it("scrubs sensitive substrings out of the top-level message", () => {
    const event: TestSentryEvent = { message: `Deposit failed for ${WALLET}` };
    const out = scrubSentryEvent(event);
    expect(out?.message).not.toContain(WALLET);
  });

  it("scrubs Stellar contract addresses (C...), base64 XDR blobs, and amounts", () => {
    const event: TestSentryEvent = {
      message: `Contract ${CONTRACT} simulation failed with XDR ${SAMPLE_XDR} for $1,250.50`,
      exception: {
        values: [
          {
            value: `Envelope ${SAMPLE_XDR} on contract ${CONTRACT} amount 500.2500000`,
          },
        ],
      },
      extra: {
        amount: "500.2500000",
        xdr: SAMPLE_XDR,
        contract: CONTRACT,
      },
      tags: {
        vault: CONTRACT,
      },
    };
    const out = scrubSentryEvent(event);
    expect(out?.message).not.toContain(CONTRACT);
    expect(out?.message).not.toContain(SAMPLE_XDR);
    expect(out?.message).not.toContain("1,250.50");
    expect(out?.exception?.values?.[0].value).not.toContain(SAMPLE_XDR);
    expect(out?.exception?.values?.[0].value).not.toContain(CONTRACT);
    expect(out?.exception?.values?.[0].value).not.toContain("500.2500000");
    expect(out?.extra?.amount).toBe("[redacted]");
    expect(out?.extra?.xdr).toBe("[redacted]");
    expect(out?.extra?.contract).toBe("[redacted]");
    expect(out?.tags?.vault).toBe("[redacted]");
  });

  it("scrubs exception values", () => {
    const event: TestSentryEvent = {
      exception: { values: [{ value: `Withdraw failed, token ${JWT}` }] },
    };
    const out = scrubSentryEvent(event);
    expect(out?.exception?.values?.[0].value).not.toContain(JWT);
  });

  it("sanitizes the transaction (route template) name", () => {
    const event: TestSentryEvent = { transaction: `/savings/${WALLET}` };
    const out = scrubSentryEvent(event);
    expect(out?.transaction).toBe("/savings/[id]");
  });

  it("drops cookies and raw request body wholesale", () => {
    const event: TestSentryEvent = {
      request: {
        url: `/vaults/${WALLET}`,
        cookies: { session: "abc" },
        data: { secret: "leak" },
      },
    };
    const out = scrubSentryEvent(event);
    expect(out?.request?.cookies).toBeUndefined();
    expect(out?.request?.data).toBeUndefined();
    expect(out?.request?.url).toBe("/vaults/[id]");
  });

  it("redacts denylisted request headers regardless of value", () => {
    const event: TestSentryEvent = {
      request: {
        headers: {
          Authorization: "Bearer abc.def.ghi",
          Cookie: "session=abc",
          "X-Custom": "safe-value",
        },
      },
    };
    const out = scrubSentryEvent(event);
    expect(out?.request?.headers?.Authorization).toBe("[redacted]");
    expect(out?.request?.headers?.Cookie).toBe("[redacted]");
    expect(out?.request?.headers?.["X-Custom"]).toBe("safe-value");
  });

  it("scrubs breadcrumb messages and data via scrubSentryBreadcrumb and scrubSentryEvent", () => {
    const crumb = {
      message: `submit XDR ${SAMPLE_XDR} from ${WALLET} to ${CONTRACT}`,
      data: {
        address: WALLET,
        amount: "99.95",
        xdr: SAMPLE_XDR,
        url: `/api/v1/vaults/${CONTRACT}/deposit?token=${JWT}`,
        ok: true,
      },
    };
    const scrubbedCrumb = scrubSentryBreadcrumb(crumb);
    expect(scrubbedCrumb?.message).not.toContain(SAMPLE_XDR);
    expect(scrubbedCrumb?.message).not.toContain(WALLET);
    expect(scrubbedCrumb?.message).not.toContain(CONTRACT);
    expect(scrubbedCrumb?.data?.address).toBe("[redacted]");
    expect(scrubbedCrumb?.data?.amount).toBe("[redacted]");
    expect(scrubbedCrumb?.data?.xdr).toBe("[redacted]");
    expect(scrubbedCrumb?.data?.url).toBe("/api/v1/vaults/[id]/deposit");
    expect(scrubbedCrumb?.data?.ok).toBe(true);
  });

  it("sanitizes the extra context bag", () => {
    const event: TestSentryEvent = {
      extra: { balance: 1200.55, address: WALLET, section: "vaults" },
    };
    const out = scrubSentryEvent(event);
    expect(out?.extra?.balance).toBe("[redacted]");
    expect(out?.extra?.address).toBe("[redacted]");
    expect(out?.extra?.section).toBe("vaults");
  });

  it("passes through an event with none of the optional fields", () => {
    const event: TestSentryEvent = {};
    expect(scrubSentryEvent(event)).toEqual({});
  });

  it("never throws on a malformed event and drops it instead", () => {
    // A getter that throws when the scrub logic accesses it.
    const hostile: TestSentryEvent = {
      get message(): string {
        throw new Error("boom");
      },
    };
    expect(scrubSentryEvent(hostile)).toBeNull();
  });
});

