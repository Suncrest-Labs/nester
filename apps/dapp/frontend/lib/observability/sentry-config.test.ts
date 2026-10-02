import { describe, it, expect, beforeEach, afterEach } from "vitest";

const ENV_KEYS = [
  "NEXT_PUBLIC_SENTRY_DSN",
  "SENTRY_DSN",
  "NEXT_PUBLIC_SENTRY_TRACES_SAMPLE_RATE",
  "NEXT_PUBLIC_SENTRY_ENVIRONMENT",
] as const;

const original: Record<string, string | undefined> = {};

beforeEach(() => {
  for (const key of ENV_KEYS) {
    original[key] = process.env[key];
    delete process.env[key];
  }
});

afterEach(() => {
  for (const key of ENV_KEYS) {
    if (original[key] === undefined) delete process.env[key];
    else process.env[key] = original[key];
  }
});

describe("getClientDsn", () => {
  it("is undefined when NEXT_PUBLIC_SENTRY_DSN is unset", async () => {
    const { getClientDsn } = await import("@/lib/observability/sentry-config");
    expect(getClientDsn()).toBeUndefined();
  });

  it("returns the configured DSN", async () => {
    process.env.NEXT_PUBLIC_SENTRY_DSN = "https://key@o0.ingest.sentry.io/1";
    const { getClientDsn } = await import("@/lib/observability/sentry-config");
    expect(getClientDsn()).toBe("https://key@o0.ingest.sentry.io/1");
  });
});

describe("getServerDsn", () => {
  it("is undefined when neither SENTRY_DSN nor the public DSN is set", async () => {
    const { getServerDsn } = await import("@/lib/observability/sentry-config");
    expect(getServerDsn()).toBeUndefined();
  });

  it("prefers SENTRY_DSN over the public DSN", async () => {
    process.env.SENTRY_DSN = "https://server@o0.ingest.sentry.io/1";
    process.env.NEXT_PUBLIC_SENTRY_DSN = "https://client@o0.ingest.sentry.io/1";
    const { getServerDsn } = await import("@/lib/observability/sentry-config");
    expect(getServerDsn()).toBe("https://server@o0.ingest.sentry.io/1");
  });

  it("falls back to the public DSN when SENTRY_DSN is unset", async () => {
    process.env.NEXT_PUBLIC_SENTRY_DSN = "https://client@o0.ingest.sentry.io/1";
    const { getServerDsn } = await import("@/lib/observability/sentry-config");
    expect(getServerDsn()).toBe("https://client@o0.ingest.sentry.io/1");
  });
});

describe("getTracesSampleRate", () => {
  it("defaults to 0.1 when unset", async () => {
    const { getTracesSampleRate } = await import("@/lib/observability/sentry-config");
    expect(getTracesSampleRate()).toBe(0.1);
  });

  it("uses a configured in-range value", async () => {
    process.env.NEXT_PUBLIC_SENTRY_TRACES_SAMPLE_RATE = "0.5";
    const { getTracesSampleRate } = await import("@/lib/observability/sentry-config");
    expect(getTracesSampleRate()).toBe(0.5);
  });

  it("falls back to the default for a non-numeric value", async () => {
    process.env.NEXT_PUBLIC_SENTRY_TRACES_SAMPLE_RATE = "not-a-number";
    const { getTracesSampleRate } = await import("@/lib/observability/sentry-config");
    expect(getTracesSampleRate()).toBe(0.1);
  });

  it("falls back to the default for an out-of-range value", async () => {
    process.env.NEXT_PUBLIC_SENTRY_TRACES_SAMPLE_RATE = "2";
    const { getTracesSampleRate } = await import("@/lib/observability/sentry-config");
    expect(getTracesSampleRate()).toBe(0.1);

    process.env.NEXT_PUBLIC_SENTRY_TRACES_SAMPLE_RATE = "-1";
    const mod = await import("@/lib/observability/sentry-config");
    expect(mod.getTracesSampleRate()).toBe(0.1);
  });

  it("accepts the boundary values 0 and 1", async () => {
    process.env.NEXT_PUBLIC_SENTRY_TRACES_SAMPLE_RATE = "0";
    const { getTracesSampleRate } = await import("@/lib/observability/sentry-config");
    expect(getTracesSampleRate()).toBe(0);

    process.env.NEXT_PUBLIC_SENTRY_TRACES_SAMPLE_RATE = "1";
    const mod = await import("@/lib/observability/sentry-config");
    expect(mod.getTracesSampleRate()).toBe(1);
  });
});

describe("getEnvironment", () => {
  it("falls back to NODE_ENV when unset", async () => {
    const { getEnvironment } = await import("@/lib/observability/sentry-config");
    expect(getEnvironment()).toBe(process.env.NODE_ENV ?? "development");
  });

  it("uses the configured environment override", async () => {
    process.env.NEXT_PUBLIC_SENTRY_ENVIRONMENT = "staging";
    const { getEnvironment } = await import("@/lib/observability/sentry-config");
    expect(getEnvironment()).toBe("staging");
  });
});
